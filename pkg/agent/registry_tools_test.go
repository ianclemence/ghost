package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
)

// The registry the agent loop builds must contain every provider-backed
// capability tool; otherwise the capability contracts allow tools the
// model can never invoke (cutover regression fails closed here).
func TestCreateToolRegistryHasProviderTools(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := createToolRegistry(t.TempDir(), false, cfg, bus.NewMessageBus())
	for _, name := range []string{
		"weather_now", "flight_status", "aqi_now",
		"currency_convert", "crypto_price", "places_nearby",
	} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("registry missing provider tool %s", name)
		}
	}
}
