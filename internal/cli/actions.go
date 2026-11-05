package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/davidalecrim1/amx/internal/state"
	"github.com/davidalecrim1/amx/internal/target"
	"github.com/davidalecrim1/amx/internal/tmux"
)

func (a *App) jumpCmd(args []string) int {
	if len(args) < 1 {
		return a.fail("jump needs a target")
	}
	t, err := target.Parse(args[0])
	if err != nil {
		return a.fail("%v", err)
	}
	s, err := a.load(false)
	if err != nil {
		return a.fail("%v", err)
	}
	return a.jump(s, t)
}

func (a *App) jump(s *snapshot, t target.Target) int {
	switch t.Kind {
	case target.Pane:
		pane, ok := resolvePane(s, t)
		if !ok {
			return a.fail("pane %s is gone", t.PaneID)
		}
		if err := tmux.Jump(pane.SessionName, pane.SessionName+":"+pane.WindowIndex, pane.PaneID); err != nil {
			return a.fail("%v", err)
		}
	case target.Window:
		if err := tmux.Jump(t.Session, t.Session+":"+t.Window, ""); err != nil {
			return a.fail("%v", err)
		}
	case target.Session:
		if err := tmux.Jump(t.Session, "", ""); err != nil {
			return a.fail("%v", err)
		}
	}
	return 0
}

func (a *App) killCmd(args []string) int {
	yes := false
	var ref string
	for _, arg := range args {
		if arg == "--yes" || arg == "-y" {
			yes = true
			continue
		}
		ref = arg
	}
	if ref == "" {
		return a.fail("kill needs a target")
	}
	t, err := target.Parse(ref)
	if err != nil {
		return a.fail("%v", err)
	}
	s, err := a.load(false)
	if err != nil {
		return a.fail("%v", err)
	}
	label := t.Describe()
	if t.Kind == target.Pane {
		if p, ok := resolvePane(s, t); ok {
			label = fmt.Sprintf("pane %s (%s:%s.%s)", p.PaneID, p.SessionName, p.WindowIndex, p.PaneIndex)
		}
	}
	if !yes && !confirm("kill "+label+"? [y/N] ") {
		return 0
	}
	switch t.Kind {
	case target.Pane:
		err = tmux.KillPane(t.PaneID)
	case target.Window:
		err = tmux.KillWindow(t.Session + ":" + t.Window)
	default:
		err = tmux.KillSession(t.Session)
	}
	if err != nil {
		return a.fail("%v", err)
	}
	return 0
}

func (a *App) resetCmd(args []string) int {
	return a.mutateTargets(args, "reset", func(rec *state.Record) bool {
		if rec.State == state.Done {
			return false
		}
		rec.State = state.Done
		rec.UpdatedAt = a.Now()
		return true
	})
}

func (a *App) unreadCmd(args []string) int {
	return a.mutateTargets(args, "unread", func(rec *state.Record) bool {
		if rec.State != state.Done {
			return false
		}
		rec.State = state.Review
		rec.UpdatedAt = a.Now()
		return true
	})
}

func (a *App) ackCmd(args []string) int {
	return a.mutateTargets(args, "ack", func(rec *state.Record) bool {
		if rec.State != state.Review {
			return false
		}
		rec.State = state.Done
		rec.UpdatedAt = a.Now()
		return true
	})
}

func (a *App) mutateTargets(args []string, cmd string, fn func(*state.Record) bool) int {
	if len(args) < 1 {
		return a.fail("%s needs a target", cmd)
	}
	t, err := target.Parse(args[0])
	if err != nil {
		return a.fail("%v", err)
	}
	s, err := a.load(false)
	if err != nil {
		return a.fail("%v", err)
	}
	ids := []string{}
	switch t.Kind {
	case target.Pane:
		ids = append(ids, t.PaneID)
	default:
		for _, p := range s.Panes {
			if p.SessionName != t.Session {
				continue
			}
			if t.Kind == target.Window && p.WindowIndex != t.Window {
				continue
			}
			ids = append(ids, p.PaneID)
		}
	}
	for _, id := range ids {
		rec, err := s.Store.Get(s.PID, id)
		if err != nil || rec == nil {
			continue
		}
		if fn(rec) {
			if err := s.Store.Save(*rec); err != nil {
				return a.fail("%v", err)
			}
		}
	}
	return 0
}

func (a *App) focusCmd(args []string) int {
	if len(args) < 1 || args[0] == "" {
		return 0
	}
	arg := args[0]
	paneID := ""
	if strings.HasPrefix(arg, "%") {
		paneID = arg
	} else {
		idx := strings.LastIndex(arg, ":")
		if idx <= 0 {
			return 0
		}
		session, window := arg[:idx], arg[idx+1:]
		s, err := a.load(false)
		if err != nil {
			return 0
		}
		for _, p := range s.Panes {
			if p.SessionName == session && p.WindowIndex == window && p.PaneActive {
				paneID = p.PaneID
				break
			}
		}
		if paneID == "" {
			return 0
		}
	}
	pid, err := tmux.ServerPID()
	if err != nil {
		return 0
	}
	now := a.Now()
	rec, err := a.Store.Get(pid, paneID)
	if err != nil || rec == nil {
		return 0
	}
	rec.LastFocus = now
	if rec.State == state.Review {
		rec.State = state.Done
		rec.UpdatedAt = now
	}
	_ = a.Store.Save(*rec)
	return 0
}

func (a *App) copyCmd(args []string) int {
	useCwd := false
	var ref string
	for _, arg := range args {
		if arg == "--cwd" {
			useCwd = true
			continue
		}
		ref = arg
	}
	if ref == "" {
		return a.fail("copy needs a target")
	}
	t, err := target.Parse(ref)
	if err != nil {
		return a.fail("%v", err)
	}
	s, err := a.load(false)
	if err != nil {
		return a.fail("%v", err)
	}
	value := t.String()
	if p, ok := resolvePane(s, t); ok {
		if useCwd {
			value = p.PanePath
		} else {
			value = fmt.Sprintf("%s:%s.%s", p.SessionName, p.WindowIndex, p.PaneIndex)
		}
	} else if useCwd {
		return a.fail("pane is gone")
	}
	if err := clipboard(value); err != nil {
		return a.fail("%v", err)
	}
	return 0
}

func clipboard(value string) error {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	default:
		candidates = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	}
	for _, c := range candidates {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(value)
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no clipboard tool available")
}

func confirm(prompt string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprint(os.Stderr, "amx: cannot prompt, pass --yes\n")
		return false
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt)
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
