package setup

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeClaudePreservesExistingAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "/usr/local/bin/mempalace hook stop"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{Home: home, Out: io.Discard}
	if err := mergeClaude(path, "/bin/amux", opts); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if string(root["model"]) != `"opus"` {
		t.Fatalf("model lost: %s", root["model"])
	}
	var hooks map[string][]json.RawMessage
	json.Unmarshal(root["hooks"], &hooks)
	if len(hooks["Stop"]) != 2 {
		t.Fatalf("Stop groups = %d, want 2", len(hooks["Stop"]))
	}
	if !strings.Contains(string(hooks["Stop"][0]), "mempalace") {
		t.Fatal("existing hook was reordered or lost")
	}
	for _, event := range claudeEvents {
		want := 1
		if event == "Stop" {
			want = 2
		}
		if len(hooks[event]) != want {
			t.Fatalf("%s groups = %d, want %d", event, len(hooks[event]), want)
		}
	}

	if err := mergeClaude(path, "/bin/amux", opts); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(path)
	var root2 map[string]json.RawMessage
	json.Unmarshal(data2, &root2)
	var hooks2 map[string][]json.RawMessage
	json.Unmarshal(root2["hooks"], &hooks2)
	if len(hooks2["Stop"]) != 2 {
		t.Fatalf("second run duplicated hooks: %d", len(hooks2["Stop"]))
	}

	backups, _ := filepath.Glob(path + ".amux-*.bak")
	if len(backups) != 1 {
		t.Fatalf("expected one backup, got %v", backups)
	}
}

func TestMergeClaudeCreatesMissingFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := mergeClaude(path, "/bin/amux", Options{Home: home, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/bin/amux hook claude Stop") {
		t.Fatalf("missing hook: %s", data)
	}
}

func TestSetupDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	if err := Run(Options{Home: home, Out: io.Discard, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "amux", "tmux.conf")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote the tmux snippet")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote claude settings")
	}
}

func TestEnsureSourceUsesTildeAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	main := filepath.Join(home, ".config", "tmux", "tmux.conf")
	if err := os.MkdirAll(filepath.Dir(main), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte("set -g mouse on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snippet := filepath.Join(home, ".config", "amux", "tmux.conf")
	opts := Options{Home: home, Out: io.Discard}
	if err := ensureSource(main, snippet, opts); err != nil {
		t.Fatal(err)
	}
	if err := ensureSource(main, snippet, opts); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(main)
	if got := strings.Count(string(data), "source-file ~/.config/amux/tmux.conf"); got != 1 {
		t.Fatalf("source line count = %d\n%s", got, data)
	}
	if !strings.Contains(string(data), "set -g mouse on") {
		t.Fatal("existing config was clobbered")
	}
}

func TestSnippetContent(t *testing.T) {
	s := snippetContent("/bin/amux", true)
	if !strings.Contains(s, "'/bin/amux pick'") {
		t.Fatalf("bind is malformed:\n%s", s)
	}
	if !strings.Contains(s, "-s 'bg=terminal'") {
		t.Fatalf("popup should use the terminal background:\n%s", s)
	}
	if !strings.Contains(s, "-T ' Agent Multiplexer '") {
		t.Fatalf("popup title missing:\n%s", s)
	}
	if !strings.Contains(s, "-S 'fg=brightblack'") {
		t.Fatalf("popup border style missing:\n%s", s)
	}
	old := snippetContent("/bin/amux", false)
	if strings.Contains(old, "bg=terminal") {
		t.Fatalf("old tmux should not get the terminal colour flag:\n%s", old)
	}
	if !strings.Contains(old, "set-option -gu popup-style") {
		t.Fatalf("old tmux should clear the theme popup style:\n%s", old)
	}
	if strings.Contains(s, "''") {
		t.Fatalf("double quoting in snippet:\n%s", s)
	}
	if !strings.Contains(s, `focus #{pane_id}`) {
		t.Fatal("missing pane focus hook")
	}
	if !strings.Contains(s, `focus #{session_name}:#{window_index}`) {
		t.Fatal("missing window focus hook")
	}
}

func TestPluginTemplateTargetsV2(t *testing.T) {
	if !strings.Contains(pluginTemplate, "Plugin.define") {
		t.Fatal("plugin must use the OpenCode v2 Plugin.define API")
	}
	if !strings.Contains(pluginTemplate, `ctx.event.subscribe`) {
		t.Fatal("plugin must subscribe to the v2 event stream")
	}
	for _, ev := range []string{"session.execution.started", "session.execution.succeeded", "session.execution.failed", "session.idle", "session.status", "permission.asked", "permission.replied", "session.error"} {
		if !strings.Contains(pluginTemplate, ev) {
			t.Fatalf("plugin missing %s", ev)
		}
	}
}

func TestPluginIsNotClobbered(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config", "opencode", "plugins", "amux.js")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("// hand written\nexport const X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePlugin(path, "/bin/amux", Options{Home: home, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "hand written") {
		t.Fatal("hand-written plugin was overwritten")
	}
}
