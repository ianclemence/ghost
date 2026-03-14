package utils

func DerefStr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}
