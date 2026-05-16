package main

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestConfigIncludesUpdateSettings(t *testing.T) {
	cfg := defaultConfig()
	out := buildConfigTOML(cfg)

	for _, want := range []string{
		`show_updates  = "U"`,
		"[updates]",
		"disable_checks = false",
		`current_commit = ""`,
		`repo_path      = ""`,
		`terminal       = ""`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("config TOML missing %q\n%s", want, out)
		}
	}
}

func TestRecordUpdateMetadataPreservesPreferences(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := defaultConfig()
	cfg.Updates.DisableChecks = true
	cfg.Updates.Terminal = "alacritty"
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	if err := RecordUpdateMetadata("abc123", "/tmp/teapi-src"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Updates.DisableChecks || got.Updates.Terminal != "alacritty" {
		t.Fatalf("preferences changed: %+v", got.Updates)
	}
	if got.Updates.CurrentCommit != "abc123" || got.Updates.RepoPath != "/tmp/teapi-src" {
		t.Fatalf("metadata not recorded: %+v", got.Updates)
	}
}

func TestUnixUpdateScriptRecordsMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix script test")
	}
	path, err := writeUnixUpdateScript(UpdateInstallRequest{
		RepoPath:       "/tmp/teapi-src",
		TargetCommit:   "abc123",
		RecorderBinary: "/usr/bin/teapi",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"make install", "--record-update", "--update-commit", "teapi update complete"} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q\n%s", want, s)
		}
	}
}
