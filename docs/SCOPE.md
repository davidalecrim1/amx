# amx — Scope

Status: draft for approval
Repo: `github.com/davidalecrim1/amx`
Supersedes: `samleeney/tmux-agent-status` (architecture only; no code reuse)

## 1. Problem

`prefix + w` (tmux `choose-tree`) shows sessions, windows, and panes as undifferentiated
text. With one tmux session per Jira task and one to three panes per session, it is
impossible to tell which pane has an agent, whether that agent is working, and whether it
is blocked waiting on a permission prompt. The current hook-driven status is noisy and
sometimes sticks on the wrong state after an interrupt or a pane lifecycle change.

`tmux-agent-status` solves the state problem but adds a daemon, a persistent sidebar
pane, a status-line summary, an unread inbox, park/wait modes, and ~11.8k lines of Bash.
The sidebar is the main surface there; it is not wanted here.

## 2. What amx is

A single Go binary plus a tmux popup.

- `prefix + W` opens a temporary popup with an fzf-driven tree of the whole tmux server.
- Every pane is listed. Panes running an agent carry a status marker; plain shells do not.
- Typing filters the tree; Enter jumps to the exact session/window/pane.
- Status comes from agent hooks writing small state files. No daemon, no polling loop, no
  sidebar, no status bar.

## 3. Non-goals

Cut permanently unless re-scoped later:

- No background daemon or collector loop.
- No persistent sidebar pane, no status-right widget, no per-window marker.
- No park/wait modes, no unread-completion inbox or history.
- No remote/SSH status, no notification sounds, no desktop toasts.
- No session deploy or worktree orchestration.
- No TPM plugin, Homebrew tap, or release pipeline. Local build and install only.
- No process-level agent detection (no pgrep/pane-scraping state machine).

## 4. UX

### 4.1 Entry point

```
bind W display-popup -E -w 90% -h 85% -T ' Agent Multiplexer ' -b rounded -s 'bg=terminal' -S 'fg=brightblack' '~/.local/bin/amx pick'
```

`-s 'bg=terminal'` overrides the theme's solid `popup-style` for this popup only and makes
tmux emit the terminal's default background (SGR 49), so whatever terminal is running
composites its own background behind the popup. `bg=terminal` is a tmux 3.8 colour
keyword. On older versions setup drops the flag and clears `popup-style` with
`set-option -gu` immediately before opening, so tmux's default (the terminal background)
applies.
`prefix + w` is left untouched as the plain `choose-tree` fallback.

### 4.2 Layout

Every row is selectable. Box-drawing tree, sessions as headers with windows and panes
nested beneath. Sessions and windows start expanded; `h` collapses and `l` expands the
selected node, the same way `choose-tree` does. Collapsed rows switch their marker from
`▾` to `▸` and keep the aggregate state of everything hidden inside them.

```
 ▾ PROJ-123  🚨 NEEDS  add retry to webhook consumer
   ├─ ▾ 1 claude
   │   ├─ 🚨 NEEDS   1.1 claude  feature/retry  fix webhook timeout…   12s
   │   └─ 🏃 RUNNING 1.2 zsh     feature/retry  ~/api
   └─ ▾ 2 review
       └─ 🚨 NEEDS   2.1 claude  main           why does this fail?   2m
```

Row anatomy:

| Row | Prefix | Columns |
| --- | --- | --- |
| session | `▾` (underlined name if attached here) | session name (bold), aggregate badge when it has more than one agent or is collapsed, path basename |
| window | `├─` / `└─` + `▾` | window index, window name, aggregate badge only when collapsed |
| pane | `├─` / `└─` | state badge for agent panes only, `window.pane`, agent or foreground command, git branch, cwd basename (`~` for `$HOME`), age, session title |

Session summary line is `#{session_path}` basename when it differs from the session name,
otherwise omitted.

### 4.3 Emojis and labels

Four states, one emoji plus an uppercase word. There is no background color; the emoji does
the scanning and the label stays in the terminal's default foreground.

| State | Marker | Meaning |
| --- | --- | --- |
| NEEDS_INPUT | 🚨 NEEDS | agent is blocked on you (permission, question, error) |
| RUNNING | 🏃 RUNNING | agent is working |
| DONE_UNSEEN | ✅ DONE | agent finished since you last looked at the pane |
| IDLE | 💤 IDLE | agent is alive and waiting for a prompt |
| none | blank | no agent in this pane |

