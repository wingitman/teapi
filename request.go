package main

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ── HTTP execution ────────────────────────────────────────────────────────────
//
// doRequest is a tea.Cmd factory. It returns a function that runs an HTTP
// request in a goroutine and returns the result as a message to Update().
//
// Before sending, variables are interpolated into the URL, headers, and body.

// doRequest builds and executes an HTTP request.
// It returns a tea.Cmd (a function) that bubbletea will run in a goroutine.
func doRequest(req Request, groupVars, globalVars []Variable, data AppData) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()

		// 1. Resolve the base URL (prepend group BaseURL if request URL is relative)
		rawURL := resolveBaseURL(data, req)

		// 2. Interpolate variables into URL, body, and header values.
		allLocalVars := req.Vars
		finalURL := InterpolateVars(rawURL, allLocalVars, groupVars, globalVars)
		body := InterpolateVars(req.Body, allLocalVars, groupVars, globalVars)

		// 3. Execute using the shared helper.
		result := execHTTP(req.Method, finalURL, body, req.Headers, allLocalVars, groupVars, globalVars, start)
		switch r := result.(type) {
		case httpErrMsg:
			return r
		case httpResultMsg:
			// Run tests and add request metadata before returning.
			r.tests = RunTests(req.Tests, r.status, headerMapToHTTPHeader(r.headers), r.body)
			r.method = req.Method
			r.url = finalURL
			r.body_ = body
			return r
		}
		return result
	}
}

// execHTTP performs a single HTTP call and returns httpResultMsg or httpErrMsg.
// Shared between doRequest and executeStep to avoid duplication.
func execHTTP(method, rawURL, body string, headers []Header, localVars, groupVars, globalVars []Variable, start time.Time) tea.Msg {
	// Build the URL, preserving the raw path so interpolated variables
	// (e.g. /users/{userId} after resolution → /users/42) are never
	// re-encoded by url.Parse. Without this, path segments get percent-encoded.
	parsedURL, err := buildRawURL(rawURL)
	if err != nil {
		return httpErrMsg{err: err, method: method, url: rawURL, latencyMs: time.Since(start).Milliseconds()}
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	httpReq := &http.Request{
		Method: method,
		URL:    parsedURL,
		Header: make(http.Header),
	}
	if bodyReader != nil {
		rc, ok := bodyReader.(io.ReadCloser)
		if !ok {
			rc = io.NopCloser(bodyReader)
		}
		httpReq.Body = rc
	}

	// Set headers
	hasContentType := false
	for _, h := range headers {
		if !h.Enabled {
			continue
		}
		val := InterpolateVars(h.Value, localVars, groupVars, globalVars)
		httpReq.Header.Set(h.Key, val)
		if strings.EqualFold(h.Key, "content-type") {
			hasContentType = true
		}
	}
	if body != "" && !hasContentType {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return httpErrMsg{err: err, method: method, url: rawURL, latencyMs: time.Since(start).Milliseconds()}
	}
	defer resp.Body.Close()

	latency := time.Since(start).Milliseconds()
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpErrMsg{err: err, method: method, url: rawURL, latencyMs: latency}
	}

	respHeaders := make(map[string][]string)
	for k, v := range resp.Header {
		respHeaders[k] = v
	}

	return httpResultMsg{
		status:    resp.StatusCode,
		headers:   respHeaders,
		body:      string(rawBody),
		latencyMs: latency,
	}
}

// buildRawURL parses a URL string into a *url.URL while preserving the
// original path exactly as typed (no percent-encoding of braces or other
// chars that url.Parse would normally encode in path segments).
func buildRawURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	// url.Parse encodes special chars in the path. Restore the raw path by
	// re-parsing just the path component without encoding.
	if parsed.RawPath == "" {
		// No encoding happened — path is already clean.
		return parsed, nil
	}
	// RawPath is set when Parse had to encode something. Restore the original.
	parsed.RawPath = parsed.Path
	return parsed, nil
}

// headerMapToHTTPHeader converts map[string][]string to http.Header.
func headerMapToHTTPHeader(m map[string][]string) http.Header {
	h := make(http.Header)
	for k, v := range m {
		h[k] = v
	}
	return h
}

// ── Open in editor ────────────────────────────────────────────────────────────
//
// openInEditor writes content to a temp file, suspends the TUI, opens $EDITOR,
// then resumes. If editable is true, the updated content is returned.
//
// This uses tea.ExecProcess which handles suspending/resuming the TUI properly.

// openEditorCmd opens a string in $EDITOR and returns a tea.Cmd.
// editable: if true, reads the file back and returns editorClosedMsg with new content.
// If false, the file is read-only (we still open it but ignore changes).
func openEditorCmd(content string, editable bool, suffix string) tea.Cmd {
	// Write content to a temp file
	tmpFile, err := os.CreateTemp("", "teapi-*"+suffix)
	if err != nil {
		return nil
	}
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return nil
	}
	tmpFile.Close()
	tmpPath := tmpFile.Name()

	// tea.ExecProcess suspends the TUI, runs the process, then resumes.
	c := exec.Command(resolveEditor(), tmpPath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return editorClosedMsg{content: content} // return original on error
		}
		if !editable {
			os.Remove(tmpPath)
			return editorClosedMsg{content: content} // read-only: ignore changes
		}
		newContent, readErr := os.ReadFile(tmpPath)
		os.Remove(tmpPath)
		if readErr != nil {
			return editorClosedMsg{content: content}
		}
		return editorClosedMsg{content: string(newContent)}
	})
}
