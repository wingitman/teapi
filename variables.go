package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	mathrand "math/rand"
	"regexp"
	"strings"
	"time"
)

// ── Variable interpolation ────────────────────────────────────────────────────
//
// Variables are interpolated in URL, header values, and body before sending.
//
// Syntax:
//   {varName}   — local variable (scoped to this request)
//   {{varName}} — global variable (app-wide)
//   {{$builtin}} — built-in faker function
//
// Resolution order: local → group → global → builtin
// Unresolved variables are left as-is (so the user can see what's missing).

// InterpolateVars replaces all variable placeholders in s with their values.
// localVars are scoped to the current request.
// groupVars are inherited from the request's parent group.
// globalVars are app-wide.
func InterpolateVars(s string, localVars, groupVars, globalVars []Variable) string {
	// Build a flat lookup map: key → value, with precedence local > group > global
	lookup := make(map[string]string)
	for _, v := range globalVars {
		lookup[v.Key] = resolveVarValue(v)
	}
	for _, v := range groupVars {
		lookup[v.Key] = resolveVarValue(v)
	}
	for _, v := range localVars {
		lookup[v.Key] = resolveVarValue(v)
	}

	// Replace {{$builtin}} first (double-brace with $ prefix)
	s = interpolateBuiltins(s)

	// Replace {{globalVar}} — double braces
	s = replaceVarPattern(s, `\{\{([^$][^}]*)\}\}`, lookup)

	// Replace {localVar} — single braces
	s = replaceVarPattern(s, `\{([^{}]+)\}`, lookup)

	return s
}

// resolveVarValue returns the effective value of a variable.
// For faker-type variables, a new random value is generated each call.
func resolveVarValue(v Variable) string {
	switch v.Type {
	case "faker":
		return resolveFakerType(v.Value) // Value holds the faker type name
	default:
		return v.Value
	}
}

// replaceVarPattern replaces all occurrences of the regex pattern in s,
// looking up captured group 1 in the lookup map.
func replaceVarPattern(s, pattern string, lookup map[string]string) string {
	re := regexp.MustCompile(pattern)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		sub := re.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		key := strings.TrimSpace(sub[1])
		if val, ok := lookup[key]; ok {
			return val
		}
		return match // leave unknown variables as-is
	})
}

// ── Builtin faker functions ───────────────────────────────────────────────────
//
// Available as {{$name}} in any text field.
// All implemented with pure Go stdlib + small embedded data — no external deps.

// interpolateBuiltins replaces all {{$name}} patterns with generated values.
func interpolateBuiltins(s string) string {
	re := regexp.MustCompile(`\{\{\$([a-zA-Z]+)\}\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		sub := re.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		return resolveFakerType(sub[1])
	})
}

// resolveFakerType maps a faker type name to a generated value.
func resolveFakerType(name string) string {
	switch strings.ToLower(name) {
	case "uuid":
		return generateUUID()
	case "timestamp":
		return fmt.Sprintf("%d", time.Now().Unix())
	case "isodate":
		return time.Now().UTC().Format(time.RFC3339)
	case "randomint":
		return fmt.Sprintf("%d", randInt(0, 9999))
	case "randomfloat":
		return fmt.Sprintf("%.4f", randFloat())
	case "randomname":
		return randomFirstName() + " " + randomLastName()
	case "randomfirstname":
		return randomFirstName()
	case "randomlastname":
		return randomLastName()
	case "randomemail":
		return strings.ToLower(randomFirstName()) + "." +
			strings.ToLower(randomLastName()) +
			fmt.Sprintf("%d", randInt(1, 999)) +
			"@" + randomDomain()
	case "randomphone":
		return fmt.Sprintf("+1-%03d-%03d-%04d", randInt(200, 999), randInt(200, 999), randInt(1000, 9999))
	case "randomurl":
		return "https://" + randomWord() + "." + randomDomain() + "/" + randomWord()
	case "randomipv4":
		return fmt.Sprintf("%d.%d.%d.%d", randInt(1, 254), randInt(0, 255), randInt(0, 255), randInt(1, 254))
	case "randomword":
		return randomWord()
	case "randomsentence":
		return randomSentence()
	case "randombool":
		if randInt(0, 1) == 0 {
			return "true"
		}
		return "false"
	default:
		return "{{$" + name + "}}" // unknown — leave as-is
	}
}

// ── UUID generation ───────────────────────────────────────────────────────────

// generateUUID creates a random UUID v4 using crypto/rand for good entropy.
func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:]),
	)
}

// ── Random helpers ────────────────────────────────────────────────────────────

// randInt returns a random int in [min, max] (inclusive).
func randInt(min, max int) int {
	if max <= min {
		return min
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	return min + int(n.Int64())
}

// randFloat returns a random float64 in [0, 1).
func randFloat() float64 {
	return mathrand.Float64()
}

// pick returns a random element from a slice.
func pick(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[randInt(0, len(s)-1)]
}

// ── Embedded data for faker ───────────────────────────────────────────────────

var firstNames = []string{
	"Alice", "Bob", "Charlie", "Diana", "Ethan", "Fiona", "George", "Hannah",
	"Ivan", "Julia", "Kevin", "Laura", "Michael", "Nina", "Oscar", "Paula",
	"Quinn", "Rachel", "Samuel", "Tina", "Uma", "Victor", "Wendy", "Xander",
	"Yvonne", "Zach", "Aria", "Blake", "Chloe", "Dylan", "Emma", "Felix",
	"Grace", "Henry", "Isla", "James", "Kai", "Lily", "Mason", "Nova",
}

var lastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis",
	"Wilson", "Taylor", "Anderson", "Thomas", "Jackson", "White", "Harris", "Martin",
	"Thompson", "Young", "Allen", "King", "Scott", "Green", "Baker", "Adams",
	"Nelson", "Hill", "Ramirez", "Campbell", "Mitchell", "Roberts", "Carter", "Phillips",
}

var domains = []string{
	"example.com", "test.io", "mock.dev", "fake.net", "sample.org",
	"demo.co", "placeholder.app", "dummy.io", "staging.net",
}

var words = []string{
	"apple", "bridge", "cloud", "delta", "echo", "forge", "glass", "harbor",
	"iris", "jungle", "kite", "lemon", "mango", "nova", "ocean", "pearl",
	"quest", "river", "stone", "tiger", "ultra", "vapor", "waves", "xenon",
	"yacht", "zephyr", "amber", "bison", "cedar", "drift", "ember", "flare",
}

var sentenceTemplates = []string{
	"The %s is a %s %s.",
	"A %s %s walked across the %s.",
	"Every %s has a %s made of %s.",
	"The quick %s jumped over the %s.",
	"She found a %s near the %s.",
}

func randomFirstName() string { return pick(firstNames) }
func randomLastName() string  { return pick(lastNames) }
func randomDomain() string    { return pick(domains) }
func randomWord() string      { return pick(words) }

func randomSentence() string {
	tmpl := pick(sentenceTemplates)
	count := strings.Count(tmpl, "%s")
	args := make([]interface{}, count)
	for i := range args {
		args[i] = randomWord()
	}
	return fmt.Sprintf(tmpl, args...)
}
