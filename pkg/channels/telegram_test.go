package channels

import (
	"testing"
)

func TestMarkdownToTelegramHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Headers",
			input:    "# Header 1\nContent\n## Header 2",
			expected: "<b>Header 1</b>\nContent\n<b>Header 2</b>",
		},
		{
			name:     "Lists",
			input:    "- Item 1\n- Item 2",
			expected: "• Item 1\n• Item 2",
		},
		{
			name:     "Blockquotes",
			input:    "> Quote 1\nContent\n> Quote 2",
			expected: "<blockquote>Quote 1</blockquote>\nContent\n<blockquote>Quote 2</blockquote>",
		},
		{
			name:     "Bold and Italic",
			input:    "**Bold** and _Italic_",
			expected: "<b>Bold</b> and <i>Italic</i>",
		},
		{
			name:     "Code Block",
			input:    "```go\nfunc main() {}\n```",
			expected: "<pre><code>func main() {}\n</code></pre>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markdownToTelegramHTML(tt.input)
			if got != tt.expected {
				t.Errorf("markdownToTelegramHTML() = %q, want %q", got, tt.expected)
			}
		})
	}
}