Column widths come from the content: each column starts at a small base and grows to the
longest current value. The title is the last column and only appears on agent rows. Long
branch, title, and window names truncate with `…` when the table does not fit the popup. The marker is
padded to the width of `🏃 RUNNING` so the pane columns stay aligned. Status
belongs to the agent pane: a window with one nvim pane and one opencode pane marks only the
opencode row. A session shows its worst state when it contains more than one agent, or when
it is collapsed; a window shows its state only when collapsed. Agent names are ANSI cyan,
branches ANSI magenta, age faint. fzf runs with
`--color=16` and default backgrounds, so its chrome comes from the same palette. There is no
header; the list is the whole surface.

### 4.4 Ordering

Recursive sort at every level:

1. urgency of the most urgent descendant: NEEDS_INPUT → DONE_UNSEEN → RUNNING → IDLE
2. last used, newest first

"Last used" is `max(last_focus_at, last_event_at)`: the most recent of you focusing the
pane or the agent emitting an event. Sessions and windows inherit the best value from
their children. This puts old, untouched work at the bottom without shuffling rows on
every redraw.

### 4.5 Keys

| Key | Action |
| --- | --- |
| `j` / `k` | move the cursor down / up while navigating |
| `g` / `G` | jump to the first / last row |
| `h` / `l` | collapse / expand the selected session or window |
| `;` | toggle the selected session or window |
| `/` | reveal the input and enter search mode |
| `q` | quit while navigating |
| Enter | jump to the selected session, window, or pane and close the popup |
| `x` / Ctrl-X | kill the selected pane, window, or session immediately |
| Ctrl-R | reset the selected target's tracked state to IDLE |
| Ctrl-A | acknowledge DONE_UNSEEN on the selected target, clearing the marker |
| Ctrl-U | mark the selected target unread: IDLE becomes DONE_UNSEEN |
| Ctrl-Y | copy the target ref (`session:window.pane`) to the clipboard |
| Ctrl-O | copy the pane's cwd to the clipboard |
| Esc | leave search mode; when already navigating, quit |

Kill and reset on a session or window row apply to every pane beneath it.

### 4.6 Search

The list opens in navigation mode with fzf's input field hidden (`--no-input`), so a
keypress can never become query text. `/` reveals the input and enables search. Esc while
typing hides the input but keeps the query and the filtered result set, so `j`/`k` navigate
the matches; `/` brings the input back with the query intact. Esc with a filter applied
clears it; Esc with no filter quits. `q` quits in navigation mode and types normally in
search mode.

In search mode fzf fuzzy matches the visible row text: session name, window name, agent
name, branch, path, title, and badge text. `NEEDS` finds everything blocked on
you.
The list is regenerated on open and then every second while the popup is open, through fzf's
`--listen` socket. Changes are visible without closing the picker, and fzf keeps the
query and current item across the reload. The refresh goroutine lives only as long as
`amx pick`; nothing survives the command.

A right-side preview pane (half the popup, with a divider) shows the highlighted pane's
visible screen via `tmux capture-pane`, never its scrollback. The preview strips background colours and non-SGR escapes, then cuts
each line to the preview width, so a full-screen TUI cannot paint over the popup or wrap
into a block of colours.

## 5. Status model

### 5.1 State ownership

Hooks own the state. amx never infers agent state from pane contents or processes.
The picker reconciles only against live tmux panes (see 5.4).

### 5.2 Claude Code

`amx hook claude <Event>` reads the hook JSON from stdin.

| Hook event | Result |
| --- | --- |
| `SessionStart` | IDLE, register agent as `claude` |
| `UserPromptSubmit` | RUNNING, first prompt kept as the session title |
| `PreToolUse`, `PostToolUse`, `PostToolUseFailure` | RUNNING, heartbeat |
| `PermissionRequest` | NEEDS_INPUT, detail = tool name |
| `PermissionDenied` | RUNNING |
| `Notification` (`permission_prompt`, `agent_needs_input`, `elicitation_dialog`) | NEEDS_INPUT |
| `Notification` (other types) | no-op |
| `Stop` | DONE_UNSEEN, unless the payload reports running `background_tasks` → RUNNING |
| `StopFailure` | NEEDS_INPUT, detail = error type |
| `SessionEnd` | delete the record |
| anything else | no-op |

Hooks are observational and must never block the agent:

- Always exit 0, swallow all errors.
- No-op when `$TMUX` or `$TMUX_PANE` is unset.
- All handlers run synchronously. Each call is a few milliseconds, and ordered
  delivery is what stops a late `PreToolUse` from overwriting a `Stop`.
- Per-event `timeout: 5`.

### 5.3 OpenCode

A generated plugin at `~/.config/opencode/plugins/amx.js`, installed by `amx setup`,
subscribing to the OpenCode v2 public event stream and piping normalized JSON into
`amx report`. v1 plugins do not run in v2, so this targets `Plugin.define` and
`ctx.event.subscribe` exclusively.

