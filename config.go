package main

import (
	"os"
	"path/filepath"

	"charm.land/bubbles/v2/key"
	"github.com/BurntSushi/toml"
)

// ── Config ────────────────────────────────────────────────────────────────────
//
// Config is loaded from ~/.config/delbysoft/teapi.toml on startup.
// If the file doesn't exist, defaults are used and the file is created.

// ConfigKeys holds the raw string keybindings as read from TOML.
// The user edits these strings in the TOML file to rebind keys.
type ConfigKeys struct {
	FocusSidebar  string `toml:"focus_sidebar"`
	FocusBuilder  string `toml:"focus_builder"`
	FocusResponse string `toml:"focus_response"`
	SendRequest   string `toml:"send_request"`
	NewItem       string `toml:"new_item"`
	DeleteItem    string `toml:"delete_item"`
	Workflows     string `toml:"workflows"`
	Batch         string `toml:"batch"`
	GlobalVars    string `toml:"global_vars"`
	Quit          string `toml:"quit"`
	Help          string `toml:"help"`
	TabNext       string `toml:"tab_next"`
	TabPrev       string `toml:"tab_prev"`
	OpenEditor    string `toml:"open_editor"`
	OpenResponse  string `toml:"open_response"`
	OpenConfig    string `toml:"open_config"`
}

// ConfigUI holds UI preferences.
type ConfigUI struct {
	SidebarWidth  int    `toml:"sidebar_width"`
	ResponseSplit int    `toml:"response_split"` // % of right panel for builder (rest = response)
	Theme         string `toml:"theme"`
}

// Config is the top-level config struct.
type Config struct {
	Keys ConfigKeys `toml:"keys"`
	UI   ConfigUI   `toml:"ui"`
}

// defaultConfig returns the default configuration.
func defaultConfig() Config {
	return Config{
		Keys: ConfigKeys{
			// Tab/Shift+Tab drive all section navigation — no Ctrl+ needed.
			FocusSidebar:  "",
			FocusBuilder:  "",
			FocusResponse: "",
			SendRequest:   "s",
			NewItem:       "n",
			DeleteItem:    "d",
			Workflows:     "w",
			Batch:         "b",
			GlobalVars:    "g",
			Quit:          "q",
			Help:          "?",
			TabNext:       "tab",
			TabPrev:       "shift+tab",
			OpenEditor:    "E",
			OpenResponse:  "R",
			OpenConfig:    "o",
		},
		UI: ConfigUI{
			SidebarWidth:  28,
			ResponseSplit: 60,
			Theme:         "dark",
		},
	}
}

// configPath returns the path to the TOML config file.
func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	return filepath.Join(dir, "delbysoft", "teapi.toml")
}

// LoadConfig reads the config file. If it doesn't exist, it creates it with defaults.
func LoadConfig() (Config, error) {
	cfg := defaultConfig()
	path := configPath()

	// If file doesn't exist, write defaults and return them.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err2 := SaveConfig(cfg); err2 != nil {
			return cfg, err2
		}
		return cfg, nil
	}

	// Decode the TOML file into our Config struct.
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// SaveConfig writes the config to the TOML file.
func SaveConfig(cfg Config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// ── KeyMap ────────────────────────────────────────────────────────────────────
//
// KeyMap converts the raw string config into key.Binding values.
// key.Binding is the bubbletea type for matching key presses in Update().
// key.WithHelp adds the description shown in the help bar.

type KeyMap struct {
	FocusSidebar  key.Binding
	FocusBuilder  key.Binding
	FocusResponse key.Binding
	SendRequest   key.Binding
	NewItem       key.Binding
	DeleteItem    key.Binding
	Workflows     key.Binding
	Batch         key.Binding
	GlobalVars    key.Binding
	Quit          key.Binding
	Help          key.Binding
	TabNext       key.Binding
	TabPrev       key.Binding
	OpenEditor    key.Binding
	OpenResponse  key.Binding
	OpenConfig    key.Binding
	// These are always fixed — not user-configurable.
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Enter  key.Binding
	Escape key.Binding
	Space  key.Binding
}

// bindingFor returns a key.Binding for k, with optional extra always-active keys.
// If k is empty, the binding has no keys (disabled).
func bindingFor(k, help string, extra ...string) key.Binding {
	keys := extra
	if k != "" {
		keys = append([]string{k}, extra...)
	}
	if len(keys) == 0 {
		return key.NewBinding()
	}
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(k, help))
}

// NewKeyMap builds a KeyMap from a Config.
func NewKeyMap(cfg Config) KeyMap {
	k := cfg.Keys
	return KeyMap{
		// These are now unused as defaults (empty string) but kept for user rebinding.
		FocusSidebar:  bindingFor(k.FocusSidebar, "focus sidebar"),
		FocusBuilder:  bindingFor(k.FocusBuilder, "focus builder"),
		FocusResponse: bindingFor(k.FocusResponse, "focus response"),
		SendRequest:   bindingFor(k.SendRequest, "send request"),
		NewItem:       bindingFor(k.NewItem, "new item"),
		DeleteItem:    bindingFor(k.DeleteItem, "delete"),
		Workflows:     bindingFor(k.Workflows, "workflows"),
		Batch:         bindingFor(k.Batch, "batch"),
		GlobalVars:    bindingFor(k.GlobalVars, "global vars"),
		Quit:          bindingFor(k.Quit, "quit", "ctrl+c"),
		Help:          bindingFor(k.Help, "toggle help"),
		TabNext:       bindingFor(k.TabNext, "next section"),
		TabPrev:       bindingFor(k.TabPrev, "prev section"),
		OpenEditor:    bindingFor(k.OpenEditor, "edit body"),
		OpenResponse:  bindingFor(k.OpenResponse, "view response"),
		OpenConfig:    bindingFor(k.OpenConfig, "open config"),
		// Fixed bindings — not user-configurable
		Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:   key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "left")),
		Right:  key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "right")),
		Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Escape: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Space:  key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
	}
}

// ShortHelp implements key.Map — shown when the help overlay is active.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.TabNext, k.TabPrev, k.SendRequest, k.Help, k.Quit}
}

// FullHelp implements key.Map — full two-column help.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.TabNext, k.TabPrev},
		{k.SendRequest, k.NewItem, k.DeleteItem},
		{k.Workflows, k.Batch, k.GlobalVars},
		{k.OpenEditor, k.OpenResponse, k.OpenConfig},
		{k.Help, k.Quit},
	}
}
