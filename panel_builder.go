package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── Builder panel ─────────────────────────────────────────────────────────────
//
// The builder panel is the main request editor. It has 4 sub-tabs:
//   1. Request  — method picker + URL + body
//   2. Headers  — list of headers with enable/disable/add/delete
//   3. Variables — local variables for this request
//   4. Tests    — assertions to run after the request
//
// Within the Request tab, Tab moves focus between URL input and body textarea.
// Esc returns focus to URL from the body textarea.

type BuilderFocus int

const (
	BuilderFocusURL BuilderFocus = iota
	BuilderFocusBody
)

// BuilderPanel holds all state for the request builder.
type BuilderPanel struct {
	// The request being edited
	Request Request

	// Sub-tab
	activeTab BuilderTab

	// Request tab
	methodIdx  int
	urlInput   textinput.Model
	bodyInput  textarea.Model
	innerFocus BuilderFocus

	// Headers tab
	headers      []Header
	headerCursor int

	// Variables tab — local (per-request) + global (app-wide)
	variables    []Variable // local vars for this request
	globalVars   []Variable // global vars (from AppData.GlobalVars)
	varCursor    int
	varInGlobal  bool // true = cursor is in the global section

	// Tests tab
	tests      []TestCase
	testCursor int

	// Workflows tab (embedded WorkflowScreen)
	workflowScreen WorkflowScreen

	// Batch tab (embedded BatchScreen)
	batchScreen BatchScreen

	width  int
	height int
}

// NewBuilderPanel creates a new builder panel with default empty state.
func NewBuilderPanel(width, height int, data AppData) BuilderPanel {
	u := textinput.New()
	u.Placeholder = "https://api.example.com/endpoint"
	u.CharLimit = 2048
	u.Focus()

	b := textarea.New()
	b.Placeholder = `{"key": "value"}`
	b.ShowLineNumbers = false

	return BuilderPanel{
		Request:        Request{Method: "GET"},
		urlInput:       u,
		bodyInput:      b,
		innerFocus:     BuilderFocusURL,
		globalVars:     data.GlobalVars,
		workflowScreen: NewWorkflowScreen(data.Workflows, width, height-4),
		batchScreen:    NewBatchScreen(nil, width, height-4),
		width:          width,
		height:         height,
	}
}

// LoadRequest loads a saved request into the builder.
func (bp *BuilderPanel) LoadRequest(req Request) {
	bp.Request = req
	bp.urlInput.SetValue(req.URL)
	bp.bodyInput.SetValue(req.Body)
	bp.headers = req.Headers
	bp.variables = req.Vars
	bp.tests = req.Tests
	bp.innerFocus = BuilderFocusURL
	bp.bodyInput.Blur()
	bp.urlInput.Focus()

	// Set method index
	bp.methodIdx = 0
	for i, m := range httpMethods {
		if m == req.Method {
			bp.methodIdx = i
			break
		}
	}
}

// CurrentRequest builds a Request from the current builder state.
func (bp *BuilderPanel) CurrentRequest() Request {
	req := bp.Request
	req.Method = httpMethods[bp.methodIdx]
	req.URL = bp.urlInput.Value()
	req.Body = bp.bodyInput.Value()
	req.Headers = bp.headers
	req.Vars = bp.variables
	req.Tests = bp.tests
	return req
}

// SetSize updates the panel dimensions and resizes child components.
func (bp *BuilderPanel) SetSize(width, height int) {
	bp.width = width
	bp.height = height
	bp.urlInput.SetWidth(width - 12)
	bp.bodyInput.SetWidth(width - 4)
	bodyHeight := height - 10
	if bodyHeight < 3 {
		bodyHeight = 3
	}
	bp.bodyInput.SetHeight(bodyHeight)
}

// ── Update ────────────────────────────────────────────────────────────────────