| Event | Result |
| --- | --- |
| `prompt` session hook | RUNNING, records the session ID |
| `tool.execute.before` hook | RUNNING, heartbeat |
| `session.execution.started` | RUNNING |
| `session.execution.succeeded`, `interrupted` | DONE_UNSEEN |
| `session.execution.failed` | NEEDS_INPUT, detail = error type |
| `session.updated` / `ctx.session.get` | session title |
| `permission.asked` | NEEDS_INPUT |
| `permission.replied` | RUNNING |
| `session.idle`, `session.status` | fallback for RUNNING/DONE |
| `session.error` | NEEDS_INPUT, detail = error |
| `session.deleted` | delete the record |

OpenCode rows show the directory basename and branch, like every other row.

### 5.4 Staleness and reconciliation

There is no clock and no daemon, so the picker reconciles at open time:

1. Read live panes with one `tmux list-panes -a -F ...`. Any state file whose pane id is
   not in that list is deleted. This handles kill, respawn, and window moves.
2. Records are namespaced by tmux server pid (the pid field from `$TMUX`). Records from
   another server are ignored; directories for dead server pids are pruned. This handles
   a tmux server restart, which reuses pane ids.
3. If a pane still exists but its `pane_current_command` is a login shell (`sh`, `bash`,
   `zsh`, `fish`, `nu`, `dash`) and the record's last event is older than 60 seconds, the
   record is demoted to IDLE. This catches an agent that died without `SessionEnd`
   without flickering during Claude's `!` shell mode.
4. A stuck RUNNING is never auto-demoted. The row shows its age (`12m`), so a lie is
   visible, and `Ctrl-R` fixes it.

DONE_UNSEEN → IDLE happens when you focus the pane. tmux hooks (`after-select-pane`,
`after-select-window`, `client-session-changed`) call `amx focus <pane-id or
session:window>`, which clears the marker and stamps `last_focus_at`. `Ctrl-A`
acknowledges without visiting. A `Stop` inside the pane you are currently looking at is
recorded as IDLE immediately, so watching an agent finish never leaves a stale `DONE`.

The reverse is explicit: `amx unread <target>` promotes IDLE to DONE_UNSEEN and bumps
`updated_at`. It never touches RUNNING or NEEDS_INPUT. `prefix+u` runs it for the current
pane; `Ctrl-U` runs it for the selected picker row.

### 5.5 Age

Age is time since the state last changed (`updated_at`), formatted `12s`, `4m`, `2h`, `3d`.
It is the honest signal for stale state.

## 6. Architecture

```
Claude hooks ─┐
              ├─> amx report / amx hook ─> ~/.local/state/amx/<server-pid>/<pane>.json
OpenCode  ────┘                                        │
                                                       ▼
tmux list-panes -a ──────────────> amx pick (fzf) ─> jump / kill / reset / ack / copy
tmux focus hooks ─> amx focus ──> state files
```

Read path only. No goroutine runs longer than the popup, no background process survives
the command, nothing listens on a socket.

Startup budget: popup list renders in under 100 ms with 30 panes and 10 sessions. Git
branch lookup runs in parallel per pane with a 1.5 s cap; failures render `-`.

## 7. State format

`~/.local/state/amx/<server-pid>/<pane-id>.json`, one file per pane, written atomically
(temp file + rename). Different panes never contend; same-pane writes are last-wins.

```json
{
  "pane_id": "%12",
  "server_pid": 12345,
  "agent": "claude",
  "state": "needs_input",
  "session_id": "0f3c…",
  "detail": "Bash",
  "title": "fix the webhook timeout",
  "updated_at": "2026-10-26T16:40:12Z",
  "last_event_at": "2026-10-26T16:40:12Z",
  "last_focus_at": "2026-10-26T16:31:02Z"
}
```

`XDG_STATE_HOME` is honored. Files older than 14 days are pruned at pick time.

## 8. CLI

| Command | Purpose |
| --- | --- |
| `amx pick` | popup entrypoint: build list, run fzf, dispatch the chosen action |
| `amx list [--ansi] [--json]` | render the tree (used by fzf reload and for debugging) |
| `amx preview <target>` | capture-pane preview for fzf |
| `amx jump <target>` | switch client, window, pane |
| `amx kill <target> [--yes]` | kill pane, window, or session with confirmation |
| `amx reset <target>` | set tracked state to IDLE for the target |
| `amx ack <target>` | clear DONE_UNSEEN for the target |
| `amx copy <target> [--cwd]` | put the target ref or cwd on the clipboard |
| `amx focus <pane-id>` | tmux hook callback: mark focused, clear DONE_UNSEEN |
| `amx hook claude <Event>` | Claude Code hook adapter (stdin JSON) |
| `amx report` | normalized writer (stdin JSON), used by the OpenCode plugin |
| `amx doctor` | dump pane/state mismatches, hook installation status, binary path |
| `amx setup [--dry-run]` | install tmux bindings/hooks, Claude hooks, OpenCode plugin |

