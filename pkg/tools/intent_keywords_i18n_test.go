package tools

import "testing"

func TestEveryI18nKeyAttachesToAGate(t *testing.T) {
	have := map[string]bool{}
	for _, it := range turnIntentTools {
		if len(it.tools) > 0 {
			have[it.tools[0]] = true
		}
	}
	for key := range intentKeywordsI18n {
		if !have[key] {
			t.Errorf("no turnIntentTools entry starts with %q; its words would never be used", key)
		}
	}
}

func TestOwnersGetTheirToolsInTheirOwnLanguage(t *testing.T) {
	reg := NewToolRegistry()
	for _, n := range []string{"finances", "vault", "people", "trips", "dashboard", "film", "phone", "update", "device", "self_check",
		"draft", "image_generate", "tts", "document", "knowledge", "video_frames", "motion", "pipeline",
		"browser_navigate", "browser_submit", "browser_upload", "computer_inspect_ui"} {
		reg.Register(described{n, "x"})
	}
	cases := []struct{ msg, tool string }{
		{"¿cuánto gasté en comida este mes?", "finances"},
		{"quand expire mon passeport ?", "vault"},
		{"Wann hat meine Schwester Geburtstag?", "people"},
		{"reserve um hotel para a viagem", "trips"},
		{"nionyeshe grafu ya mauzo", "dashboard"},
		{"帮我画一只猫", "film"},
		{"パスポートの期限はいつ？", "vault"},
		{"오늘 걸음 수 알려줘", "phone"},
		{"ما هي فاتورتي هذا الشهر", "finances"},
		{"मेरा बजट कितना है", "finances"},
		{"обнови Ghost до последней версии", "update"},
		{"ışıkları kapat", "device"},
		{"ตรวจสอบตัวเอง", "self_check"},
		{"Responde al correo de Ana", "draft"},
		{"Genera una imagen de un gato", "image_generate"},
		{"lee en voz alta esto", "tts"},
		{"写一份简历", "document"},
		{"Quiero estudiar para el examen", "knowledge"},
		{"Cherche le prix des vols sur le web", "browser_navigate"},
		{"ödemeye geç", "browser_submit"},
		{"bitte die Datei hochladen", "browser_upload"},
		{"покажи экран", "computer_inspect_ui"},
		{"analiza este vídeo", "video_frames"},
		{"hazme una animación", "motion"},
		{"wie viele meiner Notizen erwähnen Nairobi", "pipeline"},
	}
	for _, c := range cases {
		out := FilterToolsForTurn(reg, ProfileFull, c.msg, false)
		if _, ok := out.Get(c.tool); !ok {
			t.Errorf("%q should offer %s", c.msg, c.tool)
		}
	}
	for _, msg := range []string{"hola", "bonjour", "你好", "habari", "ありがとう", "merhaba", "спасибо"} {
		if got := FilterToolsForTurn(reg, ProfileFull, msg, false).List(); len(got) != 0 {
			t.Errorf("%q is chit-chat and should offer no extra tools, got %v", msg, got)
		}
	}
}
