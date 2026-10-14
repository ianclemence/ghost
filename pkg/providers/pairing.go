package providers

const unrunToolNote = "Not run: the turn stopped before this call, for example while an earlier call waited for the owner's approval. Ask again if it is still needed."

// RepairToolPairing returns messages in which every assistant tool call is
// answered by exactly one tool message directly after it, and no tool message
// stands without its call. Providers refuse anything else, and one refused
// request must not be able to wedge a conversation for good.
func RepairToolPairing(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		m := messages[i]
		switch {
		case m.Role == "tool":
			continue
		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			out = append(out, m)
			want := make(map[string]bool, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				want[tc.ID] = true
			}
			answered := map[string]bool{}
			j := i + 1
			for ; j < len(messages) && messages[j].Role == "tool"; j++ {
				id := messages[j].ToolCallID
				if want[id] && !answered[id] {
					answered[id] = true
					out = append(out, messages[j])
				}
			}
			for _, tc := range m.ToolCalls {
				if !answered[tc.ID] {
					out = append(out, Message{Role: "tool", ToolCallID: tc.ID, Content: unrunToolNote})
				}
			}
			i = j - 1
		default:
			out = append(out, m)
		}
	}
	return out
}

// UnansweredToolCalls lists the ids of the last assistant message's tool
// calls that have no tool message after it.
func UnansweredToolCalls(messages []Message) []string {
	last := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			last = i
			break
		}
	}
	if last < 0 {
		return nil
	}
	answered := map[string]bool{}
	for _, m := range messages[last+1:] {
		if m.Role == "tool" {
			answered[m.ToolCallID] = true
		}
	}
	var out []string
	for _, tc := range messages[last].ToolCalls {
		if !answered[tc.ID] {
			out = append(out, tc.ID)
		}
	}
	return out
}

// UnrunToolMessage is the tool message that stands in for a call that never
// ran.
func UnrunToolMessage(toolCallID string) Message {
	return Message{Role: "tool", ToolCallID: toolCallID, Content: unrunToolNote}
}
