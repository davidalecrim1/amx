package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/davidalecrim1/amx/internal/agent"
	"github.com/davidalecrim1/amx/internal/state"
	"github.com/davidalecrim1/amx/internal/tmux"
)

func (a *App) hookCmd(args []string) int {
	if len(args) < 2 || args[0] != "claude" {
		return 0
	}
	event := args[1]

	var payload map[string]any
	if dec := json.NewDecoder(io.LimitReader(os.Stdin, 4<<20)); dec.Decode(&payload) != nil {
		payload = map[string]any{}
	}

	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return 0
	}
	pid, err := tmux.ServerPID()
	if err != nil {
		return 0
	}
	now := a.Now()

	rec, err := a.Store.Get(pid, paneID)
	if err != nil {
		return 0
	}
	if rec == nil {
		if event == "SessionEnd" {
			return 0
		}
		rec = &state.Record{
			PaneID:    paneID,
			ServerPID: pid,
			Agent:     "claude",
			State:     state.Done,
		}
	}

	res := agent.ApplyClaude(event, payload, rec, now)
	if res.Delete {
		_ = a.Store.Delete(pid, paneID)
		return 0
	}
	// A finish in the pane the user is staring at is already seen.
	if rec.State == state.Review && tmux.PaneFocused(paneID) {
		rec.State = state.Done
		rec.UpdatedAt = now
	}
	if res.Changed {
		_ = a.Store.Save(*rec)
	}
	return 0
}

// pickReportPane prefers the reported pane when it is still alive. OpenCode's
// service process can outlive the pane it was started from, so fall back to the
// live pane running this agent in the same directory.
func pickReportPane(panes []tmux.Pane, in reportInput) string {
	byID := make(map[string]tmux.Pane, len(panes))
	for _, p := range panes {
		byID[p.PaneID] = p
	}
	if p, ok := byID[in.PaneID]; ok && in.PaneID != "" {
		// The OpenCode service inherits the pane it was launched from, which can
		// be a stale pane or a different pane that is still alive. Only trust it
		// when that pane is actually running opencode.
		if in.Agent != "opencode" || strings.Contains(strings.ToLower(p.PaneCommand), "opencode") {
			return in.PaneID
		}
	}
	agent := strings.ToLower(in.Agent)
	for _, dir := range []string{in.CWD, in.Project} {
		if dir == "" {
			continue
		}
		want := filepath.Clean(dir)
		var matches []tmux.Pane
		for _, p := range panes {
			if filepath.Clean(p.PanePath) != want {
				continue
			}
			if agent != "" && !strings.Contains(strings.ToLower(p.PaneCommand), agent) {
				continue
			}
			matches = append(matches, p)
		}
		if len(matches) == 1 {
			return matches[0].PaneID
		}
		var active []tmux.Pane
		for _, p := range matches {
			if p.PaneActive {
				active = append(active, p)
			}
		}
		if len(active) == 1 {
			return active[0].PaneID
		}
	}
	return ""
}

type reportInput struct {
	PaneID    string `json:"pane_id"`
	CWD       string `json:"cwd"`
	Project   string `json:"project"`
	Agent     string `json:"agent"`
	State     string `json:"state"`
	Detail    string `json:"detail"`
	Title     string `json:"title"`
	SessionID string `json:"session_id"`
}

func (a *App) reportCmd(args []string) int {
	var in reportInput
	if dec := json.NewDecoder(io.LimitReader(os.Stdin, 4<<20)); dec.Decode(&in) != nil {
		return a.fail("report: invalid JSON on stdin")
	}
	if in.PaneID == "" {
		in.PaneID = os.Getenv("TMUX_PANE")
	}
	pid, err := tmux.ServerPID()
	if err != nil {
		return 0
	}
	panes, err := tmux.ListPanes()
	if err != nil {
		return 0
	}
	in.PaneID = pickReportPane(panes, in)
	if in.PaneID == "" {
		return 0
	}
	if in.State == "gone" || in.State == "end" {
		_ = a.Store.Delete(pid, in.PaneID)
		return 0
	}
	if in.State == "same" || in.State == "" {
		rec, err := a.Store.Get(pid, in.PaneID)
		if err != nil || rec == nil {
			return 0
		}
		if in.Title != "" && rec.Title != in.Title {
			rec.Title = in.Title
			if err := a.Store.Save(*rec); err != nil {
				return a.fail("%v", err)
			}
		}
		return 0
	}
	st := state.State(in.State).Normalize()
	if !st.Valid() {
		return a.fail("report: unknown state %q", in.State)
	}
	now := a.Now()
	rec, err := a.Store.Get(pid, in.PaneID)
	if err != nil {
		return a.fail("%v", err)
	}
	if rec == nil {
		rec = &state.Record{PaneID: in.PaneID, ServerPID: pid, Agent: in.Agent, State: st}
	}
	if in.Agent != "" {
		rec.Agent = in.Agent
	}
	if in.SessionID != "" {
		rec.SessionID = in.SessionID
	}
	rec.Detail = in.Detail
	if in.Title != "" {
		rec.Title = in.Title
	}
	if rec.State != st || rec.UpdatedAt.IsZero() {
		rec.State = st
		rec.UpdatedAt = now
	}
	// A finish in the pane the user is staring at is already reviewed.
	if rec.State == state.Review && tmux.PaneFocused(in.PaneID) {
		rec.State = state.Done
		rec.UpdatedAt = now
	}
	rec.LastEvent = now
	if err := a.Store.Save(*rec); err != nil {
		return a.fail("%v", err)
	}
	return 0
}
