package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
)

// ── Workflow runner ───────────────────────────────────────────────────────────
//
// A workflow is a named list of steps, each referencing a saved Request.
// Steps can be "sequential" (one at a time, each step can use vars extracted
// from the previous) or "parallel" (all run at once, no var chaining).
//
// Running a workflow returns a workflowResultMsg with all step outcomes.

// runWorkflowCmd executes a workflow and returns a tea.Cmd.
func runWorkflowCmd(wf Workflow, data AppData) tea.Cmd {
	return func() tea.Msg {
		result := WorkflowResult{WorkflowID: wf.ID}

		// Accumulated vars from previous steps (for sequential chaining)
		chainedVars := make(map[string]string)

		// Group steps by their mode: run parallel groups together, then sequential
		i := 0
		for i < len(wf.Steps) {
			step := wf.Steps[i]

			if step.Mode == "parallel" {
				// Collect all consecutive parallel steps
				var parallelSteps []WorkflowStep
				for i < len(wf.Steps) && wf.Steps[i].Mode == "parallel" {
					parallelSteps = append(parallelSteps, wf.Steps[i])
					i++
				}
				// Run them all concurrently
				stepResults := runParallelSteps(parallelSteps, data, chainedVars)
				result.Steps = append(result.Steps, stepResults...)
			} else {
				// Sequential step
				req, _ := findRequest(data, step.RequestID)
				if req == nil {
					result.Steps = append(result.Steps, StepResult{
						RequestID: step.RequestID,
						Err:       fmt.Errorf("request not found: %s", step.RequestID),
					})
					i++
					continue
				}

				// Inject chained vars as local vars for this step
				reqCopy := *req
				for k, v := range chainedVars {
					reqCopy.Vars = append(reqCopy.Vars, Variable{Key: k, Value: v, Type: "static"})
				}

				sr := executeStep(reqCopy, data)
				result.Steps = append(result.Steps, sr)

				// Extract vars from response for subsequent steps
				if sr.Err == nil {
					for varName, jsonPath := range step.ExtractVars {
						val, err := evalJSONPath(sr.Body, jsonPath)
						if err == nil {
							chainedVars[varName] = val
						}
					}
				}
				i++
			}
		}

		return workflowResultMsg{result: result}
	}
}

// executeStep runs a single workflow step and returns its result.
// Uses the shared execHTTP helper so URL encoding is handled correctly.
func executeStep(req Request, data AppData) StepResult {
	start := time.Now()
	rawURL := resolveBaseURL(data, req)
	finalURL := InterpolateVars(rawURL, req.Vars, nil, data.GlobalVars)
	body := InterpolateVars(req.Body, req.Vars, nil, data.GlobalVars)

	result := execHTTP(req.Method, finalURL, body, req.Headers, req.Vars, nil, data.GlobalVars, start)
	switch r := result.(type) {
	case httpErrMsg:
		return StepResult{RequestID: req.ID, Err: r.err, LatencyMs: r.latencyMs}
	case httpResultMsg:
		return StepResult{
			RequestID: req.ID,
			Status:    r.status,
			LatencyMs: r.latencyMs,
			Body:      r.body,
		}
	}
	return StepResult{RequestID: req.ID, Err: fmt.Errorf("unexpected result type")}
}

// runParallelSteps runs a group of steps concurrently.
func runParallelSteps(steps []WorkflowStep, data AppData, chainedVars map[string]string) []StepResult {
	results := make([]StepResult, len(steps))
	var wg sync.WaitGroup
	for i, step := range steps {
		wg.Add(1)
		go func(idx int, s WorkflowStep) {
			defer wg.Done()
			req, _ := findRequest(data, s.RequestID)
			if req == nil {
				results[idx] = StepResult{RequestID: s.RequestID, Err: fmt.Errorf("not found")}
				return
			}
			reqCopy := *req
			for k, v := range chainedVars {
				reqCopy.Vars = append(reqCopy.Vars, Variable{Key: k, Value: v})
			}
			results[idx] = executeStep(reqCopy, data)
		}(i, step)
	}
	wg.Wait()
	return results
}

// ── Workflow screen model ─────────────────────────────────────────────────────
//
// WorkflowScreen is the full-screen workflow editor/runner shown when
// the user presses Ctrl+W.

type WorkflowScreen struct {
	workflows  []Workflow
	list       list.Model  // list of workflows
	stepList   list.Model  // list of steps in the selected workflow
	results    []StepResult
	running    bool
	focusList  bool // true = workflow list focused, false = step list
	width      int
	height     int
}

// workflowItem implements list.Item for displaying a workflow in a list.
type workflowItem struct{ wf Workflow }

func (w workflowItem) Title() string       { return w.wf.Name }
func (w workflowItem) Description() string { return fmt.Sprintf("%d steps", len(w.wf.Steps)) }
func (w workflowItem) FilterValue() string { return w.wf.Name }

// stepItem implements list.Item for displaying a workflow step.
type stepItem struct {
	step  WorkflowStep
	reqName string
	result  *StepResult
}

func (s stepItem) Title() string {
	mode := "→"
	if s.step.Mode == "parallel" {
		mode = "⇉"
	}
	return fmt.Sprintf("%s %s", mode, s.reqName)
}

