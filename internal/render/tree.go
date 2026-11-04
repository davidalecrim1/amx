package render

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/davidalecrim1/amux/internal/state"
	"github.com/davidalecrim1/amux/internal/target"
	"github.com/davidalecrim1/amux/internal/tmux"
)

const (
	reset     = "\x1b[0m"
	bold      = "\x1b[1m"
	faint     = "\x1b[2m"
	underline = "\x1b[4m"
	red       = "\x1b[31m"
	green     = "\x1b[32m"
	blue      = "\x1b[34m"
	magenta   = "\x1b[35m"
	cyan      = "\x1b[36m"
	defaultFG = "\x1b[39m"
)

type Options struct {
	Now       time.Time
	Branches  map[string]string
	ANSI      bool
	Collapsed map[string]bool
	Width     int
}

type colWidths struct {
	branch int
	path   int
	window int
}

// columnWidths adapts the flexible columns to the list width. Width is the
// visible list width reported by the popup; zero falls back to the old fixed
// sizes for `amux list` outside the picker.
func columnWidths(width int) colWidths {
	if width <= 0 {
		return colWidths{branch: 16, path: 20, window: 40}
	}
	const fixed = 40
	avail := width - fixed
	if avail < 16 {
		avail = 16
	}
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	branch := clamp(avail*40/100, 8, 24)
	path := clamp(avail-branch, 8, 24)
	window := width - 12
	if window < 12 {
		window = 12
	}
	return colWidths{branch: branch, path: path, window: window}
}

type paneView struct {
	pane     tmux.Pane
	rec      *state.Record
	branch   string
	lastUsed time.Time
	rank     int
}

type windowView struct {
	index    string
	name     string
	panes    []paneView
	hasAgent bool
	rank     int
	lastUsed time.Time
}

type sessionView struct {
	name     string
	path     string
	attached bool
	windows  []windowView
	counts   map[state.State]int
	hasAgent bool
	rank     int
	lastUsed time.Time
}

// Rows renders the tree and returns fzf input lines. Each line is the visible
// row, a tab, and the opaque target ref.
func Rows(panes []tmux.Pane, records map[string]state.Record, opt Options) []string {
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	sessions := group(panes, records, opt)

	var out []string
	for _, s := range sessions {
		out = append(out, renderSession(s, opt)+"\t"+target.Ref(target.Target{
			Kind: target.Session, Session: s.name,
		}))
		if opt.Collapsed[sessionKey(s.name)] {
			continue
		}
		for wi, w := range s.windows {
			lastWin := wi == len(s.windows)-1
			out = append(out, renderWindow(s, w, lastWin, opt)+"\t"+target.Ref(target.Target{
				Kind: target.Window, Session: s.name, Window: w.index,
			}))
			if opt.Collapsed[windowKey(s.name, w.index)] {
				continue
			}
			for pi, p := range w.panes {
				lastPane := pi == len(w.panes)-1
				out = append(out, renderPane(s, w, p, lastWin, lastPane, opt)+"\t"+target.Ref(target.Target{
					Kind: target.Pane, PaneID: p.pane.PaneID,
				}))
			}
		}
	}
	return out
}

func sessionKey(name string) string {
	return "s:" + name
}

func windowKey(session, index string) string {
	return "w:" + session + ":" + index
}

