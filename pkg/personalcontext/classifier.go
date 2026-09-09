package personalcontext

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Controlled vocabulary for memory classification.
// These are the only allowed values for kind and domain.
// The model must classify into these categories, not invent new ones.

// MemoryKind is the controlled vocabulary for memory kinds.
type MemoryKind string

const (
	MemoryKindIdentity     MemoryKind = "identity"
	MemoryKindPreference   MemoryKind = "preference"
	MemoryKindFact         MemoryKind = "fact"
	MemoryKindGoal         MemoryKind = "goal"
	MemoryKindRelationship MemoryKind = "relationship"
	MemoryKindRoutine      MemoryKind = "routine"
	MemoryKindDecision     MemoryKind = "decision"
	MemoryKindConsent      MemoryKind = "consent"
	MemoryKindProject      MemoryKind = "project"
	MemoryKindConstraint   MemoryKind = "constraint"
	MemoryKindInterest     MemoryKind = "interest"
)

// MemoryDomain is the controlled vocabulary for memory domains.
type MemoryDomain string

const (
	MemoryDomainIdentity      MemoryDomain = "identity"
	MemoryDomainFood          MemoryDomain = "food"
	MemoryDomainLocation      MemoryDomain = "location"
	MemoryDomainWork          MemoryDomain = "work"
	MemoryDomainFamily        MemoryDomain = "family"
	MemoryDomainHealth        MemoryDomain = "health"
	MemoryDomainFinance       MemoryDomain = "finance"
	MemoryDomainTechnology    MemoryDomain = "technology"
	MemoryDomainTravel        MemoryDomain = "travel"
	MemoryDomainLifestyle     MemoryDomain = "lifestyle"
	MemoryDomainCommunication MemoryDomain = "communication"
	MemoryDomainEducation     MemoryDomain = "education"
	MemoryDomainEntertainment MemoryDomain = "entertainment"
	MemoryDomainRelationship  MemoryDomain = "relationship"
	MemoryDomainOther         MemoryDomain = "other"
)

// ValidMemoryKinds is the set of valid memory kinds.
var ValidMemoryKinds = map[MemoryKind]bool{
	MemoryKindIdentity:     true,
	MemoryKindPreference:   true,
	MemoryKindFact:         true,
	MemoryKindGoal:         true,
	MemoryKindRelationship: true,
	MemoryKindRoutine:      true,
	MemoryKindDecision:     true,
	MemoryKindConsent:      true,
	MemoryKindProject:      true,
	MemoryKindConstraint:   true,
	MemoryKindInterest:     true,
}

// ValidMemoryDomains is the set of valid memory domains.
var ValidMemoryDomains = map[MemoryDomain]bool{
	MemoryDomainIdentity:      true,
	MemoryDomainFood:          true,
	MemoryDomainLocation:      true,
	MemoryDomainWork:          true,
	MemoryDomainFamily:        true,
	MemoryDomainHealth:        true,
	MemoryDomainFinance:       true,
	MemoryDomainTechnology:    true,
	MemoryDomainTravel:        true,
	MemoryDomainLifestyle:     true,
	MemoryDomainCommunication: true,
	MemoryDomainEducation:     true,
	MemoryDomainEntertainment: true,
	MemoryDomainRelationship:  true,
	MemoryDomainOther:         true,
}

// ClassificationInput is what we send to the model for semantic classification.
type ClassificationInput struct {
	Text      string `json:"text"`
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Value     string `json:"value"`
}

// ClassificationOutput is the model's response with semantic classification.
type ClassificationOutput struct {
	ShouldRemember bool    `json:"should_remember"`
	Kind           string  `json:"kind"`
	Domain         string  `json:"domain"`
	Confidence     float64 `json:"confidence"`
	Title          string  `json:"title,omitempty"`
	Summary        string  `json:"summary,omitempty"`
}

// ClassificationResult is the validated classification output.
type ClassificationResult struct {
	ShouldRemember bool
	Kind           MemoryKind
	Domain         MemoryDomain
	Confidence     float64
	Title          string
	Summary        string
	Valid          bool
	Reason         string
}