func (s stepItem) Description() string {
	if s.result == nil {
		return "pending"
	}
	if s.result.Err != nil {
		return "error: " + s.result.Err.Error()
	}
	return fmt.Sprintf("%s  %dms", statusText(s.result.Status), s.result.LatencyMs)
}

func (s stepItem) FilterValue() string { return s.reqName }

// NewWorkflowScreen creates a new workflow screen.
func NewWorkflowScreen(workflows []Workflow, width, height int) WorkflowScreen {
	wfItems := make([]list.Item, len(workflows))
	for i, wf := range workflows {
		wfItems[i] = workflowItem{wf}
	}

	wfList := list.New(wfItems, list.NewDefaultDelegate(), width/2, height-4)
	wfList.Title = "Workflows"
	wfList.SetShowStatusBar(false)
	wfList.SetShowHelp(false)
	wfList.DisableQuitKeybindings()

	stepList := list.New([]list.Item{}, list.NewDefaultDelegate(), width/2, height-4)
	stepList.Title = "Steps"
	stepList.SetShowStatusBar(false)
	stepList.SetShowHelp(false)
	stepList.DisableQuitKeybindings()

	return WorkflowScreen{
		workflows: workflows,
		list:      wfList,
		stepList:  stepList,
		focusList: true,
		width:     width,
		height:    height,
	}
}

// SetSize resizes the workflow screen and its internal lists.
// Must be called whenever the parent panel changes size.
func (ws *WorkflowScreen) SetSize(width, height int) {
	ws.width = width
	ws.height = height
	halfW := width/2 - 2
	if halfW < 4 {
		halfW = 4
	}
	rightW := width - halfW - 4
	if rightW < 4 {
		rightW = 4
	}
	listH := height - 4
	if listH < 2 {
		listH = 2
	}
	ws.list.SetSize(halfW, listH)
	ws.stepList.SetSize(rightW, listH)
}

// Update handles key events for the workflow screen.
func (ws WorkflowScreen) Update(msg tea.Msg, keys KeyMap, data AppData) (WorkflowScreen, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key_matches(msg, keys.Left):
			ws.focusList = true
		case key_matches(msg, keys.Right):
			ws.focusList = false
		case key_matches(msg, keys.SendRequest):
			// Run the selected workflow
			if ws.focusList {
				if item, ok := ws.list.SelectedItem().(workflowItem); ok {
					ws.running = true
					cmds = append(cmds, runWorkflowCmd(item.wf, data))
				}
			}
		}
	case workflowResultMsg:
		ws.running = false
		ws.results = msg.result.Steps
		// Refresh step list with results
		ws.refreshStepList(data)
	}

	if ws.focusList {
		var cmd tea.Cmd
		ws.list, cmd = ws.list.Update(msg)
		cmds = append(cmds, cmd)
		ws.refreshStepList(data)
	} else {
		var cmd tea.Cmd
		ws.stepList, cmd = ws.stepList.Update(msg)
		cmds = append(cmds, cmd)
	}

	return ws, tea.Batch(cmds...)
}

// refreshStepList rebuilds the step list from the selected workflow.
func (ws *WorkflowScreen) refreshStepList(data AppData) {
	item, ok := ws.list.SelectedItem().(workflowItem)
	if !ok {
		return
	}
	items := make([]list.Item, len(item.wf.Steps))
	for i, step := range item.wf.Steps {
		req, _ := findRequest(data, step.RequestID)
		name := step.RequestID
		if req != nil {
			name = req.Name
		}
		var result *StepResult
		if i < len(ws.results) {
			r := ws.results[i]
			result = &r
		}
		items[i] = stepItem{step: step, reqName: name, result: result}
	}
	ws.stepList.SetItems(items)
}

// View renders the workflow screen.
func (ws WorkflowScreen) View() string {
	leftWidth := ws.width/2 - 2
	rightWidth := ws.width - leftWidth - 4

	leftStyle := panelBlurredStyle.Width(leftWidth).Height(ws.height - 4)
	rightStyle := panelBlurredStyle.Width(rightWidth).Height(ws.height - 4)

	if ws.focusList {
		leftStyle = panelFocusedStyle.Width(leftWidth).Height(ws.height - 4)
	} else {
		rightStyle = panelFocusedStyle.Width(rightWidth).Height(ws.height - 4)
	}

	layout := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(ws.list.View()),
		rightStyle.Render(ws.stepList.View()),
	)

	if ws.running {
		return lipgloss.JoinVertical(lipgloss.Left, layout, loadingStyle.Render("Running workflow..."))
	}
	return layout
}

// ── JSON extraction helper ────────────────────────────────────────────────────

// extractVarFromJSON extracts a value from JSON using a simple JSONPath.
// This is re-exported for use in workflow var chaining.
func extractVarFromJSON(body, jsonPath string) (string, error) {
	var v interface{}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return "", err
	}
	return evalJSONPath(body, jsonPath)
}

// key_matches is a helper to check if a key press matches a binding,
// avoiding conflict with the "key" package import name.
func key_matches(msg tea.KeyPressMsg, binding interface{ Keys() []string }) bool {
	for _, k := range binding.Keys() {
		if msg.String() == k {
			return true
		}
	}
	return false
}
