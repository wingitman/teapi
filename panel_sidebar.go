package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── Sidebar panel ─────────────────────────────────────────────────────────────
//
// The sidebar shows a tree of collections (groups + requests) and a history
// section below. Navigation is with j/k (up/down), Enter to expand/load.
//
// The tree is rendered as a flat []SidebarNode — we build this list fresh
// whenever the data changes. Each node has a depth and kind.

// SidebarPanel holds all sidebar state.
type SidebarPanel struct {
	nodes    []SidebarNode // flat rendered tree
	cursor   int           // currently highlighted row
	offset   int           // scroll offset (top of viewport)
	height   int           // available height for the list
	width    int
	expanded map[string]bool // groupID → expanded?
}

// NewSidebarPanel creates a new sidebar.
func NewSidebarPanel(width, height int) SidebarPanel {
	return SidebarPanel{
		width:    width,
		height:   height,
		expanded: make(map[string]bool),
	}
}

// Rebuild reconstructs the flat node list from AppData.
// Call this whenever data changes (new request saved, history updated, etc.)
func (sp *SidebarPanel) Rebuild(data AppData) {
	var nodes []SidebarNode

	// ── Collections ──
	nodes = append(nodes, SidebarNode{Kind: NodeGroup, Label: "COLLECTIONS", Depth: -1})

	for _, g := range data.Collections {
		expanded := sp.expanded[g.ID]
		nodes = append(nodes, SidebarNode{
			Kind:     NodeGroup,
			Label:    g.Name,
			Depth:    0,
			Expanded: expanded,
			GroupID:  g.ID,
		})

		if expanded {
			// Sub-groups
			for _, sg := range g.Groups {
				sgExpanded := sp.expanded[sg.ID]
				nodes = append(nodes, SidebarNode{
					Kind:     NodeGroup,
					Label:    sg.Name,
					Depth:    1,
					Expanded: sgExpanded,
					GroupID:  sg.ID,
				})
				if sgExpanded {
					for _, r := range sg.Requests {
						nodes = append(nodes, SidebarNode{
							Kind:      NodeRequest,
							Label:     r.Name,
							Depth:     2,
							GroupID:   sg.ID,
							RequestID: r.ID,
						})
					}
				}
			}
			// Requests in this group
			for _, r := range g.Requests {
				nodes = append(nodes, SidebarNode{
					Kind:      NodeRequest,
					Label:     r.Name,
					Depth:     1,
					GroupID:   g.ID,
					RequestID: r.ID,
				})
			}
		}
	}

	// ── Workflows section ──
	if len(data.Workflows) > 0 {
		nodes = append(nodes, SidebarNode{Kind: NodeWorkflow, Label: "WORKFLOWS", Depth: -1})
		for _, wf := range data.Workflows {
			nodes = append(nodes, SidebarNode{
				Kind:  NodeWorkflow,
				Label: wf.Name,
				Depth: 0,
			})
		}
	}

	// ── History section ──
	nodes = append(nodes, SidebarNode{Kind: NodeHistory, Label: "HISTORY", Depth: -1})
	maxHistory := 15
	for i, h := range data.History {
		if i >= maxHistory {
			break
		}
		label := fmt.Sprintf("%-7s %s", h.Method, shortenURL(h.URL))
		nodes = append(nodes, SidebarNode{
			Kind:    NodeHistEntry,
			Label:   label,
			Depth:   0,
			HistIdx: i,
		})
	}

	sp.nodes = nodes
	// Keep cursor in bounds
	if sp.cursor >= len(sp.nodes) {
		sp.cursor = max(0, len(sp.nodes)-1)
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

// SidebarSelectMsg is sent when the user selects a request in the sidebar.
type SidebarSelectMsg struct {
	RequestID string
	HistIdx   int // -1 if not a history entry
}

// SidebarOpenWorkflowMsg is sent when the user selects a workflow node.
type SidebarOpenWorkflowMsg struct{}

func (sp SidebarPanel) Update(msg tea.Msg, keys KeyMap, data AppData) (SidebarPanel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key_matches(msg, keys.Up):
			sp.moveCursor(-1)

		case key_matches(msg, keys.Down):
			sp.moveCursor(1)

		case key_matches(msg, keys.Enter):
			return sp.handleEnter(data)

		case key_matches(msg, keys.Space):
			// Toggle group expand
			if n := sp.currentNode(); n != nil && n.Kind == NodeGroup && n.Depth >= 0 {
				sp.expanded[n.GroupID] = !sp.expanded[n.GroupID]
				sp.Rebuild(data)
			}

		case key_matches(msg, keys.DeleteItem):
			// Groups need a confirm modal — send a message to root model.
			// Requests are deleted directly by root model.
			if n := sp.currentNode(); n != nil && n.Kind == NodeGroup && n.Depth >= 0 {
				return sp, func() tea.Msg { return sidebarDeleteGroupMsg{node: *n} }
			}
			// Request deletion handled by root model via the same DeleteItem key.

		case key_matches(msg, keys.NewItem):
			// Handled by root model (opens modal)

		case msg.String() == "r":
			// Rename — works for both groups and requests
			if n := sp.currentNode(); n != nil && n.Depth >= 0 {
				return sp, func() tea.Msg { return sidebarRenameMsg{node: *n} }
			}

		case msg.String() == "e":
			// Edit group meta (name + base URL), or load request into builder
			if n := sp.currentNode(); n != nil {
				switch n.Kind {
				case NodeGroup:
					return sp, func() tea.Msg { return sidebarEditMsg{node: *n} }
				case NodeRequest:
					// Load into builder — same as Enter
					return sp.handleEnter(data)
				}
			}

		case msg.String() == "o":
			// Open teapi.json in $EDITOR
			return sp, func() tea.Msg { return sidebarOpenDataMsg{} }
		}
	}
	return sp, nil
}

