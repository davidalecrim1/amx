package agent

import (
	"strings"
	"time"

	"github.com/davidalecrim1/amx/internal/state"
)

type Result struct {
	Delete  bool
	Changed bool
}

// ApplyClaude maps a Claude Code hook event onto a record. Unknown events are
// ignored so that installing extra hooks never invents state.
func ApplyClaude(event string, p map[string]any, rec *state.Record, now time.Time) Result {
	if rec.Agent == "" {
		rec.Agent = "claude"
	}
	if sid := str(p["session_id"]); sid != "" {
		rec.SessionID = sid
	}

	switch event {
	case "SessionStart":
		setState(rec, state.Done, now)
		rec.Detail = ""
	case "UserPromptSubmit":
		setState(rec, state.Running, now)
		rec.Detail = ""
		if rec.Title == "" {
			rec.Title = collapse(str(p["prompt"]))
		}
	case "PreToolUse", "PostToolUse", "PostToolUseFailure":
		setState(rec, state.Running, now)
	case "PermissionRequest":
		setState(rec, state.NeedsInput, now)
		rec.Detail = collapse(str(p["tool_name"]))
	case "PermissionDenied":
		setState(rec, state.Running, now)
	case "Notification":
		switch str(p["notification_type"]) {
		case "permission_prompt", "agent_needs_input", "elicitation_dialog", "elicitation_url_dialog":
			setState(rec, state.NeedsInput, now)
			msg := collapse(str(p["message"]))
			if msg == "" {
				msg = collapse(str(p["title"]))
			}
			rec.Detail = msg
		default:
			return Result{}
		}
	case "Stop":
		if len(list(p["background_tasks"])) > 0 {
			setState(rec, state.Running, now)
		} else {
			setState(rec, state.Review, now)
		}
	case "StopFailure":
		setState(rec, state.NeedsInput, now)
		rec.Detail = "api error"
	case "SessionEnd":
		return Result{Delete: true, Changed: true}
	default:
		return Result{}
	}
	rec.LastEvent = now
	return Result{Changed: true}
}

func setState(rec *state.Record, s state.State, now time.Time) {
	if rec.State != s {
		rec.State = s
		rec.UpdatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func collapse(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return s
}
