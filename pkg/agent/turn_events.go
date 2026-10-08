package agent

import (
	"context"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ianclemence/ghost/pkg/tools"
)

// Turn events: what a turn did, told to whoever is watching it.
//
// The loop already announces each tool call as a bare label. A watcher that
// wants to show "ran 2 commands, one failed" needs more: which call it was,
// when it finished, whether it worked. The observer rides on the context (like
// the trajectory id), so it reaches the loop through every entry point without
// widening their signatures, and a turn with no watcher pays nothing.

type turnObserverKey struct{}

// WithTurnObserver attaches a watcher for the turn's events.
func WithTurnObserver(ctx context.Context, fn func(map[string]interface{})) context.Context {
	if ctx == nil || fn == nil {
		return ctx
	}
	return context.WithValue(ctx, turnObserverKey{}, fn)
}

func emitTurnEvent(ctx context.Context, ev map[string]interface{}) {
	if ctx == nil {
		return
	}
	if fn, ok := ctx.Value(turnObserverKey{}).(func(map[string]interface{})); ok && fn != nil {
		fn(ev)
	}
}

// ToolStart is the event for a call about to run.
func toolStartEvent(id, name string, args map[string]interface{}) map[string]interface{} {
	ev := map[string]interface{}{"type": "tool_start", "id": id, "tool": name}
	if d := ToolDetail(name, args); d != "" {
		ev["detail"] = d
	}
	return ev
}

// ToolEndEvent is the event for a call that finished (or was refused).
func toolEndEvent(id, name string, ok bool, ms int64, note string) map[string]interface{} {
	ev := map[string]interface{}{"type": "tool_result", "id": id, "tool": name, "ok": ok, "ms": ms}
	if note = SafeLine(note, 120); note != "" {
		ev["note"] = note
	}
	return ev
}

var (
	secretAssign = regexp.MustCompile(`(?i)(authorization|bearer|token|api[_-]?key|apikey|secret|passw(or)?d|passwd|credential)(["'\s:=]+)((?:bearer|basic)\s+)?([^\s"']+)`)
	longOpaque   = regexp.MustCompile(`[A-Za-z0-9+/_\-]{32,}={0,2}`)
	skKey        = regexp.MustCompile(`\b(sk|pk|ghp|gho|xox[a-z])[-_][A-Za-z0-9_\-]{8,}`)
)

// SafeLine reduces text to one short line that is fit to show on the owner's
// phone: whitespace collapsed, anything that looks like a credential masked,
// cut to max runes.
func SafeLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.Join(strings.Fields(s), " ")
	s = secretAssign.ReplaceAllString(s, "${1}${3}…")
	s = skKey.ReplaceAllString(s, "…")
	s = longOpaque.ReplaceAllString(s, "…")
	if utf8.RuneCountInString(s) > max {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:max-1])) + "…"
	}
	return s
}

func argString(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// ToolDetail is the one line that says what a call was about: the search, the
// site, the command, the file name. Never the whole of the arguments, never a
// credential. The phone shows it only when a step is opened.
func ToolDetail(name string, args map[string]interface{}) string {
	switch {
	case name == "web_search":
		return SafeLine(argString(args, "query"), 90)
	case name == "web_fetch":
		return hostOf(argString(args, "url"))
	case name == "browser_navigate":
		return hostOf(argString(args, "url"))
	case name == "exec" || name == "sandbox":
		return SafeLine(argString(args, "command"), 100)
	case name == "read_file" || name == "write_file" || name == "edit_file" || name == "append_file":
		p := argString(args, "path")
		if p == "" {
			p = argString(args, "file_path")
		}
		if p == "" {
			return ""
		}
		return SafeLine(filepath.Base(p), 60)
	}
	return ""
}

// toolEndNote says why a call did not work, in one short line. A call that
// worked has nothing to add. A command's output is stdout, then "STDERR:" and
// stderr, then "Exit code: ...": the line worth showing is the first line of
// what the command itself said, not those markers.
func toolEndNote(r *tools.ToolResult) string {
	if r == nil {
		return ""
	}
	if r.TimedOut {
		return "Timed out"
	}
	if !r.IsError {
		return ""
	}
	text := r.ForLLM
	if r.Err != nil && strings.TrimSpace(text) == "" {
		text = r.Err.Error()
	}
	return meaningfulLine(text)
}

// meaningfulLine picks the line of a failure that says what went wrong: the
// first line that is not a marker the tool added, falling back to the exit
// status when the command said nothing.
func meaningfulLine(text string) string {
	exit := ""
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case ln == "" || ln == "STDERR:" || ln == "(no output)":
			continue
		case strings.HasPrefix(ln, "Exit code:"):
			if exit == "" {
				exit = strings.TrimSpace(strings.TrimPrefix(ln, "Exit code:"))
			}
			continue
		}
		return ln
	}
	return exit
}

// steerPickedEvent tells a watcher that messages it sent into the running turn
// have been read by the model.
func steerPickedEvent(msgs []SteeringMessage) map[string]interface{} {
	contents := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.IsInterrupt || m.IsHardAbort {
			continue
		}
		contents = append(contents, m.Content)
	}
	return map[string]interface{}{"type": "steer_picked", "contents": contents}
}
