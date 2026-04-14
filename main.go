package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── Tabs ──────────────────────────────────────────────────────────────────────
//
// We cycle through three tabs: Request, Response, History.

type tab int

const (
	tabRequest tab = iota
	tabResponse
	tabHistory
	numTabs
)

// ── HTTP Methods ──────────────────────────────────────────────────────────────

var methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// ── Focus (within the Request tab) ───────────────────────────────────────────
//
// On the Request tab, Tab moves focus between the URL input and the body textarea.

type focus int

const (
	focusURL focus = iota
	focusBody
)

// ── Async message types ───────────────────────────────────────────────────────
//
// When the HTTP request finishes (or fails), bubbletea delivers one of these
// messages to Update(). This is how async work flows back into the model.

type httpDoneMsg struct {
	status  int
	headers http.Header
	body    string
}

type httpErrMsg struct {
	err error
}

// ── History list item ─────────────────────────────────────────────────────────
//
// historyItem implements list.Item so it can live in the bubbles list component.

type historyItem struct {
	method string
	url    string
	body   string // saved request body so we can restore it
	status int
	at     time.Time
}

func (h historyItem) Title() string {
	return fmt.Sprintf("%-7s %s", h.method, h.url)
}

func (h historyItem) Description() string {
	return fmt.Sprintf("Status: %s  •  %s", statusText(h.status), h.at.Format("15:04:05"))
}

func (h historyItem) FilterValue() string { return h.url }

// statusText turns a status code into a short readable string.
func statusText(code int) string {
	if code == 0 {
		return "error"
	}
	text := http.StatusText(code)
	if text == "" {
		return fmt.Sprintf("%d", code)
	}
	return fmt.Sprintf("%d %s", code, text)
}

// ── Model ─────────────────────────────────────────────────────────────────────
//
// The model holds ALL application state. Bubbletea calls Init, Update, and View
// on this struct. We return a new copy from Update — never mutate in place.

type model struct {
	// which of the 3 tabs is visible
	activeTab tab

	// Request tab state
	methodIdx int // index into the methods slice
	urlInput  textinput.Model
	bodyInput textarea.Model
	reqFocus  focus // which widget has keyboard focus on the Request tab

	// Response tab state
	respStatus int
	respBody   string
	respErr    string
	loading    bool
	respView   viewport.Model // scrollable viewport for the response body

	// History tab state
	histList list.Model

	// Terminal size (updated on WindowSizeMsg)
	width  int
	height int
}

// ── Constructor ───────────────────────────────────────────────────────────────

func initialModel() model {
	// --- URL input ---
	u := textinput.New()
	u.Placeholder = "https://example.com/api/endpoint"
	u.Focus()
	u.CharLimit = 2048

	// --- Body textarea ---
	b := textarea.New()
	b.Placeholder = `{"key": "value"}`
	b.SetHeight(8)
	b.ShowLineNumbers = false

	// --- History list ---
	// We start with an empty list. list.New needs an initial size; we'll
	// resize it properly when the first WindowSizeMsg arrives.
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 80, 20)
	l.Title = "Request History"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true) // fuzzy filter is nice for history

	// --- Response viewport ---
	v := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	return model{
		urlInput:  u,
		bodyInput: b,
		histList:  l,
		respView:  v,
		reqFocus:  focusURL,
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────
//
// Init runs once when the program starts. We kick off cursor blinking for the
// URL input.

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

// ── Commands ──────────────────────────────────────────────────────────────────
//
// A tea.Cmd is just a function that returns a tea.Msg. Bubbletea runs it in a
// goroutine so the UI stays responsive. The returned Msg is delivered to Update.

func doRequest(method, rawURL, body string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 30 * time.Second}

		var bodyReader io.Reader
		if body != "" {
			bodyReader = strings.NewReader(body)
		}

		req, err := http.NewRequest(method, rawURL, bodyReader)
		if err != nil {
			return httpErrMsg{err}
		}

		// Auto-set Content-Type when a body is provided.
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			return httpErrMsg{err}
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return httpErrMsg{err}
		}

		return httpDoneMsg{
			status:  resp.StatusCode,
			headers: resp.Header,
			body:    string(raw),
		}
	}
}

