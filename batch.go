package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// ── Batch runner ──────────────────────────────────────────────────────────────
//
// Batch allows importing a CSV or TXT file and running the same request
// template against each row/line.
//
// URL/body templates use placeholders:
//   CSV: {col0}, {col1}, ... or {columnName} (if first row is headers)
//   TXT: {line}  — the full line content

// runBatchCmd executes a batch run and returns a tea.Cmd.
func runBatchCmd(batch Batch, globalVars []Variable) tea.Cmd {
	return func() tea.Msg {
		rows, err := loadBatchSource(batch)
		if err != nil {
			return httpErrMsg{err: err}
		}

		results := make([]BatchResult, 0, len(rows))
		var mu sync.Mutex
		var wg sync.WaitGroup

		concurrency := batch.Concurrency
		if concurrency <= 0 {
			concurrency = 1
		}

		// semaphore channel to limit concurrency
		sem := make(chan struct{}, concurrency)

		for i, row := range rows {
			wg.Add(1)
			sem <- struct{}{} // acquire
			go func(rowIdx int, r map[string]string) {
				defer wg.Done()
				defer func() { <-sem }() // release

				result := executeBatchRow(batch, r, rowIdx, globalVars)

				mu.Lock()
				results = append(results, result)
				mu.Unlock()

				if batch.StopOnError && result.Err != nil {
					// Signal stop (simplistic — we don't actually cancel other goroutines here)
					return
				}
			}(i, row)
		}

		wg.Wait()
		return batchDoneMsg{results: results}
	}
}

// loadBatchSource reads the source file and returns a slice of row maps.
// Each map has string keys (column names or "col0", "col1", ... for CSV;
// "line" for TXT).
func loadBatchSource(batch Batch) ([]map[string]string, error) {
	f, err := os.Open(batch.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", batch.SourcePath, err)
	}
	defer f.Close()

	switch batch.SourceType {
	case "csv":
		return loadCSV(f, batch.Delimiter)
	default: // "txt"
		return loadTXT(f)
	}
}

// loadCSV reads a CSV file. If the first row looks like headers (non-numeric),
// those are used as column names; otherwise col0, col1, ... are used.
func loadCSV(r io.Reader, delimiter string) ([]map[string]string, error) {
	reader := csv.NewReader(r)
	if delimiter != "" {
		reader.Comma = rune(delimiter[0])
	}

	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}

	// Check if first row is headers (all values look like identifiers, not numbers)
	headers := records[0]
	isHeaderRow := true
	for _, h := range headers {
		// If it looks like a number, treat first row as data
		trimmed := strings.TrimSpace(h)
		if trimmed != "" {
			// Simple heuristic: if it starts with a digit, not a header
			if trimmed[0] >= '0' && trimmed[0] <= '9' {
				isHeaderRow = false
				break
			}
		}
	}

	startRow := 0
	colNames := make([]string, len(headers))
	if isHeaderRow {
		startRow = 1
		copy(colNames, headers)
	} else {
		for i := range colNames {
			colNames[i] = fmt.Sprintf("col%d", i)
		}
	}

	rows := make([]map[string]string, 0, len(records)-startRow)
	for _, record := range records[startRow:] {
		row := make(map[string]string)
		for i, val := range record {
			if i < len(colNames) {
				row[colNames[i]] = val
			}
			row[fmt.Sprintf("col%d", i)] = val // always available by index too
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// loadTXT reads a plain text file — one URL/value per line.
func loadTXT(r io.Reader) ([]map[string]string, error) {
	var rows []map[string]string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // skip empty lines and comments
		}
		rows = append(rows, map[string]string{"line": line})
	}
	return rows, scanner.Err()
}

// executeBatchRow runs one row of a batch and returns its result.
func executeBatchRow(batch Batch, row map[string]string, rowIdx int, globalVars []Variable) BatchResult {
	start := time.Now()

	// Replace {placeholder} patterns with values from the row
	url := substituteRow(batch.URLTemplate, row)
	body := substituteRow(batch.BodyTemplate, row)

	// Also apply global vars
	url = InterpolateVars(url, nil, nil, globalVars)
	body = InterpolateVars(body, nil, nil, globalVars)

	req := Request{
		Method: batch.Method,
		URL:    url,
		Body:   body,
	}

	// Build and execute the HTTP request directly (no tea.Cmd here)
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	httpReq, err := NewHTTPRequest(req.Method, url, bodyReader)
	if err != nil {
		return BatchResult{Row: rowIdx, URL: url, Err: err}
	}

	if body != "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	client := &httpClient{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return BatchResult{Row: rowIdx, URL: url, Err: err, LatencyMs: time.Since(start).Milliseconds()}
	}
	resp.Body.Close()

	return BatchResult{
		Row:       rowIdx,
		URL:       url,
		Status:    resp.StatusCode,
		LatencyMs: time.Since(start).Milliseconds(),
	}
}

