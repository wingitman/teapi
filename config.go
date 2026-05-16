package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"charm.land/bubbles/v2/key"
	"github.com/BurntSushi/toml"
)

// ── Config ────────────────────────────────────────────────────────────────────
//
// Config is loaded from the platform-appropriate path on startup:
//   Linux:   ~/.config/delbysoft/teapi.toml
//   macOS:   ~/Library/Application Support/delbysoft/teapi.toml
//   Windows: %AppData%\Roaming\delbysoft\teapi.toml
//
// If the file doesn't exist it is created with all defaults and comments.
// If the file exists but is missing new keys (migration), it is rewritten
// with the new keys added while preserving all existing user values.

// ConfigKeys holds every configurable key binding.
// Every key used anywhere in teapi is listed here so the user can remap
// anything by editing the TOML file.
type ConfigKeys struct {
	// ── Navigation ──────────────────────────────────────────────────────────
	Up     string `toml:"up"`
	Down   string `toml:"down"`
	Left   string `toml:"left"`
	Right  string `toml:"right"`
	Enter  string `toml:"enter"`
	Escape string `toml:"escape"`
	Space  string `toml:"space"`

	// Section cycling
	TabNext string `toml:"tab_next"`
	TabPrev string `toml:"tab_prev"`

	// ── Actions ─────────────────────────────────────────────────────────────
	SendRequest string `toml:"send_request"`
	NewItem     string `toml:"new_item"`
	DeleteItem  string `toml:"delete_item"`
	CopyItem    string `toml:"copy_item"`

	// ── Open in editor ───────────────────────────────────────────────────────
	OpenEditor   string `toml:"open_editor"`
	OpenResponse string `toml:"open_response"`
	OpenConfig   string `toml:"open_config"`
	ShowUpdates  string `toml:"show_updates"`

	// ── App-level ────────────────────────────────────────────────────────────
	Quit string `toml:"quit"`

	// Kept for config file compatibility with older versions but unused.
	FocusSidebar  string `toml:"focus_sidebar,omitempty"`
	FocusBuilder  string `toml:"focus_builder,omitempty"`
	FocusResponse string `toml:"focus_response,omitempty"`
	Workflows     string `toml:"workflows,omitempty"`
	Batch         string `toml:"batch,omitempty"`
	GlobalVars    string `toml:"global_vars,omitempty"`
	Help          string `toml:"help,omitempty"`
}

// ConfigUI holds UI preferences.
type ConfigUI struct {
	SidebarWidth  int    `toml:"sidebar_width"`
	ResponseSplit int    `toml:"response_split"` // % of right panel height for builder
	Theme         string `toml:"theme"`
}

// ConfigUpdates holds update-check and installer preferences.
type ConfigUpdates struct {
	DisableChecks bool   `toml:"disable_checks"`
	CurrentCommit string `toml:"current_commit"`
	RepoPath      string `toml:"repo_path"`
	Terminal      string `toml:"terminal"`
}

// Config is the top-level config struct.
type Config struct {
	Keys    ConfigKeys    `toml:"keys"`
	UI      ConfigUI      `toml:"ui"`
	Updates ConfigUpdates `toml:"updates"`
}

// defaultConfig returns the full set of defaults.
func defaultConfig() Config {
	return Config{
		Keys: ConfigKeys{
			Up:           "up",
			Down:         "down",
			Left:         "left",
			Right:        "right",
			Enter:        "enter",
			Escape:       "esc",
			Space:        " ",
			TabNext:      "tab",
			TabPrev:      "shift+tab",
			SendRequest:  "s",
			NewItem:      "n",
			DeleteItem:   "d",
			CopyItem:     "y",
			OpenEditor:   "E",
			OpenResponse: "R",
			OpenConfig:   "o",
			ShowUpdates:  "U",
			Quit:         "q",
		},
		UI: ConfigUI{
			SidebarWidth:  28,
			ResponseSplit: 60,
			Theme:         "dark",
		},
	}
}

// ── Paths ─────────────────────────────────────────────────────────────────────

// configPath returns the platform-appropriate path to teapi.toml.
// Uses os.UserConfigDir() which returns:
//
//	Linux:   $HOME/.config
//	macOS:   $HOME/Library/Application Support
//	Windows: %AppData%\Roaming
func configPath() string {
	return filepath.Join(configDir(), "teapi.toml")
}

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	return filepath.Join(dir, "delbysoft")
}

// dataPath is also in data.go but referenced from config.go for migration
// messages. It uses the same base directory.

// ── Load / Save ───────────────────────────────────────────────────────────────

