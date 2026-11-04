package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/davidalecrim1/amux/internal/render"
	"github.com/davidalecrim1/amux/internal/target"
	"github.com/davidalecrim1/amux/internal/tmux"
)

const treeStateEnv = "AMUX_TREE_STATE"

func (a *App) foldCmd(args []string) int {
	return a.toggleCmd(args, true)
}

func (a *App) unfoldCmd(args []string) int {
	return a.toggleCmd(args, false)
}

func (a *App) toggleCmd(args []string, fold bool) int {
	t, s, path, collapsed, ok := a.treeTarget(args)
	if !ok {
		return 0
	}
	return a.applyFold(s, path, collapsed, t, fold)
}

// toggleNodeCmd flips the node: folds an expanded session or window and
// unfolds a collapsed one.
func (a *App) toggleNodeCmd(args []string) int {
	t, s, path, collapsed, ok := a.treeTarget(args)
	if !ok {
		return 0
	}
	return a.applyFold(s, path, collapsed, t, !isCollapsed(t, s.Panes, collapsed))
}

func (a *App) treeTarget(args []string) (target.Target, *snapshot, string, map[string]bool, bool) {
	if len(args) < 1 {
		return target.Target{}, nil, "", nil, false
	}
	t, err := target.Parse(args[0])
	if err != nil {
		return target.Target{}, nil, "", nil, false
	}
	path := os.Getenv(treeStateEnv)
	if path == "" {
		return target.Target{}, nil, "", nil, false
	}
	s, err := a.load(false)
	if err != nil {
		return target.Target{}, nil, "", nil, false
	}
	return t, s, path, loadCollapsed(path), true
}

func (a *App) applyFold(s *snapshot, path string, collapsed map[string]bool, t target.Target, fold bool) int {
	focus, changed := foldState(t, s.Panes, collapsed, fold)
	if !changed {
		return 0
	}
	if err := saveCollapsed(path, collapsed); err != nil {
		return 0
	}
	width, _ := strconv.Atoi(os.Getenv("AMUX_LIST_WIDTH"))
	rows := render.Rows(s.Panes, s.Records, render.Options{
		Now:       a.Now(),
		Branches:  branches(s.Panes),
		ANSI:      true,
		Collapsed: collapsed,
		Width:     width,
	})
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	action := fmt.Sprintf("reload-sync(%s list --ansi)", shellQuote(exe))
	if pos := rowIndex(rows, focus); pos > 0 {
		action += fmt.Sprintf("+pos(%d)", pos)
	}
	fmt.Fprintln(a.Stdout, action)
	return 0
}

func isCollapsed(t target.Target, panes []tmux.Pane, collapsed map[string]bool) bool {
	switch t.Kind {
	case target.Session:
		return collapsed["s:"+t.Session]
	case target.Window:
		return collapsed["w:"+t.Session+":"+t.Window]
	default:
		for _, p := range panes {
			if p.PaneID == t.PaneID {
				return collapsed["w:"+p.SessionName+":"+p.WindowIndex]
			}
		}
	}
	return false
}

// foldState applies h/l to the selected row and returns the row the cursor
// should land on after the reload.
func foldState(t target.Target, panes []tmux.Pane, collapsed map[string]bool, fold bool) (string, bool) {
	switch t.Kind {
	case target.Session:
		return foldSession(t.Session, collapsed, fold)
	case target.Window:
		return foldWindow(t.Session, t.Window, collapsed, fold)
	default:
		for _, p := range panes {
			if p.PaneID == t.PaneID {
				return foldWindow(p.SessionName, p.WindowIndex, collapsed, fold)
			}
		}
		return "", false
	}
}

func foldSession(session string, collapsed map[string]bool, fold bool) (string, bool) {
	key := "s:" + session
	if fold {
		if collapsed[key] {
			return key, false
		}
		collapsed[key] = true
		return key, true
	}
	if !collapsed[key] {
		return key, false
	}
	delete(collapsed, key)
	return key, true
}

func foldWindow(session, window string, collapsed map[string]bool, fold bool) (string, bool) {
	wkey := "w:" + session + ":" + window
	skey := "s:" + session
	if fold {
		if !collapsed[wkey] {
			collapsed[wkey] = true
			return wkey, true
		}
		if !collapsed[skey] {
			collapsed[skey] = true
			return skey, true
		}
		return wkey, false
	}
	if collapsed[wkey] {
		delete(collapsed, wkey)
		return wkey, true
	}
	return wkey, false
}

func rowIndex(rows []string, ref string) int {
	for i, row := range rows {
		if strings.HasSuffix(row, "\t"+ref) {
			return i + 1
		}
	}
	return 0
}

func loadCollapsed(path string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]bool{}
	}
	return out
}

func saveCollapsed(path string, collapsed map[string]bool) error {
	data, err := json.Marshal(collapsed)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
