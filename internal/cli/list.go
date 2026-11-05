package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/davidalecrim1/amx/internal/state"
	"github.com/davidalecrim1/amx/internal/target"
	"github.com/davidalecrim1/amx/internal/tmux"
)

func (a *App) list(args []string) int {
	ansi, asJSON := false, false
	for _, arg := range args {
		switch arg {
		case "--ansi":
			ansi = true
		case "--json":
			asJSON = true
		}
	}
	s, err := a.load(true)
	if err != nil {
		return a.fail("%v", err)
	}
	if asJSON {
		return a.writeJSON(s.Records)
	}
	rows := a.renderRows(s, ansi)
	for _, row := range rows {
		if ansi {
			fmt.Fprintln(a.Stdout, row)
			continue
		}
		if i := strings.LastIndex(row, "\t"); i >= 0 {
			row = row[:i]
		}
		fmt.Fprintln(a.Stdout, row)
	}
	return 0
}

func (a *App) preview(args []string) int {
	if len(args) < 1 {
		return a.fail("preview needs a target")
	}
	t, err := target.Parse(args[0])
	if err != nil {
		return a.fail("%v", err)
	}
	s, err := a.load(false)
	if err != nil {
		return a.fail("%v", err)
	}
	pane, ok := resolvePane(s, t)
	if !ok {
		fmt.Fprintln(a.Stdout, "pane is gone")
		return 0
	}
	out, err := tmux.Capture(pane.PaneID, 60)
	if err != nil {
		fmt.Fprintf(a.Stdout, "cannot capture %s: %v\n", pane.PaneID, err)
		return 0
	}
	width := 80
	if v := os.Getenv("FZF_PREVIEW_COLUMNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 1 {
			width = n
		}
	}
	out = tmux.SanitizePreview(out, width-1)
	if strings.TrimSpace(out) == "" {
		fmt.Fprintf(a.Stdout, "%s  %s\n\n(no output yet)", pane.SessionName, pane.PanePath)
		return 0
	}
	fmt.Fprintln(a.Stdout, out)
	return 0
}

func resolvePane(s *snapshot, t target.Target) (tmux.Pane, bool) {
	for _, p := range s.Panes {
		switch t.Kind {
		case target.Pane:
			if p.PaneID == t.PaneID {
				return p, true
			}
		case target.Window:
			if p.SessionName == t.Session && p.WindowIndex == t.Window && p.PaneActive {
				return p, true
			}
		case target.Session:
			if p.SessionName == t.Session && p.WindowActive && p.PaneActive {
				return p, true
			}
		}
	}
	// Fall back to any pane in scope when no active pane matched.
	for _, p := range s.Panes {
		switch t.Kind {
		case target.Window:
			if p.SessionName == t.Session && p.WindowIndex == t.Window {
				return p, true
			}
		case target.Session:
			if p.SessionName == t.Session {
				return p, true
			}
		}
	}
	return tmux.Pane{}, false
}

func (a *App) doctorCmd(args []string) int {
	s, err := a.load(true)
	if err != nil {
		return a.fail("%v", err)
	}
	exe, _ := os.Executable()
	fmt.Fprintf(a.Stdout, "binary:  %s\n", exe)
	fmt.Fprintf(a.Stdout, "state:   %s\n", a.Store.Root)
	fmt.Fprintf(a.Stdout, "server:  %d\n", s.PID)
	fmt.Fprintf(a.Stdout, "panes:   %d\n", len(s.Panes))

	counts := map[state.State]int{}
	for _, rec := range s.Records {
		counts[rec.State]++
	}
	fmt.Fprintf(a.Stdout, "tracked: %d (needs_input %d, review %d, running %d, done %d)\n",
		len(s.Records), counts[state.NeedsInput], counts[state.Review], counts[state.Running], counts[state.Done])

	live := map[string]bool{}
	for _, p := range s.Panes {
		live[p.PaneID] = true
	}
	var orphans []string
	for id := range s.Records {
		if !live[id] {
			orphans = append(orphans, id)
		}
	}
	sort.Strings(orphans)
	fmt.Fprintf(a.Stdout, "orphans: %d", len(orphans))
	if len(orphans) > 0 {
		fmt.Fprintf(a.Stdout, " %s", strings.Join(orphans, ", "))
	}
	fmt.Fprintln(a.Stdout)

	hooks, _ := tmux.ShowHooks()
	fmt.Fprintln(a.Stdout, "tmux hooks:")
	for _, name := range []string{"after-select-pane", "after-select-window", "client-session-changed"} {
		status := "missing"
		for _, line := range strings.Split(hooks, "\n") {
			if strings.HasPrefix(line, name+"[") && strings.Contains(line, "amx") {
				status = "installed"
				break
			}
		}
		fmt.Fprintf(a.Stdout, "  %-22s %s\n", name, status)
	}

	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	if data, err := os.ReadFile(settings); err == nil {
		if strings.Contains(string(data), "amx") {
			fmt.Fprintln(a.Stdout, "claude hooks:      installed")
		} else {
			fmt.Fprintln(a.Stdout, "claude hooks:      missing (run amx setup)")
		}
	} else {
		fmt.Fprintln(a.Stdout, "claude hooks:      no settings.json")
	}

	plugin := filepath.Join(home, ".config", "opencode", "plugins", "amx.js")
	if _, err := os.Stat(plugin); err == nil {
		fmt.Fprintln(a.Stdout, "opencode plugin:   installed")
	} else {
		fmt.Fprintln(a.Stdout, "opencode plugin:   missing (run amx setup)")
	}

	if _, err := exec.LookPath("fzf"); err != nil {
		fmt.Fprintln(a.Stdout, "fzf:               missing")
	} else {
		fmt.Fprintln(a.Stdout, "fzf:               found")
	}
	return 0
}