// LoadConfig reads the config file.
// • First launch: creates the file with all defaults + comments, returns defaults.
// • Existing file: decodes user values, then migrates missing keys if needed.
func LoadConfig() (Config, error) {
	cfg := defaultConfig()
	path := configPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		// First launch — write the full commented default.
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o750); mkErr != nil {
			return cfg, mkErr
		}
		if wErr := writeConfigFile(path, cfg); wErr != nil {
			return cfg, wErr
		}
		return cfg, nil
	}

	// File exists — decode into cfg (missing fields keep their defaults).
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, err
	}

	// Migrate: if any key that ships with this version is absent from the
	// file, rewrite with all keys while preserving the user's values.
	if configNeedsMigration(path) {
		_ = writeConfigFile(path, cfg) // non-fatal
	}

	return cfg, nil
}

// SaveConfig writes the current config back to disk (used after live-reload).
func SaveConfig(cfg Config) error {
	return writeConfigFile(configPath(), cfg)
}

// RecordUpdateMetadata stores the installed commit and source repo path without
// changing user-facing preferences.
func RecordUpdateMetadata(commit, repoPath string) error {
	cfg, err := LoadConfig()
	if err != nil {
		cfg = defaultConfig()
	}
	if commit != "" {
		cfg.Updates.CurrentCommit = commit
	}
	if repoPath != "" {
		cfg.Updates.RepoPath = repoPath
	}
	return writeConfigFile(configPath(), cfg)
}

// configNeedsMigration returns true if the file is missing any key that
// ships with this version of teapi.
//
// It derives the required key names directly from the ConfigKeys struct's
// TOML tags, so it stays in sync automatically whenever a new field is added.
// Legacy fields tagged with omitempty are intentionally excluded — they are
// kept for file-compatibility but are no longer written.
func configNeedsMigration(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	s := string(data)
	for _, key := range tomlKeys(reflect.TypeOf(ConfigKeys{}), true) {
		if !strings.Contains(s, key+" =") {
			return true
		}
	}
	for _, key := range tomlKeys(reflect.TypeOf(ConfigUpdates{}), false) {
		if !strings.Contains(s, key+" =") {
			return true
		}
	}
	if !strings.Contains(s, "[updates]") {
		return true
	}
	return false
}

func tomlKeys(t reflect.Type, skipOmitEmpty bool) []string {
	keys := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("toml")
		if tag == "" || (skipOmitEmpty && strings.Contains(tag, "omitempty")) {
			continue
		}
		keys = append(keys, strings.Split(tag, ",")[0])
	}
	return keys
}

// writeConfigFile writes a fully-commented TOML file with the user's values
// baked in. This produces a much more readable file than the TOML encoder
// because it includes section headers and inline comments for every key.
func writeConfigFile(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(buildConfigTOML(cfg))
	return err
}

// buildConfigTOML produces the full commented TOML string for writing.
func buildConfigTOML(cfg Config) string {
	k := cfg.Keys
	ui := cfg.UI
	u := cfg.Updates
	q := func(s string) string { return `"` + s + `"` }
	boolStr := func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	}
	itoa := func(n int) string {
		if n == 0 {
			return "0"
		}
		s := ""
		for n > 0 {
			s = string(rune('0'+n%10)) + s
			n /= 10
		}
		return s
	}

	return "# teapi configuration file\n" +
		"# Edit any value below and press o inside teapi to reload it live.\n" +
		"# Key names: letters, \"up\", \"down\", \"left\", \"right\", \"enter\", \"esc\",\n" +
		"#             \"space\", \"tab\", \"shift+tab\", \"ctrl+x\", \"alt+x\", etc.\n" +
		"\n" +
		"[keys]\n" +
		"\n" +
		"# ── Navigation ─────────────────────────────────────────────────────────\n" +
		"up           = " + q(k.Up) + "       # move cursor / scroll up\n" +
		"down         = " + q(k.Down) + "     # move cursor / scroll down\n" +
		"left         = " + q(k.Left) + "     # move left / focus workflow list\n" +
		"right        = " + q(k.Right) + "    # move right / focus steps list\n" +
		"enter        = " + q(k.Enter) + "    # confirm / enter edit mode\n" +
		"escape       = " + q(k.Escape) + "   # cancel / exit edit mode\n" +
		"space        = " + q(k.Space) + "    # toggle (e.g. enable/disable header)\n" +
		"\n" +
		"# ── Section cycling (Tab / Shift+Tab by default) ────────────────────────\n" +
		"tab_next     = " + q(k.TabNext) + "  # go to next section\n" +
		"tab_prev     = " + q(k.TabPrev) + "  # go to previous section\n" +
		"\n" +
		"# ── Actions ─────────────────────────────────────────────────────────────\n" +
		"send_request = " + q(k.SendRequest) + "       # send HTTP request (or run workflow/batch)\n" +
		"new_item     = " + q(k.NewItem) + "       # add new item in the focused section\n" +
		"delete_item  = " + q(k.DeleteItem) + "       # delete selected item\n" +
		"copy_item    = " + q(k.CopyItem) + "       # copy focused content to clipboard\n" +
		"\n" +
		"# ── Open in editor ──────────────────────────────────────────────────────\n" +
		"open_editor   = " + q(k.OpenEditor) + "      # open request body in $EDITOR\n" +
		"open_response = " + q(k.OpenResponse) + "      # open response body in $EDITOR (read-only)\n" +
		"open_config   = " + q(k.OpenConfig) + "      # open this config file in $EDITOR\n" +
		"show_updates  = " + q(k.ShowUpdates) + "      # show update history and installers\n" +
		"\n" +
		"# ── App-level ────────────────────────────────────────────────────────────\n" +
		"quit         = " + q(k.Quit) + "       # quit teapi (Ctrl+C always works too)\n" +
		"\n" +
		"[ui]\n" +
		"sidebar_width  = " + itoa(ui.SidebarWidth) + "   # width of the sidebar in columns\n" +
		"response_split = " + itoa(ui.ResponseSplit) + "  # % of right panel height for request builder\n" +
		"theme          = " + q(ui.Theme) + "     # colour theme (currently only \"dark\" is supported)\n" +
		"\n" +
		"[updates]\n" +
		"disable_checks = " + boolStr(u.DisableChecks) + "   # true disables startup update checks\n" +
		"current_commit = " + q(u.CurrentCommit) + "   # installed app commit, maintained by teapi\n" +
		"repo_path      = " + q(u.RepoPath) + "   # source checkout used for updates\n" +
		"terminal       = " + q(u.Terminal) + "   # optional terminal command for detached updates\n"
}

