package render

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/davidalecrim1/amux/internal/state"
	"github.com/davidalecrim1/amux/internal/tmux"
)

func pane(session, win, paneIdx, id, cmd string, active, winActive bool) tmux.Pane {
	return tmux.Pane{
		SessionName:  session,
		SessionPath:  "/Users/me/Code/" + strings.ToLower(session),
		WindowIndex:  win,
		WindowName:   "w" + win,
		PaneID:       id,
		PaneIndex:    paneIdx,
		PaneCommand:  cmd,
		PanePath:     "/Users/me/Code/" + strings.ToLower(session) + "/sub",
		PaneActive:   active,
		WindowActive: winActive,
	}
}

func TestTreeOrderingAndGlyphs(t *testing.T) {
	now := time.Date(2026, 10, 26, 16, 0, 0, 0, time.UTC)
	panes := []tmux.Pane{
		pane("PROJ-123", "0", "0", "%1", "node", true, true),
		pane("PROJ-123", "0", "1", "%2", "node", false, true),
		pane("PROJ-456", "0", "0", "%3", "zsh", true, true),
	}
	records := map[string]state.Record{
		"%1": {PaneID: "%1", Agent: "claude", State: state.Running, UpdatedAt: now.Add(-10 * time.Second), LastEvent: now.Add(-10 * time.Second)},
		"%2": {PaneID: "%2", Agent: "claude", State: state.NeedsInput, UpdatedAt: now.Add(-2 * time.Minute), LastEvent: now.Add(-2 * time.Minute)},
	}

	rows := Rows(panes, records, Options{Now: now, Branches: map[string]string{"%1": "feature/retry"}, ANSI: false})
	if len(rows) != 7 {
		t.Fatalf("got %d rows: %v", len(rows), rows)
	}
	if !strings.Contains(rows[0], "PROJ-123") || !strings.Contains(rows[0], "NEEDS") {
		t.Fatalf("first row = %q", rows[0])
	}
	if !strings.HasSuffix(rows[0], "\ts:PROJ-123") {
		t.Fatalf("missing session ref: %q", rows[0])
	}

	// Needs-input pane must sort before the running pane inside the window.
	var order []string
	for _, r := range rows {
		if strings.HasSuffix(r, "\tp:%1") || strings.HasSuffix(r, "\tp:%2") {
			order = append(order, r[strings.LastIndex(r, "%"):])
		}
	}
	if len(order) != 2 || !strings.HasPrefix(order[0], "%2") {
		t.Fatalf("pane order = %v", order)
	}

	// Session without agents sorts last and its pane carries no state badge.
	last := rows[len(rows)-1]
	if !strings.Contains(last, "sleep") && !strings.Contains(last, "zsh") {
		t.Fatalf("last row = %q", last)
	}
	for _, label := range []string{"NEEDS", "RUNNING", "DONE", "IDLE"} {
		if strings.Contains(last, label) {
			t.Fatalf("non-agent pane should have no %s badge: %q", label, last)
		}
	}

	var needsRow string
	for _, r := range rows {
		if strings.HasSuffix(r, "\tp:%2") {
			needsRow = r
		}
	}
	if !strings.Contains(needsRow, "2m") {
		t.Fatalf("age missing: %q", needsRow)
	}
}

func TestSingleAgentSessionHasNoAggregate(t *testing.T) {
	now := time.Now()
	panes := []tmux.Pane{
		pane("S", "0", "0", "%1", "opencode", true, true),
		pane("S", "0", "1", "%2", "zsh", false, true),
	}
	records := map[string]state.Record{
		"%1": {PaneID: "%1", Agent: "opencode", State: state.Running, UpdatedAt: now, LastEvent: now},
	}
	rows := Rows(panes, records, Options{Now: now, ANSI: false})

	for _, r := range rows {
		if strings.HasSuffix(r, "\ts:S") && strings.Contains(r, "RUNNING") {
			t.Fatalf("session should not aggregate a single agent: %q", r)
		}
		if strings.HasSuffix(r, "\tw:S:0") && strings.Contains(r, "RUNNING") {
			t.Fatalf("expanded window should not show a badge: %q", r)
		}
	}
	var agentRow, shellRow string
	for _, r := range rows {
		if strings.HasSuffix(r, "\tp:%1") {
			agentRow = r
		}
		if strings.HasSuffix(r, "\tp:%2") {
			shellRow = r
		}
	}
	if !strings.Contains(agentRow, "RUNNING") {
		t.Fatalf("agent pane should carry the badge: %q", agentRow)
	}
	if strings.Contains(shellRow, "RUNNING") || strings.Contains(shellRow, "IDLE") {
		t.Fatalf("shell pane should have no badge: %q", shellRow)
	}
}

func TestCollapsedWindowShowsAggregate(t *testing.T) {
	now := time.Now()
	panes := []tmux.Pane{pane("S", "0", "0", "%1", "opencode", true, true)}
	records := map[string]state.Record{
		"%1": {PaneID: "%1", Agent: "opencode", State: state.Running, UpdatedAt: now, LastEvent: now},
	}
	rows := Rows(panes, records, Options{Now: now, ANSI: false, Collapsed: map[string]bool{"w:S:0": true}})
	for _, r := range rows {
		if strings.HasSuffix(r, "\tw:S:0") && !strings.Contains(r, "RUNNING") {
			t.Fatalf("collapsed window should show its state: %q", r)
		}
	}
}

