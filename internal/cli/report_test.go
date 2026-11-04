package cli

import (
	"testing"

	"github.com/davidalecrim1/amx/internal/tmux"
)

func TestPickReportPane(t *testing.T) {
	panes := []tmux.Pane{
		{PaneID: "%0", PanePath: "/work/app", PaneCommand: "opencode", PaneActive: true},
		{PaneID: "%4", PanePath: "/work/app", PaneCommand: "zsh"},
		{PaneID: "%7", PanePath: "/work/other", PaneCommand: "opencode"},
	}

	if got := pickReportPane(panes, reportInput{PaneID: "%7", Agent: "opencode"}); got != "%7" {
		t.Fatalf("live agent pane should win, got %q", got)
	}

	// A live pane that is not running the agent must not win just because the
	// service inherited its id.
	if got := pickReportPane(panes, reportInput{PaneID: "%4", Agent: "opencode", CWD: "/work/app"}); got != "%0" {
		t.Fatalf("non-agent live pane should be skipped, got %q", got)
	}

	// OpenCode's service outlives the pane, leaving a stale TMUX_PANE. The
	// live pane running opencode in the launch directory should be used.
	got := pickReportPane(panes, reportInput{PaneID: "%3", CWD: "/work/app", Agent: "opencode"})
	if got != "%0" {
		t.Fatalf("cwd resolution = %q, want %%0", got)
	}

	// Fall back to the project directory when the launch directory is not the
	// pane's directory (plugin loaded for a parent project).
	got = pickReportPane(panes, reportInput{PaneID: "%3", CWD: "/work", Project: "/work/other", Agent: "opencode"})
	if got != "%7" {
		t.Fatalf("project fallback = %q, want %%7", got)
	}

	if got := pickReportPane(panes, reportInput{PaneID: "%3", CWD: "/nope", Agent: "opencode"}); got != "" {
		t.Fatalf("unmatched report should be dropped, got %q", got)
	}
}

func TestPickReportPanePrefersActiveWhenAmbiguous(t *testing.T) {
	panes := []tmux.Pane{
		{PaneID: "%1", PanePath: "/work", PaneCommand: "opencode"},
		{PaneID: "%2", PanePath: "/work", PaneCommand: "opencode", PaneActive: true},
	}
	if got := pickReportPane(panes, reportInput{CWD: "/work", Agent: "opencode"}); got != "%2" {
		t.Fatalf("got %q, want %%2", got)
	}
}
