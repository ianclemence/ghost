package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ResultBudget struct {
	MaxSummaryChars int
	HeadPercent     int
	TailPercent     int
	SpillDir        string
}

func NewResultBudget(workspace string) *ResultBudget {
	spillDir := filepath.Join(workspace, "media", "delegation")
	os.MkdirAll(spillDir, 0755)

	return &ResultBudget{
		MaxSummaryChars: 24000,
		HeadPercent:     75,
		TailPercent:     25,
		SpillDir:        spillDir,
	}
}

func (b *ResultBudget) TrimResult(summary string, budget int) (string, *string) {
	if budget <= 0 {
		budget = b.MaxSummaryChars
	}

	if len(summary) <= budget {
		return summary, nil
	}

	headSize := budget * b.HeadPercent / 100
	tailSize := budget * b.TailPercent / 100

	if headSize < 100 {
		headSize = 100
	}
	if tailSize < 100 {
		tailSize = 100
	}

	head := summary[:headSize]
	tail := summary[len(summary)-tailSize:]

	fullPath := b.spillToFile(summary)

	trimmed := fmt.Sprintf(
		"%s\n\n[... %d characters omitted, full text saved to %s ...]\n\n%s",
		head,
		len(summary)-headSize-tailSize,
		fullPath,
		tail,
	)

	return trimmed, &fullPath
}

func (b *ResultBudget) spillToFile(content string) string {
	timestamp := fmt.Sprintf("%d", os.Getpid())
	filename := fmt.Sprintf("result-%s.txt", timestamp)
	fullPath := filepath.Join(b.SpillDir, filename)

	os.WriteFile(fullPath, []byte(content), 0644)
	return fullPath
}

func (b *ResultBudget) FormatForLLM(trimmed string, spillPath *string) string {
	if spillPath == nil {
		return trimmed
	}

	return fmt.Sprintf(
		"%s\n\n[Note: Full result available at %s. Use read_file to access the complete output if needed.]",
		trimmed,
		*spillPath,
	)
}

func (b *ResultBudget) EstimateTokens(text string) int {
	return len(text) / 4
}

func (b *ResultBudget) WithinBudget(text string, budget int) bool {
	return len(text) <= budget
}

func (b *ResultBudget) Summary(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}

	truncated := text[:maxLen]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxLen-100 {
		truncated = truncated[:lastSpace]
	}

	return truncated + "..."
}