func TestTableWidths(t *testing.T) {
	now := time.Now()
	_ = now
	panes := []tmux.Pane{
		{SessionName: "S", WindowIndex: "10", WindowName: "w", PaneID: "%1", PaneIndex: "10", PaneCommand: "opencode", PanePath: "/tmp/a"},
		{SessionName: "S", WindowIndex: "0", WindowName: "w", PaneID: "%2", PaneIndex: "0", PaneCommand: "zsh", PanePath: "/tmp/a"},
	}
	records := map[string]state.Record{"%1": {Agent: "opencode"}}
	tw := buildTable(panes, records, map[string]string{"%1": "main"}, 80)
	if tw.ref < 5 || tw.agent < 8 {
		t.Fatalf("content widths not used: %+v", tw)
	}
	total := 8 + tw.status + 2 + tw.age + 2 + tw.ref + 2 + tw.agent + 2 + tw.branch + 2 + tw.dir
	if total > 80 {
		t.Fatalf("table overflows: total=%d %+v", total, tw)
	}

	long := buildTable(panes, records, map[string]string{"%1": strings.Repeat("feature/", 10)}, 200)
	if long.branch <= tw.branch {
		t.Fatalf("long branch did not grow: %+v vs %+v", long, tw)
	}
}

func TestLongWindowAndBranchTruncate(t *testing.T) {
	now := time.Now()
	p := tmux.Pane{
		SessionName: "S", SessionPath: "/tmp/s",
		WindowIndex: "0", WindowName: strings.Repeat("window-name-", 8),
		PaneID: "%1", PaneIndex: "0", PaneCommand: "opencode",
		PanePath: "/tmp/s/very-long-directory-name", PaneActive: true, WindowActive: true,
	}
	records := map[string]state.Record{
		"%1": {PaneID: "%1", Agent: "opencode", State: state.Running, UpdatedAt: now, LastEvent: now},
	}
	rows := Rows([]tmux.Pane{p}, records, Options{
		Now: now, ANSI: false, Width: 60,
		Branches: map[string]string{"%1": strings.Repeat("feature/branch-", 8)},
	})
	for _, r := range rows {
		visible := r
		if i := strings.LastIndex(r, "\t"); i >= 0 {
			visible = r[:i]
		}
		if strings.HasSuffix(r, "\tw:S:0") {
			if !strings.Contains(r, "…") {
				t.Fatalf("long window name was not truncated: %q", r)
			}
			if utf8.RuneCountInString(visible) > 60 {
				t.Fatalf("window row overflows: %d %q", utf8.RuneCountInString(visible), r)
			}
		}
		if strings.HasSuffix(r, "\tp:%1") {
			if !strings.Contains(r, "…") {
				t.Fatalf("long branch was not truncated: %q", r)
			}
			if utf8.RuneCountInString(visible) > 60 {
				t.Fatalf("pane row overflows: %d %q", utf8.RuneCountInString(visible), r)
			}
		}
	}
}

func TestAgeFormats(t *testing.T) {
	cases := map[time.Duration]string{
		5 * time.Second:  "5s",
		3 * time.Minute:  "3m",
		2 * time.Hour:    "2h",
		72 * time.Hour:   "3d",
		-1 * time.Second: "0s",
	}
	for d, want := range cases {
		if got := FormatAge(d); got != want {
			t.Fatalf("FormatAge(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestCollapsedSessionHidesChildren(t *testing.T) {
	now := time.Now()
	panes := []tmux.Pane{
		pane("S1", "0", "0", "%1", "node", true, true),
		pane("S1", "1", "0", "%2", "zsh", false, false),
		pane("S2", "0", "0", "%3", "zsh", true, true),
	}
	rows := Rows(panes, nil, Options{Now: now, Collapsed: map[string]bool{"s:S1": true}})

	for _, r := range rows {
		if strings.HasSuffix(r, "\tw:S1:0") || strings.HasSuffix(r, "\tw:S1:1") {
			t.Fatalf("collapsed session leaked a child: %q", r)
		}
	}
	if !strings.Contains(rows[0], "▸") {
		t.Fatalf("collapsed session marker missing: %q", rows[0])
	}
	if !strings.Contains(rows[1], "S2") {
		t.Fatalf("expanded session missing: %q", rows[1])
	}
}

func TestCollapsedWindowHidesPanes(t *testing.T) {
	now := time.Now()
	panes := []tmux.Pane{
		pane("S1", "0", "0", "%1", "node", true, true),
		pane("S1", "1", "0", "%2", "zsh", false, false),
	}
	rows := Rows(panes, nil, Options{Now: now, Collapsed: map[string]bool{"w:S1:1": true}})

	if len(rows) != 4 {
		t.Fatalf("rows = %d: %v", len(rows), rows)
	}
	for _, r := range rows {
		if strings.HasSuffix(r, "\tp:%2") {
			t.Fatalf("collapsed window leaked a pane: %q", r)
		}
	}
	var windowRow string
	for _, r := range rows {
		if strings.HasSuffix(r, "\tw:S1:1") {
			windowRow = r
		}
	}
	if windowRow == "" || !strings.Contains(windowRow, "▸") {
		t.Fatalf("collapsed window marker missing: %q", windowRow)
	}
}

func TestANSIOnlyWhenAsked(t *testing.T) {
	now := time.Now()
	panes := []tmux.Pane{pane("S", "0", "0", "%1", "node", true, true)}
	records := map[string]state.Record{
		"%1": {PaneID: "%1", Agent: "claude", State: state.Running, UpdatedAt: now, LastEvent: now},
	}
	plain := Rows(panes, records, Options{Now: now, ANSI: false})
	if strings.Contains(plain[0], "\x1b[") {
		t.Fatal("plain rows must not contain escape codes")
	}
	colored := Rows(panes, records, Options{Now: now, ANSI: true})
	if !strings.Contains(colored[0], "\x1b[") {
		t.Fatal("ansi rows should contain escape codes")
	}
}
