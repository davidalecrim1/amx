# AGENTS.md

Guidance for coding agents working in this repository.

## What this is

amx is a Go CLI plus an fzf popup. It shows which tmux panes run an AI agent, what state
each agent is in, and jumps to the selected pane. Hooks write small JSON files; the popup
reads them. There is no daemon.

- `README.md` is the user-facing contract.
- `docs/SCOPE.md` records the design decisions and the reasoning behind them.

Keep both in sync when behavior changes.

## Commands

```sh
make build      # builds bin/amx
make install    # installs to ~/.local/bin/amx
make test       # go test ./...
make vet
make fmt
```

The tmux binding and the agent hooks call the installed binary
(`~/.local/bin/amx`), not `bin/amx`. Run `make install` after changing the binary if you
want the live popup to use it.

Useful manual commands:

```sh
amx doctor                 # state, hooks, integrations, fzf
amx list --ansi            # the exact rows fzf receives
amx list --json            # tracked records
amx preview p:%0           # one pane preview
amx setup --dry-run        # show every config mutation without applying it
```

## Layout

```
cmd/amx/main.go          command dispatch
internal/cli/             pick, list, jump, kill, reset, ack, copy, focus,
                          hook claude, report, fold/unfold/toggle, doctor, setup
internal/render/          rows, ordering, ANSI palette
internal/tmux/            tmux CLI wrapper, capture, ANSI sanitizing
internal/state/           atomic per-pane JSON store
internal/agent/           Claude hook event → state transition
internal/setup/           tmux, Claude, OpenCode installation
internal/target/          target ref parsing (`s:`, `w:`, `p:`)
docs/SCOPE.md             design decisions
```

## Data flow

```
Claude hooks ─┐
              ├─> amx hook / amx report ─> ~/.local/state/amx/<server-pid>/<pane>.json
OpenCode  ────┘                                        │
                                                       ▼
tmux list-panes -a ──────────────> amx pick (fzf) ─> jump / kill / reset / ack / copy
tmux focus hooks ─> amx focus ──> state files
```

## Invariants

- No background processes, timers, or polling daemons. Everything happens in response to a
  command or an event.
- Hook adapters must always exit 0, absorb their own errors, and never write to stdout
  that an agent could interpret as a decision.
- State is one JSON file per pane, written atomically (temp file + rename).
- Pane IDs are unique only per tmux server. Every path and record carries
  `ServerPID()`; never store bare pane IDs across servers.
- Reconciliation happens at read time: delete records for panes that no longer exist,
  prune directories for dead server PIDs, and never trust a record just because a file
  exists.
- The renderer never draws status from process names or pane contents. If a hook did not
  report it, it is not known.
- Focus clears REVIEW to DONE. `amx unread` is the only path back: it promotes DONE
  to REVIEW and leaves RUNNING and NEEDS_INPUT alone.
- Dependencies: standard library only. Do not add modules.

## Hard-won details

These are easy to regress. Each one has a test or a documented reason.

### fzf

- `--disabled` still accepts and displays query text; it only disables filtering. Use
  `--no-input` to actually block typing, then `show-input+enable-search` for `/`.
- `FZF_INPUT_STATE` is `hidden` at startup, `enabled` in search mode. Conditional
  bindings must test `!= enabled` for navigation, not `= disabled`.
- `hide-input` keeps the query and the filtered matches. Esc while typing hides the input
  to lock the filter, so navigation keys work on the results; Esc again clears the query.
- `put(...)` inserts text into the query. `print(...)` prints and exits; never use it to
  re-insert a key.
- `reload(...)` is asynchronous. When the cursor must land on a row after a reload, emit
  `reload-sync(...)+pos(N)` from the transform.
- The list is pre-sorted; `--no-sort` preserves that order.
- The popup refreshes live via `--listen=<unix socket>` plus a one-second ticker that
  POSTs `reload(amx list --ansi)`. It is a goroutine inside `amx pick`, not a daemon;
  fzf 0.71+ keeps the query and current item across the reload.

### tmux

- `bg=default` in a style means the default value of that option (the theme surface), not
  the terminal background. The terminal background is `bg=terminal`, which emits SGR 49.
  It exists in tmux 3.8+, so `amx setup` version-gates it.
- The tokyo-night theme re-applies `popup-style` on every status render. A global override
  gets clobbered; the popup must pass `-s` on `display-popup`.
