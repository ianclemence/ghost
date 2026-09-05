package tools

// Provider-backed capability tools: the L3 runtime cutover.
//
// Each tool below fronts a pkg/providers/* capability service (strategy +
// validation + breaker + cache) instead of exec/curl. Properties:
//   - Deterministic: no LLM inside; validated data or honest product error.
//   - Bounded: 30s tool timeout; retry/breaker live inside the strategy.
//   - Least privilege: capabilities list these tools by name; the agent
//     loop's capability enforcement gates wandering generically.
//   - Honest: failures return product-language messages (no API keys,
//     URLs, or stack traces); IsError=true so the loop treats them as
//     errors, not data.
//
// Credentials are read at execution time (skills secrets-first helpers)
// so settings changes apply without restart.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/providers/aqi"
	"github.com/ianclemence/ghost/pkg/providers/crypto"
	"github.com/ianclemence/ghost/pkg/providers/currency"
	"github.com/ianclemence/ghost/pkg/providers/flight"
	"github.com/ianclemence/ghost/pkg/providers/hass"
	"github.com/ianclemence/ghost/pkg/providers/nearby"
	"github.com/ianclemence/ghost/pkg/providers/weather"
	"github.com/ianclemence/ghost/pkg/skills"
)

const providerToolTimeout = 30 * time.Second

func sarg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func farg(args map[string]interface{}, key string) (float64, bool) {
	switch v := args[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

func iarg(args map[string]interface{}, key string, def int) int {
	if f, ok := farg(args, key); ok && f > 0 {
		return int(f)
	}
	return def
}

// providerError converts a failed strategy result into an honest,
// product-language tool error.
func providerError(msg string) *ToolResult {
	return &ToolResult{
		ForLLM:  msg + " (completion: failed; do not present fabricated data)",
		Silent:  false,
		IsError: true,
	}
}

// ---------- weather_now ----------

// WeatherTool fronts the weather capability (Open-Meteo primary,
// OpenWeather fallback). Location wins as lat+lon; otherwise a place
// name is geocoded by the service.
type WeatherTool struct {
	key string
	cfg *weather.Config // nil = defaults (tests inject httptest URLs)
}

func NewWeatherTool(openWeatherKey string) *WeatherTool { return &WeatherTool{key: openWeatherKey} }
func (t *WeatherTool) Name() string                     { return "weather_now" }
func (t *WeatherTool) Description() string {
	return "Get current validated weather for a place or coordinates. Prefer this over shell curl for weather."
}
func (t *WeatherTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location":  map[string]interface{}{"type": "string", "description": "Place name, e.g. Bangkok"},
			"latitude":  map[string]interface{}{"type": "number"},
			"longitude": map[string]interface{}{"type": "number"},
		},
	}
}
func (t *WeatherTool) Timeout() time.Duration { return providerToolTimeout }
func (t *WeatherTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	wcfg := weather.Config{OpenWeatherKey: t.key}
	if t.cfg != nil {
		wcfg = *t.cfg
	}
	svc := weather.New(wcfg)
	ctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	if lat, ok1 := farg(args, "latitude"); ok1 {
		if lon, ok2 := farg(args, "longitude"); ok2 {
			cur, r := svc.CurrentByCoords(ctx, lat, lon, false)
			if r.Err != nil {
				o := product.OutcomeForProviderFailure("weather", r.Failure, r.Err)
				return providerError(o.UserMessage)
			}
			return NewToolResult(fmt.Sprintf("Weather: %.1f°C%s (via %s, observed %s).",
				cur.TemperatureC, descSuffix(cur.Description), r.Provider, cur.ObservedAt.Format("15:04")))
		}
	}
	loc := sarg(args, "location")
	if loc == "" {
		return ErrorResult("weather_now needs location or latitude+longitude. Ask: Which location should I check?")
	}
	cur, r := svc.CurrentByPlace(ctx, loc, false)
	if r.Err != nil {
		o := product.OutcomeForProviderFailure("weather", r.Failure, r.Err)
		return providerError(o.UserMessage)
	}
	return NewToolResult(fmt.Sprintf("Weather in %s: %.1f°C%s (via %s, observed %s).",
		loc, cur.TemperatureC, descSuffix(cur.Description), r.Provider, cur.ObservedAt.Format("15:04")))
}

func descSuffix(d string) string {
	if strings.TrimSpace(d) == "" {
		return ""
	}
	return ", " + d
}

// ---------- flight_status ----------

// FlightTool fronts the flight capability (AviationStack primary,
// AeroDataBox fallback). Either credential enables it.
type FlightTool struct {
	cfg *flight.Config // nil = live defaults (tests inject httptest URLs)
}

