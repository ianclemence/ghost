package agent

import (
	"strings"
	"testing"
)

func TestAmbiguousAnswerListsEverythingWaiting(t *testing.T) {
	out := ambiguousAnswerText([]string{"Call the bank at 9", "Brief every Monday at 8"})
	for _, want := range []string{"More than one thing", "Call the bank at 9", "Brief every Monday at 8", "tell me which"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestBareAnswerRecognisesYesAndNoButNotSentences(t *testing.T) {
	for _, s := range []string{"yes", "Yes.", "no", "ใช่", "sure"} {
		if !isBareAnswer(s) {
			t.Errorf("%q is a bare answer", s)
		}
	}
	for _, s := range []string{"yes and also remind me tomorrow", "what's waiting?"} {
		if isBareAnswer(s) {
			t.Errorf("%q is not a bare answer", s)
		}
	}
}

func TestSingleWaitingItemStillTakesAPlainYes(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())
	if _, ambiguous := al.ambiguousAnswer("s", "yes"); ambiguous {
		t.Fatal("nothing is waiting, so a yes is not ambiguous")
	}
}
