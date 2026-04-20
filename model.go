package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

	// Help bar
	help     help.Model
	showHelp bool

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
		focus: PanelBuilder, // start focused on the builder so the user can type immediately
		help:  help.New(),
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
		})
		m.saveCurrentRequest()
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

		// ── Edit mode: only Esc and Ctrl+C are intercepted globally ─────────
		//
		// When a text field is active, every key except Esc types into it.
		// We pass the message straight to the builder (which owns the field).
		if m.editMode {
			switch msg.String() {
			case "esc":
				m.exitEditMode()
			case "tab":
				// Tab from URL → Body; Tab from Body → next section (Headers)
				if m.builder.innerFocus == BuilderFocusURL {
					m.builder.urlInput.Blur()
					cmd := m.builder.bodyInput.Focus()
					m.builder.innerFocus = BuilderFocusBody
					cmds = append(cmds, cmd)
				} else {
					// Exit body, move to Headers sub-tab
					m.exitEditMode()
					m.builder.activeTab = BuilderTabHeaders
				}
			default:
				// Forward everything else to the active text field
				cmd := m.routeKeyToPanel(msg)
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		// ── Navigate mode: single-letter keys drive all navigation ────────

		switch msg.String() {

		// Quit — always works
		case "q", "ctrl+c":
			return m, tea.Quit

		// Help overlay toggle
		case "?":
			m.showHelp = !m.showHelp

		// Tab / Shift+Tab — the primary navigation mechanism.
		// Cycles: Sidebar → Request → Headers → Variables → Tests → Response → Sidebar
		case "tab":
			m.tabForward()

		case "shift+tab":
			m.tabBackward()

		// Enter — context-sensitive confirm / activate
		case "enter":
			switch m.focus {
			case PanelSidebar:
				// Expand group or load request
				cmd := m.routeKeyToPanel(msg)
				cmds = append(cmds, cmd)
			case PanelBuilder:
				if m.builder.activeTab == BuilderTabRequest {
					// Enter edit mode for the URL field
					m.editMode = true
					m.builder.innerFocus = BuilderFocusURL
					cmd := m.builder.urlInput.Focus()
					cmds = append(cmds, cmd)
				}
			}

		// Send request / run workflow / run batch — context-sensitive
		case "s":
			switch {
			case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabWorkflows:
				// Run the selected workflow
				cmd := m.routeKeyToPanel(msg)
				cmds = append(cmds, cmd)
			case m.focus == PanelBuilder && m.builder.activeTab == BuilderTabBatch:
				// Run the selected batch
				cmd := m.routeKeyToPanel(msg)
				cmds = append(cmds, cmd)
			default:
				// Send HTTP request
				m.saveCurrentRequest()
				req := m.builder.CurrentRequest()
				m.response.Loading = true
				m.focus = PanelResponse
				groupVars := m.groupVarsFor(req)
				cmds = append(cmds, doRequest(req, groupVars, m.data.GlobalVars, m.data))
				cmds = append(cmds, saveDataCmd(m.data))
			}

		// Cycle HTTP method (builder, request tab only)
		case "m":
			if m.focus == PanelBuilder && m.builder.activeTab == BuilderTabRequest {
				m.builder.methodIdx = (m.builder.methodIdx + 1) % len(httpMethods)
			}

		// Open teapi.toml in editor
		case "o":
			cmds = append(cmds, openConfigCmd())

		// Open request body in editor
		case "E":
			if m.focus == PanelBuilder {
				cmds = append(cmds, openEditorCmd(m.builder.bodyInput.Value(), true, ".json"))
			}

		// Open response body in editor
		case "R":
			if m.focus == PanelResponse {
				cmds = append(cmds, openEditorCmd(m.response.Body, false, ".json"))
			}

		// New item (sidebar or builder list tabs)
		case "n":
			if m.focus == PanelSidebar {
				cmd := m.openNewItemModal()
				cmds = append(cmds, cmd)
			} else if m.focus == PanelBuilder {
				cmd := m.openNewItemModalForBuilder()
				cmds = append(cmds, cmd)
			}

		// N (shift+n) — add global variable (only on Variables tab)
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

		// Delete item
		case "d":
			if m.focus == PanelSidebar {
				cmd := m.openDeleteModal()
				cmds = append(cmds, cmd)
			} else if m.focus == PanelBuilder {
				// Delete selected row in Headers/Variables/Tests
				cmd := m.routeKeyToPanel(msg)
				cmds = append(cmds, cmd)
			}

		// Space — toggle header enabled/disabled
		case " ":
			if m.focus == PanelBuilder {
				cmd := m.routeKeyToPanel(msg)
				cmds = append(cmds, cmd)
			}

		// Up / Down / j / k — navigate within sidebar or builder list tabs
		case "up", "down", "k", "j":
			cmd := m.routeKeyToPanel(msg)
			cmds = append(cmds, cmd)

		// Sidebar-specific single-letter actions
		case "r", "e":
			if m.focus == PanelSidebar {
				cmd := m.routeKeyToPanel(msg)
				cmds = append(cmds, cmd)
			} else if msg.String() == "e" && m.focus == PanelBuilder {
				// Edit selected header
				if m.builder.activeTab == BuilderTabHeaders {
					cmd := m.openEditHeaderModal()
					cmds = append(cmds, cmd)
				}
			}

		// Open teapi.json in editor (sidebar only)
		case "O":
			if m.focus == PanelSidebar {
				cmds = append(cmds, openDataFileCmd())
			}
		}
	}

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
		modal := NewModal(
			ModalAddVariable,
			"Add Test",
			[]ModalField{
				{Label: "Name", Placeholder: "Status is 200"},
				{Label: "Type", Placeholder: "status_equals / body_contains / jsonpath_equals"},
				{Label: "Expected", Placeholder: "200 / text / $.field value"},
			},
			m.width,
			func(vals []string) tea.Msg {
				return addTestMsg{
					name:     vals[0],
					testType: vals[1],
					expected: vals[2],
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

func (m *Model) openDeleteModal() tea.Cmd {
	node := m.sidebar.currentNode()
	if node == nil {
		return nil
	}
	if node.Kind == NodeRequest {
		// Delete request directly — no confirm needed for a single request
		deleteRequest(&m.data, node.RequestID)
		m.sidebar.Rebuild(m.data)
		m.statusMsg = dimStyle.Render("Request deleted.")
		return saveDataCmd(m.data)
	}
	// Groups are handled via sidebarDeleteGroupMsg (which opens a confirm modal)
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
	// Keep builder's embedded screens sized to the builder panel
	m.builder.workflowScreen.width = mainW
	m.builder.workflowScreen.height = builderH - 4
	m.builder.batchScreen.width = mainW
	m.builder.batchScreen.height = builderH - 4

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
	rightHints := dimStyle.Render("o:config  ?:help  q:quit")
	pad := m.width - lipgloss.Width(brand) - lipgloss.Width(rightHints)
	if pad < 0 {
		pad = 0
	}
	titleBar := brand + strings.Repeat(" ", pad) + rightHints

	// Hint bar — below the response panel, outside all borders, full terminal width
	var hintBar string
	if m.showHelp {
		hintBar = m.help.View(m.keys)
	} else {
		hintBar = m.buildHintBar()
	}

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

	// Global line — always the same regardless of section
	global := hintKeyStyle.Render("Tab") + hintStyle.Render(":next section  ") +
		hintKeyStyle.Render("Shift+Tab") + hintStyle.Render(":prev section  ") +
		hintKeyStyle.Render("s") + hintStyle.Render(":send  ") +
		hintKeyStyle.Render("?") + hintStyle.Render(":help  ") +
		hintKeyStyle.Render("q") + hintStyle.Render(":quit")

	// Context line — changes based on focus + state
	var ctx string
	switch m.focus {
	case PanelSidebar:
		ctx = hintKeyStyle.Render("↑↓") + hintStyle.Render(":nav  ") +
			hintKeyStyle.Render("enter") + hintStyle.Render(":load  ") +
			hintKeyStyle.Render("n") + hintStyle.Render(":new  ") +
			hintKeyStyle.Render("r") + hintStyle.Render(":rename  ") +
			hintKeyStyle.Render("e") + hintStyle.Render(":edit  ") +
			hintKeyStyle.Render("d") + hintStyle.Render(":delete  ") +
			hintKeyStyle.Render("o") + hintStyle.Render(":open file")

	case PanelBuilder:
		switch m.builder.activeTab {
		case BuilderTabRequest:
			if m.editMode && m.builder.innerFocus == BuilderFocusBody {
				ctx = hintKeyStyle.Render("Esc") + hintStyle.Render(":exit body  ") +
					hintKeyStyle.Render("Tab") + hintStyle.Render(":next section")
			} else if m.editMode && m.builder.innerFocus == BuilderFocusURL {
				ctx = hintKeyStyle.Render("Esc") + hintStyle.Render(":exit URL  ") +
					hintKeyStyle.Render("Tab") + hintStyle.Render(":edit body  ") +
					hintKeyStyle.Render("m") + hintStyle.Render(":method  ") +
					hintKeyStyle.Render("E") + hintStyle.Render(":open in editor")
			} else {
				ctx = hintKeyStyle.Render("enter") + hintStyle.Render(":edit URL  ") +
					hintKeyStyle.Render("m") + hintStyle.Render(":method  ") +
					hintKeyStyle.Render("E") + hintStyle.Render(":open body in editor")
			}
		case BuilderTabHeaders:
			ctx = hintKeyStyle.Render("↑↓") + hintStyle.Render(":nav  ") +
				hintKeyStyle.Render("n") + hintStyle.Render(":add  ") +
				hintKeyStyle.Render("e") + hintStyle.Render(":edit  ") +
				hintKeyStyle.Render("d") + hintStyle.Render(":delete  ") +
				hintKeyStyle.Render("space") + hintStyle.Render(":toggle")
		case BuilderTabVariables:
			ctx = hintKeyStyle.Render("↑↓") + hintStyle.Render(":nav  ") +
				hintKeyStyle.Render("n") + hintStyle.Render(":add local  ") +
				hintKeyStyle.Render("N") + hintStyle.Render(":add global  ") +
				hintKeyStyle.Render("d") + hintStyle.Render(":delete")
		case BuilderTabTests:
			ctx = hintKeyStyle.Render("↑↓") + hintStyle.Render(":nav  ") +
				hintKeyStyle.Render("n") + hintStyle.Render(":add  ") +
				hintKeyStyle.Render("d") + hintStyle.Render(":delete")
		case BuilderTabWorkflows:
			ctx = hintKeyStyle.Render("↑↓") + hintStyle.Render(":nav  ") +
				hintKeyStyle.Render("s") + hintStyle.Render(":run workflow  ") +
				hintKeyStyle.Render("n") + hintStyle.Render(":new  ") +
				hintKeyStyle.Render("d") + hintStyle.Render(":delete  ") +
				hintKeyStyle.Render("←→") + hintStyle.Render(":switch panel")
		case BuilderTabBatch:
			ctx = hintKeyStyle.Render("↑↓") + hintStyle.Render(":nav  ") +
				hintKeyStyle.Render("s") + hintStyle.Render(":run batch  ") +
				hintKeyStyle.Render("n") + hintStyle.Render(":new  ") +
				hintKeyStyle.Render("d") + hintStyle.Render(":delete")
		}

	case PanelResponse:
		ctx = hintKeyStyle.Render("↑↓") + hintStyle.Render(":scroll  ") +
			hintKeyStyle.Render("R") + hintStyle.Render(":open in editor")
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
