package main

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── Modal overlay ─────────────────────────────────────────────────────────────
//
// Modal is a simple overlay dialog for collecting user input.
// It renders on top of the background layout using lipgloss positioning.
//
// Usage:
//   m.modal = NewModal("Add Header", []ModalField{
//     {Label: "Key",   Placeholder: "Content-Type"},
//     {Label: "Value", Placeholder: "application/json"},
//   }, func(vals []string) tea.Msg { return addHeaderMsg{vals[0], vals[1]} })
//
// The modal captures all key input until confirmed (Enter) or cancelled (Esc).

// ModalField defines one text input field in a modal.
type ModalField struct {
	Label       string
	Placeholder string
	Value       string // pre-filled value (for edit mode)
}

// ModalKind identifies what the modal is for (used to route the confirm message).
type ModalKind int

const (
	ModalAddHeader    ModalKind = iota
	ModalEditHeader
	ModalAddVariable
	ModalEditVariable
	ModalAddGroup
	ModalAddRequest
	ModalRenameItem
	ModalAddWorkflow
	ModalAddWorkflowStep
	ModalAddBatch
	ModalAddTest
	ModalConfirmDelete
)

// assertionTypes is the ordered list of test assertion types shown in the selector.
var assertionTypes = []struct {
	display  string // human-readable label shown in the UI
	internal string // value stored in TestCase.Type
	hint     string // placeholder hint for the Expected field
}{
	{"status equals", AssertStatusEquals, "e.g. 200"},
	{"body contains", AssertBodyContains, "e.g. success"},
	{"body equals", AssertBodyEquals, "exact body string"},
	{"header equals", AssertHeaderEquals, "e.g. application/json"},
	{"json path equals", AssertJSONPathEquals, "e.g. 42  or  true  or  Alice"},
}

// Modal is the overlay dialog model.
type Modal struct {
	Kind    ModalKind
	Title   string
	Fields  []textinput.Model
	labels  []string
	focused int
	Width   int

	// Selector — used for the test type field (cycles with ←/→)
	// selectorAt is the index into Fields where the selector lives (-1 = none)
	selectorAt      int
	selectorIdx     int
	selectorOptions []string // display strings for each option

	// onConfirm is called with the field values when the user confirms.
	// It returns a tea.Msg that will be dispatched to Update().
	onConfirm func(values []string) tea.Msg
}

// NewModal creates a new modal with the given text-input fields.
// onConfirm is called with a slice of field values when the user confirms.
func NewModal(kind ModalKind, title string, fields []ModalField, width int, onConfirm func(values []string) tea.Msg) Modal {
	inputs := make([]textinput.Model, len(fields))
	labels := make([]string, len(fields))
	for i, f := range fields {
		ti := textinput.New()
		ti.Placeholder = f.Placeholder
		ti.CharLimit = 1024
		ti.SetWidth(width - 10)
		if f.Value != "" {
			ti.SetValue(f.Value)
		}
		inputs[i] = ti
		labels[i] = f.Label
	}
	if len(inputs) > 0 {
		inputs[0].Focus()
	}

	return Modal{
		Kind:        kind,
		Title:       title,
		Fields:      inputs,
		labels:      labels,
		focused:     0,
		Width:       width,
		selectorAt:  -1, // no selector
		onConfirm:   onConfirm,
	}
}

// NewTestModal creates the "Add Test" modal with an inline type selector.
// Layout: Name (text) → Type (selector, ←/→ to cycle) → Path (text, jsonpath only) → Expected (text)
func NewTestModal(width int) Modal {
	// Fields: 0=Name, 1=Expected, 2=JSONPath (shown/hidden by type)
	nameInput := textinput.New()
	nameInput.Placeholder = "e.g. Status is 200"
	nameInput.CharLimit = 128
	nameInput.SetWidth(width - 10)
	nameInput.Focus()

	expectedInput := textinput.New()
	expectedInput.Placeholder = assertionTypes[0].hint
	expectedInput.CharLimit = 256
	expectedInput.SetWidth(width - 10)

	pathInput := textinput.New()
	pathInput.Placeholder = "e.g. $.user.id"
	pathInput.CharLimit = 256
	pathInput.SetWidth(width - 10)

	opts := make([]string, len(assertionTypes))
	for i, a := range assertionTypes {
		opts[i] = a.display
	}

	return Modal{
		Kind:            ModalAddTest,
		Title:           "Add Test",
		Fields:          []textinput.Model{nameInput, expectedInput, pathInput},
		labels:          []string{"Name", "Expected", "JSON Path"},
		focused:         0,
		Width:           width,
		selectorAt:      1, // the selector sits between Name (0) and Expected (2 in fields, but visually index 1)
		selectorIdx:     0,
		selectorOptions: opts,
		onConfirm:       nil, // handled specially in Update
	}
}

