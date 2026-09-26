package routing

import (
	"strings"
	"sync"

	"github.com/ianclemence/ghost/pkg/providers"
)

// defaultThreshold is used when the config threshold is zero or negative.
const defaultThreshold = 0.35

// Router selects the appropriate model tier for each incoming message.
type Router struct {
	mu         sync.RWMutex
	lightModel string
	threshold  float64
	classifier Classifier
}

// NewRouter creates a new Router with the given light model and threshold.
// SetLightModel updates the light-model target. A model switch must reach the
// router, or routing keeps choosing a model that is no longer the default.
func (r *Router) SetLightModel(model string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lightModel = strings.TrimSpace(model)
}

// LightModel reports the configured light-model target, "" when unset.
func (r *Router) LightModel() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lightModel
}

func NewRouter(lightModel string, threshold float64) *Router {
	if threshold <= 0 {
		threshold = defaultThreshold
	}
	return &Router{
		lightModel: lightModel,
		threshold:  threshold,
		classifier: &RuleClassifier{},
	}
}

// SelectModel returns the model to use for this conversation turn.
func (r *Router) SelectModel(
	msg string,
	history []providers.Message,
	hasMedia bool,
	primaryModel string,
) (model string, score float64) {
	if r == nil || r.LightModel() == "" {
		return primaryModel, 1.0
	}

	features := ExtractFeatures(msg, history, hasMedia)
	score = r.classifier.Score(features)

	if score < r.threshold {
		return r.LightModel(), score
	}
	return primaryModel, score
}

// Threshold returns the complexity threshold in use.
func (r *Router) Threshold() float64 {
	return r.threshold
}