func NewFlightTool() *FlightTool   { return &FlightTool{} }
func (t *FlightTool) Name() string { return "flight_status" }
func (t *FlightTool) Description() string {
	return "Check validated flight status by flight number (e.g. TG123). Prefer this over shell curl for flights."
}
func (t *FlightTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"flight_number": map[string]interface{}{"type": "string", "description": "IATA flight code, e.g. TG123"},
		},
		"required": []string{"flight_number"},
	}
}
func (t *FlightTool) Timeout() time.Duration { return providerToolTimeout }
func (t *FlightTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	num := sarg(args, "flight_number")
	if num == "" {
		return ErrorResult("flight_status needs flight_number. Ask: Which flight number should I check? (e.g., TG123 or AA456)")
	}
	fcfg := flight.Config{AviationKey: skills.AviationKey(nil), AeroDataBoxKey: skills.AeroDataBoxKey()}
	if t.cfg != nil {
		fcfg = *t.cfg
	}
	svc := flight.New(fcfg)
	if !svc.Configured() {
		return ErrorResult(product.FriendlyFor("flight", product.ErrConfigRequired))
	}
	ctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	f, r := svc.Lookup(ctx, num)
	if r.Err != nil {
		o := product.OutcomeForProviderFailure("flight", r.Failure, r.Err)
		return providerError(o.UserMessage)
	}
	extra := ""
	if f.Gate != "" {
		extra += " Gate " + f.Gate
	}
	if f.DelayMin != nil && *f.DelayMin > 0 {
		extra += fmt.Sprintf(" (delayed %d min)", *f.DelayMin)
	}
	return NewToolResult(fmt.Sprintf("Flight %s (%s): %s, %s -> %s%s (via %s).",
		f.Number, f.Airline, f.Status, f.From, f.To, extra, r.Provider))
}

// ---------- aqi_now ----------

// AQITool fronts the air-quality capability (Open-Meteo). Accepts
// coordinates or a place name (geocoded internally).
type AQITool struct {
	cfg *aqi.Config    // nil = defaults (tests inject httptest URLs)
	geo *nearby.Config // nil = defaults (geocoding for place names)
}

func NewAQITool() *AQITool      { return &AQITool{} }
func (t *AQITool) Name() string { return "aqi_now" }
func (t *AQITool) Description() string {
	return "Get validated air quality (US AQI) for coordinates or a place. Prefer this over shell curl for AQI."
}
func (t *AQITool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location":  map[string]interface{}{"type": "string"},
			"latitude":  map[string]interface{}{"type": "number"},
			"longitude": map[string]interface{}{"type": "number"},
		},
	}
}
func (t *AQITool) Timeout() time.Duration { return providerToolTimeout }
func (t *AQITool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	ctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	lat, lon := 0.0, 0.0
	if la, ok1 := farg(args, "latitude"); ok1 {
		if lo, ok2 := farg(args, "longitude"); ok2 {
			lat, lon = la, lo
		} else {
			return ErrorResult("aqi_now needs location or latitude+longitude. Ask: Which location should I check?")
		}
	} else if loc := sarg(args, "location"); loc != "" {
		ncfg := nearby.Config{}
		if t.geo != nil {
			ncfg = *t.geo
		}
		nsvc := nearby.New(ncfg)
		var err error
		if lat, lon, err = nsvc.Geocode(ctx, loc); err != nil {
			return ErrorResult("I couldn't find that location. Ask the user to clarify it.")
		}
	} else {
		return ErrorResult("aqi_now needs location or latitude+longitude. Ask: Which location should I check?")
	}
	svc := aqi.New(aqi.Config{})
	if t.cfg != nil {
		svc = aqi.New(*t.cfg)
	}
	rep, r := svc.CurrentByCoords(ctx, lat, lon, false)
	if r.Err != nil {
		o := product.OutcomeForProviderFailure("aqi", r.Failure, r.Err)
		return providerError(o.UserMessage)
	}
	return NewToolResult(fmt.Sprintf("Air quality: US AQI %d (%s) (via %s).", rep.AQI, rep.Category, r.Provider))
}

// ---------- currency_convert ----------

// CurrencyTool fronts the currency capability (er-api primary,
// Frankfurter fallback). Keyless: always ready.
type CurrencyTool struct {
	cfg *currency.Config // nil = defaults (tests inject httptest URLs)
}

