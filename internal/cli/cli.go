package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davidalecrim1/amux/internal/render"
	"github.com/davidalecrim1/amux/internal/state"
	"github.com/davidalecrim1/amux/internal/tmux"
)

type App struct {
	Store  *state.Store
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
}

func New() *App {
	return &App{
		Store:  state.NewStore(""),
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Now:    time.Now,
	}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.usage()
		return 1
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "pick":
		return a.pick(rest)
	case "list":
		return a.list(rest)
	case "preview":
		return a.preview(rest)
	case "jump":
		return a.jumpCmd(rest)
	case "kill":
		return a.killCmd(rest)
	case "reset":
		return a.resetCmd(rest)
	case "ack":
		return a.ackCmd(rest)
	case "unread":
		return a.unreadCmd(rest)
	case "copy":
		return a.copyCmd(rest)
	case "focus":
		return a.focusCmd(rest)
	case "hook":
		return a.hookCmd(rest)
	case "report":
		return a.reportCmd(rest)
	case "fold":
		return a.foldCmd(rest)
	case "unfold":
		return a.unfoldCmd(rest)
	case "toggle":
		return a.toggleNodeCmd(rest)
	case "doctor":
		return a.doctorCmd(rest)
	case "setup":
		return a.setupCmd(rest)
	case "version":
		fmt.Fprintln(a.Stdout, "amux 0.1.0")
		return 0
	case "help", "-h", "--help":
		a.usage()
		return 0
	default:
		fmt.Fprintf(a.Stderr, "amux: unknown command %q\n", cmd)
		a.usage()
		return 1
	}
}

func (a *App) usage() {
	fmt.Fprint(a.Stderr, `amux - tmux agent status picker

  amux pick                      open the fzf tree (usually bound to prefix+W)
  amux list [--ansi] [--json]    print the tree
  amux jump <target>             switch to a session, window, or pane
  amux kill <target>             kill a pane, window, or session
  amux reset <target>            clear tracked state
  amux ack <target>              clear done markers
  amux unread <target>           mark idle agents as done-unseen
  amux focus <pane-id>           tmux focus hook callback
  amux hook claude <Event>       Claude Code hook adapter
  amux report                    state writer (stdin JSON)
  amux doctor                    diagnose pane/state mismatches
  amux setup [--dry-run]         install tmux bindings, hooks, and plugins
`)
}

type snapshot struct {
	PID     int
	Panes   []tmux.Pane
	Records map[string]state.Record
	Store   *state.Store
	Now     time.Time
}

func (a *App) load(reconcile bool) (*snapshot, error) {
	pid, err := tmux.ServerPID()
	if err != nil {
		return nil, err
	}
	panes, err := tmux.ListPanes()
	if err != nil {
		return nil, err
	}
	now := a.Now()
	live := make(map[string]bool, len(panes))
	byPane := make(map[string]tmux.Pane, len(panes))
	for _, p := range panes {
		live[p.PaneID] = true
		byPane[p.PaneID] = p
	}
	if reconcile {
		a.Store.PruneServers(pid)
		a.Store.PruneServer(pid, live)
		a.Store.PruneOld(14*24*time.Hour, now)
	}
	records, err := a.Store.Load(pid)
	if err != nil {
		return nil, err
	}
	if reconcile {
		for id, rec := range records {
			p, ok := byPane[id]
			if !ok {
				delete(records, id)
				continue
			}
			// An opencode record on a pane that is back at a shell is stale. The
			// TUI owns its pane, so the foreground command can never be a shell.
			if rec.Agent == "opencode" && isLoginShell(p.PaneCommand) {
				delete(records, id)
				_ = a.Store.Delete(pid, id)
				continue
			}
			if rec.State != state.Idle && isLoginShell(p.PaneCommand) && now.Sub(rec.LastEvent) > time.Minute {
				rec.State = state.Idle
				rec.UpdatedAt = rec.LastEvent
				records[id] = rec
				_ = a.Store.Save(rec)
			}
		}
	}
	return &snapshot{PID: pid, Panes: panes, Records: records, Store: a.Store, Now: now}, nil
}

func isLoginShell(cmd string) bool {
	switch strings.ToLower(filepath.Base(cmd)) {
	case "sh", "bash", "zsh", "fish", "nu", "dash", "ksh", "tcsh", "csh":
		return true
	}
	return false
}

func (a *App) fail(format string, args ...any) int {
	fmt.Fprintf(a.Stderr, "amux: "+format+"\n", args...)
	return 1
}

// branches resolves git branches for every pane in parallel.
func branches(panes []tmux.Pane) map[string]string {
	out := make(map[string]string, len(panes))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, p := range panes {
		wg.Add(1)
		go func(p tmux.Pane) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			b := gitBranch(p.PanePath)
			if b == "" {
				return
			}
			mu.Lock()
			out[p.PaneID] = b
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return out
}

func gitBranch(dir string) string {
	if dir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return ""
	}
	b := strings.TrimSpace(buf.String())
	if b == "HEAD" {
		return ""
	}
	return b
}

func (a *App) renderRows(s *snapshot, ansi bool) []string {
	width, _ := strconv.Atoi(os.Getenv("AMUX_LIST_WIDTH"))
	return render.Rows(s.Panes, s.Records, render.Options{
		Now:       s.Now,
		Branches:  branches(s.Panes),
		ANSI:      ansi,
		Collapsed: loadCollapsed(os.Getenv(treeStateEnv)),
		Width:     width,
	})
}

func (a *App) writeJSON(recs map[string]state.Record) int {
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(recs); err != nil {
		return a.fail("%v", err)
	}
	return 0
}
