package personalcontext

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
)

// SemanticExtractor uses the LLM to extract memories from natural language.
// It preserves the fast deterministic regex path and falls back to semantic
// extraction when regex doesn't find anything.
type SemanticExtractor struct {
	provider providers.LLMProvider
	model    string
}

// NewSemanticExtractor creates a new SemanticExtractor.
func NewSemanticExtractor(provider providers.LLMProvider, model string) *SemanticExtractor {
	if model == "" {
		model = provider.GetDefaultModel()
	}
	return &SemanticExtractor{
		provider: provider,
		model:    model,
	}
}

// ExtractResult is the result of semantic extraction.
type ExtractResult struct {
	ShouldRemember bool
	Entries        []Entry
	Reason         string
}

// Extract performs semantic extraction of a user message.
// It first tries the fast regex path, then falls back to LLM-based extraction.
func (se *SemanticExtractor) Extract(ctx context.Context, text string, existing []Entry) ExtractResult {
	// Skip extraction for short messages - they don't contain durable personal information
	lower := strings.ToLower(strings.TrimSpace(text))
	if len(lower) < 15 {
		return ExtractResult{
			ShouldRemember: false,
			Reason:         "too_short",
		}
	}

	// Skip extraction for questions - they don't contain durable personal information
	if strings.HasSuffix(lower, "?") || strings.HasPrefix(lower, "what") || strings.HasPrefix(lower, "how") || strings.HasPrefix(lower, "why") || strings.HasPrefix(lower, "when") || strings.HasPrefix(lower, "where") || strings.HasPrefix(lower, "who") || strings.HasPrefix(lower, "can") || strings.HasPrefix(lower, "do") || strings.HasPrefix(lower, "is") || strings.HasPrefix(lower, "are") || strings.HasPrefix(lower, "could") || strings.HasPrefix(lower, "would") || strings.HasPrefix(lower, "should") {
		return ExtractResult{
			ShouldRemember: false,
			Reason:         "question",
		}
	}

	// Skip extraction for commands and instructions
	if strings.HasPrefix(lower, "run ") || strings.HasPrefix(lower, "execute ") || strings.HasPrefix(lower, "create ") || strings.HasPrefix(lower, "delete ") || strings.HasPrefix(lower, "remove ") || strings.HasPrefix(lower, "add ") || strings.HasPrefix(lower, "update ") || strings.HasPrefix(lower, "set ") || strings.HasPrefix(lower, "get ") || strings.HasPrefix(lower, "show ") || strings.HasPrefix(lower, "list ") || strings.HasPrefix(lower, "find ") || strings.HasPrefix(lower, "search ") {
		return ExtractResult{
			ShouldRemember: false,
			Reason:         "command",
		}
	}

	// Fast path: try regex extraction first
	actions, err := Extract(Input{
		Text:    text,
		Current: existing,
	})
	if err == nil && len(actions) > 0 {
		// Regex found something, use that with high confidence
		entries := make([]Entry, 0, len(actions))
		for _, action := range actions {
			entries = append(entries, action.Entry)
		}
		return ExtractResult{
			ShouldRemember: true,
			Entries:        entries,
			Reason:         "regex_extraction",
		}
	}

	// Slow path: use LLM for semantic extraction
	result, err := se.extractWithLLM(ctx, text, existing)
	if err != nil {
		// Malformed JSON after bounded retries is an observable extraction
		// failure, never a silently saved memory. Keep the precise reason
		// (parse_error) so callers can log it distinctly from a provider
		// outage (llm_unavailable).
		logger.WarnCF("personalcontext", "semantic extraction produced malformed output", map[string]interface{}{
			"error":  err.Error(),
			"reason": result.Reason,
		})
		if result.Reason == "" {
			result.Reason = "parse_error"
		}
		return result
	}

	return result
}

// extractionAttempts bounds how many times the LLM classifier is asked for
// one message. Retries are deterministic (a fixed number, no backoff loops)
// and only triggered by a malformed JSON parse, never by content.
const extractionAttempts = 2

// retryInstruction is appended on the retry attempt when the first response
// was not strict JSON. It steers the model back to a single object without
// changing what is being asked.
const retryInstruction = "\n\nYour previous response was not valid JSON. Respond with ONLY a single JSON object matching the requested schema. No markdown, no prose, no extra text."