// ── Update ────────────────────────────────────────────────────────────────────
//
// Update is called for every event (key press, window resize, async result).
// It returns the updated model and an optional command to run next.

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd // collect multiple commands, batch them at the end

	switch msg := msg.(type) {

	// --- Window resize ---
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Leave some room for tab bar (2 lines) + status bar (2 lines) + padding
		contentHeight := m.height - 6
		if contentHeight < 4 {
			contentHeight = 4
		}
		m.urlInput.SetWidth(m.width - 8)
		m.bodyInput.SetWidth(m.width - 4)
		m.bodyInput.SetHeight(contentHeight - 6)
		m.histList.SetSize(m.width, contentHeight)
		m.respView.SetWidth(m.width - 4)
		m.respView.SetHeight(contentHeight - 2)

	// --- Async HTTP result ---
	case httpDoneMsg:
		m.loading = false
		m.respStatus = msg.status
		m.respBody = msg.body
		m.respErr = ""
		m.respView.SetContent(msg.body)
		m.respView.GotoTop()

		// Add to history
		entry := historyItem{
			method: methods[m.methodIdx],
			url:    m.urlInput.Value(),
			body:   m.bodyInput.Value(),
			status: msg.status,
			at:     time.Now(),
		}
		cmds = append(cmds, m.histList.InsertItem(0, entry))

		// Switch to the Response tab automatically
		m.activeTab = tabResponse

	case httpErrMsg:
		m.loading = false
		m.respStatus = 0
		m.respErr = msg.err.Error()
		m.respView.SetContent("Error:\n" + msg.err.Error())
		m.respView.GotoTop()

		// Add error entry to history
		entry := historyItem{
			method: methods[m.methodIdx],
			url:    m.urlInput.Value(),
			body:   m.bodyInput.Value(),
			status: 0,
			at:     time.Now(),
		}
		cmds = append(cmds, m.histList.InsertItem(0, entry))

		m.activeTab = tabResponse

	// --- Key presses ---
	case tea.KeyPressMsg:
		// Ctrl+C always quits, regardless of which tab is active
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		switch m.activeTab {

		// ── Request tab key handling ──────────────────────────────────────
		case tabRequest:
			switch msg.String() {

			// Tab cycles focus: URL → Body → (next app tab)
			case "tab":
				if m.reqFocus == focusURL {
					// Move focus to the body textarea
					m.reqFocus = focusBody
					m.urlInput.Blur()
					cmd := m.bodyInput.Focus()
					cmds = append(cmds, cmd)
				} else {
					// Move focus back to URL and advance to the next app tab
					m.reqFocus = focusURL
					m.bodyInput.Blur()
					cmd := m.urlInput.Focus()
					cmds = append(cmds, cmd)
					m.activeTab = tabResponse
				}

			case "shift+tab":
				m.activeTab = tabHistory // go backwards

			// Esc always returns focus to the URL input
			case "esc":
				m.reqFocus = focusURL
				m.bodyInput.Blur()
				cmd := m.urlInput.Focus()
				cmds = append(cmds, cmd)

			// Left/Right change the HTTP method when URL input is focused
			case "left":
				if m.reqFocus == focusURL {
					m.methodIdx = (m.methodIdx - 1 + len(methods)) % len(methods)
				}
			case "right":
				if m.reqFocus == focusURL {
					m.methodIdx = (m.methodIdx + 1) % len(methods)
				}

			// Enter sends the request (only when URL has focus, not the body)
			case "enter":
				if m.reqFocus == focusURL {
					url := strings.TrimSpace(m.urlInput.Value())
					if url == "" {
						break
					}
					m.loading = true
					cmds = append(cmds, doRequest(
						methods[m.methodIdx],
						url,
						m.bodyInput.Value(),
					))
				}
				// If body is focused, Enter inserts a newline (textarea handles it below)
			}

		// ── Response tab key handling ─────────────────────────────────────
		case tabResponse:
			switch msg.String() {
			case "tab":
				m.activeTab = tabHistory
			case "shift+tab":
				m.activeTab = tabRequest
			}

		// ── History tab key handling ──────────────────────────────────────
		case tabHistory:
			switch msg.String() {
			case "tab":
				m.activeTab = tabRequest
			case "shift+tab":
				m.activeTab = tabResponse

			// Enter on a history item loads it back into the Request tab
			case "enter":
				if item, ok := m.histList.SelectedItem().(historyItem); ok {
					m.urlInput.SetValue(item.url)
					m.bodyInput.SetValue(item.body)
					// Find the method index
					for i, meth := range methods {
						if meth == item.method {
							m.methodIdx = i
							break
						}
					}
					m.activeTab = tabRequest
					m.reqFocus = focusURL
					m.bodyInput.Blur()
					cmd := m.urlInput.Focus()
					cmds = append(cmds, cmd)
				}
			}
		}
	}

	// ── Delegate remaining messages to the focused sub-component ─────────────
	//
	// After handling our own keys above, pass the message along to whichever
	// component currently has focus so it can do its own processing (typing,
	// cursor movement, scroll, etc.).
	switch m.activeTab {
	case tabRequest:
		if m.reqFocus == focusURL {
			var cmd tea.Cmd
			m.urlInput, cmd = m.urlInput.Update(msg)
			cmds = append(cmds, cmd)
		} else {
			var cmd tea.Cmd
			m.bodyInput, cmd = m.bodyInput.Update(msg)
			cmds = append(cmds, cmd)
		}
	case tabResponse:
		var cmd tea.Cmd
		m.respView, cmd = m.respView.Update(msg)
		cmds = append(cmds, cmd)
	case tabHistory:
		var cmd tea.Cmd
		m.histList, cmd = m.histList.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// ── View ──────────────────────────────────────────────────────────────────────
//
// View is a pure function: given the model, return what to display.
// It's called after every Update. Keep it fast — no I/O here.

func (m model) View() tea.View {
	var sb strings.Builder

	// --- Tab bar ---
	sb.WriteString(m.renderTabBar())
	sb.WriteString("\n\n")

	// --- Active tab content ---
	switch m.activeTab {
	case tabRequest:
		sb.WriteString(m.renderRequestTab())
	case tabResponse:
		sb.WriteString(m.renderResponseTab())
	case tabHistory:
		sb.WriteString(m.histList.View())
	}

	// --- Help bar at the bottom ---
	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render(m.helpText()))

	v := tea.NewView(sb.String())
	v.AltScreen = true
	return v
}

