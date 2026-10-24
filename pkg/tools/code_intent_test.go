package tools

import "testing"

func TestCodeWorkIntentIsLanguageNeutral(t *testing.T) {
	yes := []string{
		"Are you able to clone repos and work on them?",
		"This one i made: https://github.com/ianclemence/scout",
		"look at gitlab.com/foo/bar please",
		"git clone git@host.example:me/app.git",
		"fix the failing test in main.go",
		"corre go test en el proyecto",
		"npm install y luego build",
		// repository / clone in the languages owners write in
		"¿Puedes clonar mi repositorio?",
		"Peux-tu cloner mon dépôt git ?",
		"Kannst du mein Repository klonen?",
		"Unaweza kuclone repo yangu?",
		"Posso clonar o repositório?",
		"Czy możesz sklonować repozytorium?",
		"можешь клонировать мой репозиторий?",
		"リポジトリをクローンできますか",
		"你能克隆我的仓库吗",
		"저장소를 클론해 줄 수 있어?",
		"โคลนคลังโค้ดของฉันได้ไหม",
		"هل يمكنك استنساخ المستودع",
		"क्या तुम मेरा रिपॉजिटरी क्लोन कर सकते हो",
	}
	for _, m := range yes {
		if !CodeWorkIntent(m) {
			t.Errorf("should be code work: %q", m)
		}
	}
	no := []string{
		"", "what's the weather in Nairobi",
		"write a report for my boss",
		"the dress code is smart casual",
		"remind me to call mum tomorrow",
		"¿qué tiempo hace hoy?",
		"今天天气怎么样",
		"I need to report a problem with my bill",
	}
	for _, m := range no {
		if CodeWorkIntent(m) {
			t.Errorf("should not be code work: %q", m)
		}
	}
}