// Update handles messages for the builder panel.
//
// Navigation (tab switching, panel switching) is handled entirely by the root
// model. This function only deals with:
//   - Text field input (URL/body) when editMode is active
//   - List navigation (↑/↓) in Headers/Variables/Tests tabs
//   - Space to toggle headers
//   - Delete to remove list rows
func (bp BuilderPanel) Update(msg tea.Msg, keys KeyMap) (BuilderPanel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		// ── List navigation — safe in any mode ───────────────────────────
		case key_matches(msg, keys.Up):
			switch bp.activeTab {
			case BuilderTabHeaders:
				if bp.headerCursor > 0 {
					bp.headerCursor--
				}
			case BuilderTabVariables:
				if bp.varInGlobal {
					if bp.varCursor > 0 {
						bp.varCursor--
					} else {
						// Cross boundary into local section
						bp.varInGlobal = false
						bp.varCursor = max(0, len(bp.variables)-1)
					}
				} else {
					if bp.varCursor > 0 {
						bp.varCursor--
					}
				}
			case BuilderTabTests:
				if bp.testCursor > 0 {
					bp.testCursor--
				}
			case BuilderTabWorkflows:
				var cmd tea.Cmd
				bp.workflowScreen, cmd = bp.workflowScreen.Update(msg, keys, AppData{Workflows: bp.workflowScreen.workflows})
				cmds = append(cmds, cmd)
			case BuilderTabBatch:
				var cmd tea.Cmd
				bp.batchScreen, cmd = bp.batchScreen.Update(msg, keys, nil)
				cmds = append(cmds, cmd)
			}

		case key_matches(msg, keys.Down):
			switch bp.activeTab {
			case BuilderTabHeaders:
				if bp.headerCursor < len(bp.headers)-1 {
					bp.headerCursor++
				}
			case BuilderTabVariables:
				if !bp.varInGlobal {
					if bp.varCursor < len(bp.variables)-1 {
						bp.varCursor++
					} else {
						// Cross boundary into global section
						bp.varInGlobal = true
						bp.varCursor = 0
					}
				} else {
					if bp.varCursor < len(bp.globalVars)-1 {
						bp.varCursor++
					}
				}
			case BuilderTabTests:
				if bp.testCursor < len(bp.tests)-1 {
					bp.testCursor++
				}
			case BuilderTabWorkflows:
				var cmd tea.Cmd
				bp.workflowScreen, cmd = bp.workflowScreen.Update(msg, keys, AppData{Workflows: bp.workflowScreen.workflows})
				cmds = append(cmds, cmd)
			case BuilderTabBatch:
				var cmd tea.Cmd
				bp.batchScreen, cmd = bp.batchScreen.Update(msg, keys, nil)
				cmds = append(cmds, cmd)
			}

		// s — run workflow or batch (handled here when routed from root model)
		case msg.String() == "s":
			switch bp.activeTab {
			case BuilderTabWorkflows:
				var cmd tea.Cmd
				bp.workflowScreen, cmd = bp.workflowScreen.Update(msg, keys, AppData{Workflows: bp.workflowScreen.workflows})
				cmds = append(cmds, cmd)
			case BuilderTabBatch:
				var cmd tea.Cmd
				bp.batchScreen, cmd = bp.batchScreen.Update(msg, keys, nil)
				cmds = append(cmds, cmd)
			}

		// Toggle header enabled/disabled
		case key_matches(msg, keys.Space):
			if bp.activeTab == BuilderTabHeaders && bp.headerCursor < len(bp.headers) {
				bp.headers[bp.headerCursor].Enabled = !bp.headers[bp.headerCursor].Enabled
			}

		// Delete selected row
		case key_matches(msg, keys.DeleteItem):
			switch bp.activeTab {
			case BuilderTabHeaders:
				if bp.headerCursor < len(bp.headers) {
					bp.headers = append(bp.headers[:bp.headerCursor], bp.headers[bp.headerCursor+1:]...)
					if bp.headerCursor > 0 {
						bp.headerCursor--
					}
				}
			case BuilderTabVariables:
				if bp.varInGlobal {
					if bp.varCursor < len(bp.globalVars) {
						bp.globalVars = append(bp.globalVars[:bp.varCursor], bp.globalVars[bp.varCursor+1:]...)
						if bp.varCursor > 0 {
							bp.varCursor--
						}
					}
				} else {
					if bp.varCursor < len(bp.variables) {
						bp.variables = append(bp.variables[:bp.varCursor], bp.variables[bp.varCursor+1:]...)
						if bp.varCursor > 0 {
							bp.varCursor--
						}
					}
				}
			case BuilderTabTests:
				if bp.testCursor < len(bp.tests) {
					bp.tests = append(bp.tests[:bp.testCursor], bp.tests[bp.testCursor+1:]...)
					if bp.testCursor > 0 {
						bp.testCursor--
					}
				}
			}
		}
	}

	// ── Text field delegation ─────────────────────────────────────────────
	// Only reached when root model is in editMode (URL or body active).
	// Root model calls builder.Update only when it has routed a key here.
	if bp.activeTab == BuilderTabRequest {
		if bp.innerFocus == BuilderFocusURL {
			var cmd tea.Cmd
			bp.urlInput, cmd = bp.urlInput.Update(msg)
			cmds = append(cmds, cmd)
		} else if bp.innerFocus == BuilderFocusBody {
			var cmd tea.Cmd
			bp.bodyInput, cmd = bp.bodyInput.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return bp, tea.Batch(cmds...)
}

// ── incoming messages from modals ─────────────────────────────────────────────

// ApplyAddHeader adds a new header from a modal confirm.
func (bp *BuilderPanel) ApplyAddHeader(key, value string) {
	bp.headers = append(bp.headers, Header{Key: key, Value: value, Enabled: true})
}

// ApplyEditHeader updates an existing header.
func (bp *BuilderPanel) ApplyEditHeader(idx int, key, value string) {
	if idx < len(bp.headers) {
		bp.headers[idx].Key = key
		bp.headers[idx].Value = value
	}
}

// ApplyAddVariable adds a new local variable.
func (bp *BuilderPanel) ApplyAddVariable(key, value, varType string) {
	bp.variables = append(bp.variables, Variable{Key: key, Value: value, Type: varType})
}

// ApplyAddTest adds a new test case.
func (bp *BuilderPanel) ApplyAddTest(tc TestCase) {
	bp.tests = append(bp.tests, tc)
}

// ── View ──────────────────────────────────────────────────────────────────────

func (bp BuilderPanel) View(focused bool) string {
	var sb strings.Builder

	// Sub-tab bar
	sb.WriteString(bp.renderTabBar())
	sb.WriteString("\n\n")

	// Content of the active sub-tab
	switch bp.activeTab {
	case BuilderTabRequest:
		sb.WriteString(bp.renderRequestTab())
	case BuilderTabHeaders:
		sb.WriteString(bp.renderHeadersTab())
	case BuilderTabVariables:
		sb.WriteString(bp.renderVariablesTab())
	case BuilderTabTests:
		sb.WriteString(bp.renderTestsTab())
	case BuilderTabWorkflows:
		sb.WriteString(bp.workflowScreen.View())
	case BuilderTabBatch:
		sb.WriteString(bp.batchScreen.View())
	}

	content := sb.String()

	border := panelBlurredStyle
	if focused {
		border = panelFocusedStyle
	}
	return border.Width(bp.width).Height(bp.height).Render(content)
}

func (bp BuilderPanel) renderTabBar() string {
	names := []string{"Request", "Headers", "Variables", "Tests", "Workflows", "Batch"}
	var parts []string
	for i, name := range names {
		if BuilderTab(i) == bp.activeTab {
			parts = append(parts, builderTabActiveStyle.Render(name))
		} else {
			parts = append(parts, builderTabInactiveStyle.Render(name))
		}
	}
	return strings.Join(parts, "  ")
}

func (bp BuilderPanel) renderRequestTab() string {
	var sb strings.Builder

	// Method selector
	sb.WriteString(labelStyle.Render("Method: "))
	for i, m := range httpMethods {
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Padding(0, 1)
		if i == bp.methodIdx {
			style = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#000000")).
				Background(methodColor(m)).
				Padding(0, 1)
		}
		sb.WriteString(style.Render(m) + " ")
	}
	sb.WriteString("\n\n")

	// URL input
	urlLabel := labelStyle.Render("URL:  ")
	if bp.innerFocus == BuilderFocusURL {
		urlLabel = labelFocusedStyle.Render("URL:  ")
	}
	sb.WriteString(urlLabel + bp.urlInput.View())
	sb.WriteString("\n\n")

	// Body textarea
	bodyLabel := labelStyle.Render("Body:")
	if bp.innerFocus == BuilderFocusBody {
		bodyLabel = labelFocusedStyle.Render("Body:")
	}
	sb.WriteString(bodyLabel + "\n")
	sb.WriteString(bp.bodyInput.View())
	sb.WriteString("\n")

	return sb.String()
}

func (bp BuilderPanel) renderHeadersTab() string {
	var sb strings.Builder

	sb.WriteString(tableHeaderStyle.Render(fmt.Sprintf("%-3s  %-25s  %-30s", "On?", "Key", "Value")))
	sb.WriteString("\n")

	if len(bp.headers) == 0 {
		sb.WriteString(dimStyle.Render("  No headers yet."))
	}

	for i, h := range bp.headers {
		enabled := "✓ "
		if !h.Enabled {
			enabled = "✗ "
		}
		row := fmt.Sprintf("%-3s  %-25s  %-30s", enabled, truncate(h.Key, 24), truncate(h.Value, 29))
		if i == bp.headerCursor {
			sb.WriteString(tableSelectedStyle.Render(row))
		} else if !h.Enabled {
			sb.WriteString(tableDisabledStyle.Render(row))
		} else {
			sb.WriteString(tableRowStyle.Render(row))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (bp BuilderPanel) renderVariablesTab() string {
	var sb strings.Builder

	header := tableHeaderStyle.Render(fmt.Sprintf("%-22s  %-28s  %-8s", "Key", "Value", "Type"))

	// ── Local variables {varName} ─────────────────────────────────────────
	sb.WriteString(sidebarTitleStyle.Render("LOCAL  {varName}"))
	sb.WriteString("\n")
	sb.WriteString(header)
	sb.WriteString("\n")

	if len(bp.variables) == 0 {
		sb.WriteString(dimStyle.Render("  No local variables yet."))
		sb.WriteString("\n")
	}
	for i, v := range bp.variables {
		valDisplay := truncate(v.Value, 27)
		typeDisplay := v.Type
		if typeDisplay == "" {
			typeDisplay = "static"
		}
		row := fmt.Sprintf("%-22s  %-28s  %-8s", truncate(v.Key, 21), valDisplay, typeDisplay)
		selected := !bp.varInGlobal && i == bp.varCursor
		if selected {
			sb.WriteString(tableSelectedStyle.Render(row))
		} else {
			sb.WriteString(tableRowStyle.Render(row))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	// ── Global variables {{varName}} ──────────────────────────────────────
	sb.WriteString(sidebarTitleStyle.Render("GLOBAL  {{varName}}"))
	sb.WriteString("\n")
	sb.WriteString(header)
	sb.WriteString("\n")

	if len(bp.globalVars) == 0 {
		sb.WriteString(dimStyle.Render("  No global variables yet."))
		sb.WriteString("\n")
	}
	for i, v := range bp.globalVars {
		valDisplay := truncate(v.Value, 27)
		typeDisplay := v.Type
		if typeDisplay == "" {
			typeDisplay = "static"
		}
		row := fmt.Sprintf("%-22s  %-28s  %-8s", truncate(v.Key, 21), valDisplay, typeDisplay)
		selected := bp.varInGlobal && i == bp.varCursor
		if selected {
			sb.WriteString(tableSelectedStyle.Render(row))
		} else {
			sb.WriteString(tableRowStyle.Render(row))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (bp BuilderPanel) renderTestsTab() string {
	var sb strings.Builder

	sb.WriteString(tableHeaderStyle.Render(fmt.Sprintf("%-25s  %-20s  %-20s", "Name", "Type", "Expected")))
	sb.WriteString("\n")

	if len(bp.tests) == 0 {
		sb.WriteString(dimStyle.Render("  No tests yet."))
	}

	for i, tc := range bp.tests {
		row := fmt.Sprintf("%-25s  %-20s  %-20s",
			truncate(tc.Name, 24),
			truncate(tc.Type, 19),
			truncate(tc.Expected, 19),
		)
		if i == bp.testCursor {
			sb.WriteString(tableSelectedStyle.Render(row))
		} else {
			sb.WriteString(tableRowStyle.Render(row))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
