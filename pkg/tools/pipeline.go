package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/ianclemence/ghost/pkg/capability"
)

const (
	pipelineMaxSteps  = 12
	pipelineMaxHold   = 400 << 10
	pipelineMaxReturn = 6000
)

var pipelineReadOnlyExtra = map[string]bool{
	"read_file": true, "list_dir": true, "search_files": true,
	"session_state": true, "context_get": true, "memory_recall": true, "session_search": true,
}

// isPipelineReadable is true for a call that only reads: the same class the
// broker allows without asking.
func isPipelineReadable(name string, args map[string]interface{}) bool {
	if name == "pipeline" || name == "subagent" || name == "spawn" || name == "batch_delegate" {
		return false
	}
	if pipelineReadOnlyExtra[name] {
		return true
	}
	spec, ok := capability.ForToolAction(name, args)
	return ok && spec.Risk == capability.RiskReadOnly
}

// PipelineTool runs a short, fixed recipe of read-only lookups and line
// filters, and returns only the last result. What each step produced stays
// out of the conversation, so asking a question of many results costs the
// answer and not the results.
type PipelineTool struct {
	reg *ToolRegistry
}

func NewPipelineTool(reg *ToolRegistry) *PipelineTool { return &PipelineTool{reg: reg} }

func (t *PipelineTool) Name() string { return "pipeline" }

// UntrustedOutput marks the result as outside content: any step may have read
// the web or an inbox.
func (t *PipelineTool) UntrustedOutput() bool { return true }

func (t *PipelineTool) Description() string {
	return "Ask a question of many lookups at once without reading each one. Give a short list of steps; only the last result comes back to you. " +
		"Use it when you would otherwise search or read several things and only need what matches (\"which of my notes mention the invoice\", \"how many unread mails are from the bank\"). " +
		"Steps: {call: tool, args: {...}, as: name} runs a read-only tool; {parallel: [call steps]} runs several at once; " +
		"{grep: contains|regex, from: name, as: name, invert?: true} keeps matching lines; {head: n, from: name, as: name}; {count: true, from: name, as: name} counts lines; " +
		"{unique: true, from: name, as: name}; {join: [names], as: name}. Pass the name to return in \"return\" (default: the last step). Only read-only tools may be called."
}

func (t *PipelineTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"steps":  map[string]interface{}{"type": "array", "description": "The steps, in order (at most 12)", "items": map[string]interface{}{"type": "object"}},
			"return": map[string]interface{}{"type": "string", "description": "Name of the step result to return"},
		},
		"required": []string{"steps"},
	}
}

type pipelineRun struct {
	t       *PipelineTool
	ctx     context.Context
	vars    map[string]string
	last    string
	held    int
	callIDs int
}

func (r *pipelineRun) store(name, val string) error {
	r.held += len(val)
	if r.held > pipelineMaxHold {
		return fmt.Errorf("the steps hold more than %d KB between them; narrow a lookup or filter sooner", pipelineMaxHold>>10)
	}
	if name == "" {
		name = fmt.Sprintf("_%d", len(r.vars)+1)
	}
	r.vars[name] = val
	r.last = name
	return nil
}

func (r *pipelineRun) get(name string) (string, error) {
	if name == "" {
		name = r.last
	}
	v, ok := r.vars[name]
	if !ok {
		return "", fmt.Errorf("there is no result called %q yet", name)
	}
	return v, nil
}

