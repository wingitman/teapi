package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
)

// ── Root model ────────────────────────────────────────────────────────────────
//
// Model is the top-level application model. It owns:
//   - Which panel has focus
//   - Which full-screen overlay is active (if any)
//   - All three panels (sidebar, builder, response)
//   - The modal overlay (nil if none)
//   - App data (collections, history, global vars, workflows)
//   - Config + keybindings
//
// Update routes messages to the right panel based on focus.
// View composes panels into the final layout.

type Model struct {
	// Panel focus
	focus Panel

	// Panels
	sidebar  SidebarPanel
	builder  BuilderPanel
	response ResponsePanel

	// Modal overlay (nil = none open)
	modal *Modal

	// Persisted data
	data AppData

	// Config + keybindings
	cfg  Config
	keys KeyMap

	// (help overlay removed — the dynamic hint bar covers all keybinds)

	// Terminal size
	width  int
	height int

	// editMode is true when a text field (URL input or body textarea) is active.
	// In edit mode, almost all single-letter keys pass through to the field.
	// Esc always exits edit mode.
	editMode bool

	// Status bar message (transient)
	statusMsg string
}

// ── Sizing constants ──────────────────────────────────────────────────────────

const (
	titleBarHeight = 1
)

// ── Constructor ───────────────────────────────────────────────────────────────

func NewModel() (Model, error) {
	cfg, err := LoadConfig()
	if err != nil {
		cfg = defaultConfig()
	}

	data, err := LoadData()
	if err != nil {
		data = AppData{}
	}

	keys := NewKeyMap(cfg)

	m := Model{
		cfg:   cfg,
		keys:  keys,
		data:  data,
		focus: PanelBuilder,
	}

	// Panels will be properly sized on the first WindowSizeMsg.
	// Give them a reasonable default so they don't panic before then.
	m.sidebar = NewSidebarPanel(cfg.UI.SidebarWidth, 30)

	// Expand the first group by default so requests are visible in the sidebar.
	if len(data.Collections) > 0 {
		m.sidebar.expanded[data.Collections[0].ID] = true
	}
	m.sidebar.Rebuild(data)

	m.builder = NewBuilderPanel(80, 20, data)
	m.response = NewResponsePanel(80, 15)

	// Pre-load the first request if we have one.
	if len(data.Collections) > 0 && len(data.Collections[0].Requests) > 0 {
		m.builder.LoadRequest(data.Collections[0].Requests[0])
		// Advance sidebar cursor to land on that request (skip the group header node).
		m.sidebar.cursor = 2 // node 0 = "COLLECTIONS" sep, node 1 = group, node 2 = first request
	}

	return m, nil
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return textinput_Blink()
}

