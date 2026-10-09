package agent

import "strings"

// NoteOwnerAction records, in the conversation Ghost reads, something the
// owner did outside a turn: sent a draft Ghost wrote, answered a card after the
// turn that asked had moved on. Ghost then knows it happened ("did you send
// it?") instead of guessing. It is a system line: the owner never sees it as a
// message, and nothing is run because of it.
func (al *AgentLoop) NoteOwnerAction(sessionKey, note string) {
	note = strings.TrimSpace(note)
	if al == nil || al.sessions == nil || note == "" {
		return
	}
	if sessionKey == "" {
		sessionKey = "main"
	}
	al.sessions.AddMessage(sessionKey, "system", note)
	al.sessions.Save(sessionKey)
}

// ClarifyWaiting reports whether a turn is still waiting on the question.
func (al *AgentLoop) ClarifyWaiting(questionID string) bool {
	if al == nil || al.tools == nil {
		return false
	}
	t, ok := al.tools.Get("clarify")
	if !ok {
		return false
	}
	type waiter interface{ Waiting(string) bool }
	w, ok := t.(waiter)
	return ok && w.Waiting(questionID)
}

// conversationOf is the conversation a card belongs to. A routine runs in a
// session of its own, but what it says (and shows) is delivered to the owner's
// conversation, so its cards go there too: otherwise the phone, which shows
// the owner's conversation, would not draw them as they arrive.
func conversationOf(sessionID string) string {
	if strings.HasPrefix(sessionID, "routine:") {
		return "main"
	}
	return sessionID
}

// SilentRoutineReply reports whether a routine's reply means it has nothing to
// say (NOTHING, alone, give or take punctuation and case).
func SilentRoutineReply(resp string) bool {
	t := strings.Trim(strings.TrimSpace(resp), ".!\"'`*")
	return strings.EqualFold(t, "NOTHING")
}