// extractWithLLM uses the LLM to extract memories from natural language.
func (se *SemanticExtractor) extractWithLLM(ctx context.Context, text string, existing []Entry) (ExtractResult, error) {
	prompt := se.buildExtractionPrompt(text, existing)

	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= extractionAttempts; attempt++ {
		user := prompt
		if attempt > 1 {
			user += retryInstruction
		}
		messages := []providers.Message{
			{Role: "system", Content: extractionSystemPrompt},
			{Role: "user", Content: user},
		}
		resp, err := se.provider.Chat(callCtx, messages, nil, se.model, map[string]interface{}{
			"temperature": 0.0,
			"max_tokens":  300,
		})
		if err != nil {
			// LLM call failed - this is expected in tests with mock providers.
			// A provider outage is not a parse problem; retrying the same
			// failing endpoint is pointless, so we surface it once.
			return ExtractResult{
				ShouldRemember: false,
				Reason:         "llm_unavailable",
			}, nil
		}

		// Parse the LLM response.
		output, perr := ParseClassificationOutput(resp.Content)
		if perr != nil {
			lastErr = perr
			continue // bounded, deterministic retry on malformed JSON only
		}

		// Validate against controlled vocabulary.
		validated := ValidateClassification(output)
		if !validated.Valid || !validated.ShouldRemember {
			return ExtractResult{
				ShouldRemember: false,
				Reason:         validated.Reason,
			}, nil
		}

		// Build entry from validated classification. This is model
		// inference, not user testimony: the promotion policy decides
		// whether it may become current or must be held as a candidate.
		entry := Entry{
			ID:         generateSemanticID(),
			Kind:       Kind(validated.Kind),
			Subject:    "user",
			Predicate:  buildPredicate(string(validated.Kind), string(validated.Domain), text),
			Value:      json.RawMessage(fmt.Sprintf("%q", extractValueFromText(text))),
			Status:     StatusCurrent,
			Confidence: validated.Confidence,
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
			Sources: []Source{
				{
					Type:      SourceConversation,
					Kind:      "inferred",
					Timestamp: time.Now().UTC(),
				},
			},
		}
		if d := DefaultPromotionPolicy().Evaluate(entry); !d.Promote {
			entry.Status = d.Status
		}

		return ExtractResult{
			ShouldRemember: true,
			Entries:        []Entry{entry},
			Reason:         "semantic_extraction",
		}, nil
	}

	// Both attempts produced malformed JSON. This is a genuine extraction
	// failure: observable via the reason, and NEVER reported as a saved
	// memory. Ghost does not claim persistence it does not have.
	return ExtractResult{
		ShouldRemember: false,
		Reason:         "parse_error",
	}, lastErr
}

