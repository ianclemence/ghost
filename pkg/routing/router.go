package routing

import (
	"github.com/ianclemence/ghost/pkg/providers"
)

// defaultThreshold is used when the config threshold is zero or negative.
const defaultThreshold = 0.35

// Router selects the appropriate model tier for each incoming message.
type Router struct {
	lightModel string
	threshold  float64
	classifier Classifier
}

// NewRouter creates a new Router with the given light model and threshold.
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
	if r == nil || r.lightModel == "" {
		return primaryModel, 1.0
	}

	features := ExtractFeatures(msg, history, hasMedia)
	score = r.classifier.Score(features)

	if score < r.threshold {
		return r.lightModel, score
	}
	return primaryModel, score
}

// LightModel returns the configured light model name.
func (r *Router) LightModel() string {
	return r.lightModel
}

// Threshold returns the complexity threshold in use.
func (r *Router) Threshold() float64 {
	return r.threshold
}
