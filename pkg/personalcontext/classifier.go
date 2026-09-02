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

// ParseClassificationOutput parses JSON into a ClassificationOutput.
func ParseClassificationOutput(jsonStr string) (ClassificationOutput, error) {
	var output ClassificationOutput

	// Try to extract JSON from the response (in case there's surrounding text)
	jsonStr = strings.TrimSpace(jsonStr)

	// Find the first { and last } to extract JSON
	start := strings.Index(jsonStr, "{")
	end := strings.LastIndex(jsonStr, "}")
	if start >= 0 && end > start {
		jsonStr = jsonStr[start : end+1]
	}

	if err := json.Unmarshal([]byte(jsonStr), &output); err != nil {
		return output, fmt.Errorf("failed to parse classification output: %w", err)
	}
	return output, nil
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
