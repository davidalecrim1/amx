package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/davidalecrim1/amx/internal/target"
)

func (a *App) pick(args []string) int {
	s, err := a.load(true)
	if err != nil {
		return a.fail("%v", err)
	}
	if cols := terminalWidth(); cols > 0 {
		os.Setenv("AMX_LIST_WIDTH", strconv.Itoa(cols/2-2))
	}
	rows := a.renderRows(s, true)
	if len(rows) == 0 {
		fmt.Fprintln(a.Stderr, "amx: no tmux panes")
		return 0
	}
	exe, err := os.Executable()
	if err != nil {
		return a.fail("%v", err)
	}
	bin := shellQuote(exe)
	statePath := fmt.Sprintf("%s/amx-tree-%d.json", os.TempDir(), os.Getpid())
	_ = os.Remove(statePath)
	defer os.Remove(statePath)
	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("amx-fzf-%d.sock", os.Getpid()))
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	fzfArgs := []string{
		"--ansi",
		"--layout=reverse",
		"--height=100%",
		"--no-sort",
		"--no-input",
		"--color=16,bg:-1,bg+:-1,alt-bg:-1,list-bg:-1,input-bg:-1,header-bg:-1,footer-bg:-1,preview-bg:-1",
		"--delimiter=\t",
		"--with-nth=1",
		"--nth=1",
		"--pointer=▌",
		"--marker=┃",
		"--prompt=amx> ",
		"--preview=" + bin + " preview {2}",
		"--preview-window=right:50%:border-left",
		"--listen=" + sockPath,
		"--bind", `j:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then echo down; else echo "put(j)"; fi`,
		"--bind", `k:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then echo up; else echo "put(k)"; fi`,
		"--bind", `g:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then echo first; else echo "put(g)"; fi`,
		"--bind", `G:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then echo last; else echo "put(G)"; fi`,
		"--bind", `/:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then echo "show-input+enable-search+change-prompt(search> )"; else echo "put(/)"; fi`,
		"--bind", `esc:transform:if [ "$FZF_INPUT_STATE" = enabled ]; then echo "hide-input"; elif [ -n "$FZF_QUERY" ]; then echo "clear-query"; else echo abort; fi`,
		"--bind", `q:transform:if [ "$FZF_INPUT_STATE" = enabled ]; then echo "put(q)"; else echo abort; fi`,
		"--bind", fmt.Sprintf(`h:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then %s fold {2}; else echo "put(h)"; fi`, bin),
		"--bind", fmt.Sprintf(`H:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then %s fold {2}; else echo "put(H)"; fi`, bin),
		"--bind", fmt.Sprintf(`l:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then %s unfold {2}; else echo "put(l)"; fi`, bin),
		"--bind", fmt.Sprintf(`L:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then %s unfold {2}; else echo "put(L)"; fi`, bin),
		"--bind", fmt.Sprintf(`;:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then %s toggle {2}; else echo "put(;)"; fi`, bin),
		"--bind=ctrl-x:execute-silent(" + bin + " kill --yes {2})+reload(" + bin + " list --ansi)",
		"--bind=ctrl-r:execute-silent(" + bin + " reset {2})+reload(" + bin + " list --ansi)",
		"--bind=ctrl-a:execute-silent(" + bin + " ack {2})+reload(" + bin + " list --ansi)",
		"--bind", fmt.Sprintf(`x:transform:if [ "$FZF_INPUT_STATE" != enabled ]; then echo "execute-silent(%s kill --yes {2})+reload(%s list --ansi)"; else echo "put(x)"; fi`, bin, bin),
		"--bind", fmt.Sprintf(`ctrl-u:transform:if [ "$FZF_INPUT_STATE" = enabled ]; then echo unix-line-discard; else echo "execute-silent(%s unread {2})+reload(%s list --ansi)"; fi`, bin, bin),
		"--bind=ctrl-y:execute-silent(" + bin + " copy {2})",
		"--bind=ctrl-o:execute-silent(" + bin + " copy --cwd {2})",
	}

	cmd := exec.Command("fzf", fzfArgs...)
	cmd.Env = append(os.Environ(), treeStateEnv+"="+statePath)
	cmd.Stdin = strings.NewReader(strings.Join(rows, "\n") + "\n")
	cmd.Stderr = a.Stderr
	var out strings.Builder
	cmd.Stdout = &out

	stop := make(chan struct{})
	go refreshLoop(sockPath, fmt.Sprintf("reload(%s list --ansi)", bin), stop)
	err = cmd.Run()
	close(stop)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			switch ee.ExitCode() {
			case 130, 1:
				return 0
			}
		}
		if _, ok := err.(*exec.ExitError); ok {
			return 0
		}
		return a.fail("fzf: %v", err)
	}
	sel := strings.TrimRight(out.String(), "\n")
	if sel == "" {
		return 0
	}
	t, err := parseSelection(sel)
	if err != nil {
		return a.fail("%v", err)
	}
	return a.jump(s, t)
}

func parseSelection(line string) (target.Target, error) {
	idx := strings.LastIndex(line, "\t")
	if idx < 0 {
		return target.Target{}, fmt.Errorf("unexpected fzf output")
	}
	return target.Parse(line[idx+1:])
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t'\"$`\\!*?()[]{};&|<>#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