// textinput_Blink is a thin wrapper to avoid import collisions.
func textinput_Blink() tea.Cmd {
	return func() tea.Msg {
		return nil
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── Window resize ─────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.applyLayout()

	// ── Async results ─────────────────────────────────────────────────────
	case httpResultMsg:
		m.response.SetResult(msg.status, msg.latencyMs, msg.body, msg.headers, msg.tests)
		m.response.Loading = false
		m.builder.bodyInput.Blur()

		// Save to history
		addHistEntry(&m.data, HistEntry{
			Method:    msg.method,
			URL:       msg.url,
			Status:    msg.status,
			LatencyMs: msg.latencyMs,
			At:        now(),
			Body:      msg.body_,
		})
		m.sidebar.Rebuild(m.data)

		// Auto-focus response panel
		m.focus = PanelResponse

		// Save data asynchronously
		cmds = append(cmds, saveDataCmd(m.data))

		m.statusMsg = fmt.Sprintf("%s → %s", msg.method, statusText(msg.status))

	case httpErrMsg:
		m.response.SetError(msg.err.Error(), msg.latencyMs)
		m.response.Loading = false

		addHistEntry(&m.data, HistEntry{
			Method:    msg.method,
			URL:       msg.url,
			Status:    0,
			LatencyMs: msg.latencyMs,
			At:        now(),
		})
		m.sidebar.Rebuild(m.data)
		m.focus = PanelResponse
		cmds = append(cmds, saveDataCmd(m.data))

		m.statusMsg = errorStyle.Render("Error: " + msg.err.Error())

	case dataSavedMsg:
		// Data saved — nothing to do

	case editorClosedMsg:
		// User closed $EDITOR — update builder body if it was the body editor
		if msg.content != "" {
			m.builder.bodyInput.SetValue(msg.content)
		}

	case configReloadedMsg:
		// Rebuild keybindings from the reloaded config so changes apply live.
		m.cfg = msg.cfg
		m.keys = NewKeyMap(msg.cfg)
		m.statusMsg = dimStyle.Render("Config reloaded.")

	// ── Sidebar selections ────────────────────────────────────────────────
	case SidebarSelectMsg:
		if msg.HistIdx >= 0 && msg.HistIdx < len(m.data.History) {
			// Load from history
			h := m.data.History[msg.HistIdx]
			req := Request{
				Method: h.Method,
				URL:    h.URL,
				Body:   h.Body,
			}
			m.builder.LoadRequest(req)
			m.focus = PanelBuilder
		} else if msg.RequestID != "" {
			// Load saved request
			req, _ := findRequest(m.data, msg.RequestID)
			if req != nil {
				m.builder.LoadRequest(*req)
				m.focus = PanelBuilder
			}
		}

	case SidebarOpenWorkflowMsg:
		// Navigate to Workflows sub-tab in the builder
		m.focus = PanelBuilder
		m.builder.activeTab = BuilderTabWorkflows

	// ── Modal results ─────────────────────────────────────────────────────
	case addHeaderMsg:
		m.builder.ApplyAddHeader(msg.key, msg.value)
		m.saveCurrentRequest()
		cmds = append(cmds, saveDataCmd(m.data))

	case editHeaderMsg:
		m.builder.ApplyEditHeader(msg.index, msg.key, msg.value)
		m.saveCurrentRequest()
		cmds = append(cmds, saveDataCmd(m.data))

	case addVariableMsg:
		if msg.global {
			m.data.GlobalVars = append(m.data.GlobalVars, Variable{Key: msg.key, Value: msg.value, Type: msg.varType})
			m.builder.globalVars = m.data.GlobalVars
		} else {
			m.builder.ApplyAddVariable(msg.key, msg.value, msg.varType)
			m.saveCurrentRequest()
		}
		cmds = append(cmds, saveDataCmd(m.data))

	case addGroupMsg:
		m.data.Collections = append(m.data.Collections, Group{
			ID:      newID(),
			Name:    msg.name,
			BaseURL: msg.baseURL,
		})
		m.sidebar.Rebuild(m.data)
		cmds = append(cmds, saveDataCmd(m.data))

	case addRequestMsg:
		newReq := Request{
			ID:     newID(),
			Name:   msg.name,
			Method: "GET",
		}
		for i, g := range m.data.Collections {
			if g.ID == msg.groupID {
				m.data.Collections[i].Requests = append(m.data.Collections[i].Requests, newReq)
				break
			}
		}
		m.sidebar.Rebuild(m.data)
		m.builder.LoadRequest(newReq)
		m.focus = PanelBuilder
		cmds = append(cmds, saveDataCmd(m.data))

	case addTestMsg:
		m.builder.ApplyAddTest(TestCase{
			Name:     msg.name,
			Type:     msg.testType,
			Expected: msg.expected,
			JSONPath: msg.jsonPath,
		})
		m.saveCurrentRequest()
		cmds = append(cmds, saveDataCmd(m.data))

	case addWorkflowMsg:
		wf := Workflow{ID: newID(), Name: msg.name}
		m.data.Workflows = append(m.data.Workflows, wf)
		m.rebuildWorkflowScreen()
		m.statusMsg = dimStyle.Render("Workflow created.")
		cmds = append(cmds, saveDataCmd(m.data))

	case addWorkflowStepMsg:
		// Look up the request by name in the current collections.
		req := findRequestByName(m.data, msg.requestName)
		if req == nil {
			m.statusMsg = errorStyle.Render("Request not found: " + msg.requestName)
		} else {
			step := WorkflowStep{RequestID: req.ID, Mode: msg.mode}
			addWorkflowStep(&m.data, msg.workflowID, step)
			m.rebuildWorkflowScreen()
			m.statusMsg = dimStyle.Render("Step added: " + req.Name)
			cmds = append(cmds, saveDataCmd(m.data))
		}

	case deleteWorkflowMsg:
		deleteWorkflow(&m.data, msg.workflowID)
		m.rebuildWorkflowScreen()
		m.statusMsg = dimStyle.Render("Workflow deleted.")
		cmds = append(cmds, saveDataCmd(m.data))

	case deleteWorkflowStepMsg:
		deleteWorkflowStep(&m.data, msg.workflowID, msg.stepIdx)
		m.rebuildWorkflowScreen()
		m.statusMsg = dimStyle.Render("Step removed.")
		cmds = append(cmds, saveDataCmd(m.data))

	case addBatchMsg:
		b := Batch{
			ID:          newID(),
			Name:        msg.name,
			SourcePath:  msg.sourcePath,
			SourceType:  msg.sourceType,
			URLTemplate: msg.urlTemplate,
			Method:      msg.method,
			Concurrency: 1,
		}
		m.data.Batches = append(m.data.Batches, b)
		// Rebuild the batch screen with updated data
		m.builder.batchScreen = NewBatchScreen(m.data.Batches, m.builder.width, m.builder.height-4)
		m.statusMsg = dimStyle.Render("Batch run created.")
		cmds = append(cmds, saveDataCmd(m.data))

	case modalCancelMsg:
		m.modal = nil

	// ── Collection CRUD ───────────────────────────────────────────────────

	case renameGroupMsg:
		renameGroup(&m.data, msg.groupID, msg.name)
		m.sidebar.Rebuild(m.data)
		m.statusMsg = dimStyle.Render("Collection renamed.")
		cmds = append(cmds, saveDataCmd(m.data))

	case renameRequestMsg:
		renameRequest(&m.data, msg.requestID, msg.name)
		// Also update the builder if this request is currently loaded
		if m.builder.Request.ID == msg.requestID {
			m.builder.Request.Name = msg.name
		}
		m.sidebar.Rebuild(m.data)
		m.statusMsg = dimStyle.Render("Request renamed.")
		cmds = append(cmds, saveDataCmd(m.data))

	case editGroupMsg:
		updateGroupMeta(&m.data, msg.groupID, msg.name, msg.baseURL)
		m.sidebar.Rebuild(m.data)
		m.statusMsg = dimStyle.Render("Collection updated.")
		cmds = append(cmds, saveDataCmd(m.data))

	case deleteGroupMsg:
		deleteGroup(&m.data, msg.groupID)
		m.sidebar.Rebuild(m.data)
		m.statusMsg = dimStyle.Render("Collection deleted.")
		cmds = append(cmds, saveDataCmd(m.data))

	// ── Sidebar action messages ───────────────────────────────────────────

	case sidebarRenameMsg:
		cmd := m.openRenameModal(msg.node)
		cmds = append(cmds, cmd)

	case sidebarEditMsg:
		cmd := m.openEditGroupModal(msg.node)
		cmds = append(cmds, cmd)

	case sidebarDeleteGroupMsg:
		cmd := m.openDeleteGroupModal(msg.node)
		cmds = append(cmds, cmd)

	case sidebarOpenDataMsg:
		cmds = append(cmds, openDataFileCmd())

	// ── Data file reload ──────────────────────────────────────────────────

	case dataFileClosedMsg:
		// User may have edited teapi.json directly — reload everything.
		if d, err := LoadData(); err == nil {
			m.data = d
			m.sidebar.Rebuild(m.data)
			// Re-expand the first group so the tree doesn't collapse
			if len(m.data.Collections) > 0 {
				m.sidebar.expanded[m.data.Collections[0].ID] = true
				m.sidebar.Rebuild(m.data)
			}
			m.statusMsg = dimStyle.Render("teapi.json reloaded.")
		} else {
			m.statusMsg = errorStyle.Render("Reload failed: " + err.Error())
		}

	case workflowResultMsg:
		// Route to builder's workflow tab
		var cmd tea.Cmd
		m.builder.workflowScreen, cmd = m.builder.workflowScreen.Update(msg, m.keys, m.data)
		cmds = append(cmds, cmd)

	case batchDoneMsg:
		// Route to builder's batch tab
		var cmd tea.Cmd
		m.builder.batchScreen, cmd = m.builder.batchScreen.Update(msg, m.keys, m.data.GlobalVars)
		cmds = append(cmds, cmd)

	// ── Paste (bracketed-paste / terminal paste) ─────────────────────────
	case tea.PasteMsg:
		if m.editMode {
			cmds = append(cmds, m.routeKeyToPanel(msg))
		}

	// ── Clipboard feedback ────────────────────────────────────────────────
	case clipboardCopiedMsg:
		m.statusMsg = testPassStyle.Render("Copied " + msg.label + " to clipboard!")
		cmds = append(cmds, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
			return clipboardClearMsg{}
		}))

	case clipboardClearMsg:
		m.statusMsg = ""

	// ── Key presses ───────────────────────────────────────────────────────
	case tea.KeyPressMsg:
		// Modal captures all keys first
		if m.modal != nil {
			updated, cmd, done := m.modal.Update(msg)
			if done {
				m.modal = nil
				cmds = append(cmds, cmd)
			} else {
				m.modal = &updated
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		// ── Edit mode: Esc, Enter, and Tab are intercepted; everything else types ──
		if m.editMode {
			switch {
			case key_matches(msg, m.keys.Escape):
				// Revert to the snapshotted values and exit.
				m.builder.urlInput.SetValue(m.builder.urlSnapshot)
				m.builder.bodyInput.SetValue(m.builder.bodySnapshot)
				m.exitEditMode()
			case key_matches(msg, m.keys.Enter) && m.builder.innerFocus == BuilderFocusURL:
				// Confirm URL edit and exit edit mode.
				m.exitEditMode()
			case key_matches(msg, m.keys.TabNext):
				// Tab from URL → Body; Tab from Body → exit edit + next section
				if m.builder.innerFocus == BuilderFocusURL {
					m.builder.urlInput.Blur()
					cmd := m.builder.bodyInput.Focus()
					m.builder.innerFocus = BuilderFocusBody
					cmds = append(cmds, cmd)
				} else {
					m.exitEditMode()
					m.builder.activeTab = BuilderTabHeaders
				}
			case msg.String() == "ctrl+v":
				// ctrl+v: read clipboard and synthesise a PasteMsg so the
				// focused input's own PasteMsg handler does the insertion.
				if str, err := clipboard.ReadAll(); err == nil {
					cmd := m.routeKeyToPanel(tea.PasteMsg{Content: str})
					cmds = append(cmds, cmd)
				}
			default:
				cmd := m.routeKeyToPanel(msg)
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		// ── Navigate mode ────────────────────────────────────────────────────
		//
		// All keys are matched via key_matches() against the KeyMap, which is
		// built from teapi.toml. This means every binding is fully remappable.

		switch {

		// Quit
		case key_matches(msg, m.keys.Quit):
			return m, tea.Quit

		// Tab / Shift+Tab — primary section navigation
		case key_matches(msg, m.keys.TabNext):
			m.tabForward()

		case key_matches(msg, m.keys.TabPrev):
			m.tabBackward()

		// Up / Down — list navigation and response scrolling
		case key_matches(msg, m.keys.Up) || key_matches(msg, m.keys.Down):
			// On the Request tab, Up/Down moves the field selection cursor
			// (URL ↔ Body) instead of navigating a list.
			if m.focus == PanelBuilder && m.builder.activeTab == BuilderTabRequest {
				if key_matches(msg, m.keys.Up) {
					m.builder.innerFocus = BuilderFocusURL
				} else {
					m.builder.innerFocus = BuilderFocusBody
				}
			} else {
				cmds = append(cmds, m.routeKeyToPanel(msg))
			}

		// Left / Right — cycle method on Request tab; switch workflow panels elsewhere
		case key_matches(msg, m.keys.Left) || key_matches(msg, m.keys.Right):
			if m.focus == PanelBuilder {
				if m.builder.activeTab == BuilderTabRequest {
					// Cycle HTTP method backwards or forwards
					if key_matches(msg, m.keys.Left) {
						m.builder.methodIdx = (m.builder.methodIdx - 1 + len(httpMethods)) % len(httpMethods)
					} else {
						m.builder.methodIdx = (m.builder.methodIdx + 1) % len(httpMethods)
					}
				} else {
					cmds = append(cmds, m.routeKeyToPanel(msg))
				}
			}

		// Enter — confirm / activate
		case key_matches(msg, m.keys.Enter):
			switch m.focus {
			case PanelSidebar:
				cmds = append(cmds, m.routeKeyToPanel(msg))
			case PanelBuilder:
				if m.builder.activeTab == BuilderTabRequest {
					// Snapshot current values so Esc can revert them.
					m.builder.urlSnapshot = m.builder.urlInput.Value()
					m.builder.bodySnapshot = m.builder.bodyInput.Value()
					m.editMode = true
					// Focus whichever field the cursor is already on.
					if m.builder.innerFocus == BuilderFocusBody {
						cmd := m.builder.bodyInput.Focus()
						cmds = append(cmds, cmd)
					} else {
						m.builder.innerFocus = BuilderFocusURL
						cmd := m.builder.urlInput.Focus()
						cmds = append(cmds, cmd)
					}
				}
			}

		// Space — toggle header enabled/disabled
		case key_matches(msg, m.keys.Space):
			if m.focus == PanelBuilder {
				cmds = append(cmds, m.routeKeyToPanel(msg))
			}

		// Send request / run workflow / run batch
		case key_matches(msg, m.keys.SendRequest):
			switch {
			case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabWorkflows:
				if item, ok := m.builder.workflowScreen.list.SelectedItem().(workflowItem); ok {
					m.builder.workflowScreen.running = true
					cmds = append(cmds, runWorkflowCmd(item.wf, m.data))
				}
			case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabBatch:
				if item, ok := m.builder.batchScreen.list.SelectedItem().(batchItem); ok {
					m.builder.batchScreen.running = true
					cmds = append(cmds, runBatchCmd(item.b, m.data.GlobalVars))
				}
			default:
				m.saveCurrentRequest()
				req := m.builder.CurrentRequest()
				m.response.Loading = true
				m.focus = PanelResponse
				groupVars := m.groupVarsFor(req)
				cmds = append(cmds, doRequest(req, groupVars, m.data.GlobalVars, m.data))
				cmds = append(cmds, saveDataCmd(m.data))
			}

		// New item
		case key_matches(msg, m.keys.NewItem):
			if m.focus == PanelSidebar {
				cmds = append(cmds, m.openNewItemModal())
			} else if m.focus == PanelBuilder {
				if m.builder.activeTab == BuilderTabWorkflows && !m.builder.workflowScreen.focusList {
					// Steps sub-panel: add a step to the selected workflow
					cmds = append(cmds, m.openAddWorkflowStepModal())
				} else {
					cmds = append(cmds, m.openNewItemModalForBuilder())
				}
			}

		// Delete item
		case key_matches(msg, m.keys.DeleteItem):
			if m.focus == PanelSidebar {
				cmds = append(cmds, m.openDeleteModal())
			} else if m.focus == PanelBuilder {
				switch m.builder.activeTab {
				case BuilderTabWorkflows:
					if m.builder.workflowScreen.focusList {
						cmds = append(cmds, m.deleteSelectedWorkflow())
					} else {
						cmds = append(cmds, m.deleteSelectedWorkflowStep())
					}
				case BuilderTabBatch:
					cmds = append(cmds, m.deleteSelectedBatch())
				default:
					// Headers, Variables, Tests — handled in builder.Update
					cmds = append(cmds, m.routeKeyToPanel(msg))
				}
			}

		// Open config in editor
		case key_matches(msg, m.keys.OpenConfig):
			cmds = append(cmds, openConfigCmd())

		// Open in editor — context-sensitive
		case key_matches(msg, m.keys.OpenEditor):
			if m.focus == PanelBuilder {
				if m.builder.activeTab == BuilderTabBatch {
					// Open the selected batch's source file directly in $EDITOR.
					// No-op if no batch is selected or source path is empty.
					if item, ok := m.builder.batchScreen.list.SelectedItem().(batchItem); ok {
						if item.b.SourcePath != "" {
							cmds = append(cmds, openFileInEditorCmd(item.b.SourcePath))
						}
					}
				} else {
					// All other builder tabs: open the request body.
					cmds = append(cmds, openEditorCmd(m.builder.bodyInput.Value(), true, ".json"))
				}
			}

		// Open response body in editor
		case key_matches(msg, m.keys.OpenResponse):
			if m.focus == PanelResponse {
				cmds = append(cmds, openEditorCmd(m.response.Body, false, ".json"))
			}

		// Copy focused content to clipboard
		case key_matches(msg, m.keys.CopyItem):
			cmds = append(cmds, m.copyFocusedContent())

		// Single-letter fallback keys not in the main keymap
		// (these are hardcoded UX shortcuts that don't warrant a config entry)
		default:
			switch msg.String() {
			// N (Shift+N) — add global variable
			case "N":
				if m.focus == PanelBuilder && m.builder.activeTab == BuilderTabVariables {
					modal := NewModal(
						ModalAddVariable,
						"Add Global Variable",
						[]ModalField{
							{Label: "Key", Placeholder: "api_token"},
							{Label: "Value", Placeholder: "my-secret-value"},
						},
						m.width,
						func(vals []string) tea.Msg {
							return addVariableMsg{key: vals[0], value: vals[1], varType: "static", global: true}
						},
					)
					m.modal = &modal
				}

			// r — rename (sidebar)
			case "r":
				if m.focus == PanelSidebar {
					cmds = append(cmds, m.routeKeyToPanel(msg))
				}

			// e — edit (sidebar or builder headers)
			case "e":
				if m.focus == PanelSidebar {
					cmds = append(cmds, m.routeKeyToPanel(msg))
				} else if m.focus == PanelBuilder && m.builder.activeTab == BuilderTabHeaders {
					cmds = append(cmds, m.openEditHeaderModal())
				}

			// O (Shift+O) — open teapi.json in editor (sidebar only)
			case "O":
				if m.focus == PanelSidebar {
					cmds = append(cmds, openDataFileCmd())
				}
			}
		}
	}

	// Keep builder's appData in sync so workflow/batch screens have full data.
	m.builder.appData = m.data

	return m, tea.Batch(cmds...)
}

// ── Panel routing ─────────────────────────────────────────────────────────────

func (m *Model) routeKeyToPanel(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	switch m.focus {
	case PanelSidebar:
		updated, cmd := m.sidebar.Update(msg, m.keys, m.data)
		m.sidebar = updated
		cmds = append(cmds, cmd)

	case PanelBuilder:
		// Only route to builder when in editMode (text field active) or for
		// list-navigation keys (↑/↓/space/d) that are safe to pass through.
		updated, cmd := m.builder.Update(msg, m.keys)
		m.builder = updated
		cmds = append(cmds, cmd)

	case PanelResponse:
		updated, cmd := m.response.Update(msg, m.keys)
		m.response = updated
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// ── Clipboard copy ────────────────────────────────────────────────────────────

// copyFocusedContent copies the contextually relevant content to the system
// clipboard and returns a cmd that delivers clipboardCopiedMsg to trigger
// the "Copied!" status flash.
//
// Context:
//   - Response panel focused → copy response body
//   - Builder, Request tab   → copy URL
//   - Builder, Headers tab   → copy selected header value
//   - Builder, Body tab (Request tab body) → copy body (via URL+body logic)
//   - Builder, Variables tab → copy selected variable value
func (m *Model) copyFocusedContent() tea.Cmd {
	var text, label string

	switch m.focus {
	case PanelResponse:
		text = m.response.Body
		label = "response"

	case PanelBuilder:
		switch m.builder.activeTab {
		case BuilderTabRequest:
			text = m.builder.urlInput.Value()
			label = "URL"

		case BuilderTabHeaders:
			if m.builder.headerCursor < len(m.builder.headers) {
				h := m.builder.headers[m.builder.headerCursor]
				text = h.Value
				label = "header value"
			}

		case BuilderTabVariables:
			if m.builder.varInGlobal {
				if m.builder.varCursor < len(m.builder.globalVars) {
					text = m.builder.globalVars[m.builder.varCursor].Value
					label = "variable value"
				}
			} else {
				if m.builder.varCursor < len(m.builder.variables) {
					text = m.builder.variables[m.builder.varCursor].Value
					label = "variable value"
				}
			}
		}
	}

	if text == "" {
		return nil
	}

	_ = clipboard.WriteAll(text)
	return func() tea.Msg { return clipboardCopiedMsg{label: label} }
}

// ── Modal openers ─────────────────────────────────────────────────────────────

func (m *Model) openNewItemModal() tea.Cmd {
	if m.focus == PanelSidebar {
		// Determine if we're on a group (add request) or top-level (add group)
		node := m.sidebar.currentNode()
		if node != nil && (node.Kind == NodeGroup || node.Kind == NodeRequest) && node.GroupID != "" {
			groupID := node.GroupID
			modal := NewModal(
				ModalAddRequest,
				"New Request",
				[]ModalField{
					{Label: "Name", Placeholder: "Get Users"},
				},
				m.width,
				func(vals []string) tea.Msg {
					return addRequestMsg{name: vals[0], groupID: groupID}
				},
			)
			m.modal = &modal
		} else {
			modal := NewModal(
				ModalAddGroup,
				"New Collection",
				[]ModalField{
					{Label: "Name", Placeholder: "My API"},
					{Label: "Base URL", Placeholder: "https://api.example.com"},
				},
				m.width,
				func(vals []string) tea.Msg {
					return addGroupMsg{name: vals[0], baseURL: vals[1]}
				},
			)
			m.modal = &modal
		}
	}
	return nil
}

func (m *Model) openNewItemModalForBuilder() tea.Cmd {
	switch m.builder.activeTab {
	case BuilderTabHeaders:
		modal := NewModal(
			ModalAddHeader,
			"Add Header",
			[]ModalField{
				{Label: "Key", Placeholder: "Authorization"},
				{Label: "Value", Placeholder: "Bearer token123"},
			},
			m.width,
			func(vals []string) tea.Msg {
				return addHeaderMsg{key: vals[0], value: vals[1], enabled: true}
			},
		)
		m.modal = &modal

	case BuilderTabVariables:
		modal := NewModal(
			ModalAddVariable,
			"Add Variable",
			[]ModalField{
				{Label: "Key", Placeholder: "userId"},
				{Label: "Value", Placeholder: "123  or  faker:randomName"},
			},
			m.width,
			func(vals []string) tea.Msg {
				varType := "static"
				value := vals[1]
				if strings.HasPrefix(value, "faker:") {
					varType = "faker"
					value = strings.TrimPrefix(value, "faker:")
				}
				return addVariableMsg{key: vals[0], value: value, varType: varType}
			},
		)
		m.modal = &modal

	case BuilderTabTests:
		modal := NewTestModal(m.width)
		m.modal = &modal

	case BuilderTabWorkflows:
		modal := NewModal(
			ModalAddWorkflow,
			"New Workflow",
			[]ModalField{
				{Label: "Name", Placeholder: "Login then Get Users"},
			},
			m.width,
			func(vals []string) tea.Msg {
				return addWorkflowMsg{name: vals[0]}
			},
		)
		m.modal = &modal

	case BuilderTabBatch:
		modal := NewModal(
			ModalAddBatch,
			"New Batch Run",
			[]ModalField{
				{Label: "Name", Placeholder: "Load test users"},
				{Label: "Source file", Placeholder: "/path/to/urls.txt  or  /path/to/data.csv"},
				{Label: "URL template", Placeholder: "https://api.example.com/{line}  or  https://api.example.com/{id}"},
				{Label: "Method", Placeholder: "GET"},
			},
			m.width,
			func(vals []string) tea.Msg {
				sourceType := "txt"
				if len(vals[1]) > 4 && vals[1][len(vals[1])-4:] == ".csv" {
					sourceType = "csv"
				}
				method := vals[3]
				if method == "" {
					method = "GET"
				}
				return addBatchMsg{
					name:        vals[0],
					sourcePath:  vals[1],
					sourceType:  sourceType,
					urlTemplate: vals[2],
					method:      strings.ToUpper(method),
				}
			},
		)
		m.modal = &modal
	}
	return nil
}

func (m *Model) openEditHeaderModal() tea.Cmd {
	idx := m.builder.headerCursor
	if idx >= len(m.builder.headers) {
		return nil
	}
	h := m.builder.headers[idx]
	modal := NewModal(
		ModalEditHeader,
		"Edit Header",
		[]ModalField{
			{Label: "Key", Placeholder: "Content-Type", Value: h.Key},
			{Label: "Value", Placeholder: "application/json", Value: h.Value},
		},
		m.width,
		func(vals []string) tea.Msg {
			return editHeaderMsg{index: idx, key: vals[0], value: vals[1]}
		},
	)
	m.modal = &modal
	return nil
}

// ── Workflow helpers ──────────────────────────────────────────────────────────

// rebuildWorkflowScreen rebuilds the embedded WorkflowScreen from m.data
// and re-applies the correct size. Call after any change to m.data.Workflows.
func (m *Model) rebuildWorkflowScreen() {
	ws := NewWorkflowScreen(m.data.Workflows, m.builder.width, m.builder.height-4)
	ws.SetSize(m.builder.width, m.builder.height-4)
	m.builder.workflowScreen = ws
}

// openAddWorkflowStepModal opens a modal for adding a step to the selected workflow.
func (m *Model) openAddWorkflowStepModal() tea.Cmd {
	item, ok := m.builder.workflowScreen.list.SelectedItem().(workflowItem)
	if !ok {
		return nil
	}
	wfID := item.wf.ID

	// Build a hint listing available request names.
	var names []string
	for _, g := range m.data.Collections {
		for _, r := range g.Requests {
			names = append(names, r.Name)
		}
		for _, sg := range g.Groups {
			for _, r := range sg.Requests {
				names = append(names, r.Name)
			}
		}
	}
	placeholder := "e.g. Get Users"
	if len(names) > 0 && len(names) <= 4 {
		placeholder = strings.Join(names, " / ")
	}

	modal := NewModal(
		ModalAddWorkflowStep,
		"Add Step to: "+item.wf.Name,
		[]ModalField{
			{Label: "Request name", Placeholder: placeholder},
			{Label: "Mode", Placeholder: "sequential"},
		},
		m.width,
		func(vals []string) tea.Msg {
			mode := strings.TrimSpace(vals[1])
			if mode != "parallel" {
				mode = "sequential"
			}
			return addWorkflowStepMsg{
				workflowID:  wfID,
				requestName: strings.TrimSpace(vals[0]),
				mode:        mode,
			}
		},
	)
	m.modal = &modal
	return nil
}

// deleteSelectedBatch deletes the currently highlighted batch config.
func (m *Model) deleteSelectedBatch() tea.Cmd {
	item, ok := m.builder.batchScreen.list.SelectedItem().(batchItem)
	if !ok {
		return nil
	}
	deleteBatch(&m.data, item.b.ID)
	m.builder.batchScreen = NewBatchScreen(m.data.Batches, m.builder.width, m.builder.height-4)
	m.builder.batchScreen.SetSize(m.builder.width, m.builder.height-4)
	m.statusMsg = dimStyle.Render("Batch deleted.")
	return saveDataCmd(m.data)
}

// deleteSelectedWorkflow deletes the currently highlighted workflow.
func (m *Model) deleteSelectedWorkflow() tea.Cmd {
	item, ok := m.builder.workflowScreen.list.SelectedItem().(workflowItem)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		return deleteWorkflowMsg{workflowID: item.wf.ID}
	}
}

// deleteSelectedWorkflowStep removes the currently highlighted step.
func (m *Model) deleteSelectedWorkflowStep() tea.Cmd {
	wfItem, ok := m.builder.workflowScreen.list.SelectedItem().(workflowItem)
	if !ok {
		return nil
	}
	stepIdx := m.builder.workflowScreen.stepList.Index()
	return func() tea.Msg {
		return deleteWorkflowStepMsg{workflowID: wfItem.wf.ID, stepIdx: stepIdx}
	}
}

func (m *Model) openDeleteModal() tea.Cmd {
	node := m.sidebar.currentNode()
	if node == nil {
		return nil
	}
	switch node.Kind {
	case NodeRequest:
		deleteRequest(&m.data, node.RequestID)
		m.sidebar.Rebuild(m.data)
		m.statusMsg = dimStyle.Render("Request deleted.")
		return saveDataCmd(m.data)

	case NodeHistEntry:
		idx := node.HistIdx
		if idx >= 0 && idx < len(m.data.History) {
			m.data.History = append(m.data.History[:idx], m.data.History[idx+1:]...)
			m.sidebar.Rebuild(m.data)
			m.statusMsg = dimStyle.Render("History entry deleted.")
			return saveDataCmd(m.data)
		}
	}
	// Groups handled via sidebarDeleteGroupMsg (confirm modal)
	return nil
}

// openRenameModal opens a single-field modal pre-filled with the node's current name.
func (m *Model) openRenameModal(node SidebarNode) tea.Cmd {
	title := "Rename Request"
	if node.Kind == NodeGroup {
		title = "Rename Collection"
	}
	modal := NewModal(
		ModalRenameItem,
		title,
		[]ModalField{
			{Label: "New name", Value: node.Label},
		},
		m.width,
		func(vals []string) tea.Msg {
			if node.Kind == NodeGroup {
				return renameGroupMsg{groupID: node.GroupID, name: vals[0]}
			}
			return renameRequestMsg{requestID: node.RequestID, name: vals[0]}
		},
	)
	m.modal = &modal
	return nil
}

// openEditGroupModal opens a two-field modal pre-filled with the group's name + base URL.
func (m *Model) openEditGroupModal(node SidebarNode) tea.Cmd {
	// Look up the current base URL from data
	baseURL := ""
	if g := findGroup(m.data, node.GroupID); g != nil {
		baseURL = g.BaseURL
	}
	modal := NewModal(
		ModalRenameItem,
		"Edit Collection",
		[]ModalField{
			{Label: "Name", Value: node.Label},
			{Label: "Base URL", Placeholder: "https://api.example.com", Value: baseURL},
		},
		m.width,
		func(vals []string) tea.Msg {
			return editGroupMsg{groupID: node.GroupID, name: vals[0], baseURL: vals[1]}
		},
	)
	m.modal = &modal
	return nil
}

// openDeleteGroupModal opens a confirm modal before deleting a collection.
func (m *Model) openDeleteGroupModal(node SidebarNode) tea.Cmd {
	groupID := node.GroupID
	modal := NewModal(
		ModalConfirmDelete,
		"Delete Collection — "+node.Label,
		[]ModalField{
			// Single read-only-ish field: just press Enter to confirm, Esc to cancel.
			// We use a placeholder so the user knows what to do.
			{Label: "Type collection name to confirm", Placeholder: node.Label},
		},
		m.width,
		func(vals []string) tea.Msg {
			// Only delete if they typed the name correctly
			if vals[0] == node.Label {
				return deleteGroupMsg{groupID: groupID}
			}
			// Wrong name — silently cancel (modal already closed)
			return modalCancelMsg{}
		},
	)
	m.modal = &modal
	return nil
}

// ── Navigation ────────────────────────────────────────────────────────────────

// tabForward advances through the section cycle:
// Sidebar → Request → Headers → Variables → Tests → Workflows → Batch → Response → Sidebar → ...
func (m *Model) tabForward() {
	switch {
	case m.focus == PanelSidebar:
		m.focus = PanelBuilder
		m.builder.activeTab = BuilderTabRequest

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabRequest:
		m.builder.activeTab = BuilderTabHeaders

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabHeaders:
		m.builder.activeTab = BuilderTabVariables

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabVariables:
		m.builder.activeTab = BuilderTabTests

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabTests:
		m.builder.activeTab = BuilderTabWorkflows

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabWorkflows:
		m.builder.activeTab = BuilderTabBatch

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabBatch:
		m.focus = PanelResponse

	case m.focus == PanelResponse:
		m.focus = PanelSidebar
	}
}

// tabBackward is the reverse of tabForward.
func (m *Model) tabBackward() {
	switch {
	case m.focus == PanelSidebar:
		m.focus = PanelResponse

	case m.focus == PanelResponse:
		m.focus = PanelBuilder
		m.builder.activeTab = BuilderTabBatch

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabBatch:
		m.builder.activeTab = BuilderTabWorkflows

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabWorkflows:
		m.builder.activeTab = BuilderTabTests

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabTests:
		m.builder.activeTab = BuilderTabVariables

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabVariables:
		m.builder.activeTab = BuilderTabHeaders

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabHeaders:
		m.builder.activeTab = BuilderTabRequest

	case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabRequest:
		m.focus = PanelSidebar
	}
}

// exitEditMode blurs all text fields and returns to navigate mode.
func (m *Model) exitEditMode() {
	m.editMode = false
	m.builder.urlInput.Blur()
	m.builder.bodyInput.Blur()
	m.builder.innerFocus = BuilderFocusURL
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// saveCurrentRequest saves the builder's current state back to AppData.
func (m *Model) saveCurrentRequest() {
	req := m.builder.CurrentRequest()
	if req.ID != "" {
		if !upsertRequest(&m.data, req) {
			// Not found — was created ad-hoc, don't auto-save
		}
	}
}

// groupVarsFor finds the variables of the group that owns this request.
func (m *Model) groupVarsFor(req Request) []Variable {
	for _, g := range m.data.Collections {
		for _, r := range g.Requests {
			if r.ID == req.ID {
				return g.Vars
			}
		}
		for _, sg := range g.Groups {
			for _, r := range sg.Requests {
				if r.ID == req.ID {
					return sg.Vars
				}
			}
		}
	}
	return nil
}

// applyLayout recalculates and applies panel sizes based on terminal dimensions.
func (m Model) applyLayout() Model {
	sidebarW := m.cfg.UI.SidebarWidth
	mainW := m.width - sidebarW - 2 // -2 for sidebar border

	// hintBarLines: 1 divider + global line + context line + 1 spare for wrapping = 4
	// These sit below the response panel, outside all borders.
	const hintBarLines = 4
	totalContentH := m.height - titleBarHeight - hintBarLines - 2

	// Builder gets cfg.UI.ResponseSplit% of the right panel height
	builderH := totalContentH * m.cfg.UI.ResponseSplit / 100
	responseH := totalContentH - builderH

	if builderH < 6 {
		builderH = 6
	}
	if responseH < 6 {
		responseH = 6
	}

	m.sidebar.width = sidebarW
	m.sidebar.height = totalContentH
	m.builder.SetSize(mainW, builderH)
	m.response.SetSize(mainW, responseH)
	// Resize embedded workflow/batch screens — this also resizes their
	// internal bubbles/list components so they render correctly.
	m.builder.workflowScreen.SetSize(mainW, builderH-4)
	m.builder.batchScreen.SetSize(mainW, builderH-4)

	return m
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() tea.View {
	if m.width == 0 {
		v := tea.NewView("Loading...")
		v.AltScreen = true
		return v
	}

	screen := m.renderNormalLayout()

	// Modal composited on top
	if m.modal != nil {
		screen = OverlayModal(screen, m.modal.View(), m.width, m.height)
	}

	v := tea.NewView(screen)
	v.AltScreen = true
	return v
}

func (m Model) renderNormalLayout() string {
	// Sidebar
	sidebarView := m.sidebar.View(m.focus == PanelSidebar)

	// Builder + response stacked vertically
	builderView := m.builder.View(m.focus == PanelBuilder)
	responseView := m.response.View(m.focus == PanelResponse)
	rightPanel := lipgloss.JoinVertical(lipgloss.Left, builderView, responseView)

	// Main layout: sidebar left, panels right
	mainLayout := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, rightPanel)

	// Title bar — delbysoft brand left, global shortcuts right
	delby := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render("delby")
	soft := lipgloss.NewStyle().Foreground(lipgloss.Color("#5865F2")).Bold(true).Render("soft")
	appName := lipgloss.NewStyle().Foreground(colGray).Render(" / teapi")
	brand := " " + delby + soft + appName + " "
	rightHints := dimStyle.Render("o:config  q:quit")
	pad := m.width - lipgloss.Width(brand) - lipgloss.Width(rightHints)
	if pad < 0 {
		pad = 0
	}
	titleBar := brand + strings.Repeat(" ", pad) + rightHints

	// Hint bar — below the response panel, outside all borders, full terminal width
	hintBar := m.buildHintBar()

	return lipgloss.JoinVertical(lipgloss.Left,
		titleBar,
		mainLayout,
		hintBar,
	)
}

// buildHintBar returns a two-line hint string rendered below the response panel.
// Line 1: global navigation — always the same.
// Line 2: context-sensitive keys for the current section.
func (m Model) buildHintBar() string {
	// Divider across full width
	divider := hintDividerStyle.Render(strings.Repeat("─", m.width))

	// Helper: render a key+description hint pair using the actual bound key string.
	hint := func(binding key.Binding, desc string) string {
		keys := binding.Keys()
		if len(keys) == 0 {
			return ""
		}
		return hintKeyStyle.Render(keys[0]) + hintStyle.Render(":"+desc+"  ")
	}

	// Global line — always shown, uses actual bound keys from config.
	global := hint(m.keys.TabNext, "next section") +
		hint(m.keys.TabPrev, "prev section") +
		hint(m.keys.SendRequest, "send") +
		hint(m.keys.Quit, "quit")

	// Context line — changes based on focus + state.
	var ctx string
	// nav shows the up/down keys from config.
	nav := hint(m.keys.Up, "up") + hint(m.keys.Down, "down")

	switch m.focus {
	case PanelSidebar:
		ctx = nav +
			hint(m.keys.Enter, "load") +
			hint(m.keys.NewItem, "new") +
			hintKeyStyle.Render("r") + hintStyle.Render(":rename  ") +
			hintKeyStyle.Render("e") + hintStyle.Render(":edit  ") +
			hint(m.keys.DeleteItem, "delete") +
			hintKeyStyle.Render("o") + hintStyle.Render(":open file")

	case PanelBuilder:
		switch m.builder.activeTab {
		case BuilderTabRequest:
			if m.editMode && m.builder.innerFocus == BuilderFocusBody {
				ctx = hint(m.keys.Escape, "revert & exit") +
					hint(m.keys.TabNext, "next section")
			} else if m.editMode && m.builder.innerFocus == BuilderFocusURL {
				ctx = hint(m.keys.Enter, "confirm") +
					hint(m.keys.Escape, "revert") +
					hint(m.keys.TabNext, "edit body") +
					hint(m.keys.OpenEditor, "open in editor")
			} else {
				ctx = hint(m.keys.Up, "URL") +
					hint(m.keys.Down, "body") +
					hint(m.keys.Enter, "edit selected") +
					hint(m.keys.Left, "prev method") +
					hint(m.keys.Right, "next method") +
					hint(m.keys.CopyItem, "copy URL")
			}
		case BuilderTabHeaders:
			ctx = nav +
				hint(m.keys.NewItem, "add") +
				hintKeyStyle.Render("e") + hintStyle.Render(":edit  ") +
				hint(m.keys.DeleteItem, "delete") +
				hint(m.keys.CopyItem, "copy value") +
				hint(m.keys.Space, "toggle")
		case BuilderTabVariables:
			ctx = nav +
				hint(m.keys.NewItem, "add local") +
				hintKeyStyle.Render("N") + hintStyle.Render(":add global  ") +
				hint(m.keys.DeleteItem, "delete") +
				hint(m.keys.CopyItem, "copy value")
		case BuilderTabTests:
			ctx = nav +
				hint(m.keys.NewItem, "add") +
				hint(m.keys.DeleteItem, "delete")
		case BuilderTabWorkflows:
			if m.builder.workflowScreen.focusList {
				ctx = nav +
					hint(m.keys.Right, "→ steps") +
					hint(m.keys.SendRequest, "run") +
					hint(m.keys.NewItem, "new workflow") +
					hint(m.keys.DeleteItem, "delete workflow")
			} else {
				ctx = nav +
					hint(m.keys.Left, "← workflows") +
					hint(m.keys.SendRequest, "run") +
					hint(m.keys.NewItem, "add step") +
					hint(m.keys.DeleteItem, "remove step")
			}
		case BuilderTabBatch:
			ctx = nav +
				hint(m.keys.SendRequest, "run") +
				hint(m.keys.NewItem, "new") +
				hint(m.keys.DeleteItem, "delete") +
				hint(m.keys.OpenEditor, "edit source file")
		}

	case PanelResponse:
		ctx = nav +
			hint(m.keys.CopyItem, "copy response") +
			hint(m.keys.OpenResponse, "open in editor")
	}

	// Status message: show on the context line when present
	if m.statusMsg != "" {
		ctx = m.statusMsg + "  " + ctx
	}

	// Do NOT double-wrap with hintStyle.Render() here — the inner hintKeyStyle
	// and hintStyle renders already apply colours. Wrapping again strips them.
	return divider + "\n" + global + "\n" + ctx
}

// ── Missing message types ─────────────────────────────────────────────────────

type addTestMsg struct {
	name     string
	testType string
	expected string
	jsonPath string
}

// now returns the current time.
func now() time.Time {
	return time.Now()
}

// ── Config editor ─────────────────────────────────────────────────────────────

// configReloadedMsg is sent after the user closes the config editor.
// We reload config so any keybinding changes take effect immediately.
type configReloadedMsg struct {
	cfg Config
}

// openConfigCmd suspends the TUI, opens teapi.toml in $EDITOR, then resumes.
// After the editor closes, the config is reloaded so keybind changes apply live.
func openConfigCmd() tea.Cmd {
	path := configPath()
	editor := resolveEditor()
	return tea.ExecProcess(exec.Command(editor, path), func(err error) tea.Msg {
		// Reload config regardless of error — user may have saved a partial edit.
		cfg, _ := LoadConfig()
		return configReloadedMsg{cfg: cfg}
	})
}

// openFileInEditorCmd opens an existing file at the given path directly in
// $EDITOR. Unlike openEditorCmd it does not copy to a temp file — the user
// edits the real file in place. Nothing is read back on close.
func openFileInEditorCmd(path string) tea.Cmd {
	return tea.ExecProcess(exec.Command(resolveEditor(), path), func(err error) tea.Msg {
		// We don't reload anything — the file is the source of truth.
		// Return an editorClosedMsg with empty content so the handler no-ops.
		return editorClosedMsg{content: ""}
	})
}

// openDataFileCmd suspends the TUI, opens teapi.json in $EDITOR, then resumes.
// After the editor closes, the data is reloaded from disk.
func openDataFileCmd() tea.Cmd {
	path := dataPath()
	editor := resolveEditor()
	return tea.ExecProcess(exec.Command(editor, path), func(err error) tea.Msg {
		return dataFileClosedMsg{}
	})
}

// resolveEditor returns the user's preferred editor, falling back to nano.
func resolveEditor() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	switch runtime.GOOS {
	case "windows":
		return "notepad"
	default:
		return "nano"
	}
}
