package main

import (
	"io"
	"net/http"
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

		// 2. Interpolate variables into URL, body, and header values
		allLocalVars := req.Vars
		url := InterpolateVars(rawURL, allLocalVars, groupVars, globalVars)
		body := InterpolateVars(req.Body, allLocalVars, groupVars, globalVars)

		// 3. Build the HTTP request
		var bodyReader io.Reader
		if body != "" {
			bodyReader = strings.NewReader(body)
		}

		httpReq, err := http.NewRequest(req.Method, url, bodyReader)
		if err != nil {
			return httpErrMsg{
				err:       err,
				method:    req.Method,
				url:       url,
				latencyMs: time.Since(start).Milliseconds(),
			}
		}

		// 4. Set headers — only enabled ones
		hasContentType := false
		for _, h := range req.Headers {
			if !h.Enabled {
				continue
			}
			val := InterpolateVars(h.Value, allLocalVars, groupVars, globalVars)
			httpReq.Header.Set(h.Key, val)
			if strings.EqualFold(h.Key, "content-type") {
				hasContentType = true
			}
		}
		// Auto-set Content-Type if we have a body and user didn't set it
		if body != "" && !hasContentType {
			httpReq.Header.Set("Content-Type", "application/json")
		}

		// 5. Execute the request
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			return httpErrMsg{
				err:       err,
				method:    req.Method,
				url:       url,
				latencyMs: time.Since(start).Milliseconds(),
			}
		}
		defer resp.Body.Close()

		latency := time.Since(start).Milliseconds()

		rawBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return httpErrMsg{
				err:       err,
				method:    req.Method,
				url:       url,
				latencyMs: latency,
			}
		}

		// 6. Run tests against the response
		tests := RunTests(req.Tests, resp.StatusCode, resp.Header, string(rawBody))

		// Convert header map to a simpler map[string][]string
		headers := make(map[string][]string)
		for k, v := range resp.Header {
			headers[k] = v
		}

		return httpResultMsg{
			status:    resp.StatusCode,
			headers:   headers,
			body:      string(rawBody),
			latencyMs: latency,
			tests:     tests,
			method:    req.Method,
			url:       url,
			body_:     body,
		}
	}
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
