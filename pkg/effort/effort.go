// Package effort turns a request into an explicit execution budget. It is the
// controller that keeps Ghost cheap for simple work and expensive only when
// justified: Quick, Normal, and Deep each map to bounded output tokens, tool
// calls, execution time, retries, verification strength, and retrieval depth.
//
// Effort is not just a token count. It governs the whole agent: how much is
// retrieved, how many tools may run, how hard the result is verified, and
// whether cloud escalation is permitted.
package effort

import (
	"regexp"
	"strings"
)

// Level is the public reasoning-effort tier.
type Level string

const (
	Quick  Level = "quick"
	Normal Level = "normal"
	Deep   Level = "deep"
)

// VerificationLevel is how strongly a result is checked before it is trusted.
type VerificationLevel string

const (
	VerifyNone     VerificationLevel = "none"
	VerifyBasic    VerificationLevel = "basic"
	VerifyStandard VerificationLevel = "standard"
	VerifyStrict   VerificationLevel = "strict"
)

// Budget is the concrete limits one turn runs under. Every field is enforced
// by a caller; nothing here is decorative.
type Budget struct {
	Level                Level
	MaxOutputTokens      int
	MaxToolCalls         int
	MaxExecutionMs       int
	MaxRetries           int
	Verification         VerificationLevel
	MemoryDepth          int // retrieval passes / candidate breadth
	AllowCloudEscalation bool
}

// Policy maps a level to its budget. Numbers are generous: a capped turn
// that stops mid-research produces a worse outcome than the tokens it
// saves. The loop-level iteration ceiling remains the backstop against
// runaway turns; these budgets make sure legitimate work finishes.
func Policy(l Level) Budget {
	switch l {
	case Quick:
		return Budget{Level: Quick, MaxOutputTokens: 2000, MaxToolCalls: 6,
			MaxExecutionMs: 15000, MaxRetries: 0, Verification: VerifyBasic,
			MemoryDepth: 1, AllowCloudEscalation: false}
	case Deep:
		return Budget{Level: Deep, MaxOutputTokens: 8000, MaxToolCalls: 40,
			MaxExecutionMs: 120000, MaxRetries: 2, Verification: VerifyStrict,
			MemoryDepth: 3, AllowCloudEscalation: true}
	default:
		return Budget{Level: Normal, MaxOutputTokens: 4000, MaxToolCalls: 30,
			MaxExecutionMs: 45000, MaxRetries: 1, Verification: VerifyStandard,
			MemoryDepth: 2, AllowCloudEscalation: true}
	}
}

// Ladder is the internal complexity rung used for routing (0 reflex … 5 cloud).
type Ladder int

const (
	LadderReflex Ladder = iota
	LadderSimpleLocal
	LadderToolAssisted
	LadderMultiStep
	LadderDeepReasoning
	LadderCloud
)

// LadderFor maps a level to its default complexity rung.
func LadderFor(l Level) Ladder {
	switch l {
	case Quick:
		return LadderSimpleLocal
	case Deep:
		return LadderDeepReasoning
	default:
		return LadderToolAssisted
	}
}

// Signals are the deterministic complexity inputs. They are cheap to compute
// and inspectable; a model-based classifier is deliberately not used until
// there is telemetry to justify it.
type Signals struct {
	Length            int
	ToolHints         int
	MultiStep         bool
	CodeOrDebug       bool
	Ambiguous         bool
	MemoryDep         bool
	Research          bool
	Urgent            bool
	PriorFailures     int
	VerificationFails int
}

var (
	multiStepRE = regexp.MustCompile(`\b(then|after that|step by step|first .* then|and also|followed by|sequence|workflow)\b`)
	codeRE      = regexp.MustCompile("```|\\b(func|compile|debug|stack trace|refactor|regex|sql|bug|error:|exception)\\b")
	ambiguousRE = regexp.MustCompile(`\b(somehow|maybe|not sure|whatever|etc\.?|and so on|something like)\b`)
	memoryRE    = regexp.MustCompile(`\b(remember|recall|previously|last time|we discussed|i told you|my (preference|schedule|routine))\b`)
	// Research asks need room to search, fetch, and verify — they must
	// never be starved by the Quick tier. Kept narrow so greetings and
	// clock questions still stay cheap.
	researchRE = regexp.MustCompile(`\b(latest|breaking|trending|research|look for|look up|find out|dig into|compare|news (on|about)|price of|what .* costs?)\b`)
	urgentRE   = regexp.MustCompile(`\b(now|asap|urgent|immediately|right away)\b`)
	toolRE     = regexp.MustCompile(`\b(schedule|remind|calendar|email|send|create|delete|move|rename|download|upload|browse|open|run|install|summarize|summary|save|write|edit|translate|analyze|plan|organize|search|look up|generate|draft)\b`)
)

// Analyze extracts deterministic signals from a raw message.
func Analyze(msg string) Signals {
	m := strings.ToLower(msg)
	return Signals{
		Length:      len([]rune(msg)),
		ToolHints:   countMatches(m, toolRE),
		MultiStep:   multiStepRE.MatchString(m),
		CodeOrDebug: codeRE.MatchString(m),
		Ambiguous:   ambiguousRE.MatchString(m),
		MemoryDep:   memoryRE.MatchString(m),
		Research:    researchRE.MatchString(m),
		Urgent:      urgentRE.MatchString(m),
	}
}

// ClassifySignals applies the ladder to signals. It is conservative: it only
// escalates on clear, multiple signals, so ordinary requests stay Normal.
func ClassifySignals(s Signals) Level {
	score := 0
	if s.MultiStep {
		score += 2
	}
	if s.CodeOrDebug {
		score += 2
	}
	if s.Ambiguous {
		score += 1
	}
	if s.MemoryDep {
		score += 1
	}
	if s.Research {
		score += 2
	}
	if s.ToolHints >= 2 {
		score += 1
	}
	if s.Length > 1200 {
		score += 1
	}
	if s.PriorFailures > 0 {
		score += 1
	}
	if s.VerificationFails > 0 {
		score += 1
	}

	// Trivial: short, no tools, no multi-step, no ambiguity.
	if score == 0 && s.Length > 0 && s.Length <= 160 && s.ToolHints == 0 && !s.MemoryDep {
		return Quick
	}
	if score >= 4 {
		return Deep
	}
	return Normal
}

// Classify analyzes and classifies in one call.
func Classify(msg string) Level { return ClassifySignals(Analyze(msg)) }

// Escalate moves one rung deeper, reporting whether a deeper level exists.
// Every escalation consumes from a budget; there is no infinite climb.
func Escalate(l Level) (Level, bool) {
	switch l {
	case Quick:
		return Normal, true
	case Normal:
		return Deep, true
	default:
		return Deep, false
	}
}

// EscalateForFailure returns the level to use after a failure signal (failed
// verification, repeated tool error), bounded at Deep.
func EscalateForFailure(l Level) Level {
	if next, ok := Escalate(l); ok {
		return next
	}
	return l
}

func countMatches(s string, re *regexp.Regexp) int {
	return len(re.FindAllStringIndex(s, -1))
}
