# Ghost Appliance Architecture (product direction: "Your AI. Your memory. Your machine.")

## Layers

- **User concepts** (`pkg/product`): completion states (success/failed/waiting_for_* /
  temporarily_unavailable/offline/cancelled), error classes mapped to product
  language, typed event visibility (only `user_visible_*` reaches UI/API).
- **Provider resilience** (`pkg/provider`): failure classes, bounded
  exponential-backoff retry (no retry on auth/credential failures), circuit
  breaker with cooldown + probe, capability-declared ordered fallback, response
  validation, selective TTL cache with provenance + stale semantics.
- **Canonical health** (`pkg/health`): one source of truth (core, local AI,
  memory, storage, security, network, remote access, automations,
  integrations, backup, updates). Health ≠ configuration: `needs_authorization`
  vs `provider down` vs `revoked` are distinct states. Served at
  `GET /api/admin/health`; the Web Console renders it, never rebuilds it.
- **Provisioning** (`pkg/setup`): idempotent orchestrator
  (`not_initialized → initializing → ready/degraded/action_required/recovering`)
  with disk checkpoints; interrupted setup resumes, double-run never destroys
  state.
- **Calendar**: product path is OAuth 2.0 web-server flow
  (`pkg/skills/calendar_web_oauth.go`): narrowest scopes (readonly default,
  events only for writing), random single-use state (CSRF, 10-min TTL),
  LAN-direct callback (`/oauth/calendar/callback` on ghost-web and the
  gateway) plus relay-hosted callback for remote setup
  (`GET /oauth/calendar/callback?ghost=<id>&code=..&state=..` on
  `pkg/relay/server`: allowlisted provider/path/query, per-IP rate limit,
  forwards over the authenticated device tunnel; query strings now traverse
  the tunnel via `proto.HTTPMetadata.Query`). Start: `POST
  /api/admin/integrations/calendar/oauth/start` → Google consent URL.
  Scope justification + verification checklist:
  `pkg/skills/calendar_verification.go` (code-enforced items tested) +
  generated submission packet `VerificationPacketFor` served at
  `GET /api/admin/integrations/calendar/verify-packet` +
  `pkg/doctor` `calendar_oauth` readiness check (client-ID format,
  redirect scheme/host/path, DNS resolution). Only the Google form clicks
  themselves remain human.
- **Weather** (`pkg/providers/weather`): reference implementation — Open-Meteo
  primary (keyless), OpenWeather fallback (keyed, skipped honestly when
  unconfigured), semantic validation (rejects impossible values), 10-min cache,
  explicit stale semantics offline. **Live-tested against real vendors.**
- **Flight** (`pkg/providers/flight`): AviationStack primary (100 req/mo
  free, schedule+status+gates) + AeroDataBox fallback (600 units/mo free,
  history + future schedules + codeshare resolution); either key alone =
  READY (`skills.FlightConfigured`); normalized statuses; 2-min repeat
  cache (live status is mutable). Rejected: OpenSky (no flight-number
  lookup, non-commercial terms), paid-only vendors.
- **AQI / currency / crypto / nearby** (`pkg/providers/{aqi,currency,
  crypto,nearby}` + shared `pkg/providers/httpx`): on the strategy with
  keyless primary+fallback pairs (er-api→Frankfurter, CoinGecko→Coinbase,
  Overpass primary→mirror; AQI single-provider Open-Meteo — no honest
  second keyless vendor exists, documented). **Runtime cutover complete:**
  `pkg/tools/providers.go` registers `weather_now`, `flight_status`,
  `aqi_now`, `currency_convert`, `crypto_price`, `places_nearby` in the
  agent loop; capability contracts allow them (enforcement promotes +
  gates generically); each SKILL.md instructs the model to call the tool
  first with exec/curl retained as fallback. Cutover regressions fail
  closed via `TestCapabilityAllowsProviderTools` +
  `TestCreateToolRegistryHasProviderTools`.
- **Backups** (`pkg/backup`): centralized exclusion enforced by the Web
  Console walker (was ad-hoc): `.secrets.json`, `.env`, `.credentials/`,
  `.calendar/`, `calendar-token.json`, gcalcli oauth, logs, transient
  state dirs. Synthetic-tree test proves zero secrets archived. Restore
  requires reconnecting integrations — by design.
- **Live tests** (`pkg/providers/live`, `GHOST_LIVE_TESTS=1`): real HTTP
  through strategy+validation+cache; infra failures skip as
  EXTERNAL_SERVICE_UNAVAILABLE, 200-but-invalid fails as GHOST_DEFECT;
  keyed vendors skip as NEEDS_CONFIGURATION without keys.
- **Scheduler** (`pkg/scheduled/policy.go`): missed-run policy per type
  (reminders → notify, automations → next, one-shots → run-once inside 24h
  grace), timezone precedence (explicit > user > Ghost > UTC), deterministic
  execution keys (restart-safe idempotency with existing `HasExecution` dedup).
- **Pending requests** (`pkg/skills/pending_store.go`): durable continuations
  (unique ID, expiry, session, capability, sanitized intent, continuation
  state, completion/cancellation) surviving restart; no secrets stored.
- **Personal model**: `pkg/personalcontext` (append-only log, current /
  superseded / conflicting / uncertain states, provenance, temporal validity)
  remains the store; retrieval combines keyword, recency, importance, and
  validity signals.

## Rules

- No fake success: report `Completion*` honestly; validate before success.
- No implementation jargon in user messages (`pkg/product.FriendlyFor`).
- No LLM-invented fallbacks: capability declares providers in order.
- No secrets in logs/SSE/activity/memory/backups/diagnostics (redacted structs).
- Deterministic fast paths where no intelligence is needed (readiness, health,
  retry, validation, titles); LLM for understanding, extraction, and response.