// ── Modal messages ────────────────────────────────────────────────────────────

// These message types are sent by the modal when the user confirms/cancels.

type modalConfirmMsg struct {
	kind   ModalKind
	values []string
}

type modalCancelMsg struct{}

// ── Specific modal confirm messages ──────────────────────────────────────────

type addHeaderMsg struct {
	key     string
	value   string
	enabled bool
}

type editHeaderMsg struct {
	index int
	key   string
	value string
}

type addVariableMsg struct {
	key     string
	value   string
	varType string
	global  bool // true = add to AppData.GlobalVars, false = add to current request
}

type editVariableMsg struct {
	index int
	key   string
	value string
}

type addGroupMsg struct {
	name    string
	baseURL string
}

type addRequestMsg struct {
	name    string
	groupID string
}

type renameItemMsg struct {
	id   string
	name string
}

type confirmDeleteMsg struct {
	id string
}

// ── Modal Update ──────────────────────────────────────────────────────────────

// numVisualFields returns the total number of visual rows in the modal.
// For the test modal: Name, [selector row], Expected, [optional JSON Path]
func (m Modal) numVisualFields() int {
	if m.selectorAt < 0 {
		return len(m.Fields)
	}
	// Fields are split by the selector: fields[0..selectorAt-1], selector, fields[selectorAt..]
	// But for the test modal specifically: visual rows are Name(0), Selector, Expected(1), JSONPath(2 if jsonpath type)
	if m.Kind == ModalAddTest {
		if m.selectorIdx == len(assertionTypes)-1 { // jsonpath_equals
			return 4 // Name, Selector, Expected, JSONPath
		}
		return 3 // Name, Selector, Expected
	}
	return len(m.Fields) + 1 // generic: fields + 1 selector
}

// visualToField maps a visual row index to a Fields index.
// Returns -1 if the visual row is the selector.
func (m Modal) visualToField(visual int) int {
	if m.selectorAt < 0 {
		return visual
	}
	if m.Kind == ModalAddTest {
		// visual 0 → Fields[0] (Name)
		// visual 1 → selector (-1)
		// visual 2 → Fields[1] (Expected)
		// visual 3 → Fields[2] (JSONPath)
		switch visual {
		case 0:
			return 0
		case 1:
			return -1
		case 2:
			return 1
		case 3:
			return 2
		}
	}
	return visual
}

// Update handles key input for the modal.
// Returns the updated modal, a tea.Cmd, and a bool indicating if the modal
// should be closed (true = close, false = keep open).
func (m Modal) Update(msg tea.Msg) (Modal, tea.Cmd, bool) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {

		case "esc":
			return m, func() tea.Msg { return modalCancelMsg{} }, true

		case "left":
			// Cycle selector backwards (when selector is focused)
			if m.selectorAt >= 0 && m.visualFocused() == -1 {
				m.selectorIdx = (m.selectorIdx - 1 + len(m.selectorOptions)) % len(m.selectorOptions)
				// Update Expected placeholder to match new type
				if m.Kind == ModalAddTest && m.selectorIdx < len(assertionTypes) {
					m.Fields[1].Placeholder = assertionTypes[m.selectorIdx].hint
				}
			}

		case "right":
			// Cycle selector forwards (when selector is focused)
			if m.selectorAt >= 0 && m.visualFocused() == -1 {
				m.selectorIdx = (m.selectorIdx + 1) % len(m.selectorOptions)
				if m.Kind == ModalAddTest && m.selectorIdx < len(assertionTypes) {
					m.Fields[1].Placeholder = assertionTypes[m.selectorIdx].hint
				}
			}

		case "enter":
			lastVisual := m.numVisualFields() - 1
			if m.focused >= lastVisual {
				// Confirm
				cmd := m.buildConfirmCmd()
				return m, cmd, true
			}
			// Move to next visual row
			m.advanceFocus(1)

		case "tab":
			m.advanceFocus(1)

		case "shift+tab":
			m.advanceFocus(-1)
		}
	}

	// Forward to the focused text field (if not on selector)
	fieldIdx := m.visualToField(m.focused)
	if fieldIdx >= 0 && fieldIdx < len(m.Fields) {
		var cmd tea.Cmd
		m.Fields[fieldIdx], cmd = m.Fields[fieldIdx].Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...), false
}