func group(panes []tmux.Pane, records map[string]state.Record, opt Options) []sessionView {
	bySession := map[string]*sessionView{}
	var order []string
	for _, p := range panes {
		s, ok := bySession[p.SessionName]
		if !ok {
			s = &sessionView{name: p.SessionName, path: p.SessionPath, attached: p.Attached, counts: map[state.State]int{}, rank: state.Idle.Rank()}
			bySession[p.SessionName] = s
			order = append(order, p.SessionName)
		}
		wIdx := -1
		for i := range s.windows {
			if s.windows[i].index == p.WindowIndex {
				wIdx = i
				break
			}
		}
		if wIdx < 0 {
			s.windows = append(s.windows, windowView{index: p.WindowIndex, name: p.WindowName, rank: state.Idle.Rank()})
			wIdx = len(s.windows) - 1
		}
		pv := paneView{pane: p, rank: state.Idle.Rank(), branch: opt.Branches[p.PaneID]}
		if rec, ok := records[p.PaneID]; ok {
			r := rec
			pv.rec = &r
			pv.rank = rec.State.Rank()
			pv.lastUsed = rec.LastUsed()
			s.counts[rec.State]++
			if !s.hasAgent || pv.rank < s.rank {
				s.rank = pv.rank
			}
			s.hasAgent = true
			if pv.lastUsed.After(s.lastUsed) {
				s.lastUsed = pv.lastUsed
			}
			if !s.windows[wIdx].hasAgent || pv.rank < s.windows[wIdx].rank {
				s.windows[wIdx].rank = pv.rank
				s.windows[wIdx].hasAgent = true
			}
			if pv.lastUsed.After(s.windows[wIdx].lastUsed) {
				s.windows[wIdx].lastUsed = pv.lastUsed
			}
		}
		s.windows[wIdx].panes = append(s.windows[wIdx].panes, pv)
	}

	out := make([]sessionView, 0, len(order))
	for _, name := range order {
		s := *bySession[name]
		for i := range s.windows {
			w := &s.windows[i]
			sort.SliceStable(w.panes, func(a, b int) bool {
				if w.panes[a].rank != w.panes[b].rank {
					return w.panes[a].rank < w.panes[b].rank
				}
				if !w.panes[a].lastUsed.Equal(w.panes[b].lastUsed) {
					return w.panes[a].lastUsed.After(w.panes[b].lastUsed)
				}
				return indexOf(w.panes[a].pane) < indexOf(w.panes[b].pane)
			})
		}
		sort.SliceStable(s.windows, func(a, b int) bool {
			x, y := s.windows[a], s.windows[b]
			if x.rank != y.rank {
				return x.rank < y.rank
			}
			if !x.lastUsed.Equal(y.lastUsed) {
				return x.lastUsed.After(y.lastUsed)
			}
			return indexOfStr(x.index) < indexOfStr(y.index)
		})
		out = append(out, s)
	}
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a], out[b]
		if x.rank != y.rank {
			return x.rank < y.rank
		}
		if !x.lastUsed.Equal(y.lastUsed) {
			return x.lastUsed.After(y.lastUsed)
		}
		return x.name < y.name
	})
	return out
}

func renderSession(s sessionView, opt Options) string {
	marker := "▾"
	if opt.Collapsed[sessionKey(s.name)] {
		marker = "▸"
	}
	nameColor := defaultFG
	if s.hasAgent && s.rank != state.Idle.Rank() {
		nameColor = stateColor(state.State(rankState(s.rank)))
	}
	nameStyle := bold + nameColor
	if s.attached {
		nameStyle += underline
	}
	var b strings.Builder
	b.WriteString(paint(opt, marker+" ", faint))
	b.WriteString(paint(opt, s.name, nameStyle))
	if s.hasAgent && (agentCount(s) > 1 || opt.Collapsed[sessionKey(s.name)]) {
		b.WriteString("  ")
		b.WriteString(statusText(state.State(rankState(s.rank))))
	}
	if base := pathBase(s.path); base != "" && base != s.name {
		b.WriteString("  ")
		b.WriteString(paint(opt, base, faint))
	}
	return b.String()
}

func renderWindow(s sessionView, w windowView, last bool, opt Options) string {
	branch := "  ├─ "
	if last {
		branch = "  └─ "
	}
	marker := "▾"
	if opt.Collapsed[windowKey(s.name, w.index)] {
		marker = "▸"
	}
	var b strings.Builder
	b.WriteString(paint(opt, branch, faint))
	b.WriteString(paint(opt, marker+" ", faint))
	cw := columnWidths(opt.Width)
	winWidth := cw.window
	collapsed := w.hasAgent && opt.Collapsed[windowKey(s.name, w.index)]
	if collapsed {
		winWidth -= 12
	}
	if winWidth < 12 {
		winWidth = 12
	}
	b.WriteString(paint(opt, w.index, bold+defaultFG))
	b.WriteString(" ")
	b.WriteString(paint(opt, truncate(w.name, winWidth), faint))
	if collapsed {
		b.WriteString("  ")
		b.WriteString(statusText(state.State(rankState(w.rank))))
	}
	return b.String()
}