- `capture-pane` without `-S` returns the visible screen. `-S` includes scrollback and
  leaks old content into previews.
- `#{pane_id}` can be used directly in `after-select-pane`; use
  `#{session_name}:#{window_index}` for window/session focus hooks and resolve the active
  pane in Go.
- Pane `current_path` is the pane's working directory, not the project root. Do not use it
  as a project identity.

### OpenCode

- Plugins run in the singleton `opencode serve --service` process, not in the TUI. Its
  `TMUX_PANE` can point at a pane that no longer exists.
- `ctx.location.directory` is the project root, which may be a parent of the pane's
  directory. The service's `process.cwd()` is the launch directory and usually matches.
- The plugin sends both `cwd` (launch dir) and `project`; `amx report` prefers a live
  `pane_id`, then a unique pane whose path equals `cwd`, then `project`, filtered by the
  pane running the agent. Ambiguity is resolved by the active pane, otherwise dropped.
- The OpenCode v2 API is different from v1. v1 plugins do not run at all.
- v2 emits `session.execution.started|succeeded|failed|interrupted`, not
  `session.idle`; the session ID is at `event.data.sessionID`, not
  `event.properties.sessionID`. The plugin filters events on the session ID captured by the
  prompt hook, otherwise every plugin instance reacts to every session on the server.

### Rendering

- ANSI 16-color palette only: red 31, green 32, blue 34, magenta 35, cyan 36, faint 2,
  reverse 7, default fg 39. Never hardcode truecolor; the terminal theme decides the shades.
- The last pane column is the session title. Claude stores the first prompt as the title;
  OpenCode sends the real session title from `ctx.session.get`/`session.updated`.
- State markers are an emoji plus an uppercase word (`🚨 NEEDS`, `🏃 RUNNING`,
  `📩 REVIEW`, `✅ DONE`), never a background colour. Only the agent pane carries a
  marker; session and window rows stay clean even when collapsed or when they contain several agents. There is
  no picker header; keep the surface minimal.
- The picker exports `AMX_LIST_WIDTH` from the popup's tty width; `amx list` uses it to
  size the branch/path columns and truncate window names. Do not restore fixed
  widths.
- The preview strips background SGR and every non-SGR escape, then cuts each line to
  `FZF_PREVIEW_COLUMNS - 1`. A TUI with a 200-column frame must not wrap into blocks.
- Ordering is urgency first (needs-input → review → running → done), then last used
  descending, recursively at every level.
- Collapse state lives in the file named by `AMX_TREE_STATE`, created per picker
  invocation and removed on exit.

### Setup

- `amx setup` must be idempotent. It backs up `~/.claude/settings.json` before merging,
  skips handlers whose command already exists, and refuses to overwrite an OpenCode plugin
  that lacks the generated marker.
- Claude hook handlers are synchronous with `timeout: 5`. Ordered delivery is what
  prevents a late tool event from overwriting `Stop`.
- The tmux snippet is generated, not hand-edited; edit `internal/setup/setup.go`.

## Testing notes

- Unit tests cover transitions, ordering, collapse, target parsing, ANSI sanitizing, setup
  merging, and pane resolution.
- fzf interaction is not unit-tested. Use a pty to verify modal behavior:

  ```sh
  seq 1 5 > /tmp/list
  ( (sleep 1; printf 'a'; sleep 0.5) | script -q /dev/null sh -c \
      "fzf --no-input --bind 'enter:execute-silent(echo \$FZF_QUERY > /tmp/q)+accept' < /tmp/list" ) >/dev/null 2>&1
  cat /tmp/q   # must be empty with --no-input
  ```

- tmux-dependent tests need a live server. Always use a throwaway socket
  (`tmux -L amx-test ...`) and `-f /dev/null` so the user's config and sessions are not
  touched. Never kill the default server.

## Do not

- Add a daemon, sidebar, status line, notification system, or remote monitoring.
- Infer agent state from processes or pane text.
- Hardcode terminal colors or assume a dark theme.
- Trust `$TMUX_PANE` for OpenCode plugin events.
- Use `capture-pane -S` for the preview.
- Run destructive tmux commands against the default socket in tests.
- Add third-party Go dependencies.

## Commits

Imperative subject, prefixed `feat:`, `fix:`, `docs:`, or `chore:`. Keep the body focused
on why. The public repo is `davidalecrim1/amx`.