func NewCurrencyTool() *CurrencyTool { return &CurrencyTool{} }
func (t *CurrencyTool) Name() string { return "currency_convert" }
func (t *CurrencyTool) Description() string {
	return "Convert amounts between currencies with validated rates. Prefer this over shell curl for currency."
}
func (t *CurrencyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"from":   map[string]interface{}{"type": "string", "description": "Source ISO code, e.g. USD"},
			"to":     map[string]interface{}{"type": "string", "description": "Target ISO code, e.g. EUR"},
			"amount": map[string]interface{}{"type": "number"},
		},
		"required": []string{"from", "to"},
	}
}
func (t *CurrencyTool) Timeout() time.Duration { return providerToolTimeout }
func (t *CurrencyTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	from, to := sarg(args, "from"), sarg(args, "to")
	if from == "" || to == "" {
		return ErrorResult("currency_convert needs from and to currency codes.")
	}
	amount, _ := farg(args, "amount")
	svc := currency.New(currency.Config{})
	if t.cfg != nil {
		svc = currency.New(*t.cfg)
	}
	ctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	c, r := svc.Convert(ctx, from, to, amount)
	if r.Err != nil {
		o := product.OutcomeForProviderFailure("currency", r.Failure, r.Err)
		return providerError(o.UserMessage)
	}
	if amount > 0 {
		return NewToolResult(fmt.Sprintf("%.2f %s = %.2f %s (rate %.4f, via %s).", amount, c.From, c.Converted, c.To, c.Rate, r.Provider))
	}
	return NewToolResult(fmt.Sprintf("Rate: 1 %s = %.4f %s (via %s).", c.From, c.Rate, c.To, r.Provider))
}

// ---------- crypto_price ----------

// CryptoTool fronts the crypto capability (CoinGecko primary, Coinbase
// fallback). Keyless: always ready.
type CryptoTool struct {
	cfg *crypto.Config // nil = defaults (tests inject httptest URLs)
}

func NewCryptoTool() *CryptoTool   { return &CryptoTool{} }
func (t *CryptoTool) Name() string { return "crypto_price" }
func (t *CryptoTool) Description() string {
	return "Get validated crypto price (e.g. bitcoin in USD). Prefer this over shell curl for crypto."
}
func (t *CryptoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{"type": "string", "description": "Coin id, e.g. bitcoin"},
			"vs": map[string]interface{}{"type": "string", "description": "Fiat currency, default USD"},
		},
		"required": []string{"id"},
	}
}
func (t *CryptoTool) Timeout() time.Duration { return providerToolTimeout }
func (t *CryptoTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	id := sarg(args, "id")
	if id == "" {
		return ErrorResult("crypto_price needs a coin id (e.g. bitcoin).")
	}
	vs := sarg(args, "vs")
	if vs == "" {
		vs = "USD"
	}
	svc := crypto.New(crypto.Config{})
	if t.cfg != nil {
		svc = crypto.New(*t.cfg)
	}
	ctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	p, r := svc.GetPrice(ctx, id, vs)
	if r.Err != nil {
		o := product.OutcomeForProviderFailure("crypto", r.Failure, r.Err)
		return providerError(o.UserMessage)
	}
	ch := ""
	if p.Change24h != nil {
		ch = fmt.Sprintf(" (%+.1f%% 24h)", *p.Change24h)
	}
	return NewToolResult(fmt.Sprintf("%s: %.2f %s%s (via %s).", p.ID, p.Value, p.VS, ch, r.Provider))
}

// ---------- places_nearby ----------

// NearbyTool fronts the nearby-places capability (Overpass primary +
// mirror). Accepts coordinates or a place name (geocoded internally).
type NearbyTool struct {
	cfg *nearby.Config // nil = defaults (tests inject httptest URLs)
}

