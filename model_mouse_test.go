package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSidebarMouseClickUsesVisibleContentRow(t *testing.T) {
	m := Model{}
	m.sidebar = NewSidebarPanel(28, 20)
	m.builder = NewBuilderPanel(80, 20, AppData{})
	m.sidebar.offset = 5
	for i := range 12 {
		m.sidebar.nodes = append(m.sidebar.nodes, SidebarNode{Kind: NodeRequest, Label: "req", Depth: 0, RequestID: string(rune('a' + i))})
	}

	m.handleMouseClick(tea.MouseClickMsg{X: 4, Y: titleBarHeight + 1 + 3, Button: tea.MouseLeft})

	if m.sidebar.cursor != 8 {
		t.Fatalf("cursor = %d, want 8", m.sidebar.cursor)
	}
}

func TestSidebarMouseClickWrappedRowSelectsWrappedNode(t *testing.T) {
	m := Model{}
	m.sidebar = NewSidebarPanel(28, 20)
	m.builder = NewBuilderPanel(80, 20, AppData{})
	m.sidebar.nodes = []SidebarNode{
		{Kind: NodeGroup, Label: "COLLECTIONS", Depth: -1},
		{Kind: NodeHistEntry, Label: "POST    localhost:5954/very-long-path", Depth: 0},
		{Kind: NodeHistEntry, Label: "GET     google.com", Depth: 0},
	}

	m.handleMouseClick(tea.MouseClickMsg{X: 4, Y: titleBarHeight + 1 + 2, Button: tea.MouseLeft})

	if m.sidebar.cursor != 1 {
		t.Fatalf("cursor = %d, want wrapped node 1", m.sidebar.cursor)
	}
}

func TestBuilderMouseClickTabs(t *testing.T) {
	m := mouseTestModel()

	m.handleMouseClick(tea.MouseClickMsg{X: builderGlobalX(m, 12), Y: builderGlobalY(builderTabRow), Button: tea.MouseLeft})

	if m.builder.activeTab != BuilderTabHeaders {
		t.Fatalf("activeTab = %v, want %v", m.builder.activeTab, BuilderTabHeaders)
	}
}

func TestBuilderMouseClickMethod(t *testing.T) {
	m := mouseTestModel()

	m.handleMouseClick(tea.MouseClickMsg{X: builderGlobalX(m, 36), Y: builderGlobalY(builderMethodRow), Button: tea.MouseLeft})

	if got := httpMethods[m.builder.methodIdx]; got != "DELETE" {
		t.Fatalf("method = %q, want DELETE", got)
	}
}

func TestBuilderMouseClickURLAndBodyEnterEditMode(t *testing.T) {
	m := mouseTestModel()

	m.handleMouseClick(tea.MouseClickMsg{X: builderGlobalX(m, 8), Y: builderGlobalY(builderURLRow), Button: tea.MouseLeft})
	if !m.editMode || m.builder.innerFocus != BuilderFocusURL || !m.builder.urlInput.Focused() {
		t.Fatalf("URL click did not focus URL edit mode")
	}

	m.handleMouseClick(tea.MouseClickMsg{X: builderGlobalX(m, 2), Y: builderGlobalY(builderBodyLabelRow), Button: tea.MouseLeft})
	if !m.editMode || m.builder.innerFocus != BuilderFocusBody || !m.builder.bodyInput.Focused() {
		t.Fatalf("Body click did not focus body edit mode")
	}
}

func mouseTestModel() Model {
	m := Model{}
	m.sidebar = NewSidebarPanel(28, 20)
	m.builder = NewBuilderPanel(80, 20, AppData{})
	m.response = NewResponsePanel(80, 20)
	m.focus = PanelBuilder
	return m
}

func builderGlobalX(m Model, contentCol int) int {
	return m.sidebar.width + 2 + 1 + contentCol
}

func builderGlobalY(contentRow int) int {
	return titleBarHeight + 1 + contentRow
}