// ── View helpers ──────────────────────────────────────────────────────────────

func (m model) renderTabBar() string {
	tabs := []string{"Request", "Response", "History"}
	var parts []string
	for i, name := range tabs {
		if tab(i) == m.activeTab {
			parts = append(parts, activeTabStyle.Render(name))
		} else {
			parts = append(parts, inactiveTabStyle.Render(name))
		}
	}
	return strings.Join(parts, "  ")
}

func (m model) renderRequestTab() string {
	var sb strings.Builder

	// Method selector: highlight the current method
	sb.WriteString("Method:  ")
	for i, meth := range methods {
		if i == m.methodIdx {
			sb.WriteString(activeMethodStyle.Render(meth))
		} else {
			sb.WriteString(inactiveMethodStyle.Render(meth))
		}
		sb.WriteString("  ")
	}
	sb.WriteString("\n\n")

	// URL input
	urlLabel := labelStyle.Render("URL:  ")
	sb.WriteString(urlLabel + m.urlInput.View())
	sb.WriteString("\n\n")

	// Body textarea
	bodyLabel := "Body: "
	if m.reqFocus == focusBody {
		bodyLabel = labelStyle.Render(bodyLabel)
	} else {
		bodyLabel = dimLabelStyle.Render(bodyLabel)
	}
	sb.WriteString(bodyLabel + "\n")
	sb.WriteString(m.bodyInput.View())

	if m.loading {
		sb.WriteString("\n\n")
		sb.WriteString(loadingStyle.Render("Sending request..."))
	}

	return sb.String()
}

func (m model) renderResponseTab() string {
	var sb strings.Builder

	if m.loading {
		return loadingStyle.Render("Waiting for response...")
	}

	if m.respStatus == 0 && m.respErr == "" {
		return dimLabelStyle.Render("No response yet. Send a request from the Request tab.")
	}

	// Status line
	statusLine := statusStyle(m.respStatus).Render(statusText(m.respStatus))
	sb.WriteString("Status: " + statusLine)
	sb.WriteString("\n\n")

	// Scrollable body
	sb.WriteString(m.respView.View())

	return sb.String()
}

func (m model) helpText() string {
	switch m.activeTab {
	case tabRequest:
		if m.reqFocus == focusBody {
			return "Esc: back to URL  •  Tab: next tab  •  Ctrl+C: quit"
		}
		return "←/→: method  •  Enter: send  •  Tab: edit body  •  Ctrl+C: quit"
	case tabResponse:
		return "↑/↓: scroll  •  Tab/Shift+Tab: switch tab  •  Ctrl+C: quit"
	case tabHistory:
		return "↑/↓: navigate  •  Enter: load  •  Tab/Shift+Tab: switch tab  •  Ctrl+C: quit"
	}
	return "Ctrl+C: quit"
}

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888888")).
				Padding(0, 1)

	activeMethodStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#000000")).
				Background(lipgloss.Color("#00D7AF")).
				Padding(0, 1)

	inactiveMethodStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888888")).
				Padding(0, 1)

	labelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#DDDDDD"))

	dimLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	loadingStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("#F0A500"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555"))
)

// statusStyle picks a colour based on the HTTP status code.
func statusStyle(code int) lipgloss.Style {
	switch {
	case code >= 200 && code < 300:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D7AF"))
	case code >= 300 && code < 400:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F0A500"))
	case code >= 400:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF4672"))
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#888888"))
	}
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