func NewNearbyTool() *NearbyTool   { return &NearbyTool{} }
func (t *NearbyTool) Name() string { return "places_nearby" }
func (t *NearbyTool) Description() string {
	return "Find nearby places (cafes, restaurants, pharmacies...) by coordinates or place name. Prefer this over scripts/shell for nearby search."
}
func (t *NearbyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"near":      map[string]interface{}{"type": "string", "description": "Place name to search near"},
			"latitude":  map[string]interface{}{"type": "number"},
			"longitude": map[string]interface{}{"type": "number"},
			"type":      map[string]interface{}{"type": "string", "description": "Place type, e.g. cafe"},
			"radius":    map[string]interface{}{"type": "number"},
			"limit":     map[string]interface{}{"type": "number"},
		},
	}
}
func (t *NearbyTool) Timeout() time.Duration { return providerToolTimeout }
func (t *NearbyTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	ctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	svc := nearby.New(nearby.Config{})
	if t.cfg != nil {
		svc = nearby.New(*t.cfg)
	}
	lat, lon := 0.0, 0.0
	if la, ok1 := farg(args, "latitude"); ok1 {
		if lo, ok2 := farg(args, "longitude"); ok2 {
			lat, lon = la, lo
		}
	}
	if lat == 0 && lon == 0 {
		near := sarg(args, "near")
		if near == "" {
			return ErrorResult("places_nearby needs near or latitude+longitude. Ask: Which location should I search near?")
		}
		var err error
		if lat, lon, err = svc.Geocode(ctx, near); err != nil {
			return ErrorResult("I couldn't find that location. Ask the user to clarify it.")
		}
	}
	amenity := sarg(args, "type")
	if amenity == "" {
		amenity = "restaurant"
	}
	places, r := svc.SearchByCoords(ctx, amenity, lat, lon, iarg(args, "radius", 1500), iarg(args, "limit", 10))
	if r.Err != nil {
		o := product.OutcomeForProviderFailure("nearby", r.Failure, r.Err)
		return providerError(o.UserMessage)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d %s(s) (via %s):", len(places), amenity, r.Provider)
	for i, p := range places {
		fmt.Fprintf(&b, "\n%d. %s (%dm)", i+1, p.Name, p.Distance)
	}
	return NewToolResult(b.String())
}

// ---------- hass ----------

// HassTool fronts the Home Assistant device capability (states +
// actuation through the device REST API). Reads and writes both run
// under the capability's consequential risk: the broker asks first in
// ask mode. Credentials come from the vault path, never arguments.
type HassTool struct {
	cfg *hass.Config // nil = live defaults (tests inject httptest URLs)
}

func NewHassTool() *HassTool     { return &HassTool{} }
func (t *HassTool) Name() string { return "hass" }
func (t *HassTool) Description() string {
	return "Read Home Assistant device states or control a device (light.turn_off etc.). Prefer this over shell curl for Home Assistant."
}
func (t *HassTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":    map[string]interface{}{"type": "string", "description": "list, state, turn_on, turn_off"},
			"entity_id": map[string]interface{}{"type": "string", "description": "e.g. light.bedroom"},
			"domain":    map[string]interface{}{"type": "string", "description": "e.g. light (default from entity_id)"},
		},
		"required": []string{"action"},
	}
}
func (t *HassTool) Timeout() time.Duration { return providerToolTimeout }
func (t *HassTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	hassURL, token := skills.HassEndpoint()
	hcfg := hass.Config{Base: hassURL, Token: token}
	if t.cfg != nil {
		hcfg = *t.cfg
	}
	svc := hass.New(hcfg)
	if !hcfg.Configured() {
		return ErrorResult(product.FriendlyFor("hass", product.ErrConfigRequired))
	}
	ctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	action := strings.ToLower(sarg(args, "action"))
	switch action {
	case "list", "states", "":
		ents, r := svc.States(ctx)
		if r.Err != nil {
			o := product.OutcomeForProviderFailure("hass", r.Failure, r.Err)
			return providerError(o.UserMessage)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d devices (via home assistant):", len(ents))
		for i, e := range ents {
			if i >= 20 {
				fmt.Fprintf(&b, "\n… and %d more", len(ents)-20)
				break
			}
			name := e.Name
			if name == "" {
				name = e.ID
			}
			fmt.Fprintf(&b, "\n- %s: %s", name, e.State)
		}
		return NewToolResult(b.String())
	case "state":
		entity := sarg(args, "entity_id")
		if entity == "" {
			return ErrorResult("hass state needs entity_id (e.g. light.bedroom).")
		}
		ents, r := svc.States(ctx)
		if r.Err != nil {
			o := product.OutcomeForProviderFailure("hass", r.Failure, r.Err)
			return providerError(o.UserMessage)
		}
		for _, e := range ents {
			if strings.EqualFold(e.ID, entity) {
				name := e.Name
				if name == "" {
					name = e.ID
				}
				return NewToolResult(fmt.Sprintf("%s is %s.", name, e.State))
			}
		}
		return ErrorResult(fmt.Sprintf("I couldn't find %s among your devices.", entity))
	case "turn_on", "turn_off":
		entity := sarg(args, "entity_id")
		if entity == "" {
			return ErrorResult("hass " + action + " needs entity_id (e.g. light.bedroom).")
		}
		domain := sarg(args, "domain")
		if domain == "" {
			if parts := strings.SplitN(entity, ".", 2); len(parts) == 2 {
				domain = parts[0]
			}
		}
		service := action
		r := svc.Actuate(ctx, domain, service, entity)
		if r.Err != nil {
			o := product.OutcomeForProviderFailure("hass", r.Failure, r.Err)
			return providerError(o.UserMessage)
		}
		verb := "on"
		if action == "turn_off" {
			verb = "off"
		}
		return NewToolResult(fmt.Sprintf("Turned %s %s.", entity, verb))
	default:
		return ErrorResult("hass needs action list, state, turn_on, or turn_off.")
	}
}
