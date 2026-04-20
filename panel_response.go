package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// ── Response panel ────────────────────────────────────────────────────────────
//
// The response panel shows:
//   - Status code (coloured by range)
//   - Latency
//   - Test results summary
//   - Scrollable response body (via viewport)
//   - Ctrl+R hint to open in $EDITOR

type ResponsePanel struct {
	Status    int
	LatencyMs int64
	Body      string
	Headers   map[string][]string
	Tests     []TestResult
	ErrMsg    string
	Loading   bool

	viewport viewport.Model
	width    int
	height   int
}

// NewResponsePanel creates a new response panel.
func NewResponsePanel(width, height int) ResponsePanel {
	vp := viewport.New(viewport.WithWidth(width-4), viewport.WithHeight(height-6))
	return ResponsePanel{
		viewport: vp,
		width:    width,
		height:   height,
	}
}

// SetResult populates the response panel with a completed HTTP result.
func (rp *ResponsePanel) SetResult(status int, latency int64, body string, headers map[string][]string, tests []TestResult) {
	rp.Status = status
	rp.LatencyMs = latency
	rp.Body = body
	rp.Headers = headers
	rp.Tests = tests
	rp.ErrMsg = ""
	rp.Loading = false
	rp.viewport.SetContent(body)
	rp.viewport.GotoTop()
}

// SetError shows an error message in the response panel.
func (rp *ResponsePanel) SetError(msg string, latency int64) {
	rp.ErrMsg = msg
	rp.LatencyMs = latency
	rp.Status = 0
	rp.Loading = false
	rp.viewport.SetContent("Error:\n" + msg)
	rp.viewport.GotoTop()
}

// SetSize resizes the panel and its viewport.
func (rp *ResponsePanel) SetSize(width, height int) {
	rp.width = width
	rp.height = height
	rp.viewport.SetWidth(width - 4)
	// Reserve space for: status line (1), blank (1), content-type (1), border (2)
	vpHeight := height - 6
	if vpHeight < 3 {
		vpHeight = 3
	}
	rp.viewport.SetHeight(vpHeight)
}

// ── Update ────────────────────────────────────────────────────────────────────

func (rp ResponsePanel) Update(msg tea.Msg, keys KeyMap) (ResponsePanel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key_matches(msg, keys.OpenResponse):
			// Open response body in $EDITOR (read-only)
			if rp.Body != "" {
				cmds = append(cmds, openEditorCmd(rp.Body, false, ".json"))
			}
		}
	}

	// Delegate scrolling to viewport
	var cmd tea.Cmd
	rp.viewport, cmd = rp.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return rp, tea.Batch(cmds...)
}

// ── View ──────────────────────────────────────────────────────────────────────

// View renders the response panel.
func (rp ResponsePanel) View(focused bool) string {
	var sb strings.Builder

	if rp.Loading {
		sb.WriteString(loadingStyle.Render("⟳ Sending request..."))
	} else if rp.Status == 0 && rp.ErrMsg == "" {
		sb.WriteString(dimStyle.Render("No response yet. Press s to send a request."))
	} else {
		// Status + latency + test summary
		statusStr := statusStyle(rp.Status).Render(statusText(rp.Status))
		latencyStr := latencyStyle.Render(fmt.Sprintf("  %dms", rp.LatencyMs))
		testStr := ""
		if len(rp.Tests) > 0 {
			testStr = "  " + testSummary(rp.Tests)
		}
		sb.WriteString(statusStr + latencyStr + testStr)
		sb.WriteString("\n")

		if ct := firstHeader(rp.Headers, "Content-Type"); ct != "" {
			sb.WriteString(dimStyle.Render(ct))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")

		sb.WriteString(rp.viewport.View())

		for _, tr := range rp.Tests {
			if !tr.Passed {
				icon := testFailStyle.Render("✗")
				detail := fmt.Sprintf("  expected %q got %q", tr.Case.Expected, tr.Actual)
				sb.WriteString(fmt.Sprintf("\n%s %-25s %s", icon, tr.Case.Name, dimStyle.Render(detail)))
			}
		}
	}

	border := panelBlurredStyle
	if focused {
		border = panelFocusedStyle
	}
	return border.Width(rp.width).Height(rp.height).Render(sb.String())
}

// firstHeader returns the first value of a header, case-insensitive.
func firstHeader(headers map[string][]string, key string) string {
	for k, vals := range headers {
		if strings.EqualFold(k, key) && len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

// GlobalVarsScreen was removed — global vars are now shown in the Variables
// sub-tab of the builder panel, merged with local variables.
