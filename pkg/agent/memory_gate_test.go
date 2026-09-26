package agent

import "testing"

// The gate exists to stop Ghost paying ~2.8 s of local embedding on every
// message. These cases are the contract: memory questions retrieve, everything
// else does not.
func TestMemoryRetrievalNeeded(t *testing.T) {
	retrieve := []string{
		"What did I tell you about the deposit?",
		"do you remember my wifi password?",
		"what do you know about my sister?",
		"remind me what we decided about the car",
		"what did we discuss last week?",
		"what do I usually order?",
		"did I tell you about the passport?",
		"have I mentioned the new landlord?",
		"tell me about my notes on the trip",
		"what was the name of that place we talked about?",
		"my allergies",
		"my goals",
		"what's my sister's birthday?",
		"earlier you said something about the budget",
	}
	for _, q := range retrieve {
		if !memoryRetrievalNeeded(q) {
			t.Errorf("memoryRetrievalNeeded(%q) = false; this genuinely needs memory", q)
		}
	}

	skip := []string{
		"hey Ghost",
		"hi",
		"thanks!",
		"what reminders do I have?",
		"what reminders are overdue?",
		"what needs me?",
		"do I have anything waiting for approval?",
		"is my morning routine okay?",
		"what tasks are overdue?",
		"what did you just do?",
		"is Ghost healthy?",
		"how much disk space is left?",
		"is proactive mode on?",
		"what model are you using?",
		"remind me about the chelsea game on 9 october at 9 am",
		"send Alex those photos",
		"add milk to my shopping list",
		"what's the weather in Bangkok",
		"",
	}
	for _, q := range skip {
		if memoryRetrievalNeeded(q) {
			t.Errorf("memoryRetrievalNeeded(%q) = true; this must not pay for an embedding", q)
		}
	}
}

// A greeting with an interrogative tail is still not a memory question.
func TestMemoryGateIsNotFooledByShortTalk(t *testing.T) {
	for _, q := range []string{"hey there", "good morning!", "ok cool", "no thanks"} {
		if memoryRetrievalNeeded(q) {
			t.Errorf("memoryRetrievalNeeded(%q) = true; trivial traffic must be free", q)
		}
	}
}
