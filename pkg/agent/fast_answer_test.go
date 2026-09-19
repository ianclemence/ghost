package agent

import "testing"

func TestPhraseFastAnswer(t *testing.T) {
	cases := []struct {
		pred   string
		values []string
		want   string
	}{
		{"identity/name", []string{"The user's name is Maya"}, "You're Maya."},
		{"identity/name", []string{"Maya"}, "You're Maya."},
		{"fact/location", []string{"Lives in Bangkok"}, "You live in Bangkok."},
		{"fact/location", []string{"Bangkok"}, "You live in Bangkok."},
		{"identity/email", []string{"maya@example.com"}, "Your email is maya@example.com."},
		{"preference/likes", []string{"tea", "coffee"}, "You like tea, coffee."},
	}
	for _, c := range cases {
		if got := phraseFastAnswer(c.pred, c.values); got != c.want {
			t.Errorf("phraseFastAnswer(%q, %v) = %q, want %q", c.pred, c.values, got, c.want)
		}
	}
}
