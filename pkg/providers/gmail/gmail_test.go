package gmail

import (
	"strings"
	"testing"
)

func TestFilterSensitive(t *testing.T) {
	body := "Your one-time verification code is 123456\n" +
		"Click here to reset your password: https://x/reset?token=abc\n" +
		"Here is your magic link to sign in: https://x/magic\n" +
		"See you at noon"
	filtered := FilterSensitive(body)
	if strings.Contains(filtered, "123456") {
		t.Fatal("one-time code must be redacted")
	}
	if strings.Contains(filtered, "https://x/reset") {
		t.Fatal("reset link must be redacted")
	}
	if strings.Contains(filtered, "https://x/magic") {
		t.Fatal("magic link must be redacted")
	}
	if !strings.Contains(filtered, "See you at noon") {
		t.Fatal("benign content must survive")
	}
}