// buildExtractionPrompt builds the prompt for LLM extraction.
func (se *SemanticExtractor) buildExtractionPrompt(text string, existing []Entry) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Message to classify: %q\n\n", text))

	if len(existing) > 0 {
		sb.WriteString("Current memories about this user (most recent first, capped):\n")
		shown := 0
		// Bound the prompt so the classifier's completion never runs out of
		// tokens mid-object (a real truncation driver of parse_error).
		for i := len(existing) - 1; i >= 0 && shown < 30; i-- {
			e := existing[i]
			if e.Status == StatusCurrent {
				sb.WriteString(fmt.Sprintf("- %s: %s (kind: %s)\n", e.Predicate, string(e.Value), e.Kind))
				shown++
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Determine if this message contains something durable worth remembering about the user.")
	return sb.String()
}

// extractionSystemPrompt is the system prompt for semantic extraction.
const extractionSystemPrompt = `You are a memory extractor for Ghost, a personal AI assistant.

Analyze the user message and return a JSON object with these fields:
- should_remember: true if the message contains durable personal information, false otherwise
- kind: one of "identity", "preference", "fact", "goal", "relationship", "routine", "decision", "consent", "project", "constraint", "interest"
- domain: one of "identity", "food", "location", "work", "family", "health", "finance", "technology", "travel", "lifestyle", "communication", "education", "entertainment", "relationship", "other"
- confidence: a number between 0 and 1

ONLY remember explicit, persistent information:
- Identity: "My name is X", "I am X years old"
- Preferences: "I prefer X", "I like X", "My favorite X is Y"
- Facts: "I live in X", "I work at Y", "I am building Z"
- Goals: "My goal is to X", "I want to launch Y"
- Relationships: "Sarah is my wife", "I work with John"

Do NOT remember:
- Transient requests: "What's the weather?"
- Questions about the world
- Temporary context: "I'm going to the store", "I'm eating pizza tonight"
- Emotional states: "I'm happy today"

If unsure, set should_remember to false.

Respond with ONLY a JSON object, no explanation:
{"should_remember": true, "kind": "preference", "domain": "food", "confidence": 0.9}`

// generateSemanticID generates a unique ID for semantic extractions.
func generateSemanticID() string {
	return fmt.Sprintf("sem_%d", time.Now().UnixNano())
}

// buildPredicate builds a predicate from kind, domain, and text.
func buildPredicate(kind, domain, text string) string {
	lower := strings.ToLower(text)

	switch Kind(kind) {
	case KindIdentity:
		if strings.Contains(lower, "name") {
			return "identity/name"
		}
		return "identity/general"
	case KindPreference:
		// Check for specific favorite patterns
		if strings.Contains(lower, "favorite food") {
			return "preference/favorite_food"
		}
		if strings.Contains(lower, "favorite drink") {
			return "preference/favorite_drink"
		}
		if strings.Contains(lower, "favorite show") || strings.Contains(lower, "favorite movie") {
			return "preference/favorite_show"
		}
		if strings.Contains(lower, "favorite book") {
			return "preference/favorite_book"
		}
		if strings.Contains(lower, "favorite song") || strings.Contains(lower, "favorite music") {
			return "preference/favorite_song"
		}
		if strings.Contains(lower, "favorite place") {
			return "preference/favorite_place"
		}
		if strings.Contains(lower, "favorite") {
			return "preference/favorite"
		}
		if strings.Contains(lower, "prefer") {
			return "preference/prefers"
		}
		if strings.Contains(lower, "like") {
			return "preference/likes"
		}
		return "preference/general"
	case KindFact:
		if strings.Contains(lower, "live") {
			return "fact/location"
		}
		if strings.Contains(lower, "work") {
			return "fact/work"
		}
		return "fact/general"
	case KindGoal:
		return "goal/primary"
	case KindRelationship:
		if strings.Contains(lower, "partner") || strings.Contains(lower, "wife") || strings.Contains(lower, "husband") {
			return "relationship/partner"
		}
		if strings.Contains(lower, "work with") {
			return "relationship/colleague"
		}
		return "relationship/general"
	case KindProject:
		return "project/current"
	default:
		return fmt.Sprintf("%s/%s", kind, domain)
	}
}

// extractValueFromText extracts the value from the text.
func extractValueFromText(text string) string {
	lower := strings.ToLower(text)

	// Try to extract after common patterns
	patterns := []string{
		"my name is ",
		"i live in ",
		"i work at ",
		"i work for ",
		"i work as ",
		"i prefer ",
		"i like ",
		"is my favorite ",
		"my favorite ",
		"my goal is to ",
		"i want to ",
		"is my ",
		"are my ",
	}

	for _, pattern := range patterns {
		if idx := strings.Index(lower, pattern); idx >= 0 {
			// For "is my favorite X" patterns, extract the subject
			if strings.HasSuffix(pattern, "is my favorite ") || strings.HasSuffix(pattern, "my favorite ") {
				// "Sushi is my favorite food" -> extract "Sushi"
				value := strings.TrimSpace(text[:idx])
				if value != "" {
					return cleanValue(value)
				}
			}

			value := text[idx+len(pattern):]
			// Clean up the value
			value = trimClause(value)
			value = cleanValue(value)
			if value != "" {
				return value
			}
		}
	}

	// Try to extract subject before "is my" or "are my"
	for _, connector := range []string{" is my ", " are my ", " is a ", " is the "} {
		if idx := strings.Index(lower, connector); idx > 0 {
			value := strings.TrimSpace(text[:idx])
			if value != "" {
				return cleanValue(value)
			}
		}
	}

	// For relationship patterns like "X and I are Y", extract the name
	if idx := strings.Index(lower, " and i are "); idx > 0 {
		value := strings.TrimSpace(text[:idx])
		if value != "" {
			return cleanValue(value)
		}
	}
	if idx := strings.Index(lower, " and i is "); idx > 0 {
		value := strings.TrimSpace(text[:idx])
		if value != "" {
			return cleanValue(value)
		}
	}

	// Fallback: return the text cleaned up
	return cleanValue(text)
}