// substituteRow replaces {key} placeholders in template with values from row.
func substituteRow(template string, row map[string]string) string {
	result := template
	for k, v := range row {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}

// ── HTTP client type aliases to avoid import collision ────────────────────────

type httpClient = http.Client

// NewHTTPRequest is a thin alias to avoid naming collision with net/http
func NewHTTPRequest(method, url string, body io.Reader) (*http.Request, error) {
	return http.NewRequest(method, url, body)
}

// ── Batch screen model ────────────────────────────────────────────────────────

type BatchScreen struct {
	batches     []Batch
	list        list.Model
	results     []BatchResult
	running     bool
	showResults bool

	// New batch form fields
	editing      bool
	pathInput    textinput.Model
	urlInput     textinput.Model
	methodIdx    int
	focusedField int // 0 = path, 1 = url

	width  int
	height int
}

// batchItem implements list.Item for displaying a batch config.
type batchItem struct{ b Batch }

func (bi batchItem) Title() string { return bi.b.Name }
func (bi batchItem) Description() string {
	return fmt.Sprintf("%s • %s", bi.b.SourceType, bi.b.SourcePath)
}
func (bi batchItem) FilterValue() string { return bi.b.Name }

// NewBatchScreen creates a new batch screen.
func NewBatchScreen(batches []Batch, width, height int) BatchScreen {
	items := make([]list.Item, len(batches))
	for i, b := range batches {
		items[i] = batchItem{b}
	}
	l := list.New(items, list.NewDefaultDelegate(), width-4, height/2)
	l.Title = "Batch Runs"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()

	pathInput := textinput.New()
	pathInput.Placeholder = "/path/to/file.csv or /path/to/urls.txt"
	pathInput.SetWidth(width - 10)
	pathInput.Focus()

	urlInput := textinput.New()
	urlInput.Placeholder = "https://api.example.com/{line}"
	urlInput.SetWidth(width - 10)

	return BatchScreen{
		batches:   batches,
		list:      l,
		pathInput: pathInput,
		urlInput:  urlInput,
		width:     width,
		height:    height,
	}
}

// SetSize resizes the batch screen and its internal list.
func (bs *BatchScreen) SetSize(width, height int) {
	bs.width = width
	bs.height = height
	listH := height / 2
	if listH < 2 {
		listH = 2
	}
	bs.list.SetSize(width-4, listH)
	bs.pathInput.SetWidth(width - 10)
	bs.urlInput.SetWidth(width - 10)
}

// Update handles key events for the batch screen.
func (bs BatchScreen) Update(msg tea.Msg, keys KeyMap, globalVars []Variable) (BatchScreen, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key_matches(msg, keys.NewItem):
			bs.editing = !bs.editing
		case key_matches(msg, keys.SendRequest):
			if !bs.editing {
				if item, ok := bs.list.SelectedItem().(batchItem); ok {
					bs.running = true
					cmds = append(cmds, runBatchCmd(item.b, globalVars))
				}
			}
		}
	case batchDoneMsg:
		bs.running = false
		bs.results = msg.results
		bs.showResults = true
	}

	if bs.editing {
		switch bs.focusedField {
		case 0:
			var cmd tea.Cmd
			bs.pathInput, cmd = bs.pathInput.Update(msg)
			cmds = append(cmds, cmd)
		case 1:
			var cmd tea.Cmd
			bs.urlInput, cmd = bs.urlInput.Update(msg)
			cmds = append(cmds, cmd)
		}
	} else {
		var cmd tea.Cmd
		bs.list, cmd = bs.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return bs, tea.Batch(cmds...)
}

// View renders the batch screen.
func (bs BatchScreen) View() string {
	var sb strings.Builder

	sb.WriteString(sidebarTitleStyle.Render("Batch Runner"))
	sb.WriteString("\n\n")

	if bs.editing {
		sb.WriteString(labelStyle.Render("Source file: "))
		sb.WriteString(bs.pathInput.View())
		sb.WriteString("\n\n")
		sb.WriteString(labelStyle.Render("URL template: "))
		sb.WriteString(bs.urlInput.View())
		sb.WriteString("\n\n")
		sb.WriteString(dimStyle.Render("Use {line} for TXT, {col0}/{colName} for CSV"))
	} else {
		sb.WriteString(bs.list.View())
	}

	if bs.running {
		sb.WriteString("\n\n" + loadingStyle.Render("Running batch..."))
	}

	if bs.showResults && len(bs.results) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(tableHeaderStyle.Render(fmt.Sprintf("Results (%d rows)", len(bs.results))))
		sb.WriteString("\n")
		for _, r := range bs.results {
			status := statusStyle(r.Status).Render(fmt.Sprintf("%d", r.Status))
			if r.Err != nil {
				status = errorStyle.Render("ERR")
			}
			sb.WriteString(fmt.Sprintf("  %s  %-50s  %dms\n", status, truncate(r.URL, 50), r.LatencyMs))
		}
	}

	return sb.String()
}
