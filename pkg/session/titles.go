package session

import (
	"strings"
	"unicode"
)

// GenerateTitle produces a deterministic, human-readable title from the first
// user message in a conversation. It never calls an LLM — the title is a pure
// function of the message text. The title is capped at 60 characters and
// truncated on a word boundary if needed.
func GenerateTitle(firstUserMessage string) string {
	text := strings.TrimSpace(firstUserMessage)
	if text == "" {
		return "New conversation"
	}

	// Strip common prefixes people use when starting a chat.
	for _, prefix := range []string{
		"hey ghost, ", "hey ghost,  ", "hey ghost! ", "hey ghost!  ",
		"hi ghost, ", "hi ghost,  ", "hi ghost! ", "hi ghost!  ",
		"hello ghost, ", "hello ghost,  ", "hello ghost! ", "hello ghost!  ",
		"hey ", "hi ", "hello ",
	} {
		if strings.HasPrefix(strings.ToLower(text), prefix) {
			text = strings.TrimSpace(text[len(prefix):])
			break
		}
	}

	if text == "" {
		return "New conversation"
	}

	// Capitalize the first letter.
	text = strings.ToUpper(text[:1]) + text[1:]

	// Strip trailing punctuation for a cleaner title.
	text = strings.TrimRight(text, ".!?,;:")

	// Truncate to max 60 characters on a word boundary.
	const maxLen = 60
	if len(text) > maxLen {
		text = text[:maxLen]
		// Find the last space to avoid cutting mid-word.
		if i := strings.LastIndex(text, " "); i > 20 {
			text = text[:i]
		}
		// Clean up trailing punctuation after truncation.
		text = strings.TrimRight(text, " ")
	}

	return text
}

// GenerateSummary produces a short, factual summary of a conversation from its
// message history. It never calls an LLM — the summary is a deterministic
// function of the messages. The summary captures the topic and outcome in under
// 80 characters.
func GenerateSummary(messages []messageForSummary) string {
	if len(messages) == 0 {
		return ""
	}

	// Find the first user message and last meaningful response.
	var firstUser, lastAssistant string
	for _, m := range messages {
		if m.Role == "user" && firstUser == "" {
			firstUser = cleanMessage(m.Content)
		}
		if m.Role == "assistant" {
			lastAssistant = cleanMessage(m.Content)
		}
	}

	if firstUser == "" {
		return ""
	}

	// Build a concise summary: topic + outcome.
	topic := firstUser
	if len(topic) > 50 {
		topic = topic[:50]
		if i := strings.LastIndex(topic, " "); i > 10 {
			topic = topic[:i]
		}
	}

	if lastAssistant == "" {
		return topic
	}

	// Extract the key action or answer from the response.
	outcome := extractOutcome(lastAssistant)
	if outcome == "" {
		return topic
	}

	summary := topic + " — " + outcome
	if len(summary) > 120 {
		summary = summary[:120]
		if i := strings.LastIndex(summary, " "); i > 20 {
			summary = summary[:i]
		}
	}

	return summary
}

type messageForSummary struct {
	Role    string
	Content string
}

func cleanMessage(content string) string {
	// Take the first paragraph or first 200 chars.
	text := strings.TrimSpace(content)
	if i := strings.Index(text, "\n\n"); i > 0 {
		text = text[:i]
	}
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

func extractOutcome(response string) string {
	// Look for sentences that indicate an answer or action.
	sentences := strings.Split(response, ". ")
	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if len(s) < 10 {
			continue
		}
		// Capitalize first letter.
		s = strings.ToUpper(s[:1]) + s[1:]
		// Trim to reasonable length.
		if len(s) > 80 {
			s = s[:80]
			if i := strings.LastIndex(s, " "); i > 20 {
				s = s[:i]
			}
		}
		return strings.TrimRight(s, ".,;:!")
	}
	return ""
}

// IsEmptyOrWhitespace returns true if s is empty or contains only whitespace.
func IsEmptyOrWhitespace(s string) bool {
	return strings.TrimSpace(s) == ""
}

// TruncateTitle truncates a title to fit the given maximum length, preserving
// word boundaries and appending an ellipsis if truncated.
func TruncateTitle(title string, maxLen int) string {
	if len(title) <= maxLen {
		return title
	}
	title = title[:maxLen-1]
	if i := strings.LastIndex(title, " "); i > 20 {
		title = title[:i]
	}
	return strings.TrimRight(title, " ") + "\u2026"
}

// FormatSessionTitle returns a display-ready title for a session, falling back
// to a default if the stored title is empty.
func FormatSessionTitle(storedTitle string) string {
	if storedTitle == "" {
		return "New conversation"
	}
	return storedTitle
}

// IsTitlePunctuated checks if a string ends with punctuation that should be
// stripped from titles.
func IsTitlePunctuated(s string) bool {
	if s == "" {
		return false
	}
	r := rune(s[len(s)-1])
	return unicode.IsPunct(r)
}

// ShouldUpdateTitle determines if a title should be updated based on message
// count and conversation state. Titles should be stable and not change on
// every message. Update conditions:
// - Initial title (no existing title)
// - After 5 messages (first meaningful evolution)
// - Every 10 messages after that (gradual refinement)
// - Only if the new title is meaningfully different
func ShouldUpdateTitle(existingTitle string, messageCount int) bool {
	if existingTitle == "" || existingTitle == "New conversation" {
		return true
	}
	if messageCount == 5 {
		return true
	}
	if messageCount > 10 && messageCount%10 == 0 {
		return true
	}
	return false
}

