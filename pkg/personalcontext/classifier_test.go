package personalcontext

import (
	"testing"
)

func TestValidateClassification_ValidInput(t *testing.T) {
	output := ClassificationOutput{
		ShouldRemember: true,
		Kind:           "preference",
		Domain:         "food",
		Confidence:     0.95,
		Title:          "Prefers tea over coffee",
		Summary:        "User prefers tea over coffee",
	}
	result := ValidateClassification(output)
	if !result.Valid {
		t.Errorf("expected valid classification, got: %s", result.Reason)
	}
	if result.Kind != MemoryKindPreference {
		t.Errorf("expected kind preference, got %s", result.Kind)
	}
	if result.Domain != MemoryDomainFood {
		t.Errorf("expected domain food, got %s", result.Domain)
	}
}

func TestValidateClassification_InvalidKind(t *testing.T) {
	output := ClassificationOutput{
		ShouldRemember: true,
		Kind:           "invalid_kind",
		Domain:         "food",
		Confidence:     0.95,
	}
	result := ValidateClassification(output)
	if result.Valid {
		t.Error("expected invalid classification for invalid kind")
	}
	if result.Kind != MemoryKindFact {
		t.Errorf("expected fallback kind fact, got %s", result.Kind)
	}
}

func TestValidateClassification_InvalidDomain(t *testing.T) {
	output := ClassificationOutput{
		ShouldRemember: true,
		Kind:           "preference",
		Domain:         "invalid_domain",
		Confidence:     0.95,
	}
	result := ValidateClassification(output)
	if result.Valid {
		t.Error("expected invalid classification for invalid domain")
	}
	if result.Domain != MemoryDomainOther {
		t.Errorf("expected fallback domain other, got %s", result.Domain)
	}
}

func TestValidateClassification_InvalidConfidence(t *testing.T) {
	output := ClassificationOutput{
		ShouldRemember: true,
		Kind:           "preference",
		Domain:         "food",
		Confidence:     1.5,
	}
	result := ValidateClassification(output)
	if result.Valid {
		t.Error("expected invalid classification for out-of-range confidence")
	}
	if result.Confidence != 0.5 {
		t.Errorf("expected fallback confidence 0.5, got %f", result.Confidence)
	}
}

func TestValidateClassification_ShouldNotRemember(t *testing.T) {
	output := ClassificationOutput{
		ShouldRemember: false,
		Kind:           "fact",
		Domain:         "other",
		Confidence:     0.5,
	}
	result := ValidateClassification(output)
	if result.Valid {
		t.Error("expected invalid classification when should_remember is false")
	}
}

func TestValidateClassification_AllValidKinds(t *testing.T) {
	validKinds := []string{
		"identity", "preference", "fact", "goal", "relationship",
		"routine", "decision", "consent", "project", "constraint", "interest",
	}
	for _, kind := range validKinds {
		output := ClassificationOutput{
			ShouldRemember: true,
			Kind:           kind,
			Domain:         "other",
			Confidence:     0.8,
		}
		result := ValidateClassification(output)
		if !result.Valid {
			t.Errorf("expected kind %s to be valid, got: %s", kind, result.Reason)
		}
	}
}

func TestValidateClassification_AllValidDomains(t *testing.T) {
	validDomains := []string{
		"identity", "food", "location", "work", "family", "health",
		"finance", "technology", "travel", "lifestyle", "communication", "other",
	}
	for _, domain := range validDomains {
		output := ClassificationOutput{
			ShouldRemember: true,
			Kind:           "fact",
			Domain:         domain,
			Confidence:     0.8,
		}
		result := ValidateClassification(output)
		if !result.Valid {
			t.Errorf("expected domain %s to be valid, got: %s", domain, result.Reason)
		}
	}
}

func TestSemanticClassify_RegexMatch(t *testing.T) {
	text := "I prefer tea over coffee"
	result := SemanticClassify(text, nil)
	if !result.ShouldRemember {
		t.Error("expected should_remember true for preference statement")
	}
	if result.Kind != MemoryKindPreference {
		t.Errorf("expected kind preference, got %s", result.Kind)
	}
	// preference/likes maps to lifestyle domain (generic likes)
	if result.Domain != MemoryDomainLifestyle {
		t.Errorf("expected domain lifestyle, got %s", result.Domain)
	}
}

func TestSemanticClassify_NoMatch(t *testing.T) {
	text := "What's the weather in Bangkok?"
	result := SemanticClassify(text, nil)
	// Should not remember weather questions
	if result.ShouldRemember {
		t.Error("expected should_remember false for weather question")
	}
}

func TestParseClassificationOutput_ValidJSON(t *testing.T) {
	jsonStr := `{
		"should_remember": true,
		"kind": "preference",
		"domain": "food",
		"confidence": 0.9,
		"title": "Prefers tea",
		"summary": "User prefers tea"
	}`
	output, err := ParseClassificationOutput(jsonStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !output.ShouldRemember {
		t.Error("expected should_remember true")
	}
	if output.Kind != "preference" {
		t.Errorf("expected kind preference, got %s", output.Kind)
	}
}

func TestParseClassificationOutput_InvalidJSON(t *testing.T) {
	jsonStr := `invalid json`
	_, err := ParseClassificationOutput(jsonStr)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFormatClassificationPrompt(t *testing.T) {
	prompt := FormatClassificationPrompt("I like cats", "user", "preference/likes", "cats")
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !containsSubstring(prompt, "I like cats") {
		t.Error("expected prompt to contain the message")
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && contains(s, substr))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
