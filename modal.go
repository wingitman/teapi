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
	ModalConfirmDelete
)

// Modal is the overlay dialog model.
type Modal struct {
	Kind    ModalKind
	Title   string
	Fields  []textinput.Model
	labels  []string
	focused int
	Width   int

	// onConfirm is called with the field values when the user presses Enter.
	// It returns a tea.Msg that will be dispatched to Update().
	onConfirm func(values []string) tea.Msg
}

// NewModal creates a new modal with the given fields.
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
		Kind:      kind,
		Title:     title,
		Fields:    inputs,
		labels:    labels,
		focused:   0,
		Width:     width,
		onConfirm: onConfirm,
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

		case "enter":
			// If last field, confirm; otherwise move to next field
			if m.focused == len(m.Fields)-1 {
				values := make([]string, len(m.Fields))
				for i, f := range m.Fields {
					values[i] = f.Value()
				}
				var confirmCmd tea.Cmd
				if m.onConfirm != nil {
					result := m.onConfirm(values)
					confirmCmd = func() tea.Msg { return result }
				}
				return m, confirmCmd, true
			}
			// Move to next field
			m.Fields[m.focused].Blur()
			m.focused++
			m.Fields[m.focused].Focus()

		case "tab", "shift+tab":
			m.Fields[m.focused].Blur()
			if msg.String() == "tab" {
				m.focused = (m.focused + 1) % len(m.Fields)
			} else {
				m.focused = (m.focused - 1 + len(m.Fields)) % len(m.Fields)
			}
			cmd := m.Fields[m.focused].Focus()
			cmds = append(cmds, cmd)
		}
	}

	// Pass the message to the focused field
	if len(m.Fields) > 0 {
		var cmd tea.Cmd
		m.Fields[m.focused], cmd = m.Fields[m.focused].Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...), false
}

// ── Modal View ────────────────────────────────────────────────────────────────

// View renders the modal as a string. The caller composites it on top of
// the background using lipgloss.Place().
func (m Modal) View() string {
	var sb strings.Builder

	sb.WriteString(modalTitleStyle.Render(m.Title))
	sb.WriteString("\n\n")

	for i, field := range m.Fields {
		label := m.labels[i]
		if i == m.focused {
			sb.WriteString(labelFocusedStyle.Render(label+": "))
		} else {
			sb.WriteString(labelStyle.Render(label+": "))
		}
		sb.WriteString(field.View())
		sb.WriteString("\n\n")
	}

	sb.WriteString(dimStyle.Render("Enter: confirm  Tab: next field  Esc: cancel"))

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
