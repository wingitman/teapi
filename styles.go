package main

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

// ── Colour palette ────────────────────────────────────────────────────────────
//
// lipgloss.Color() returns a color.Color (interface), not a constant.
// We store them as package-level vars so they can be used in style definitions.

var (
	colPurple  = lipgloss.Color("#7D56F4")
	colTeal    = lipgloss.Color("#00D7AF")
	colOrange  = lipgloss.Color("#F0A500")
	colRed     = lipgloss.Color("#FF4672")
	colWhite   = lipgloss.Color("#FAFAFA")
	colGray    = lipgloss.Color("#888888")
	colDimGray = lipgloss.Color("#444444")
	colBlack   = lipgloss.Color("#1a1a1a")
	colBorder  = lipgloss.Color("#333355")
)

// ensure color is used (avoid import cycle)
var _ color.Color = colPurple

// ── App-level styles ──────────────────────────────────────────────────────────

var (
	// titleBarStyle — the top bar showing the app name
	titleBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colWhite).
			Background(colPurple).
			Padding(0, 1)

	// helpStyle — the bottom help bar text
	helpStyle = lipgloss.NewStyle().
			Foreground(colGray)

	// helpKeyStyle — individual key names in help
	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colTeal).
			Bold(true)
)

// ── Sidebar styles ────────────────────────────────────────────────────────────

var (
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(colBorder)

	sidebarTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colPurple).
				PaddingLeft(1)

	sidebarSepStyle = lipgloss.NewStyle().
			Foreground(colDimGray).
			PaddingLeft(1)

	// A selected sidebar row
	sidebarSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colWhite).
				Background(colPurple).
				PaddingLeft(1)

	// An unselected sidebar row
	sidebarItemStyle = lipgloss.NewStyle().
				Foreground(colWhite).
				PaddingLeft(1)

	// A dimmed sidebar row (history entries, sub-items)
	sidebarDimStyle = lipgloss.NewStyle().
			Foreground(colGray).
			PaddingLeft(1)
)

// ── Builder panel styles ──────────────────────────────────────────────────────

var (
	// The active builder subtab label
	builderTabActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colBlack).
				Background(colTeal).
				Padding(0, 1)

	// An inactive builder subtab label
	builderTabInactiveStyle = lipgloss.NewStyle().
				Foreground(colGray).
				Padding(0, 1)

	// The HTTP method badge — colour changes by method
	methodBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colBlack).
				Padding(0, 1)

	// Label before URL input
	labelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colGray)

	// Focused label (e.g. body label when body is focused)
	labelFocusedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colTeal)

	// Panel border — focused panel gets a highlighted border
	panelFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colPurple)

	panelBlurredStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colBorder)

	// Loading indicator
	loadingStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(colOrange)

	// Warning/error text
	errorStyle = lipgloss.NewStyle().
			Foreground(colRed)

	// Muted / placeholder text
	dimStyle = lipgloss.NewStyle().
			Foreground(colGray)

	// Table header style (for headers/vars lists)
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colTeal).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(colBorder)

	// Selected row in a list/table
	tableSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colWhite).
				Background(colDimGray)

	// Unselected row
	tableRowStyle = lipgloss.NewStyle().
			Foreground(colWhite)

	// Disabled row (e.g. header with Enabled=false)
	tableDisabledStyle = lipgloss.NewStyle().
				Foreground(colDimGray).
				Strikethrough(true)
)

// ── Response panel styles ─────────────────────────────────────────────────────

var (
	// Status code — colour chosen dynamically by statusStyle()
	statusOKStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colTeal)

	statusRedirectStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colOrange)

	statusErrorStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colRed)

	// Test result indicators
	testPassStyle = lipgloss.NewStyle().
			Foreground(colTeal).
			Bold(true)

	testFailStyle = lipgloss.NewStyle().
			Foreground(colRed).
			Bold(true)

	latencyStyle = lipgloss.NewStyle().
			Foreground(colGray)

	// hintDividerStyle — thin line separating response body from the hint bar
	hintDividerStyle = lipgloss.NewStyle().
				Foreground(colBorder)

	// hintStyle — the hint bar text (global + context keybinds)
	hintStyle = lipgloss.NewStyle().
			Foreground(colGray)

	// hintKeyStyle — individual key names in the hint bar, slightly brighter
	hintKeyStyle = lipgloss.NewStyle().
			Foreground(colTeal).
			Bold(true)
)

// ── Method colours ────────────────────────────────────────────────────────────

// methodColor returns a background color appropriate for each HTTP method.
func methodColor(method string) color.Color {
	switch method {
	case "GET":
		return colTeal
	case "POST":
		return lipgloss.Color("#5865F2") // blue
	case "PUT":
		return lipgloss.Color("#F0A500") // orange
	case "PATCH":
		return lipgloss.Color("#9B59B6") // purple
	case "DELETE":
		return colRed
	case "HEAD":
		return lipgloss.Color("#2ECC71") // green
	case "OPTIONS":
		return lipgloss.Color("#888888") // gray
	default:
		return colGray
	}
}

// statusStyle returns a lipgloss style coloured by HTTP status code range.
func statusStyle(code int) lipgloss.Style {
	switch {
	case code >= 200 && code < 300:
		return statusOKStyle
	case code >= 300 && code < 400:
		return statusRedirectStyle
	case code >= 400:
		return statusErrorStyle
	default:
		return dimStyle
	}
}

// statusText returns a human-readable status string.
func statusText(code int) string {
	if code == 0 {
		return "error"
	}
	// We import net/http in request.go which already has StatusText
	texts := map[int]string{
		200: "200 OK", 201: "201 Created", 204: "204 No Content",
		301: "301 Moved", 302: "302 Found", 304: "304 Not Modified",
		400: "400 Bad Request", 401: "401 Unauthorized", 403: "403 Forbidden",
		404: "404 Not Found", 405: "405 Method Not Allowed",
		409: "409 Conflict", 422: "422 Unprocessable", 429: "429 Too Many Requests",
		500: "500 Internal Server Error", 502: "502 Bad Gateway",
		503: "503 Service Unavailable", 504: "504 Gateway Timeout",
	}
	if t, ok := texts[code]; ok {
		return t
	}
	return fmt.Sprintf("%d", code)
}

// ── Modal styles ──────────────────────────────────────────────────────────────

var (
	modalBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colPurple).
				Padding(1, 2)

	modalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colPurple)
)
