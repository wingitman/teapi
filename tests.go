package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ── Test assertion runner ─────────────────────────────────────────────────────
//
// After each HTTP request, we run all TestCases attached to the request.
// Each assertion checks one thing about the response and produces a TestResult.

// RunTests runs all test cases against the HTTP response and returns results.
func RunTests(cases []TestCase, status int, headers http.Header, body string) []TestResult {
	results := make([]TestResult, 0, len(cases))
	for _, tc := range cases {
		results = append(results, runAssertion(tc, status, headers, body))
	}
	return results
}

// runAssertion runs a single TestCase and returns its result.
func runAssertion(tc TestCase, status int, headers http.Header, body string) TestResult {
	switch tc.Type {

	case AssertStatusEquals:
		var expected int
		if _, err := fmt.Sscanf(tc.Expected, "%d", &expected); err != nil {
			return TestResult{Case: tc, Error: "expected value must be a number: " + tc.Expected}
		}
		passed := status == expected
		actual := fmt.Sprintf("%d", status)
		return TestResult{Case: tc, Passed: passed, Actual: actual}

	case AssertBodyContains:
		passed := strings.Contains(body, tc.Expected)
		actual := truncate(body, 80)
		return TestResult{Case: tc, Passed: passed, Actual: actual}

	case AssertBodyEquals:
		passed := body == tc.Expected
		actual := truncate(body, 80)
		return TestResult{Case: tc, Passed: passed, Actual: actual}

	case AssertHeaderEquals:
		// tc.JSONPath is overloaded here to hold the header key name
		headerVal := headers.Get(tc.JSONPath)
		passed := strings.EqualFold(headerVal, tc.Expected)
		return TestResult{Case: tc, Passed: passed, Actual: headerVal}

	case AssertJSONPathEquals:
		actual, err := evalJSONPath(body, tc.JSONPath)
		if err != nil {
			return TestResult{Case: tc, Error: err.Error()}
		}
		passed := actual == tc.Expected
		return TestResult{Case: tc, Passed: passed, Actual: actual}

	default:
		return TestResult{Case: tc, Error: "unknown assertion type: " + tc.Type}
	}
}

// ── JSONPath evaluator ────────────────────────────────────────────────────────
//
// A minimal JSONPath evaluator that supports simple dot-notation paths like:
//   $.user.name
//   $.items[0].id
//   $.count
//
// We only support: root ($), dot-access (.field), and array index ([n]).
// For more complex paths, the user can use BodyContains instead.

func evalJSONPath(body, path string) (string, error) {
	// Unmarshal into a generic map/slice structure
	var root interface{}
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return "", fmt.Errorf("response is not valid JSON: %w", err)
	}

	// Strip leading $. or $
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")

	if path == "" {
		// Return the whole body
		return body, nil
	}

	// Walk the path segment by segment
	current := root
	segments := splitJSONPath(path)
	for _, seg := range segments {
		current = navigateJSON(current, seg)
		if current == nil {
			return "", fmt.Errorf("path not found: %s", path)
		}
	}

	// Convert the result to a string
	return jsonValueToString(current), nil
}

// splitJSONPath splits a dotted path like "user.address[0].street" into
// ["user", "address", "[0]", "street"].
func splitJSONPath(path string) []string {
	var segments []string
	var current strings.Builder

	for i := 0; i < len(path); i++ {
		ch := path[i]
		switch ch {
		case '.':
			if current.Len() > 0 {
				segments = append(segments, current.String())
				current.Reset()
			}
		case '[':
			if current.Len() > 0 {
				segments = append(segments, current.String())
				current.Reset()
			}
			// Read until ]
			j := i
			for j < len(path) && path[j] != ']' {
				j++
			}
			segments = append(segments, path[i:j+1]) // e.g. "[0]"
			i = j
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	return segments
}

// navigateJSON accesses one segment of a JSON structure.
func navigateJSON(current interface{}, seg string) interface{} {
	if current == nil {
		return nil
	}

	// Array index like [0]
	if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
		inner := seg[1 : len(seg)-1]
		var idx int
		if _, err := fmt.Sscanf(inner, "%d", &idx); err != nil {
			return nil
		}
		arr, ok := current.([]interface{})
		if !ok || idx >= len(arr) || idx < 0 {
			return nil
		}
		return arr[idx]
	}

	// Object field
	m, ok := current.(map[string]interface{})
	if !ok {
		return nil
	}
	return m[seg]
}

// jsonValueToString converts any JSON value to a string for comparison.
func jsonValueToString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		// If it's a whole number, don't show decimals
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// truncate shortens a string to maxLen and appends "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// testSummary returns a short "✓ 2/3 tests" string for display.
func testSummary(results []TestResult) string {
	if len(results) == 0 {
		return ""
	}
	passed := 0
	for _, r := range results {
		if r.Passed {
			passed++
		}
	}
	if passed == len(results) {
		return testPassStyle.Render(fmt.Sprintf("✓ %d/%d tests", passed, len(results)))
	}
	return testFailStyle.Render(fmt.Sprintf("✗ %d/%d tests", passed, len(results)))
}