func (sp *SidebarPanel) moveCursor(delta int) {
	sp.cursor += delta
	if sp.cursor < 0 {
		sp.cursor = 0
	}
	if sp.cursor >= len(sp.nodes) {
		sp.cursor = len(sp.nodes) - 1
	}
	// Skip separator nodes (depth = -1)
	for sp.cursor > 0 && sp.cursor < len(sp.nodes) && sp.nodes[sp.cursor].Depth == -1 {
		sp.cursor += delta
	}
	if sp.cursor < 0 {
		sp.cursor = 0
	}
	if sp.cursor >= len(sp.nodes) {
		sp.cursor = len(sp.nodes) - 1
	}
	// Adjust scroll offset
	if sp.cursor < sp.offset {
		sp.offset = sp.cursor
	}
	if sp.cursor >= sp.offset+sp.height {
		sp.offset = sp.cursor - sp.height + 1
	}
}

func (sp SidebarPanel) handleEnter(data AppData) (SidebarPanel, tea.Cmd) {
	n := sp.currentNode()
	if n == nil {
		return sp, nil
	}
	switch n.Kind {
	case NodeGroup:
		// Toggle expand
		sp.expanded[n.GroupID] = !sp.expanded[n.GroupID]
		sp.Rebuild(data)
	case NodeRequest:
		// Select the request — send a message to root model
		return sp, func() tea.Msg {
			return SidebarSelectMsg{RequestID: n.RequestID, HistIdx: -1}
		}
	case NodeHistEntry:
		return sp, func() tea.Msg {
			return SidebarSelectMsg{RequestID: "", HistIdx: n.HistIdx}
		}
	case NodeWorkflow:
		return sp, func() tea.Msg { return SidebarOpenWorkflowMsg{} }
	}
	return sp, nil
}

func (sp *SidebarPanel) currentNode() *SidebarNode {
	if sp.cursor < 0 || sp.cursor >= len(sp.nodes) {
		return nil
	}
	return &sp.nodes[sp.cursor]
}

// CurrentRequestID returns the request ID of the currently highlighted node,
// or empty string if it's not a request.
func (sp *SidebarPanel) CurrentRequestID() string {
	n := sp.currentNode()
	if n == nil {
		return ""
	}
	return n.RequestID
}

// ── View ──────────────────────────────────────────────────────────────────────

func (sp SidebarPanel) View(focused bool) string {
	var sb strings.Builder
	innerWidth := sp.renderWidth()
	renderedRows := 0

	for idx := sp.offset; idx < len(sp.nodes) && renderedRows < sp.height; idx++ {
		node := sp.nodes[idx]
		line := sp.renderNode(node, idx == sp.cursor, innerWidth)
		height := visualHeight(line)
		if renderedRows+height > sp.height {
			break
		}

		sb.WriteString(line)
		sb.WriteString("\n")
		renderedRows += height
	}

	// Pad remaining space
	rendered := sb.String()
	for renderedRows < sp.height {
		rendered += "\n"
		renderedRows++
	}

	border := panelBlurredStyle
	if focused {
		border = panelFocusedStyle
	}
	return border.Width(sp.width).Height(sp.height + 2).Render(rendered)
}

func (sp SidebarPanel) renderNode(node SidebarNode, selected bool, width int) string {
	// Section separator (depth = -1)
	if node.Depth < 0 {
		return sidebarSepStyle.Width(width).Render("─ " + node.Label + " ")
	}

	indent := strings.Repeat("  ", node.Depth)
	prefix := "  "

	switch node.Kind {
	case NodeGroup:
		if node.Expanded {
			prefix = "▼ "
		} else {
			prefix = "▶ "
		}
	case NodeRequest:
		prefix = "· "
	case NodeHistEntry:
		prefix = "  "
	}

	label := indent + prefix + node.Label

	if selected {
		return sidebarSelectedStyle.Width(width).Render(label)
	}
	switch node.Kind {
	case NodeHistEntry:
		return sidebarDimStyle.Width(width).Render(label)
	default:
		return sidebarItemStyle.Width(width).Render(label)
	}
}

func (sp SidebarPanel) visualNodeAt(row int) (int, bool) {
	if row < 0 || row >= sp.height {
		return 0, false
	}
	renderedRows := 0
	for idx := sp.offset; idx < len(sp.nodes) && renderedRows < sp.height; idx++ {
		height := sp.nodeVisualHeight(sp.nodes[idx])
		if renderedRows+height > sp.height {
			break
		}
		if row >= renderedRows && row < renderedRows+height {
			return idx, true
		}
		renderedRows += height
	}
	return 0, false
}

func (sp SidebarPanel) nodeVisualHeight(node SidebarNode) int {
	return visualHeight(sp.renderNode(node, false, sp.renderWidth()))
}

func (sp SidebarPanel) renderWidth() int {
	return max(1, sp.width-4)
}

func visualHeight(s string) int {
	if s == "" {
		return 1
	}
	return strings.Count(s, "\n") + 1
}

// ── URL shortener ─────────────────────────────────────────────────────────────

func shortenURL(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	if len(u) > 22 {
		u = u[:19] + "..."
	}
	return u
}

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// lipgloss render helpers — render a string with a colour
func renderMethod(method string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(methodColor(method)).
		Padding(0, 1).
		Render(method)
}
