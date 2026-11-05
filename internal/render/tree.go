package render

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/davidalecrim1/amx/internal/state"
	"github.com/davidalecrim1/amx/internal/target"
	"github.com/davidalecrim1/amx/internal/tmux"
)

const (
	reset     = "\x1b[0m"
	bold      = "\x1b[1m"
	faint     = "\x1b[2m"
	underline = "\x1b[4m"
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

type table struct {
	status int
	age    int
	ref    int
	agent  int
	branch int
	dir    int
	title  int
}

// buildTable sizes each column from the longest value, starting from a small
// base and shrinking the flexible columns only when the row would not fit the
// view. The last column is cut with an ellipsis by the renderer.
func buildTable(panes []tmux.Pane, records map[string]state.Record, branches map[string]string, width int) table {
	t := table{status: 10, age: 4, ref: 3, agent: 4, branch: 2, dir: 2}
	clamp := func(v, hi int) int {
		if v > hi {
			return hi
		}
		return v
	}
	for _, p := range panes {
		if n := utf8.RuneCountInString(p.WindowIndex + "." + p.PaneIndex); n > t.ref {
			t.ref = clamp(n, 8)
		}
		name := p.PaneCommand
		if r, ok := records[p.PaneID]; ok && r.Agent != "" {
			name = r.Agent
		}
		if n := utf8.RuneCountInString(name); n > t.agent {
			t.agent = clamp(n, 16)
		}
		b := branches[p.PaneID]
		if b == "" {
			b = "-"
		}
		if n := utf8.RuneCountInString(b); n > t.branch {
			t.branch = clamp(n, 32)
		}
		if n := utf8.RuneCountInString(pathBase(p.PanePath)); n > t.dir {
			t.dir = clamp(n, 28)
		}
		if r, ok := records[p.PaneID]; ok {
			if n := utf8.RuneCountInString(r.Title); n > t.title {
				t.title = clamp(n, 40)
			}
		}
	}

	const sep = 2
	if width > 0 {
		total := 8 + t.status + sep + t.age + sep + t.ref + sep + t.agent + sep + t.branch + sep + t.dir
		mins := []int{4, 2, 2}
		fields := []*int{&t.agent, &t.branch, &t.dir}
		if t.title > 0 {
			total += sep + t.title
			mins = append(mins, 4)
			fields = append(fields, &t.title)
		}
		for total > width && t.title > 4 {
			t.title--
			total--
		}
		for total > width {
			idx, size := -1, 0
			for i := range fields {
				if *fields[i] > mins[i] && *fields[i] > size {
					idx, size = i, *fields[i]
				}
			}
			if idx < 0 {
				break
			}
			*fields[idx]--
			total--
		}
	}
	return t
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
	table := buildTable(panes, records, opt.Branches, opt.Width)

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
				out = append(out, renderPane(s, w, p, lastWin, lastPane, table, opt)+"\t"+target.Ref(target.Target{
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
			s = &sessionView{name: p.SessionName, path: p.SessionPath, attached: p.Attached, rank: state.Done.Rank()}
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
			s.windows = append(s.windows, windowView{index: p.WindowIndex, name: p.WindowName, rank: state.Done.Rank()})
			wIdx = len(s.windows) - 1
		}
		pv := paneView{pane: p, rank: state.Done.Rank(), branch: opt.Branches[p.PaneID]}
		if rec, ok := records[p.PaneID]; ok {
			r := rec
			pv.rec = &r
			pv.rank = rec.State.Rank()
			pv.lastUsed = rec.LastUsed()
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
	nameStyle := bold + defaultFG
	if s.attached {
		nameStyle += underline
	}
	var b strings.Builder
	b.WriteString(paint(opt, marker+" ", faint))
	b.WriteString(paint(opt, s.name, nameStyle))
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
	winWidth := 40
	if opt.Width > 0 {
		winWidth = opt.Width - 12
	}
	if winWidth < 12 {
		winWidth = 12
	}
	b.WriteString(paint(opt, w.index, bold+defaultFG))
	b.WriteString(" ")
	b.WriteString(paint(opt, truncate(w.name, winWidth), faint))
	return b.String()
}

func renderPane(s sessionView, w windowView, p paneView, lastWin, lastPane bool, t table, opt Options) string {
	indent := "  │   "
	if lastWin {
		indent = "      "
	}
	branch := "├─ "
	if lastPane {
		branch = "└─ "
	}
	const sep = "  "
	var b strings.Builder
	b.WriteString(paint(opt, indent+branch, faint))

	if p.rec != nil {
		b.WriteString(statusText(p.rec.State))
		b.WriteString(sep)
		b.WriteString(paint(opt, pad(FormatAge(opt.Now.Sub(p.rec.UpdatedAt)), t.age), faint))
		b.WriteString(sep)
	} else {
		b.WriteString(strings.Repeat(" ", t.status))
		b.WriteString(sep)
		b.WriteString(strings.Repeat(" ", t.age))
		b.WriteString(sep)
	}

	b.WriteString(paint(opt, pad(w.index+"."+p.pane.PaneIndex, t.ref), faint))
	b.WriteString(sep)
	b.WriteString(paint(opt, pad(truncate(agentName(p), t.agent), t.agent), cyan))
	b.WriteString(sep)
	b.WriteString(paint(opt, pad(truncate(branchName(p), t.branch), t.branch), magenta))
	b.WriteString(sep)
	b.WriteString(paint(opt, truncate(pathBase(p.pane.PanePath), t.dir), faint))
	if p.rec != nil && p.rec.Title != "" && t.title > 0 {
		b.WriteString(sep)
		b.WriteString(paint(opt, truncate(p.rec.Title, t.title), defaultFG))
	}
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

// stateLabel is the uppercase word shown in the status badge.
func stateLabel(s state.State) string {
	switch s {
	case state.NeedsInput:
		return "NEEDS"
	case state.Running:
		return "RUNNING"
	case state.Review:
		return "REVIEW"
	default:
		return "DONE"
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
	case state.Review:
		return "📩"
	default:
		return "✅"
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
