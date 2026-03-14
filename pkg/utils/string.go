package utils

func DerefStr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
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
