package agent

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/session"
)

type NudgeConfig struct {
	Enabled         bool `json:"enabled"`
	MemoryInterval  int  `json:"memory_interval"`
	SkillInterval   int  `json:"skill_interval"`
}

type NudgeManager struct {
	config        NudgeConfig
	turnCount     int32
	toolIterCount int32
	sessions      *session.SessionManager
}

func NewNudgeManager(cfg NudgeConfig, sessions *session.SessionManager) *NudgeManager {
	return &NudgeManager{
		config:   cfg,
		sessions: sessions,
	}
}

func (nm *NudgeManager) OnUserTurn(sessionKey string) {
	if !nm.config.Enabled || nm.config.MemoryInterval <= 0 {
		return
	}
	atomic.AddInt32(&nm.turnCount, 1)
}

func (nm *NudgeManager) OnToolIteration(sessionKey string) {
	if !nm.config.Enabled || nm.config.SkillInterval <= 0 {
		return
	}
	atomic.AddInt32(&nm.toolIterCount, 1)
}

func (nm *NudgeManager) OnMemoryToolUsed(sessionKey string) {
	atomic.StoreInt32(&nm.turnCount, 0)
}

func (nm *NudgeManager) OnSkillToolUsed(sessionKey string) {
	atomic.StoreInt32(&nm.toolIterCount, 0)
}

func (nm *NudgeManager) ShouldReviewMemory() bool {
	if !nm.config.Enabled || nm.config.MemoryInterval <= 0 {
		return false
	}
	turns := atomic.LoadInt32(&nm.turnCount)
	if turns >= int32(nm.config.MemoryInterval) {
		atomic.StoreInt32(&nm.turnCount, 0)
		return true
	}
	return false
}

func (nm *NudgeManager) ShouldReviewSkills() bool {
	if !nm.config.Enabled || nm.config.SkillInterval <= 0 {
		return false
	}
	iters := atomic.LoadInt32(&nm.toolIterCount)
	if iters >= int32(nm.config.SkillInterval) {
		atomic.StoreInt32(&nm.toolIterCount, 0)
		return true
	}
	return false
}

func (nm *NudgeManager) BuildMemoryPrompt(history []providers.Message) string {
	var topics []string
	seen := make(map[string]bool)
	for _, msg := range history {
		if msg.Role == "user" {
			words := strings.Fields(msg.Content)
			for _, w := range words {
				w = strings.ToLower(strings.Trim(w, ".,!?;:"))
				if len(w) > 4 && !seen[w] {
					seen[w] = true
					topics = append(topics, w)
				}
			}
		}
		if len(topics) >= 10 {
			break
		}
	}
	if len(topics) == 0 {
		return ""
	}
	prompt := fmt.Sprintf(
		"[Memory Review] Consider what should be remembered from this conversation. Key topics discussed: %s. If any insights, preferences, or decisions should be persisted, use the remember tool.",
		strings.Join(topics, ", "),
	)
	return prompt
}

func (nm *NudgeManager) BuildSkillPrompt(toolsUsed []string) string {
	if len(toolsUsed) == 0 {
		return ""
	}
	toolCounts := make(map[string]int)
	for _, t := range toolsUsed {
		toolCounts[t]++
	}
	var repeated []string
	for tool, count := range toolCounts {
		if count >= 3 {
			repeated = append(repeated, fmt.Sprintf("%s(%d times)", tool, count))
		}
	}
	if len(repeated) == 0 {
		return ""
	}
	prompt := fmt.Sprintf(
		"[Skill Creation Nudge] You've been using these tools repeatedly: %s. Consider if a reusable skill or automation would be valuable here.",
		strings.Join(repeated, ", "),
	)
	return prompt
}

func (nm *NudgeManager) GetTurnCount() int32 {
	return atomic.LoadInt32(&nm.turnCount)
}

func (nm *NudgeManager) GetToolIterCount() int32 {
	return atomic.LoadInt32(&nm.toolIterCount)
}

func (nm *NudgeManager) Reset() {
	atomic.StoreInt32(&nm.turnCount, 0)
	atomic.StoreInt32(&nm.toolIterCount, 0)
}
