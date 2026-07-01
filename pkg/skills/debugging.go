package skills

import (
	"fmt"
	"strings"
)

type DebuggingSkill struct{}

type DebugSession struct {
	Issue       string
	Phase       string
	Evidence    []string
	Hypotheses  []Hypothesis
	RootCause   string
	Fix         string
	Tests       []string
}

type Hypothesis struct {
	Description string
	Confidence  float64
	Status      string
	Evidence    []string
}

func NewDebuggingSkill() *DebuggingSkill {
	return &DebuggingSkill{}
}

func (s *DebuggingSkill) Name() string {
	return "debugging"
}

func (s *DebuggingSkill) Description() string {
	return "Systematic root cause debugging with 4 phases: investigation, pattern analysis, hypothesis testing, and implementation."
}

func (s *DebuggingSkill) StartDebug(issue string) *DebugSession {
	return &DebugSession{
		Issue:  issue,
		Phase:  "investigation",
		Evidence: []string{},
		Hypotheses: []Hypothesis{},
	}
}

func (s *DebuggingSkill) AddEvidence(session *DebugSession, evidence string) {
	session.Evidence = append(session.Evidence, evidence)
}

func (s *DebuggingSkill) AddHypothesis(session *DebugSession, description string, confidence float64) {
	session.Hypotheses = append(session.Hypotheses, Hypothesis{
		Description: description,
		Confidence:  confidence,
		Status:      "pending",
		Evidence:    []string{},
	})
}

func (s *DebuggingSkill) UpdatePhase(session *DebugSession, phase string) error {
	validPhases := map[string]bool{
		"investigation":     true,
		"pattern_analysis":  true,
		"hypothesis_testing": true,
		"implementation":    true,
	}

	if !validPhases[phase] {
		return fmt.Errorf("invalid phase: %s", phase)
	}

	session.Phase = phase
	return nil
}

func (s *DebuggingSkill) SetRootCause(session *DebugSession, rootCause string) {
	session.RootCause = rootCause
	session.Phase = "implementation"
}

func (s *DebuggingSkill) SetFix(session *DebugSession, fix string) {
	session.Fix = fix
}

func (s *DebuggingSkill) AddTest(session *DebugSession, test string) {
	session.Tests = append(session.Tests, test)
}

func (s *DebuggingSkill) FormatAsReport(session *DebugSession) string {
	var sb strings.Builder

	sb.WriteString("# Debug Report\n\n")
	sb.WriteString(fmt.Sprintf("## Issue\n%s\n\n", session.Issue))

	sb.WriteString(fmt.Sprintf("## Current Phase: %s\n\n", session.Phase))

	if len(session.Evidence) > 0 {
		sb.WriteString("## Evidence\n\n")
		for _, e := range session.Evidence {
			sb.WriteString(fmt.Sprintf("- %s\n", e))
		}
		sb.WriteString("\n")
	}

	if len(session.Hypotheses) > 0 {
		sb.WriteString("## Hypotheses\n\n")
		for _, h := range session.Hypotheses {
			status := "[ ]"
			if h.Status == "confirmed" {
				status = "[x]"
			} else if h.Status == "rejected" {
				status = "[-]"
			}
			sb.WriteString(fmt.Sprintf("%s %s (confidence: %.0f%%)\n", status, h.Description, h.Confidence*100))
			if len(h.Evidence) > 0 {
				for _, e := range h.Evidence {
					sb.WriteString(fmt.Sprintf("    - %s\n", e))
				}
			}
		}
		sb.WriteString("\n")
	}

	if session.RootCause != "" {
		sb.WriteString(fmt.Sprintf("## Root Cause\n%s\n\n", session.RootCause))
	}

	if session.Fix != "" {
		sb.WriteString(fmt.Sprintf("## Fix\n%s\n\n", session.Fix))
	}

	if len(session.Tests) > 0 {
		sb.WriteString("## Regression Tests\n\n")
		for _, t := range session.Tests {
			sb.WriteString(fmt.Sprintf("- %s\n", t))
		}
	}

	return sb.String()
}

func (s *DebuggingSkill) GetTopHypothesis(session *DebugSession) *Hypothesis {
	var best *Hypothesis
	for i := range session.Hypotheses {
		h := &session.Hypotheses[i]
		if h.Status == "pending" {
			if best == nil || h.Confidence > best.Confidence {
				best = h
			}
		}
	}
	return best
}

func (s *DebuggingSkill) ValidateSession(session *DebugSession) []string {
	var issues []string

	if session.Issue == "" {
		issues = append(issues, "Issue description is required")
	}

	if session.Phase == "" {
		issues = append(issues, "Phase is required")
	}

	if session.Phase == "implementation" && session.RootCause == "" {
		issues = append(issues, "Root cause is required before implementation")
	}

	return issues
}
