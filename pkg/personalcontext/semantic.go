package personalcontext

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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
		// Regex found something. If the message is a single fact (or the
		// grammar already captured several), regex is authoritative and the
		// model is not consulted. If it is a multi-clause message where the
		// grammar captured exactly one fact but more may be stated, fall
		// through and MERGE the model's complementary memories with the
		// regex ones, so an introduction like "I'm Maya. I live in Bangkok
		// and work as a designer" keeps the location AND gains name + role.
		if len(actions) > 1 || !mayHaveMoreFacts(text, len(actions)) {
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
		semantic, serr := se.extractWithLLM(ctx, text, existing)
		if serr != nil || !semantic.ShouldRemember {
			// The model is complementary, never required: regex results stand.
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
		merged := make([]Entry, 0, len(actions)+len(semantic.Entries))
		seen := map[string]bool{}
		for _, action := range actions {
			key := action.Entry.Subject + "|" + action.Entry.Predicate + "|" + Value(action.Entry)
			seen[key] = true
			merged = append(merged, action.Entry)
		}
		for _, e := range semantic.Entries {
			key := e.Subject + "|" + e.Predicate + "|" + Value(e)
			if seen[key] {
				continue
			}
			merged = append(merged, e)
		}
		return ExtractResult{
			ShouldRemember: true,
			Entries:        merged,
			Reason:         "regex+semantic_extraction",
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
			"max_tokens":  800,
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

		// Validate against controlled vocabulary. When the model returns a
		// memories array, the per-item blocks are validated individually
		// below; only the top-level gate is required here.
		validated := ValidateClassification(output)
		if !validated.Valid && len(output.Memories) == 0 {
			return ExtractResult{
				ShouldRemember: false,
				Reason:         validated.Reason,
			}, nil
		}
		if len(output.Memories) == 0 && !validated.ShouldRemember {
			return ExtractResult{
				ShouldRemember: false,
				Reason:         validated.Reason,
			}, nil
		}

		// Build entries from the model's discrete memories. Each memory is a
		// separate fact with a clean, model-authored summary; the value is no
		// longer scraped from raw user text (which stored command language and
		// fused compound statements). Falls back to the top-level fields for
		// providers that still return the single-object shape.
		items := output.Memories
		multi := len(items) > 0
		if !multi {
			items = []MemoryItem{{
				Kind:       output.Kind,
				Domain:     output.Domain,
				Confidence: output.Confidence,
				Title:      output.Title,
				Summary:    output.Summary,
			}}
		}

		now := time.Now().UTC()
		entries := make([]Entry, 0, len(items))
		for _, item := range items {
			validatedItem := ValidateClassification(ClassificationOutput{
				ShouldRemember: true,
				Kind:           item.Kind,
				Domain:         item.Domain,
				Confidence:     item.Confidence,
				Title:          item.Title,
				Summary:        item.Summary,
			})
			if !validatedItem.Valid || !validatedItem.ShouldRemember {
				continue
			}
			// Memory is what Ghost knows about the owner, never what Ghost is
			// allowed to do. A "consent" the model inferred from chat ("has
			// granted Ghost access to their drive") could later be quoted as
			// permission, so authority never enters memory: the broker owns it.
			// A shopping errand is an errand, not a trait.
			if strings.EqualFold(string(validatedItem.Kind), "consent") || transientPurchaseIntent(validatedItem.Summary) {
				continue
			}
			// In the multi-memory path the model committed to a summary; a
			// missing one is a bad item, not an invitation to scrape raw
			// user text. Only the legacy single-object path may fall back.
			value := cleanMemoryValue(validatedItem.Summary, validatedItem.Title, text, multi)
			if value == "" {
				continue
			}
			subject := memorySubject(item.About)
			entry := Entry{
				ID:         generateSemanticID(),
				Kind:       Kind(validatedItem.Kind),
				Subject:    subject,
				Domain:     string(validatedItem.Domain),
				Sensitive:  item.Sensitive || validatedItem.Domain == MemoryDomainHealth || validatedItem.Kind == MemoryKindHealth,
				Quote:      verbatimQuote(text, item.Quote),
				ValidUntil: validityEnd(item.Lasting, item.ValidUntil, now),
				Predicate:  predicateFor(string(validatedItem.Kind), string(validatedItem.Domain), item.Topic, subject, value),
				Value:      json.RawMessage(fmt.Sprintf("%q", value)),
				Status:     StatusCurrent,
				Confidence: validatedItem.Confidence,
				CreatedAt:  now,
				UpdatedAt:  now,
				Sources: []Source{{
					Type:      SourceConversation,
					Kind:      sourceKindFor(text, verbatimQuote(text, item.Quote)),
					Timestamp: now,
				}},
			}
			if d := DefaultPromotionPolicy().Evaluate(entry); !d.Promote {
				entry.Status = d.Status
			}
			entries = append(entries, entry)
		}

		if len(entries) == 0 {
			return ExtractResult{ShouldRemember: false, Reason: "no_valid_memory"}, nil
		}
		return ExtractResult{
			ShouldRemember: true,
			Entries:        entries,
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
	// Without today's date "next month, departing on the 2nd" was stored as
	// said and would have read as true forever.
	sb.WriteString(fmt.Sprintf("Today is %s. Write every date in a summary and in valid_until as an absolute date (\"2 Nov 2026\"), never relative words like \"next month\", \"tomorrow\" or \"the 2nd\".\n\n",
		time.Now().Format("Monday 2 January 2006")))
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
//
// The model is asked to understand the statement before filing it: who it is
// about, what specifically it says, whether it will still be true next month,
// and whether it is private. Only then does it choose a kind and domain. A
// person's life is not a list of "favorites"; it is people, places, dates,
// habits, health, things they own, things they can do, and views they hold.
const extractionSystemPrompt = `You are the memory of Ghost, a personal AI that lives with one person for years. Decide what is worth remembering about them, and record it the way a thoughtful friend would understand it.

First understand the message. For each thing worth keeping, work out:
- about: who or what it concerns. "user" for the owner. For anyone else use their name or role ("Jas", "Mum", "Biscuit the dog", "Nairobi office"). A fact about someone else is a fact about that person, not about the owner.
- topic: a short key for the specific thing it is about, 1 to 3 lowercase words joined by hyphens: "home-city", "partner", "jas-birthday", "diet", "car", "ghost-project". The same topic later REPLACES this memory (I moved: home-city changes). A different topic sits beside it (two people, two projects, two allergies never overwrite each other). Never use a generic key like "general" or "fact".
- summary: one clean sentence, third person, in the owner's frame ("Jas is their girlfriend", "Is allergic to peanuts", "Flies to Paris on 12 Oct 2026"). Never "user said". One fact per memory.
- lasting: "lasting" if it will probably be true for a long time; "dated" if it is tied to a date (a trip, an appointment, a deadline, a birthday is lasting-and-yearly so use lasting); "temporary" if it is true only for a while (a cold this week, staying at a hotel).
- valid_until: for dated or temporary, the date it stops mattering, as YYYY-MM-DD. Empty for lasting.
- sensitive: true for health, medication, money, legal trouble, sexuality, religion, or anything the person would not want said aloud in front of others.
- quote: the exact words from the message this came from (copied verbatim, short).
Only then choose:
- kind: identity (who they are), person (something about someone in their life), relationship (how someone relates to them), event (a dated happening), possession (something they own: car, device, home), health (condition, allergy, medication, injury, fitness), skill (something they can do, a language they speak), opinion (a view or value they hold), preference (likes and dislikes), interest (topics they follow), habit is a routine, goal, project, decision, constraint (rules they live by), fact (only if nothing else fits).
- domain: identity, food, location, work, family, health, finance, technology, travel, lifestyle, communication, education, entertainment, sports, hobbies, pets, home, vehicles, relationship, other.
- confidence: 0 to 1.

Split compound statements: "my girlfriend Jas has a birthday on March 3 and is allergic to peanuts" is three memories (Jas is their girlfriend; Jas's birthday is March 3; Jas is allergic to peanuts), each about Jas.

Do NOT remember: questions, requests, small talk, moods, what someone is doing right now ("eating pizza"), shopping errands ("wants to buy a keyboard"), anything about permissions or consent (permissions are never memories), or insults and asides. If nothing durable is stated, return should_remember false. If unsure, do not remember.

Respond with ONLY a JSON object, no explanation:
{"should_remember": true, "memories": [{"about":"Jas","topic":"jas-allergy","kind":"health","domain":"health","confidence":0.95,"summary":"Jas is allergic to peanuts","lasting":"lasting","valid_until":"","sensitive":true,"quote":"allergic to peanuts"}]}`

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
		// Fall back to the domain so a preference keeps a meaningful key
		// (preference/food, preference/travel) instead of a generic
		// preference/general that collides across unrelated preferences.
		if strings.TrimSpace(domain) != "" && domain != "other" {
			return "preference/" + strings.ToLower(domain)
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
// cleanMemoryValue picks the stored value for an extracted memory. It prefers
// the model's own clean summary, then its title, and only falls back to the
// raw-text scraper when the model supplied neither. The fallback is stripped
// of command language and trimmed so a stored memory never reads
// "Remember that …".
func cleanMemoryValue(summary, title, rawText string, strict bool) string {
	if v := strings.TrimSpace(summary); v != "" {
		return v
	}
	if v := strings.TrimSpace(title); v != "" {
		return v
	}
	if strict {
		return ""
	}
	return strings.TrimSpace(extractValueFromText(stripRemember(rawText)))
}

// MayHaveMoreFacts conservatively reports whether a message likely states
// more facts than the deterministic grammar already captured (captured is
// the number of grammar actions). It is exported so the agent can decide
// whether to run complementary semantic extraction without duplicating the
// heuristic.
func MayHaveMoreFacts(msg string, captured int) bool {
	return mayHaveMoreFacts(msg, captured)
}

// mayHaveMoreFacts conservatively reports whether a message likely states
// more facts than the deterministic grammar already captured. Only clearly
// multi-clause messages qualify, so ordinary single-fact turns stay on the
// fast regex path with no model call.
func mayHaveMoreFacts(msg string, captured int) bool {
	if captured == 0 {
		return true
	}
	trimmed := strings.TrimSpace(msg)
	sentences := 0
	for _, r := range trimmed {
		switch r {
		case '.', '!', '?', ';':
			sentences++
		}
	}
	if sentences > 1 {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, conj := range []string{" and ", " also ", ", "} {
		if !strings.Contains(lower, conj) {
			continue
		}
		for _, verb := range []string{" i ", " my ", " work", " live", " am ", "'m "} {
			if strings.Contains(lower, conj+verb) {
				return true
			}
		}
	}
	return false
}

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

var transientPurchaseRE = regexp.MustCompile(`(?i)\b(?:wants?|would like|is looking|looking|needs?|plans?|is planning|is trying|trying|wishes)\s+(?:to\s+|for\s+)?(?:buy|purchase|order|shop(?:\s+for)?|find\s+an?\s|get\s+an?\s)`)

// transientPurchaseIntent reports whether a summary is a shopping errand
// rather than a lasting fact about the owner.
func transientPurchaseIntent(summary string) bool {
	return transientPurchaseRE.MatchString(summary)
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	return strings.Trim(slugRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
}

// memorySubject normalises who a memory is about: the owner is "user", anyone
// else keeps the name the model gave them.
func memorySubject(about string) string {
	a := strings.TrimSpace(about)
	switch strings.ToLower(a) {
	case "", "user", "me", "owner", "self", "i", "the user":
		return "user"
	}
	if len([]rune(a)) > 60 {
		a = string([]rune(a)[:60])
	}
	return a
}

// predicateFor is the key under which a memory lives. Two memories with the
// same key are the same belief (the newer replaces the older); different keys
// coexist. It is the model's topic when it gave one, so "home-city" updates but
// two people, two projects or two allergies never overwrite each other. The
// old kind-wide keys ("fact/general", "project/current") made unrelated facts
// collide and dissolve into "conflicting".
func predicateFor(kind, domain, topic, subject, value string) string {
	t := slug(topic)
	switch t {
	case "", "general", "fact", "other", "misc", "memory", "info":
		t = ""
	}
	if t == "" && subject != "user" {
		t = slug(subject)
	}
	if t == "" {
		return buildPredicate(kind, domain, value)
	}
	if len(t) > 48 {
		t = t[:48]
	}
	return kind + "/" + t
}

// validityEnd turns the model's "lasting / dated / temporary" and date into a
// ValidUntil. A dated memory stays through its day; a temporary one with no date
// lapses after a week. Lasting memories never expire.
func validityEnd(lasting, until string, now time.Time) *time.Time {
	switch strings.ToLower(strings.TrimSpace(lasting)) {
	case "dated", "temporary":
	default:
		return nil
	}
	if d, err := time.Parse("2006-01-02", strings.TrimSpace(until)); err == nil {
		end := d.Add(36 * time.Hour).UTC()
		if end.After(now) {
			return &end
		}
		return nil // already past: not worth remembering as current
	}
	if strings.EqualFold(strings.TrimSpace(lasting), "temporary") {
		end := now.Add(7 * 24 * time.Hour)
		return &end
	}
	return nil
}

// verbatimQuote keeps the model's quote only if it really is a span of what the
// owner wrote: a receipt that can be checked, never a paraphrase in quotation
// marks.
func verbatimQuote(text, quote string) string {
	q := strings.TrimSpace(quote)
	if q == "" || len([]rune(q)) > 200 {
		return ""
	}
	if strings.Contains(strings.ToLower(text), strings.ToLower(q)) {
		return q
	}
	return ""
}

// sourceKindFor tells a thing the owner said outright ("my dentist is Dr.
// Lee", "I work at a school") from something Ghost worked out. Every model
// extraction used to be filed as inferred, so the owner's own words carried
// no more weight than a guess, and the memory screen could not show which
// was which.
func sourceKindFor(message, quote string) SourceKind {
	q := strings.ToLower(strings.TrimSpace(quote))
	if q == "" || strings.HasSuffix(strings.TrimSpace(message), "?") {
		return SourceInferred
	}
	for _, p := range []string{"my ", "i am ", "i'm ", "im ", "i have ", "i've ", "i live", "i work", "i like", "i love", "i hate", "i prefer", "i don't", "i do not", "i was ", "i'll be ", "i will be ", "i speak", "i own", "i drive", "we "} {
		if strings.HasPrefix(q, p) || strings.Contains(q, " "+p) {
			return SourceUserDeclared
		}
	}
	return SourceInferred
}