func (r *pipelineRun) call(step map[string]interface{}) (string, error) {
	name, _ := step["call"].(string)
	args, _ := step["args"].(map[string]interface{})
	if args == nil {
		args = map[string]interface{}{}
	}
	if !isPipelineReadable(name, args) {
		return "", fmt.Errorf("%q is not a read-only tool, so a pipeline cannot call it", name)
	}
	res := r.t.reg.ExecuteWithContext(r.ctx, name, args, "", "", SessionKeyFromContext(r.ctx), nil)
	if res == nil {
		return "", fmt.Errorf("%s returned nothing", name)
	}
	if res.IsError {
		return "", fmt.Errorf("%s failed: %s", name, firstLine(res.ForLLM))
	}
	return res.ForLLM, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return s
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func (r *pipelineRun) step(step map[string]interface{}) error {
	as, _ := step["as"].(string)
	from, _ := step["from"].(string)
	switch {
	case step["call"] != nil:
		out, err := r.call(step)
		if err != nil {
			return err
		}
		return r.store(as, out)
	case step["parallel"] != nil:
		list, _ := step["parallel"].([]interface{})
		if len(list) == 0 || len(list) > 6 {
			return fmt.Errorf("parallel takes between 1 and 6 calls")
		}
		type result struct {
			name, out string
			err       error
		}
		results := make([]result, len(list))
		var wg sync.WaitGroup
		for i, item := range list {
			sub, _ := item.(map[string]interface{})
			if sub == nil || sub["call"] == nil {
				return fmt.Errorf("every parallel step must be a call")
			}
			wg.Add(1)
			go func(i int, sub map[string]interface{}) {
				defer wg.Done()
				out, err := r.call(sub)
				n, _ := sub["as"].(string)
				results[i] = result{n, out, err}
			}(i, sub)
		}
		wg.Wait()
		for _, res := range results {
			if res.err != nil {
				return res.err
			}
			if err := r.store(res.name, res.out); err != nil {
				return err
			}
		}
		return nil
	case step["grep"] != nil || step["regex"] != nil:
		src, err := r.get(from)
		if err != nil {
			return err
		}
		invert, _ := step["invert"].(bool)
		var match func(string) bool
		if pat, _ := step["regex"].(string); pat != "" {
			re, err := regexp.Compile("(?i)" + pat)
			if err != nil {
				return fmt.Errorf("that pattern is not valid: %v", err)
			}
			match = re.MatchString
		} else {
			needle, _ := step["grep"].(string)
			needle = strings.ToLower(needle)
			match = func(l string) bool { return strings.Contains(strings.ToLower(l), needle) }
		}
		var kept []string
		for _, l := range lines(src) {
			if match(l) != invert {
				kept = append(kept, l)
			}
		}
		return r.store(as, strings.Join(kept, "\n"))
	case step["head"] != nil:
		src, err := r.get(from)
		if err != nil {
			return err
		}
		n := 10
		if v, ok := step["head"].(float64); ok && int(v) > 0 {
			n = int(v)
		}
		ls := lines(src)
		if len(ls) > n {
			ls = ls[:n]
		}
		return r.store(as, strings.Join(ls, "\n"))
	case step["count"] != nil:
		src, err := r.get(from)
		if err != nil {
			return err
		}
		return r.store(as, fmt.Sprint(len(lines(src))))
	case step["unique"] != nil:
		src, err := r.get(from)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		var out []string
		for _, l := range lines(src) {
			if !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
		sort.Strings(out)
		return r.store(as, strings.Join(out, "\n"))
	case step["join"] != nil:
		names, _ := step["join"].([]interface{})
		var parts []string
		for _, n := range names {
			s, _ := n.(string)
			v, err := r.get(s)
			if err != nil {
				return err
			}
			parts = append(parts, v)
		}
		return r.store(as, strings.Join(parts, "\n"))
	}
	return fmt.Errorf("a step needs one of: call, parallel, grep, regex, head, count, unique, join")
}

func (t *PipelineTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	raw, _ := args["steps"].([]interface{})
	if len(raw) == 0 {
		return ErrorResult("steps is required: a short list of steps")
	}
	if len(raw) > pipelineMaxSteps {
		return ErrorResult(fmt.Sprintf("a pipeline has at most %d steps", pipelineMaxSteps))
	}
	run := &pipelineRun{t: t, ctx: ctx, vars: map[string]string{}}
	for i, item := range raw {
		step, _ := item.(map[string]interface{})
		if step == nil {
			return ErrorResult(fmt.Sprintf("step %d is not an object", i+1))
		}
		if err := run.step(step); err != nil {
			return ErrorResult(fmt.Sprintf("step %d: %v", i+1, err))
		}
	}
	want, _ := args["return"].(string)
	out, err := run.get(want)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if r := []rune(out); len(r) > pipelineMaxReturn {
		out = string(r[:pipelineMaxReturn]) + "\n[cut: narrow the last step]"
	}
	if strings.TrimSpace(out) == "" {
		out = "Nothing matched."
	}
	return &ToolResult{ForLLM: out, Silent: true}
}
