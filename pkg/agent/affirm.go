package agent

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/permissions"
)

// Saying yes and no in other languages. An owner who talks to Ghost in Spanish,
// French, Swahili, Thai or Chinese answers a confirmation in that language, and
// "sí" or "ndiyo" or "好" must count as much as "yes". Only whole, short replies
// are matched, never a word inside a longer sentence, so a story that happens to
// contain "no" is never taken for a refusal.

var affirmWords = map[string]bool{
	// English
	"yes": true, "y": true, "yeah": true, "yep": true, "yup": true, "ok": true, "okay": true, "sure": true,
	"confirm": true, "do it": true, "go ahead": true, "please do": true,
	// Spanish, Portuguese, Italian
	"sí": true, "si": true, "vale": true, "claro": true, "de acuerdo": true, "dale": true, "sim": true, "certo": true, "certamente": true,
	// French
	"oui": true, "d'accord": true, "ouais": true, "bien sûr": true, "volontiers": true,
	// German, Dutch
	"ja": true, "klar": true, "gerne": true, "jawohl": true,
	// Swahili
	"ndiyo": true, "ndio": true, "sawa": true, "sawa kabisa": true, "ruhusu": true, "kubali": true, "haya": true,
	// Thai
	"ใช่": true, "ตกลง": true, "ได้": true, "โอเค": true, "ได้เลย": true, "ยืนยัน": true,
	// Chinese, Japanese, Korean
	"好": true, "好的": true, "可以": true, "是": true, "是的": true, "确定": true, "確定": true, "行": true,
	"はい": true, "了解": true, "네": true, "예": true, "좋아": true,
	// Arabic, Hindi
	"نعم": true, "حسنا": true, "हाँ": true, "हां": true, "ठीक है": true,
}

var denyWords = map[string]bool{
	// English
	"no": true, "n": true, "nope": true, "cancel": true, "never mind": true, "nevermind": true, "stop": true,
	// Spanish, Portuguese, Italian
	"no gracias": true, "cancelar": true, "não": true, "nao": true, "cancela": true,
	// French
	"non": true, "annuler": true, "non merci": true, "laisse tomber": true,
	// German, Dutch
	"nein": true, "abbrechen": true, "nee": true,
	// Swahili
	"hapana": true, "ghairi": true, "acha": true, "siyo": true,
	// Thai
	"ไม่": true, "ไม่ใช่": true, "ไม่เอา": true, "ยกเลิก": true, "ไม่ต้อง": true,
	// Chinese, Japanese, Korean
	"不": true, "不要": true, "不行": true, "取消": true, "否": true,
	"いいえ": true, "キャンセル": true, "아니요": true, "아니": true,
	// Arabic, Hindi
	"لا": true, "नहीं": true,
}

// normalizeReply lowercases a short reply and strips the punctuation people put
// around a yes or no in any script.
func normalizeReply(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, " \t\n.!?,;:。！？、…~'\"“”‘’")
	return strings.Join(strings.Fields(s), " ")
}

// isAffirmation reports whether the whole message is a yes, in any of the
// languages above.
func isAffirmation(s string) bool { return affirmWords[normalizeReply(s)] }

// isDenial reports whether the whole message is a no.
func isDenial(s string) bool { return denyWords[normalizeReply(s)] }

func init() {
	// The approval phrase table already knows the English words; teach it the
	// rest, so a reply in the owner's own language answers an approval card.
	for w := range affirmWords {
		if _, ok := approvalPhrases[w]; !ok {
			approvalPhrases[w] = permissions.GrantOnce
		}
	}
	for w := range denyWords {
		if _, ok := approvalPhrases[w]; !ok {
			approvalPhrases[w] = permissions.GrantDeny
		}
	}
}
