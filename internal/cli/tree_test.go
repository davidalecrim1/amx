package cli

import (
	"testing"

	"github.com/davidalecrim1/amx/internal/target"
	"github.com/davidalecrim1/amx/internal/tmux"
)

var panes = []tmux.Pane{
	{SessionName: "S", WindowIndex: "0", PaneID: "%1"},
	{SessionName: "S", WindowIndex: "1", PaneID: "%2"},
}

func TestFoldPaneCollapsesWindowThenSession(t *testing.T) {
	c := map[string]bool{}
	pane := target.Target{Kind: target.Pane, PaneID: "%2"}

	ref, changed := foldState(pane, panes, c, true)
	if !changed || ref != "w:S:1" || !c["w:S:1"] {
		t.Fatalf("first fold = %q %v %v", ref, changed, c)
	}

	ref, changed = foldState(pane, panes, c, true)
	if !changed || ref != "s:S" || !c["s:S"] {
		t.Fatalf("second fold = %q %v %v", ref, changed, c)
	}

	ref, changed = foldState(pane, panes, c, true)
	if changed {
		t.Fatalf("third fold should be a no-op, got %q", ref)
	}
}

func TestUnfoldWindow(t *testing.T) {
	c := map[string]bool{"w:S:1": true, "s:S": true}
	ref, changed := foldState(target.Target{Kind: target.Pane, PaneID: "%2"}, panes, c, false)
	if !changed || ref != "w:S:1" || c["w:S:1"] {
		t.Fatalf("unfold = %q %v %v", ref, changed, c)
	}
	if !c["s:S"] {
		t.Fatal("session should stay collapsed")
	}
}

func TestFoldSession(t *testing.T) {
	c := map[string]bool{}
	ref, changed := foldState(target.Target{Kind: target.Session, Session: "S"}, panes, c, true)
	if !changed || ref != "s:S" {
		t.Fatalf("fold = %q %v", ref, changed)
	}
	ref, changed = foldState(target.Target{Kind: target.Session, Session: "S"}, panes, c, false)
	if !changed || ref != "s:S" || c["s:S"] {
		t.Fatalf("unfold = %q %v %v", ref, changed, c)
	}
}

func TestFoldUnknownPaneIsNoop(t *testing.T) {
	c := map[string]bool{}
	_, changed := foldState(target.Target{Kind: target.Pane, PaneID: "%99"}, panes, c, true)
	if changed {
		t.Fatal("unknown pane should be a no-op")
	}
}

func TestRowIndex(t *testing.T) {
	rows := []string{"a\ts:S", "b\tw:S:1", "c\tp:%2"}
	if got := rowIndex(rows, "w:S:1"); got != 2 {
		t.Fatalf("got %d", got)
	}
	if got := rowIndex(rows, "p:%9"); got != 0 {
		t.Fatalf("got %d", got)
	}
}
