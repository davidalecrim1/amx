package cli

import (
	"testing"

	"github.com/davidalecrim1/amx/internal/target"
)

func TestParseSelection(t *testing.T) {
	got, err := parseSelection("\x1b[38;2;122;162;247m●\x1b[0m 12s  1.1 claude\tp:%12")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != target.Pane || got.PaneID != "%12" {
		t.Fatalf("got %+v", got)
	}

	got, err = parseSelection("▾ PROJ-123  api  1!\ts:PROJ-123")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != target.Session || got.Session != "PROJ-123" {
		t.Fatalf("got %+v", got)
	}

	if _, err := parseSelection("no ref here"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/plain/path"); got != "/plain/path" {
		t.Fatalf("got %q", got)
	}
	if got := shellQuote("/with space/amx"); got != "'/with space/amx'" {
		t.Fatalf("got %q", got)
	}
}
