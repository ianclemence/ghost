package sting

import (
	"fmt"
	"strings"
)

// MaxRouterTools is the hard cap per routing turn. It mirrors the
// engine's own retrieval width (top-5): more tools never reach the
// grammar, they only add latency and misroutes.
const MaxRouterTools = 5

// DefaultRouterTools is the curated preference order for the fast-path:
// read-only, low-blast-radius tools first. The caller intersects this
// with actually-registered tools; entries that are not registered (or
// are disabled) are skipped, never fabricated.
var DefaultRouterTools = []string{
	"web_search",
	"web_fetch",
	"weather_now",
	"currency_convert",
	"memory_recall",
}

// Subset intersects registered tool names with the preference order and
// caps the result at max (default MaxRouterTools). Order is preference
// order, so the most constrained tools enter the grammar first.
func Subset(registered []string, prefer []string, max int) []string {
	if max <= 0 {
		max = MaxRouterTools
	}
	if len(prefer) == 0 {
		prefer = DefaultRouterTools
	}
	have := map[string]bool{}
	for _, n := range registered {
		have[strings.TrimSpace(n)] = true
	}
	var out []string
	for _, n := range prefer {
		if len(out) >= max {
			break
		}
		if have[n] {
			out = append(out, n)
		}
	}
	return out
}

// ValidateSubset rejects oversized or empty subsets before they reach
// the engine. An oversized set is a caller bug: truncate with Subset,
// do not silently widen the grammar.
func ValidateSubset(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("sting: no routable tools")
	}
	if len(names) > MaxRouterTools {
		return fmt.Errorf("sting: %d tools exceeds router cap %d", len(names), MaxRouterTools)
	}
	return nil
}
