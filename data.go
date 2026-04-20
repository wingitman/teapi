package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ── Persistence ───────────────────────────────────────────────────────────────
//
// All user data (collections, history, global vars, workflows) is stored as a
// single JSON file at ~/.config/delbysoft/teapi.json.
//
// We load it on startup and save it after any meaningful change.

// dataPath returns the path to the JSON data file.
func dataPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	return filepath.Join(dir, "delbysoft", "teapi.json")
}

// LoadData reads AppData from disk. If the file doesn't exist, an empty
// AppData is returned (not an error — first run is valid).
func LoadData() (AppData, error) {
	var d AppData
	path := dataPath()

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// First run — return empty data with a welcome group
		d.Collections = []Group{
			{
				ID:   newID(),
				Name: "My API",
				Requests: []Request{
					{
						ID:     newID(),
						Name:   "Example GET",
						Method: "GET",
						URL:    "https://jsonplaceholder.typicode.com/todos/1",
					},
				},
			},
		}
		return d, nil
	}
	if err != nil {
		return d, err
	}

	if err := json.Unmarshal(raw, &d); err != nil {
		return d, err
	}
	return d, nil
}

// SaveData writes AppData to disk.
func SaveData(d AppData) error {
	path := dataPath()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0640)
}

// saveDataCmd returns a tea.Cmd that saves AppData to disk.
// We run saves in a goroutine so the UI doesn't block.
func saveDataCmd(d AppData) tea.Cmd {
	return func() tea.Msg {
		_ = SaveData(d) // silently ignore save errors for now
		return dataSavedMsg{}
	}
}

// ── ID generation ─────────────────────────────────────────────────────────────

// newID generates a short unique ID for new groups/requests.
// We use a simple approach: timestamp + random suffix.
// This keeps things readable without pulling in a UUID library.
func newID() string {
	return generateUUID() // reuse the UUID generator from variables.go
}

// ── Data helpers ──────────────────────────────────────────────────────────────

// findRequest searches all groups (including sub-groups) for a request by ID.
// Returns a pointer to the request and its parent group ID, or nil if not found.
func findRequest(data AppData, requestID string) (*Request, string) {
	for gi := range data.Collections {
		for ri := range data.Collections[gi].Requests {
			if data.Collections[gi].Requests[ri].ID == requestID {
				return &data.Collections[gi].Requests[ri], data.Collections[gi].ID
			}
		}
		for sgi := range data.Collections[gi].Groups {
			for ri := range data.Collections[gi].Groups[sgi].Requests {
				if data.Collections[gi].Groups[sgi].Requests[ri].ID == requestID {
					return &data.Collections[gi].Groups[sgi].Requests[ri], data.Collections[gi].Groups[sgi].ID
				}
			}
		}
	}
	return nil, ""
}

// upsertRequest saves a request back into the data, updating it in-place if it
// exists, or returning false if it was not found (caller should append instead).
func upsertRequest(data *AppData, req Request) bool {
	for gi := range data.Collections {
		for ri := range data.Collections[gi].Requests {
			if data.Collections[gi].Requests[ri].ID == req.ID {
				data.Collections[gi].Requests[ri] = req
				return true
			}
		}
		for sgi := range data.Collections[gi].Groups {
			for ri := range data.Collections[gi].Groups[sgi].Requests {
				if data.Collections[gi].Groups[sgi].Requests[ri].ID == req.ID {
					data.Collections[gi].Groups[sgi].Requests[ri] = req
					return true
				}
			}
		}
	}
	return false
}

// deleteRequest removes a request from the data by ID.
func deleteRequest(data *AppData, requestID string) {
	for gi := range data.Collections {
		reqs := data.Collections[gi].Requests
		for ri, r := range reqs {
			if r.ID == requestID {
				data.Collections[gi].Requests = append(reqs[:ri], reqs[ri+1:]...)
				return
			}
		}
		for sgi := range data.Collections[gi].Groups {
			reqs := data.Collections[gi].Groups[sgi].Requests
			for ri, r := range reqs {
				if r.ID == requestID {
					data.Collections[gi].Groups[sgi].Requests = append(reqs[:ri], reqs[ri+1:]...)
					return
				}
			}
		}
	}
}

// deleteGroup removes a top-level group (and all its requests/sub-groups) by ID.
func deleteGroup(data *AppData, groupID string) {
	for gi, g := range data.Collections {
		if g.ID == groupID {
			data.Collections = append(data.Collections[:gi], data.Collections[gi+1:]...)
			return
		}
		// Also check sub-groups
		for sgi, sg := range g.Groups {
			if sg.ID == groupID {
				data.Collections[gi].Groups = append(g.Groups[:sgi], g.Groups[sgi+1:]...)
				return
			}
		}
	}
}

// renameGroup updates the Name of a group (top-level or sub-group) by ID.
func renameGroup(data *AppData, groupID, newName string) {
	for gi := range data.Collections {
		if data.Collections[gi].ID == groupID {
			data.Collections[gi].Name = newName
			return
		}
		for sgi := range data.Collections[gi].Groups {
			if data.Collections[gi].Groups[sgi].ID == groupID {
				data.Collections[gi].Groups[sgi].Name = newName
				return
			}
		}
	}
}