// visualFocused returns the field index for the currently focused visual row,
// or -1 if the selector is focused.
func (m Modal) visualFocused() int {
	return m.visualToField(m.focused)
}

// advanceFocus moves focus by delta, handling blur/focus of text inputs.
func (m *Modal) advanceFocus(delta int) {
	// Blur current text field if any
	if fi := m.visualToField(m.focused); fi >= 0 && fi < len(m.Fields) {
		m.Fields[fi].Blur()
	}
	total := m.numVisualFields()
	m.focused = (m.focused + delta + total) % total
	// Focus new text field if any
	if fi := m.visualToField(m.focused); fi >= 0 && fi < len(m.Fields) {
		m.Fields[fi].Focus()
	}
}

// buildConfirmCmd builds the tea.Cmd for when the user confirms the modal.
func (m Modal) buildConfirmCmd() tea.Cmd {
	if m.Kind == ModalAddTest {
		typeInternal := assertionTypes[m.selectorIdx].internal
		name := m.Fields[0].Value()
		expected := m.Fields[1].Value()
		jsonPath := m.Fields[2].Value()
		return func() tea.Msg {
			return addTestMsg{
				name:     name,
				testType: typeInternal,
				expected: expected,
				jsonPath: jsonPath,
			}
		}
	}
	// Generic modal
	if m.onConfirm != nil {
		values := make([]string, len(m.Fields))
		for i, f := range m.Fields {
			values[i] = f.Value()
		}
		result := m.onConfirm(values)
		return func() tea.Msg { return result }
	}
	return nil
}

// ── Modal View ────────────────────────────────────────────────────────────────

// View renders the modal as a string. The caller composites it on top of
// the background using lipgloss.Place().
func (m Modal) View() string {
	var sb strings.Builder

	sb.WriteString(modalTitleStyle.Render(m.Title))
	sb.WriteString("\n\n")

	if m.Kind == ModalAddTest {
		// Custom layout for test modal: Name, [selector], Expected, [JSONPath if applicable]
		total := m.numVisualFields()
		for visual := 0; visual < total; visual++ {
			fieldIdx := m.visualToField(visual)
			isFocused := visual == m.focused

			if fieldIdx == -1 {
				// Selector row
				label := "Type"
				if isFocused {
					sb.WriteString(labelFocusedStyle.Render(label + ": "))
				} else {
					sb.WriteString(labelStyle.Render(label + ": "))
				}
				// Render the cycling selector
				prev := "◀ "
				next := " ▶"
				display := m.selectorOptions[m.selectorIdx]
				if isFocused {
					sb.WriteString(hintKeyStyle.Render(prev) +
						labelFocusedStyle.Render(display) +
						hintKeyStyle.Render(next))
					sb.WriteString(dimStyle.Render("  ←/→ to change"))
				} else {
					sb.WriteString(dimStyle.Render(prev + display + next))
				}
			} else {
				// Text field row
				label := m.labels[fieldIdx]
				// For JSONPath row, update label based on assertion type
				if m.Kind == ModalAddTest && fieldIdx == 2 {
					label = "JSON Path"
				}
				if isFocused {
					sb.WriteString(labelFocusedStyle.Render(label + ": "))
				} else {
					sb.WriteString(labelStyle.Render(label + ": "))
				}
				sb.WriteString(m.Fields[fieldIdx].View())
			}
			sb.WriteString("\n\n")
		}
	} else {
		// Generic layout: all fields are text inputs
		for i, field := range m.Fields {
			label := m.labels[i]
			if i == m.focused {
				sb.WriteString(labelFocusedStyle.Render(label + ": "))
			} else {
				sb.WriteString(labelStyle.Render(label + ": "))
			}
			sb.WriteString(field.View())
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString(dimStyle.Render("Enter/Tab: next  ←/→: change type  Esc: cancel"))

	return modalBorderStyle.Width(m.Width - 4).Render(sb.String())
}

// ── Modal compositor ──────────────────────────────────────────────────────────

// OverlayModal renders the modal centered on top of the background string.
// It uses lipgloss.Place to center the modal in the terminal.
func OverlayModal(background, modal string, termWidth, termHeight int) string {
	return lipgloss.Place(
		termWidth, termHeight,
		lipgloss.Center, lipgloss.Center,
		modal,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#111122"))),
	)
}