// ── KeyMap ────────────────────────────────────────────────────────────────────
//
// KeyMap converts ConfigKeys into key.Binding values used in Update().

type KeyMap struct {
	// Navigation
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Enter  key.Binding
	Escape key.Binding
	Space  key.Binding

	// Section cycling
	TabNext key.Binding
	TabPrev key.Binding

	// Actions
	SendRequest  key.Binding
	NewItem      key.Binding
	DeleteItem   key.Binding
	CopyItem     key.Binding
	OpenEditor   key.Binding
	OpenResponse key.Binding
	OpenConfig   key.Binding
	ShowUpdates  key.Binding

	// App-level
	Quit key.Binding
	Help key.Binding // unused but kept for compatibility

	// Legacy — kept so old code referencing them doesn't break at compile time
	FocusSidebar  key.Binding
	FocusBuilder  key.Binding
	FocusResponse key.Binding
	Workflows     key.Binding
	Batch         key.Binding
	GlobalVars    key.Binding
}

// bindingFor returns a key.Binding for the given key string.
// If k is empty the binding is disabled (no keys).
func bindingFor(k, helpText string) key.Binding {
	if k == "" {
		return key.NewBinding()
	}
	return key.NewBinding(key.WithKeys(k), key.WithHelp(k, helpText))
}

// bindingForWithExtra returns a binding for k plus additional always-active keys.
func bindingForWithExtra(k, helpText string, extra ...string) key.Binding {
	if k == "" && len(extra) == 0 {
		return key.NewBinding()
	}
	keys := extra
	if k != "" {
		keys = append([]string{k}, extra...)
	}
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(k, helpText))
}

// NewKeyMap builds a KeyMap from a Config.
func NewKeyMap(cfg Config) KeyMap {
	k := cfg.Keys
	return KeyMap{
		Up:           bindingFor(k.Up, "up"),
		Down:         bindingFor(k.Down, "down"),
		Left:         bindingFor(k.Left, "left"),
		Right:        bindingFor(k.Right, "right"),
		Enter:        bindingFor(k.Enter, "confirm"),
		Escape:       bindingFor(k.Escape, "cancel"),
		Space:        bindingFor(k.Space, "toggle"),
		TabNext:      bindingFor(k.TabNext, "next section"),
		TabPrev:      bindingFor(k.TabPrev, "prev section"),
		SendRequest:  bindingFor(k.SendRequest, "send request"),
		NewItem:      bindingFor(k.NewItem, "new item"),
		DeleteItem:   bindingFor(k.DeleteItem, "delete"),
		CopyItem:     bindingFor(k.CopyItem, "copy to clipboard"),
		OpenEditor:   bindingFor(k.OpenEditor, "edit body"),
		OpenResponse: bindingFor(k.OpenResponse, "view response"),
		OpenConfig:   bindingFor(k.OpenConfig, "open config"),
		ShowUpdates:  bindingFor(k.ShowUpdates, "updates"),
		Quit:         bindingForWithExtra(k.Quit, "quit", "ctrl+c"),
		Help:         key.NewBinding(), // unused
	}
}

// ShortHelp implements key.Map.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.TabNext, k.TabPrev, k.SendRequest, k.ShowUpdates, k.Quit}
}

// FullHelp implements key.Map.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},
		{k.TabNext, k.TabPrev},
		{k.SendRequest, k.NewItem, k.DeleteItem},
		{k.OpenEditor, k.OpenResponse, k.OpenConfig, k.ShowUpdates},
		{k.Quit},
	}
}