// renameRequest updates the Name of a request by ID.
func renameRequest(data *AppData, requestID, newName string) {
	for gi := range data.Collections {
		for ri := range data.Collections[gi].Requests {
			if data.Collections[gi].Requests[ri].ID == requestID {
				data.Collections[gi].Requests[ri].Name = newName
				return
			}
		}
		for sgi := range data.Collections[gi].Groups {
			for ri := range data.Collections[gi].Groups[sgi].Requests {
				if data.Collections[gi].Groups[sgi].Requests[ri].ID == requestID {
					data.Collections[gi].Groups[sgi].Requests[ri].Name = newName
					return
				}
			}
		}
	}
}

// updateGroupMeta updates the Name and BaseURL of a group by ID.
func updateGroupMeta(data *AppData, groupID, name, baseURL string) {
	for gi := range data.Collections {
		if data.Collections[gi].ID == groupID {
			data.Collections[gi].Name = name
			data.Collections[gi].BaseURL = baseURL
			return
		}
		for sgi := range data.Collections[gi].Groups {
			if data.Collections[gi].Groups[sgi].ID == groupID {
				data.Collections[gi].Groups[sgi].Name = name
				data.Collections[gi].Groups[sgi].BaseURL = baseURL
				return
			}
		}
	}
}

// findGroup returns a pointer to a group by ID, or nil if not found.
func findGroup(data AppData, groupID string) *Group {
	for gi := range data.Collections {
		if data.Collections[gi].ID == groupID {
			return &data.Collections[gi]
		}
		for sgi := range data.Collections[gi].Groups {
			if data.Collections[gi].Groups[sgi].ID == groupID {
				return &data.Collections[gi].Groups[sgi]
			}
		}
	}
	return nil
}

// addHistEntry prepends an entry to the history and trims to the last 100.
func addHistEntry(data *AppData, entry HistEntry) {
	data.History = append([]HistEntry{entry}, data.History...)
	if len(data.History) > 100 {
		data.History = data.History[:100]
	}
}

// resolveBaseURL prepends the group's BaseURL to the request URL if the
// request URL is relative (doesn't start with http:// or https://).
func resolveBaseURL(data AppData, req Request) string {
	url := req.URL
	if len(url) > 0 && url[0] == '/' {
		// Find this request's group and use its BaseURL
		for _, g := range data.Collections {
			for _, r := range g.Requests {
				if r.ID == req.ID && g.BaseURL != "" {
					return g.BaseURL + url
				}
			}
			for _, sg := range g.Groups {
				for _, r := range sg.Requests {
					if r.ID == req.ID && sg.BaseURL != "" {
						return sg.BaseURL + url
					}
				}
			}
		}
	}
	return url
}

// ── Workflow data helpers ─────────────────────────────────────────────────────

// addWorkflowStep appends a step to the workflow with the given ID.
// Returns true if the workflow was found, false otherwise.
func addWorkflowStep(data *AppData, workflowID string, step WorkflowStep) bool {
	for i := range data.Workflows {
		if data.Workflows[i].ID == workflowID {
			data.Workflows[i].Steps = append(data.Workflows[i].Steps, step)
			return true
		}
	}
	return false
}

// deleteWorkflow removes the workflow with the given ID.
func deleteWorkflow(data *AppData, workflowID string) {
	for i, wf := range data.Workflows {
		if wf.ID == workflowID {
			data.Workflows = append(data.Workflows[:i], data.Workflows[i+1:]...)
			return
		}
	}
}

// deleteWorkflowStep removes the step at stepIdx from the workflow with workflowID.
func deleteWorkflowStep(data *AppData, workflowID string, stepIdx int) {
	for i := range data.Workflows {
		if data.Workflows[i].ID == workflowID {
			steps := data.Workflows[i].Steps
			if stepIdx >= 0 && stepIdx < len(steps) {
				data.Workflows[i].Steps = append(steps[:stepIdx], steps[stepIdx+1:]...)
			}
			return
		}
	}
}

// findRequestByName searches all collections (and sub-groups) for the first
// request whose Name matches (case-insensitive). Returns nil if not found.
func findRequestByName(data AppData, name string) *Request {
	nameLower := strings.ToLower(name)
	for gi := range data.Collections {
		for ri := range data.Collections[gi].Requests {
			if strings.ToLower(data.Collections[gi].Requests[ri].Name) == nameLower {
				r := data.Collections[gi].Requests[ri]
				return &r
			}
		}
		for sgi := range data.Collections[gi].Groups {
			for ri := range data.Collections[gi].Groups[sgi].Requests {
				if strings.ToLower(data.Collections[gi].Groups[sgi].Requests[ri].Name) == nameLower {
					r := data.Collections[gi].Groups[sgi].Requests[ri]
					return &r
				}
			}
		}
	}
	return nil
}
