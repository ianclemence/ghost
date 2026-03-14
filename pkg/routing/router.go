package routing

import "strings"

type Router struct {
	LightModel string
	Threshold  float64
}

func NewRouter(lightModel string, threshold float64) *Router {
	if threshold <= 0 {
		threshold = 0.35
	}
	return &Router{LightModel: lightModel, Threshold: threshold}
}

func (r *Router) SelectModel(prompt string, historyCount int, hasMedia bool, primary string) (string, float64) {
	if r == nil || r.LightModel == "" {
		return primary, 1
	}
	score := complexityScore(prompt, historyCount, hasMedia)
	if score < r.Threshold {
		return r.LightModel, score
	}
	return primary, score
}

func complexityScore(prompt string, historyCount int, hasMedia bool) float64 {
	score := 0.0
	if len(prompt) > 400 {
		score += 0.35
	}
	if len(prompt) > 1200 {
		score += 0.25
	}
	if strings.Contains(prompt, "```") {
		score += 0.25
	}
	if hasMedia {
		score += 0.35
	}
	if historyCount > 10 {
		score += 0.2
	}
	if score > 1 {
		score = 1
	}
	return score
}