func renderPane(s sessionView, w windowView, p paneView, lastWin, lastPane bool, opt Options) string {
	indent := "  │   "
	if lastWin {
		indent = "      "
	}
	branch := "├─ "
	if lastPane {
		branch = "└─ "
	}
	var b strings.Builder
	b.WriteString(paint(opt, indent+branch, faint))

	if p.rec != nil {
		b.WriteString(statusText(p.rec.State))
		b.WriteString(" ")
		b.WriteString(paint(opt, pad(FormatAge(opt.Now.Sub(p.rec.UpdatedAt)), 4), faint))
		b.WriteString(" ")
	} else {
		b.WriteString("                ")
	}

	b.WriteString(paint(opt, pad(w.index+"."+p.pane.PaneIndex, 5), faint))
	b.WriteString(" ")
	b.WriteString(paint(opt, pad(truncate(agentName(p), 8), 8), cyan))
	b.WriteString(" ")
	cw := columnWidths(opt.Width)
	b.WriteString(paint(opt, pad(truncate(branchName(p), cw.branch), cw.branch), magenta))
	b.WriteString(" ")
	b.WriteString(paint(opt, truncate(pathBase(p.pane.PanePath), cw.path), faint))
	return strings.TrimRight(b.String(), " ")
}

func agentName(p paneView) string {
	if p.rec != nil && p.rec.Agent != "" {
		return p.rec.Agent
	}
	return p.pane.PaneCommand
}

func branchName(p paneView) string {
	if p.branch == "" {
		return "-"
	}
	return p.branch
}

func agentCount(s sessionView) int {
	n := 0
	for _, c := range s.counts {
		n += c
	}
	return n
}

// stateLabel is the uppercase word shown in the status badge.
func stateLabel(s state.State) string {
	switch s {
	case state.NeedsInput:
		return "NEEDS"
	case state.Running:
		return "RUNNING"
	case state.DoneUnseen:
		return "DONE"
	default:
		return "IDLE"
	}
}

const badgeWidth = len("RUNNING")

// statusText is the emoji plus the uppercase word, padded so every state takes
// the same number of columns. Nothing is drawn with a background colour.
func statusText(s state.State) string {
	label := stateLabel(s)
	return stateEmoji(s) + " " + label + strings.Repeat(" ", badgeWidth-len(label))
}

func stateEmoji(s state.State) string {
	switch s {
	case state.NeedsInput:
		return "🚨"
	case state.Running:
		return "🏃"
	case state.DoneUnseen:
		return "✅"
	default:
		return "💤"
	}
}

func stateColor(s state.State) string {
	switch s {
	case state.NeedsInput:
		return red
	case state.Running:
		return green
	case state.DoneUnseen:
		return blue
	default:
		return faint
	}
}

func rankState(rank int) state.State {
	switch rank {
	case 0:
		return state.NeedsInput
	case 1:
		return state.DoneUnseen
	case 2:
		return state.Running
	default:
		return state.Idle
	}
}

func paint(opt Options, s, color string) string {
	if !opt.ANSI || s == "" {
		return s
	}
	return color + s + reset
}

func pathBase(path string) string {
	if path == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+"/") {
			path = "~" + strings.TrimPrefix(path, home)
		}
	}
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return path
	}
	base := trimmed
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		base = trimmed[i+1:]
	}
	if base == "" || base == "~" {
		return "~"
	}
	return base
}

func FormatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\t", " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

func pad(s string, n int) string {
	w := utf8.RuneCountInString(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

func indexOf(p tmux.Pane) int {
	v, _ := strconv.Atoi(p.PaneIndex)
	return v
}

func indexOfStr(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}