// IsMeaningfulTitleChange checks if a new title is meaningfully different
// from the existing one. Minor rephrasings or punctuation changes are not
// meaningful enough to warrant an update.
func IsMeaningfulTitleChange(oldTitle, newTitle string) bool {
	if oldTitle == newTitle {
		return false
	}
	// Normalize for comparison
	normalize := func(s string) string {
		s = strings.ToLower(s)
		s = strings.TrimRight(s, ".!?,;:")
		s = strings.TrimSpace(s)
		return s
	}
	oldNorm := normalize(oldTitle)
	newNorm := normalize(newTitle)
	
	// Same after normalization
	if oldNorm == newNorm {
		return false
	}
	
	// Check if one is just a prefix of the other (allow up to 40% longer)
	if strings.HasPrefix(newNorm, oldNorm) {
		diff := len(newNorm) - len(oldNorm)
		if float64(diff)/float64(len(oldNorm)) < 0.4 {
			return false
		}
	}
	if strings.HasPrefix(oldNorm, newNorm) {
		diff := len(oldNorm) - len(newNorm)
		if float64(diff)/float64(len(newNorm)) < 0.4 {
			return false
		}
	}
	
	// Check word overlap
	oldWords := strings.Fields(oldNorm)
	newWords := strings.Fields(newNorm)
	
	// If most words are the same, not meaningful
	overlap := 0
	for _, w := range oldWords {
		for _, nw := range newWords {
			if w == nw {
				overlap++
				break
			}
		}
	}
	
	// If 70%+ words overlap, not meaningful
	maxWords := len(oldWords)
	if len(newWords) > maxWords {
		maxWords = len(newWords)
	}
	if maxWords > 0 && float64(overlap)/float64(maxWords) > 0.7 {
		return false
	}
	
	return true
}

// EvolveTitle attempts to evolve a conversation title based on recent messages.
// This is a deterministic function that looks for topic shifts or meaningful
// additions to the conversation.
func EvolveTitle(currentTitle string, messages []messageForSummary) string {
	if len(messages) < 3 {
		return currentTitle
	}
	
	// Extract key topics from recent messages
	recentTopics := extractRecentTopics(messages)
	if len(recentTopics) == 0 {
		return currentTitle
	}
	
	// Look for the main topic shift (not just the last message)
	// Find the first user message that introduced a new topic
	for _, topic := range recentTopics {
		if !IsTopicRelated(currentTitle, topic) {
			// Topic has shifted, update title
			newTitle := GenerateTitle(topic)
			if IsMeaningfulTitleChange(currentTitle, newTitle) {
				return newTitle
			}
		}
	}
	
	return currentTitle
}

// extractRecentTopics extracts key topics from recent messages.
func extractRecentTopics(messages []messageForSummary) []string {
	var topics []string
	
	// Look at last 3-5 user messages for topic extraction
	userMessages := 0
	for i := len(messages) - 1; i >= 0 && userMessages < 5; i-- {
		if messages[i].Role == "user" {
			text := cleanMessage(messages[i].Content)
			if text != "" {
				// Prepend to maintain chronological order
				topics = append([]string{text}, topics...)
			}
			userMessages++
		}
	}
	
	return topics
}

// IsTopicRelated checks if a topic is related to the current title.
// This is a simple heuristic based on keyword matching.
func IsTopicRelated(title, topic string) bool {
	titleWords := strings.Fields(strings.ToLower(title))
	topicWords := strings.Fields(strings.ToLower(topic))
	
	// Common words to ignore
	commonWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "shall": true, "can": true,
		"to": true, "of": true, "in": true, "for": true, "on": true,
		"with": true, "at": true, "by": true, "from": true, "as": true,
		"i": true, "me": true, "my": true, "you": true, "your": true,
		"it": true, "its": true, "this": true, "that": true, "what": true,
		"how": true, "when": true, "where": true, "why": true, "which": true,
		"who": true, "whom": true, "help": true, "please": true, "want": true,
		"like": true, "prefer": true, "need": true, "going": true, "doing": true,
	}
	
	// Extract meaningful words from title
	titleMeaningful := make(map[string]bool)
	for _, w := range titleWords {
		if !commonWords[w] && len(w) > 2 {
			titleMeaningful[w] = true
		}
	}
	
	// Extract meaningful words from topic
	topicMeaningful := make(map[string]bool)
	for _, w := range topicWords {
		if !commonWords[w] && len(w) > 2 {
			topicMeaningful[w] = true
		}
	}
	
	// If either has no meaningful words, consider related
	if len(titleMeaningful) == 0 || len(topicMeaningful) == 0 {
		return true
	}
	
	// Check for overlap
	overlap := 0
	for w := range topicMeaningful {
		if titleMeaningful[w] {
			overlap++
		}
	}
	
	// Check for semantic similarity via common prefixes/stems
	prefixOverlap := 0
	for tw := range titleMeaningful {
		for tpw := range topicMeaningful {
			if len(tw) > 3 && len(tpw) > 3 {
				// Check if words share a common stem (first 4 chars)
				if tw[:4] == tpw[:4] {
					prefixOverlap++
					break
				}
			}
		}
	}
	
	totalOverlap := overlap + prefixOverlap
	
	// Calculate similarity
	totalMeaningful := len(titleMeaningful) + len(topicMeaningful) - overlap
	if totalMeaningful == 0 {
		return true
	}
	
	similarity := float64(totalOverlap) / float64(totalMeaningful)
	return similarity > 0.15 // 15% overlap means related
}