Targets: `%pane`, `session:window.pane`, `session:window`, `session`. Rows carry an opaque
target id in a tab-separated field that fzf hides.

## 9. Setup

`make install` builds to `~/.local/bin/amx`. `amx setup` is idempotent and backs up
before editing:

1. Writes `~/.config/amx/tmux.conf`:

```tmux
bind W display-popup -E -w 90% -h 85% -T ' Agent Multiplexer ' -b rounded -s 'bg=terminal' -S 'fg=brightblack' '~/.local/bin/amx pick'
bind u run-shell -b "~/.local/bin/amx unread #{pane_id}"
set-hook -g after-select-pane   'run-shell -b "~/.local/bin/amx focus #{pane_id}"'
set-hook -g after-select-window 'run-shell -b "~/.local/bin/amx focus #{pane_id}"'
set-hook -g client-session-changed 'run-shell -b "~/.local/bin/amx focus #{pane_id}"'
```

and appends `source-file ~/.config/amx/tmux.conf` to `~/.config/tmux/tmux.conf` if the
line is absent. Existing `amx` hooks are removed first so re-running setup never
duplicates them.

2. Merges the Claude Code hooks into `~/.claude/settings.json`, adding to each event's
   array without touching existing handlers. Timestamped backup at
   `~/.claude/settings.json.amx-<ts>.bak`. Detects an existing amx handler by command
   path and skips it.

3. Writes the OpenCode plugin to `~/.config/opencode/plugins/amx.js`, refusing to
   overwrite a file it did not generate (marker comment on line 1).

4. Prints what changed and tells you to run `tmux source-file`, restart Claude Code, and
   restart OpenCode.

`--dry-run` prints every file write and settings mutation without applying it.

## 10. Repo layout

```
amx/
  cmd/amx/main.go            # command dispatch
  internal/cli/               # pick, list, jump, kill, reset, ack, copy, focus, doctor
  internal/tmux/tmux.go       # list-panes, capture, jump, kill, hooks
  internal/state/store.go     # atomic read/write/prune
  internal/agent/claude.go    # Claude hook event → state transition
  internal/render/tree.go     # rows, colors, ordering
  internal/picker/fzf.go      # fzf invocation, keybinds, reload
  internal/setup/             # tmux/claude/opencode installation
  docs/SCOPE.md
  Makefile                    # build, install, test, fmt, vet
  README.md
```

## 11. Acceptance criteria

1. `prefix+W` opens the popup in under 100 ms with 10 sessions and 30 panes.
2. Submitting a Claude prompt makes that pane RUNNING before the first tool call.
3. A Claude permission prompt makes it NEEDS_INPUT before the dialog appears.
4. `Stop` makes it DONE_UNSEEN; focusing the pane makes it IDLE.
5. Esc-interrupt leaves RUNNING, the age column grows, and Ctrl-R recovers it.
6. Killing a pane removes its row on the next open; renaming a session or window is
   reflected immediately because names are read live.
7. Pane ids reused after a tmux server restart never inherit old state.
8. `ps` shows no amx process when the popup is closed.
9. Plain shells are listed, selectable, and visually quieter than agent panes.
10. Fuzzy search matches session name, window name, branch, path, and title.
11. `amx setup --dry-run` changes nothing; running it twice produces no duplicates.
12. `go test ./...` covers state transitions, ordering, rendering, and target parsing.

## 12. Open items to verify during build

Defaults below are what I will implement unless you object:

1. **tmux hook context** — confirm `#{pane_id}` resolves to the new active pane inside
   `after-select-window` and `client-session-changed`; otherwise fall back to
   `#{session_name}:#{window_index}` plus a lookup.
2. **OpenCode payload fields** — confirm `event.properties.sessionID` and
   `status.type` shapes with a temporary logging plugin; adjust the adapter.
3. **Claude hooks** — verify every handler fires and that ordered delivery keeps `Stop`
   from being overwritten by a trailing tool event.
4. **Claude `!` shell mode** — check whether `pane_current_command` flips to the shell
   while the agent is alive; if it does often, drop rule 5.4.3 entirely.
5. **Clipboard tool** — `pbcopy` on macOS, `wl-copy`/`xclip` on Linux; no-op with a
   message when neither exists.
