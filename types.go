package main

import "time"

// ── HTTP Methods ──────────────────────────────────────────────────────────────

var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// ── Core data types ───────────────────────────────────────────────────────────
//
// These are the structs that get saved to ~/.config/delbysoft/teapi.json.
// JSON tags control the field names in the file.

// AppData is the root object persisted to disk.
type AppData struct {
	Collections []Group     `json:"collections"`
	GlobalVars  []Variable  `json:"global_vars"`
	History     []HistEntry `json:"history"`
	Workflows   []Workflow  `json:"workflows"`
	Batches     []Batch     `json:"batches"`
}

// Group is a named container for requests (like a Postman collection).
// Groups can be nested one level deep via the Groups field.
type Group struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	BaseURL  string     `json:"base_url,omitempty"` // prepended to relative request URLs
	Vars     []Variable `json:"vars,omitempty"`     // group-level variables
	Requests []Request  `json:"requests"`
	Groups   []Group    `json:"groups,omitempty"` // one level of sub-groups
	Expanded bool       `json:"-"` // UI state only, not persisted
}

// Request is a saved HTTP request with all its configuration.
type Request struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Method  string     `json:"method"`
	URL     string     `json:"url"`
	Headers []Header   `json:"headers"`
	Body    string     `json:"body"`
	Vars    []Variable `json:"vars"`  // local variables for this request
	Tests   []TestCase `json:"tests"` // assertions to run after each call
}

// Header is a single HTTP header that can be toggled on/off without deleting it.
type Header struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

// Variable is a key/value pair used for interpolation.
// Type controls how the value is treated:
//   - "static"  — used as-is
//   - "faker"   — a new random value is generated each time (e.g. {{$randomName}})
//   - "builtin" — a builtin function like {{$uuid}}, {{$timestamp}}
type Variable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"` // "static" | "faker" | "builtin"
}

// TestCase is a single assertion that runs after a request completes.
type TestCase struct {
	Name     string `json:"name"`
	Type     string `json:"type"`              // see assertion types below
	Expected string `json:"expected"`           // expected value (string form)
	JSONPath string `json:"json_path,omitempty"` // for jsonpath_equals type
}

// TestResult holds the outcome of running one TestCase.
type TestResult struct {
	Case   TestCase
	Passed bool
	Actual string // what we actually got (for displaying diffs)
	Error  string // error message if the assertion itself failed
}

// Assertion type constants — these are the values for TestCase.Type.
const (
	AssertStatusEquals  = "status_equals"
	AssertBodyContains  = "body_contains"
	AssertBodyEquals    = "body_equals"
	AssertHeaderEquals  = "header_equals"
	AssertJSONPathEquals = "jsonpath_equals"
)

// HistEntry is a record of a completed request, stored in history.
type HistEntry struct {
	RequestID string    `json:"request_id,omitempty"` // empty for ad-hoc requests
	Method    string    `json:"method"`
	URL       string    `json:"url"`
	Status    int       `json:"status"`
	LatencyMs int64     `json:"latency_ms"`
	At        time.Time `json:"at"`
	Body      string    `json:"body,omitempty"` // saved request body for replay
}

// ── Workflow types ────────────────────────────────────────────────────────────

// Workflow is a named sequence of request steps.
type Workflow struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Steps []WorkflowStep `json:"steps"`
}

// WorkflowStep is one request in a workflow.
// ExtractVars maps a variable name to a JSONPath expression — after the request
// runs, the extracted value is stored as a variable for subsequent steps.
type WorkflowStep struct {
	RequestID   string            `json:"request_id"`
	Mode        string            `json:"mode"`                  // "sequential" | "parallel"
	ExtractVars map[string]string `json:"extract_vars,omitempty"` // varName → JSONPath
}

// WorkflowResult holds the outcome of running a full workflow.
type WorkflowResult struct {
	WorkflowID string
	Steps      []StepResult
}

// StepResult holds the outcome of one step in a workflow run.
type StepResult struct {
	RequestID string
	Status    int
	LatencyMs int64
	Body      string
	Err       error
}

// ── Batch types ───────────────────────────────────────────────────────────────

// Batch describes an import of many URLs/values to run against a template request.
type Batch struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SourcePath  string `json:"source_path"`  // path to the CSV or TXT file
	SourceType  string `json:"source_type"`  // "csv" | "txt"
	Delimiter   string `json:"delimiter"`    // CSV delimiter, default ","
	Method      string `json:"method"`
	URLTemplate string `json:"url_template"` // URL with {col0}/{colName}/{line} placeholders
	BodyTemplate string `json:"body_template,omitempty"`
	Concurrency int    `json:"concurrency"` // how many requests to run at once
	StopOnError bool   `json:"stop_on_error"`
}

// BatchResult holds the outcome of one row in a batch run.
type BatchResult struct {
	Row       int
	URL       string
	Status    int
	LatencyMs int64
	Err       error
}

