package utils

import "strings"

func DerefStr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

// Prettify renders an identifier for humans: "open-meteo" → "Open Meteo".
// Single home for the copies that used to live in activity/credentials.
func Prettify(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// Truncate returns a truncated version of s with at most maxLen runes.
func Truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
