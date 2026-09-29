package golden

// Formatting-decision cases: does Ghost choose Markdown appropriately, or
// does it decorate every answer?
//
// These assert restraint (nothing decorative on a one-line answer) and
// appropriateness (structure appears when the content genuinely has it).
// They are model-sensitive by nature, so assertions target unambiguous
// markers of over-formatting — headings, tables, dividers, bullet lists —
// rather than exact prose.
func formattingConversations() []Conversation {
	return []Conversation{
		{
			ID: "fmt-01", Category: CatFormatting, Title: "Simple answer stays plain prose",
			Severity: "normal", People: onePerson("maya", "maya",
				turn("Is 2 + 2 equal to 4? Just answer.")),
			Expect: Expect{
				LastResponseContainsAny: []string{"yes", "4"},
				// Over-formatting gates: a one-line answer must not grow a
				// heading, a table, a horizontal rule, or a bullet list.
				LastResponseNotContains: []string{"\n## ", "\n# ", "| ---", "\n---", "\n- "},
			},
		},
		{
			ID: "fmt-02", Category: CatFormatting, Title: "Two-word reply is not a document",
			Severity: "normal", People: onePerson("maya", "maya",
				turn("What is the capital of Japan? One word.")),
			Expect: Expect{
				LastResponseContains:    []string{"tokyo"},
				LastResponseNotContains: []string{"\n## ", "\n# ", "| ---", "\n---", "\n- ", "```"},
			},
		},
		{
			ID: "fmt-03", Category: CatFormatting, Title: "Procedure uses ordered steps",
			Severity: "normal", People: onePerson("maya", "maya",
				turn("Give me the steps to make a cup of tea, numbered.")),
			Expect: Expect{
				// A sequence must read as a sequence.
				LastResponseContainsAny: []string{"1. ", "1) ", "1 -", "Step 1"},
			},
		},
		{
			ID: "fmt-04", Category: CatFormatting, Title: "Explicit code request is a code block",
			Severity: "normal", People: onePerson("maya", "maya",
				turn("Show me a tiny Go hello-world in a code block.")),
			Expect: Expect{
				LastResponseContains: []string{"```"},
			},
		},
		{
			ID: "fmt-05", Category: CatFormatting, Title: "Comparison may use a table, options both named",
			Severity: "normal", People: onePerson("maya", "maya",
				turn("Compare tea and coffee for me in one short answer.")),
			Expect: Expect{
				// Either prose or a table is fine; both subjects must appear.
				LastResponseContains: []string{"tea", "coffee"},
			},
		},
		{
			ID: "fmt-06", Category: CatFormatting, Title: "Diagram request yields a Mermaid block",
			Severity: "normal", People: onePerson("maya", "maya",
				turn("Draw me a small flowchart of how a request flows through Ghost.")),
			Expect: Expect{
				// The owner asked for a diagram; the runtime must render one
				// (Mermaid fence) rather than ASCII art or prose paragraphs.
				LastResponseContainsAny: []string{"```mermaid", "flowchart", "graph TD", "graph LR"},
			},
		},
	}
}
