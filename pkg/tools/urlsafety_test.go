package tools

import (
	"testing"
)

func TestURLSafetyIsSafe(t *testing.T) {
	safety := NewURLSafety(URLSafetyConfig{AllowPrivateURLs: false})

	tests := []struct {
		url      string
		safe     bool
		desc     string
	}{
		{"https://example.com", true, "public URL"},
		{"http://google.com", true, "public HTTP URL"},
		{"https://169.254.169.254/metadata", false, "AWS metadata endpoint"},
		{"http://169.254.169.254/latest/meta-data/", false, "AWS metadata endpoint"},
		{"https://metadata.google.internal", false, "GCP metadata"},
		{"http://127.0.0.1:8080", false, "loopback"},
		{"http://localhost:3000", false, "localhost"},
		{"ftp://example.com", false, "FTP scheme"},
		{"javascript:alert(1)", false, "javascript scheme"},
	}

	for _, tt := range tests {
		safe, reason := safety.IsSafe(tt.url)
		if safe != tt.safe {
			t.Errorf("IsSafe(%s) = %v, want %v (reason: %s)", tt.url, safe, tt.safe, reason)
		}
	}
}

func TestURLSafetyAllowPrivate(t *testing.T) {
	safety := NewURLSafety(URLSafetyConfig{AllowPrivateURLs: true})

	safe, _ := safety.IsSafe("http://192.168.1.1:8080")
	if !safe {
		t.Error("expected private URL to be allowed when AllowPrivateURLs=true")
	}
}

func TestDetectSecretsInURL(t *testing.T) {
	tests := []struct {
		url      string
		hasMatch bool
		desc     string
	}{
		{"https://api.example.com?key=sk-abc123", true, "OpenAI key"},
		{"https://api.example.com?token=ghp_abc123", true, "GitHub token"},
		{"https://api.example.com?token=AKIAIOSFODNN7", true, "AWS key"},
		{"https://example.com", false, "no secrets"},
		{"https://example.com?data=hello", false, "clean data"},
	}

	for _, tt := range tests {
		secrets := DetectSecretsInURL(tt.url)
		if tt.hasMatch && len(secrets) == 0 {
			t.Errorf("DetectSecretsInURL(%s) expected secrets, got none", tt.url)
		}
		if !tt.hasMatch && len(secrets) > 0 {
			t.Errorf("DetectSecretsInURL(%s) unexpected secrets: %v", tt.url, secrets)
		}
	}
}

func TestValidateURL(t *testing.T) {
	config := URLSafetyConfig{AllowPrivateURLs: false}

	safe, _ := ValidateURL("https://example.com", config)
	if !safe {
		t.Error("expected public URL to be safe")
	}

	safe, _ = ValidateURL("http://169.254.169.254/metadata", config)
	if safe {
		t.Error("expected metadata endpoint to be unsafe")
	}

	safe, _ = ValidateURL("https://api.example.com?key=sk-abc123", config)
	if safe {
		t.Error("expected URL with secret to be unsafe")
	}
}

func TestURLSafetyCGNAT(t *testing.T) {
	safety := NewURLSafety(URLSafetyConfig{AllowPrivateURLs: false})

	safe, _ := safety.IsSafe("http://100.64.0.1:8080")
	if safe {
		t.Error("expected CGNAT address to be blocked")
	}
}

func TestURLSafetyBenchmark(t *testing.T) {
	safety := NewURLSafety(URLSafetyConfig{AllowPrivateURLs: false})

	safe, _ := safety.IsSafe("http://198.18.0.1:8080")
	if safe {
		t.Error("expected benchmark address to be blocked")
	}
}
