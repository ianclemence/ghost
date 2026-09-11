package affect

import (
	"regexp"
	"strings"
)

// Lexicon sentiment scoring: deterministic, private, dependency-free. It is
// the auditable base — every score traces to counted words, never to a
// black box. Crude by design; confidence reports its own limits and the
// engine treats low-confidence turns as neutral rather than inventing
// feeling.

var positiveWords = map[string]bool{
	"love": true, "great": true, "awesome": true, "amazing": true,
	"excellent": true, "wonderful": true, "fantastic": true, "perfect": true,
	"happy": true, "glad": true, "pleased": true, "delighted": true,
	"thanks": true, "thank": true, "thx": true, "appreciate": true,
	"good": true, "nice": true, "cool": true, "sweet": true,
	"brilliant": true, "superb": true, "outstanding": true, "lovely": true,
	"enjoy": true, "fun": true, "excited": true, "thrilled": true,
	"relieved": true, "proud": true, "grateful": true, "impressive": true,
	"helpful": true, "useful": true, "clever": true, "smart": true,
	"beautiful": true, "gorgeous": true, "incredible": true, "best": true,
	"win": true, "won": true, "success": true, "congrats": true,
	"haha": true, "lol": true, "yay": true, "wow": true, "yes": true,
	"agree": true, "exactly": true,
}

var negativeWords = map[string]bool{
	"hate": true, "terrible": true, "awful": true, "horrible": true,
	"bad": true, "worst": true, "sad": true, "angry": true,
	"annoyed": true, "annoying": true, "frustrated": true, "frustrating": true,
	"disappointed": true, "disappointing": true, "upset": true, "hurt": true,
	"wrong": true, "broken": true, "failed": true, "fail": true,
	"useless": true, "stupid": true, "dumb": true, "idiot": true,
	"sucks": true, "suck": true, "ugh": true, "damn": true,
	"worried": true, "anxious": true, "scared": true, "afraid": true,
	"lonely": true, "tired": true, "exhausted": true, "sick": true,
	"pain": true, "cry": true, "crying": true, "miss": true,
	"problem": true, "issue": true, "error": true, "bug": true,
	"slow": true, "confused": true, "confusing": true, "lost": true,
	"sorry": true, "apologize": true,
}

var activatedWords = map[string]bool{
	"excited": true, "thrilled": true, "amazing": true, "wow": true,
	"urgent": true, "asap": true, "quick": true, "now": true,
	"angry": true, "furious": true, "shocked": true, "alarmed": true,
	"yay": true, "haha": true, "lol": true, "incredible": true,
	"emergency": true, "hurry": true, "immediately": true,
}

var calmWords = map[string]bool{
	"calm": true, "relaxed": true, "peaceful": true, "quiet": true,
	"gentle": true, "slow": true, "steady": true, "patient": true,
	"tired": true, "sleepy": true, "rest": true, "soft": true,
}

var negations = map[string]bool{
	"not": true, "no": true, "never": true, "n't": true,
	"don't": true, "doesn't": true, "didn't": true, "won't": true,
	"can't": true, "isn't": true, "wasn't": true, "aren't": true,
	"without": true, "hardly": true, "barely": true,
}

var intensifiers = map[string]bool{
	"very": true, "really": true, "so": true, "extremely": true,
	"incredibly": true, "absolutely": true, "totally": true, "quite": true,
	"super": true, "deeply": true, "truly": true,
}

var wordRe = regexp.MustCompile(`[a-zA-Z']+`)

// frictionPatterns are explicit correction/annoyance signals. They count
// double: the user telling Ghost it got it wrong is the strongest
// negative interaction signal there is.
var frictionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(no[,!]?\s+that'?s (wrong|not))`),
	regexp.MustCompile(`(?i)\b(you'?re wrong|you got it wrong)\b`),
	regexp.MustCompile(`(?i)\b(not what i (asked|meant|wanted))\b`),
	regexp.MustCompile(`(?i)\b(you misunderstood|misunderstood me)\b`),
	regexp.MustCompile(`(?i)\b(stop( it| that)?|shut up)\b`),
	regexp.MustCompile(`(?i)\b(i (said|told you))\b`),
	regexp.MustCompile(`(?i)\b(again\?|we (just|already) (did|covered) this)\b`),
	regexp.MustCompile(`(?i)\b(useless|waste of time)\b`),
}

// Score rates text affect. Returns valence in [-1,1], arousal in [0,1],
// confidence in [0,1], and whether explicit friction was detected.
// Low-signal text scores near zero with low confidence — the engine
// treats that as neutral, never as feeling.
func Score(text string) (valence, arousal, confidence float64, friction bool) {
	words := wordRe.FindAllString(strings.ToLower(text), -1)
	if len(words) == 0 {
		return 0, 0, 0, false
	}
	for _, re := range frictionPatterns {
		if re.MatchString(text) {
			friction = true
			break
		}
	}
	pos, neg, act, calm, hits := 0.0, 0.0, 0.0, 0.0, 0
	negate := false
	for i, w := range words {
		if negations[w] {
			// "not" also reads as mild negative on its own.
			neg += 0.25
			negate = true
			continue
		}
		mult := 1.0
		if i > 0 && intensifiers[words[i-1]] {
			mult = 1.6
		}
		p, n := 0.0, 0.0
		if positiveWords[w] {
			p = mult
		}
		if negativeWords[w] {
			n = mult
		}
		if negate {
			p, n = n, p
			negate = false
		}
		pos += p
		neg += n
		if p+n > 0 {
			hits++
		}
		if activatedWords[w] {
			act += mult
		}
		if calmWords[w] {
			calm += mult
		}
	}
	// Punctuation energy: exclamation and caps read as arousal.
	if strings.Contains(text, "!") {
		act += 0.5
	}
	upper := 0
	for _, w := range strings.Fields(text) {
		if len(w) > 2 && w == strings.ToUpper(w) {
			upper++
		}
	}
	if upper >= 2 {
		act += 0.5
	}
	total := pos + neg
	if total == 0 {
		return 0, clamp01(act * 0.15), 0.1, friction
	}
	valence = (pos - neg) / (total + 1.0)
	arousal = clamp01((act+total*0.2-calm*0.3)*0.25 + 0.2)
	confidence = clamp01(float64(hits) / 6.0)
	if friction {
		valence -= 0.3
		if valence < -1 {
			valence = -1
		}
		confidence = clamp01(confidence + 0.3)
	}
	return valence, arousal, confidence, friction
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
