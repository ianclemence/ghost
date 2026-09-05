package skills

import (
	"testing"
)

// The L3 cutover contract: each migrated capability must allow its
// provider-backed tool (deterministic primary path). If a capability
// drops its tool here, the agent loop's enforcement will block the tool
// at runtime — this test fails closed on that regression.
func TestCapabilityAllowsProviderTools(t *testing.T) {
	cases := map[string]string{
		"weather":     "weather_now",
		"aqi":         "aqi_now",
		"currency":    "currency_convert",
		"crypto":      "crypto_price",
		"flight":      "flight_status",
		"find-nearby": "places_nearby",
	}
	for skill, tool := range cases {
		cap := GetCapability(skill)
		if !cap.Allows(tool) {
			t.Fatalf("capability %s (%s) must allow tool %s", skill, cap.ID, tool)
		}
		if cap.Primary == "" {
			t.Fatalf("capability %s must declare a primary", skill)
		}
	}
}
