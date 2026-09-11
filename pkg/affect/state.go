package affect

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

// Affect mechanics, stated plainly:
//   - Valence [-1,1]: how positive the relationship feels right now.
//   - Arousal [0,1]: how charged (urgent, excited, upset) vs calm.
//   - Affinity [0,1]: the slow relationship depth. Rises on sustained
//     warmth, falls on friction, cools toward neutral without contact.
//     Affinity gates PROACTIVITY (when to reach out), never CAPABILITY
//     (low affinity never means worse answers — that would be punishment
//     mechanics, and this package refuses to build that).
//
// Only aggregates persist (state.json). Raw turn scores are never stored:
// the retention bound is structural, not a policy promise.

const (
	// ValenceAlpha is the per-turn pull toward the sampled valence.
	ValenceAlpha = 0.3
	// AffinityUp rewards clearly warm turns; AffinityDown punishes
	// friction or clearly cold ones. Asymmetric by design: trust builds
	// slowly and breaks quickly.
	AffinityUp   = 0.04
	AffinityDown = 0.08
	// NeutralAffinity is the relationship baseline new installs start at
	// and idle state cools toward.
	NeutralAffinity = 0.5
	// MoodHalfLife is how fast valence/arousal cool toward neutral.
	MoodHalfLife = 6 * time.Hour
	// AffinityHalfLife is how fast affinity cools toward neutral.
	AffinityHalfLife = 30 * 24 * time.Hour
	// WarmTurn / ColdTurn thresholds on sampled valence for affinity moves.
	WarmTurn = 0.3
	ColdTurn = -0.4
	// MinConfidence turns below this score as neutral (no evidence).
	MinConfidence = 0.15
)

// State is the persisted relational aggregate.
type State struct {
	Valence   float64   `json:"valence"`
	Arousal   float64   `json:"arousal"`
	Affinity  float64   `json:"affinity"`
	Turns     int       `json:"turns"`
	WarmTurns int       `json:"warm_turns"`
	ColdTurns int       `json:"cold_turns"`
	UpdatedAt time.Time `json:"updated_at"`
}

// New returns the neutral starting state.
func New() State {
	return State{Affinity: NeutralAffinity, UpdatedAt: time.Now()}
}

// Turn applies one scored turn. Friction (explicit correction, turn error)
// moves affinity down even when the words score neutral. Low-confidence
// samples leave mood alone but still count the turn.
func (s State) Turn(v, a, confidence float64, friction bool, now time.Time) State {
	s = s.decay(now)
	s.Turns++
	// Below-confidence samples are noise: mood holds, affinity moves only
	// on explicit friction (a correction counts even in plain words).
	effective := v
	if confidence < MinConfidence {
		effective = 0
	} else {
		s.Valence += ValenceAlpha * (v - s.Valence)
		s.Arousal += ValenceAlpha * (a - s.Arousal)
	}
	switch {
	case friction || effective <= ColdTurn:
		s.Affinity -= AffinityDown
		s.ColdTurns++
	case effective >= WarmTurn:
		s.Affinity += AffinityUp
		s.WarmTurns++
	}
	s.Affinity = clamp01(s.Affinity)
	s.Valence = clampSigned(s.Valence)
	s.Arousal = clamp01(s.Arousal)
	s.UpdatedAt = now
	return s
}

// decay cools mood toward neutral and affinity toward baseline over the
// elapsed time. Relationships cool without contact — honestly modeled.
func (s State) decay(now time.Time) State {
	dt := now.Sub(s.UpdatedAt)
	if dt <= 0 {
		return s
	}
	moodK := decayFactor(dt, MoodHalfLife)
	affK := decayFactor(dt, AffinityHalfLife)
	s.Valence *= moodK
	s.Arousal *= moodK
	s.Affinity = NeutralAffinity + (s.Affinity-NeutralAffinity)*affK
	s.UpdatedAt = now
	return s
}

// Decayed returns the state cooled to now without recording a turn.
func (s State) Decayed(now time.Time) State {
	return s.decay(now)
}

func decayFactor(dt, halfLife time.Duration) float64 {
	if halfLife <= 0 || dt <= 0 {
		return 1
	}
	return math.Exp(-0.6931471805599453 * float64(dt) / float64(halfLife))
}

// AffinityBand names the relationship depth for prompts and display.
func (s State) AffinityBand() string {
	switch {
	case s.Affinity < 0.3:
		return "distant"
	case s.Affinity < 0.6:
		return "cordial"
	case s.Affinity < 0.85:
		return "warm"
	default:
		return "close"
	}
}

// MoodBand names the current mood for prompts and display.
func (s State) MoodBand() string {
	switch {
	case s.Valence >= 0.5:
		return "bright"
	case s.Valence >= 0.15:
		return "mildly positive"
	case s.Valence > -0.15:
		return "near neutral"
	case s.Valence > -0.5:
		return "mildly low"
	default:
		return "low"
	}
}

// Render returns the one-line grounded relational state for prompt
// injection. The model expresses affect from THIS, never from vibes:
// if it says it's glad, the numbers must show it.
func (s State) Render(now time.Time) string {
	s = s.Decayed(now)
	return fmt.Sprintf("Relational state: affinity %.2f (%s); mood %+.2f (%s).",
		s.Affinity, s.AffinityBand(), s.Valence, s.MoodBand())
}

// affectFileName is the aggregate file inside personal-context: personal
// data, cleared by /reset context, never synced anywhere.
const affectFileName = "affect.json"

// Path returns the affect file for a workspace.
func Path(workspace string) string {
	return filepath.Join(workspace, "personal-context", affectFileName)
}

// Load reads the aggregate, returning neutral state when absent. A corrupt
// file fails loudly (never invent feeling) — the caller decides recovery.
func Load(workspace string) (State, error) {
	data, err := os.ReadFile(Path(workspace))
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("affect: corrupt state file: %w", err)
	}
	return s.Decayed(time.Now()), nil
}

// Save persists aggregates atomically (0600). Only aggregates — callers
// must never stash raw turn text here.
func Save(workspace string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(Path(workspace))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".affect-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, Path(workspace))
}

func clampSigned(v float64) float64 {
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}
