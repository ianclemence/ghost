package agent

import (
	"strings"
	"testing"
)

const english = "Here is the summary of your schedule for tomorrow: a dentist appointment at nine, lunch with Sam at noon, and the report due at five. "

func TestSanityFlagsAFaultyReply(t *testing.T) {
	cases := map[string]string{
		"foreign_script":        english + "and also 请问 something else about the plan",
		"replacement_character": english + "caf�",
		"repetition":            english + strings.Repeat("\nSorry, I will try that again now.", 8),
	}
	for want, reply := range cases {
		got := outputAnomalies("what's on tomorrow?", reply)
		found := false
		for _, g := range got {
			found = found || g == want
		}
		if !found {
			t.Errorf("%s not flagged: %v", want, got)
		}
	}
}

func TestSanityLeavesLegitimateRepliesAlone(t *testing.T) {
	ok := []struct{ ask, reply string }{
		{"what's on tomorrow?", english},
		{"你好，明天有什么安排？", "明天你有一个牙医预约，在上午九点，然后中午和山姆吃午饭，下午五点前要交报告，别忘了带上相关的文件。"},
		{"translate this", "Bonjour tout le monde, voici la traduction que vous avez demandée pour le texte complet de la lettre."},
		{"translate the letter into Chinese", "Here is the letter in Chinese: 亲爱的朋友们，感谢你们今年的支持与陪伴，祝大家新年快乐，万事如意，阖家幸福安康，我们明年再见面。"},
	}
	for i, c := range ok {
		if got := outputAnomalies(c.ask, c.reply); len(got) != 0 {
			t.Errorf("case %d flagged: %v", i, got)
		}
	}
}

func TestTurnVersionsChangeWhenSomethingDoes(t *testing.T) {
	a := turnVersions("m1", "prompt", []string{"b", "a"})
	if a != turnVersions("m1", "prompt", []string{"a", "b"}) {
		t.Error("tool order must not matter")
	}
	for _, b := range []string{turnVersions("m2", "prompt", []string{"a", "b"}), turnVersions("m1", "prompt2", []string{"a", "b"}), turnVersions("m1", "prompt", []string{"a"})} {
		if a == b {
			t.Error("a change went unnoticed")
		}
	}
}