// ── Sidebar tree node ─────────────────────────────────────────────────────────
//
// The sidebar renders a flat list of SidebarNodes. Each node knows its depth
// so we can draw the right amount of indentation. We build this list fresh
// each time from AppData whenever the data changes.

type NodeKind int

const (
	NodeGroup   NodeKind = iota
	NodeRequest          // a request inside a group
	NodeHistory          // separator + history entries
	NodeHistEntry
	NodeWorkflow
	NodeBatch
)

// SidebarNode is one row in the sidebar tree.
type SidebarNode struct {
	Kind      NodeKind
	Label     string
	Depth     int  // 0 = top-level group, 1 = request inside group
	Expanded  bool // for group nodes
	GroupID   string
	RequestID string
	HistIdx   int // index into AppData.History
}

// ── Focus / panel enum ────────────────────────────────────────────────────────

// Panel identifies which major panel has keyboard focus.
type Panel int

const (
	PanelSidebar  Panel = iota
	PanelBuilder
	PanelResponse
)

// BuilderTab identifies which sub-tab of the builder panel is active.
type BuilderTab int

const (
	BuilderTabRequest   BuilderTab = iota
	BuilderTabHeaders
	BuilderTabVariables // local + global vars combined
	BuilderTabTests
	BuilderTabWorkflows
	BuilderTabBatch
	numBuilderTabs
)

// FullScreen is kept for compatibility but only FullScreenNone is used now.
// Workflows, Batch, and Global Vars are builder sub-tabs.
type FullScreen int

const (
	FullScreenNone FullScreen = iota
)

// ── Async message types ───────────────────────────────────────────────────────
//
// These are the message types that get returned from async tea.Cmd functions
// and delivered to Update() once the work is done.

// httpResultMsg carries the result of a completed HTTP request.
type httpResultMsg struct {
	status    int
	headers   map[string][]string
	body      string
	latencyMs int64
	tests     []TestResult
	// the request that was sent (for saving to history)
	method string
	url    string
	body_  string // request body (body is taken by response)
}

// httpErrMsg carries a network-level error (not an HTTP error status).
type httpErrMsg struct {
	err       error
	method    string
	url       string
	latencyMs int64
}

// dataSavedMsg is sent after a successful save to disk.
type dataSavedMsg struct{}

// workflowResultMsg carries the result of a completed workflow run.
type workflowResultMsg struct {
	result WorkflowResult
}

// batchDoneMsg carries all results from a completed batch run.
type batchDoneMsg struct {
	results []BatchResult
}

// batchProgressMsg carries a single result as it arrives during a batch run.
type batchProgressMsg struct {
	result BatchResult
}

// editorClosedMsg is sent when the user closes $EDITOR after editing the body.
type editorClosedMsg struct {
	content string // the new content from the editor
}

// clipboardCopiedMsg is sent when content has been written to the clipboard.
// label describes what was copied (e.g. "URL", "body", "response").
type clipboardCopiedMsg struct {
	label string
}

// clipboardClearMsg is sent after a short delay to clear the "Copied!" flash.
type clipboardClearMsg struct{}

// ── Collection CRUD messages ──────────────────────────────────────────────────
//
// These are sent by sidebar key handlers and consumed by the root model's Update.

// deleteGroupMsg requests deletion of a collection and all its requests.
type deleteGroupMsg struct {
	groupID string
}

// renameGroupMsg requests renaming a collection.
type renameGroupMsg struct {
	groupID string
	name    string
}

// renameRequestMsg requests renaming a saved request.
type renameRequestMsg struct {
	requestID string
	name      string
}

// editGroupMsg requests updating a collection's name and base URL.
type editGroupMsg struct {
	groupID string
	name    string
	baseURL string
}

// ── Data file messages ────────────────────────────────────────────────────────

// dataFileClosedMsg is sent after the user closes teapi.json in $EDITOR.
type dataFileClosedMsg struct{}

// ── Sidebar action messages ───────────────────────────────────────────────────
//
// Sent from the sidebar panel to the root model to trigger modals or actions.

type sidebarRenameMsg struct{ node SidebarNode }
type sidebarEditMsg struct{ node SidebarNode }
type sidebarDeleteGroupMsg struct{ node SidebarNode }
type sidebarOpenDataMsg struct{}

// ── Workflow / Batch creation messages ───────────────────────────────────────

type addWorkflowMsg struct {
	name string
}

// addWorkflowStepMsg adds a step to an existing workflow.
// requestName is looked up in collections to find the request ID.
type addWorkflowStepMsg struct {
	workflowID  string
	requestName string
	mode        string // "sequential" | "parallel"
}

// deleteWorkflowMsg removes an entire workflow.
type deleteWorkflowMsg struct {
	workflowID string
}

// deleteWorkflowStepMsg removes one step from a workflow.
type deleteWorkflowStepMsg struct {
	workflowID string
	stepIdx    int
}

type addBatchMsg struct {
	name        string
	sourcePath  string
	sourceType  string
	urlTemplate string
	method      string
}