// ClassificationSchema is the JSON schema we send to the model.
const ClassificationSchema = `{
  "type": "object",
  "properties": {
    "should_remember": {
      "type": "boolean",
      "description": "Whether this message contains something worth remembering about the user"
    },
    "kind": {
      "type": "string",
      "enum": ["identity", "preference", "fact", "goal", "relationship", "routine", "decision", "consent", "project", "constraint", "interest"],
      "description": "The type of knowledge this represents"
    },
    "domain": {
      "type": "string",
      "enum": ["identity", "food", "location", "work", "family", "health", "finance", "technology", "travel", "lifestyle", "communication", "education", "entertainment", "relationship", "other"],
      "description": "What the knowledge concerns"
    },
    "confidence": {
      "type": "number",
      "minimum": 0,
      "maximum": 1,
      "description": "How confident you are in this classification (0-1)"
    },
    "title": {
      "type": "string",
      "description": "A short, human-readable title for this memory (e.g., 'Prefers tea over coffee')"
    },
    "summary": {
      "type": "string",
      "description": "A natural language summary of what Ghost should remember"
    }
  },
  "required": ["should_remember", "kind", "domain", "confidence"]
}`

// ClassificationPrompt is the system prompt for the classifier.
const ClassificationPrompt = `You are a memory classifier for Ghost, a personal AI assistant.

Your job is to analyze a user message and determine:
1. Whether it contains something worth remembering about the user
2. What kind of knowledge it represents (kind)
3. What domain it concerns (domain)
4. How confident you are in this classification

CONTROLLED VOCABULARY:
- Kind: identity, preference, fact, goal, relationship, routine, decision, consent, project, constraint, interest
- Domain: identity, food, location, work, family, health, finance, technology, travel, lifestyle, communication, education, entertainment, relationship, other

RULES:
1. Only classify messages that contain explicit, persistent information about the user
2. Do NOT classify:
   - Transient requests ("What's the weather?")
   - Questions about the world
   - Commands or instructions
   - Temporary context ("I'm going to the store")
3. Be conservative: it's better to miss something than to create a false memory
4. If unsure, set should_remember to false
5. Use "other" domain if no specific domain fits
6. Generate a short, human-readable title (e.g., "Prefers tea over coffee")
7. Generate a natural summary of what should be remembered

RESPOND WITH VALID JSON ONLY. No explanation.`

// ClassifyMessage sends the message to the model for semantic classification.
// This is a placeholder that will be integrated with the actual model provider.
func ClassifyMessage(input ClassificationInput) ClassificationOutput {
	// TODO: Integrate with model provider
	// For now, return a default that indicates no memory should be created
	return ClassificationOutput{
		ShouldRemember: false,
		Kind:           string(KindFact),
		Domain:         string(DomainOther),
		Confidence:     0.5,
	}
}

// ValidateClassification validates the model's output against the controlled vocabulary.
func ValidateClassification(output ClassificationOutput) ClassificationResult {
	result := ClassificationResult{
		ShouldRemember: output.ShouldRemember,
		Confidence:     output.Confidence,
		Title:          output.Title,
		Summary:        output.Summary,
	}

	// Validate kind
	kind := MemoryKind(strings.ToLower(output.Kind))
	if !ValidMemoryKinds[kind] {
		result.Valid = false
		result.Reason = fmt.Sprintf("invalid kind: %s (must be one of: %s)", output.Kind, memoryKindsList())
		result.Kind = MemoryKindFact // fallback
	} else {
		result.Kind = kind
	}

	// Validate domain
	domain := MemoryDomain(strings.ToLower(output.Domain))
	if !ValidMemoryDomains[domain] {
		result.Valid = false
		result.Reason = fmt.Sprintf("invalid domain: %s (must be one of: %s)", output.Domain, memoryDomainsList())
		result.Domain = MemoryDomainOther // fallback
	} else {
		result.Domain = domain
	}

	// Validate confidence
	if output.Confidence < 0 || output.Confidence > 1 {
		result.Valid = false
		result.Reason = fmt.Sprintf("confidence must be between 0 and 1, got %f", output.Confidence)
		result.Confidence = 0.5 // fallback
	}

	// If should_remember is false, the classification is invalid but that's expected
	if !output.ShouldRemember {
		result.Valid = false
		result.Reason = "message not worth remembering"
	}

	// If we got this far without setting valid, mark as valid
	if result.Reason == "" {
		result.Valid = true
	}

	return result
}

// SemanticClassify performs semantic classification of a message.
// This combines the regex-based extraction with model-based classification.
func SemanticClassify(text string, existing []Entry) ClassificationResult {
	// First, check if the regex extractor already found something
	actions, err := Extract(Input{
		Text:    text,
		Current: existing,
	})
	if err == nil && len(actions) > 0 {
		// Regex found something, use that with high confidence
		action := actions[0]
		domain := ClassifyDomain(action.Entry.Predicate)
		return ClassificationResult{
			ShouldRemember: true,
			Kind:           MemoryKind(action.Entry.Kind),
			Domain:         MemoryDomain(domain),
			Confidence:     action.Entry.Confidence,
			Title:          Title(action.Entry),
			Summary:        Summary(action.Entry),
			Valid:          true,
			Reason:         "regex extraction",
		}
	}

	// Regex didn't find anything, use model classification
	output := ClassifyMessage(ClassificationInput{
		Text: text,
	})
	return ValidateClassification(output)
}

func memoryKindsList() string {
	kinds := make([]string, 0, len(ValidMemoryKinds))
	for k := range ValidMemoryKinds {
		kinds = append(kinds, string(k))
	}
	return strings.Join(kinds, ", ")
}

func memoryDomainsList() string {
	domains := make([]string, 0, len(ValidMemoryDomains))
	for d := range ValidMemoryDomains {
		domains = append(domains, string(d))
	}
	return strings.Join(domains, ", ")
}

// ParseClassificationOutput parses model output into a ClassificationOutput.
//
// Model output is not guaranteed to be a bare JSON document: it may be
// wrapped in markdown code fences, surrounded by prose, carry a trailing
// sentence, or contain stray braces inside string values. The parser is
// tolerant about WHERE the JSON lives but strict about WHAT it accepts:
// every candidate must unmarshal as a single JSON object AND carry the
// required should_remember field. Nothing is coerced, repaired, or
// partially parsed — a malformed response is a parse error, never a fact.
func ParseClassificationOutput(jsonStr string) (ClassificationOutput, error) {
	jsonStr = stripJSONFences(jsonStr)
	candidates := jsonObjectCandidates(jsonStr)

	var lastErr error
	for _, c := range candidates {
		var output ClassificationOutput
		if err := json.Unmarshal([]byte(c), &output); err != nil {
			lastErr = err
			continue
		}
		// json zero-value would silently accept "{}": require the boolean.
		var probe struct {
			ShouldRemember *bool `json:"should_remember"`
		}
		if err := json.Unmarshal([]byte(c), &probe); err != nil || probe.ShouldRemember == nil {
			lastErr = fmt.Errorf("classification missing required field should_remember")
			continue
		}
		return output, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no JSON object found in classifier output")
	}
	return ClassificationOutput{}, fmt.Errorf("failed to parse classification output: %w", lastErr)
}

// stripJSONFences removes markdown code-fence lines so fenced JSON parses.
func stripJSONFences(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// jsonObjectCandidates returns candidate object strings in decreasing
// likelihood: the trimmed text when it already is an object, the classic
// first-{…}-last-} span, and every brace-balanced {…} substring. Braces
// inside quoted strings (and escaped quotes) do not end a candidate, so a
// value that itself contains JSON-like text cannot break extraction.
func jsonObjectCandidates(s string) []string {
	s = strings.TrimSpace(s)
	var out []string
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		out = append(out, s)
	}
	if start := strings.Index(s, "{"); start >= 0 {
		if end := strings.LastIndex(s, "}"); end > start {
			out = append(out, s[start:end+1])
		}
	}
	// Brace-balanced scan: every top-level object that closes cleanly.
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		depth := 0
		inStr := false
		esc := false
		for j := i; j < len(s); j++ {
			c := s[j]
			if inStr {
				if esc {
					esc = false
					continue
				}
				if c == '\\' {
					esc = true
					continue
				}
				if c == '"' {
					inStr = false
				}
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					out = append(out, s[i:j+1])
					i = j
					break
				}
			}
		}
	}
	return out
}

// FormatClassificationPrompt formats the classification prompt with the message.
func FormatClassificationPrompt(text, subject, predicate, value string) string {
	input := ClassificationInput{
		Text:      text,
		Subject:   subject,
		Predicate: predicate,
		Value:     value,
	}
	inputJSON, _ := json.Marshal(input)

	return fmt.Sprintf("%s\n\nMessage to classify:\n%s", ClassificationPrompt, string(inputJSON))
}
