// Package calendar fronts the Google Calendar API with the same shape as
// the Gmail provider: sealed OAuth via pkg/skills (refresh handled there),
// breaker + cache for honest failure semantics, and typed results carrying
// the provider failure taxonomy. The model never sees tokens; tools receive
// only event summaries.
package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/skills"
)

// Config mirrors the other data providers: injectable HTTP client + base
// URL for tests, breaker + cache TTLs.
type Config struct {
	HTTPClient      *http.Client
	Base            string
	CacheTTL        time.Duration
	BreakerCooldown time.Duration
	// TokenSource supplies OAuth access tokens; defaults to the sealed
	// skills store. Overridden in tests so no real credential is needed.
	TokenSource func(ctx context.Context, needWrite bool) (string, error)
	// Connected reports OAuth readiness; defaults to the skills status.
	// Overridden in tests.
	Connected func() bool
}

func (c Config) withDefaults() Config {
	if c.Base == "" {
		c.Base = "https://www.googleapis.com/calendar/v3"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if c.CacheTTL == 0 {
		c.CacheTTL = 2 * time.Minute
	}
	if c.BreakerCooldown == 0 {
		c.BreakerCooldown = 60 * time.Second
	}
	if c.TokenSource == nil {
		c.TokenSource = skills.CalendarAccessToken
	}
	if c.Connected == nil {
		c.Connected = func() bool { return skills.CalendarWebStatus().Connected }
	}
	return c
}

// Event is the redacted projection handed to the model/tools.
type Event struct {
	ID      string `json:"id"`
	Summary string `json:"summary,omitempty"`
	Start   string `json:"start,omitempty"`
	End     string `json:"end,omitempty"`
	Link    string `json:"link,omitempty"`
}

// Service fronts the Calendar API with breaker + cache.
type Service struct {
	cfg    Config
	cache  *provider.Cache[[]Event]
	break_ *provider.Breaker
}

func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{
		cfg:    cfg,
		cache:  provider.NewCache[[]Event](cfg.CacheTTL),
		break_: provider.NewBreaker(3, cfg.BreakerCooldown),
	}
}

// Configured reports whether calendar OAuth is connected.
func (s *Service) Configured() bool {
	return s.cfg.Connected()
}

func (s *Service) authed(ctx context.Context, method, path string, needWrite bool) (*http.Response, error) {
	access, err := s.cfg.TokenSource(ctx, needWrite)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.Base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	return s.cfg.HTTPClient.Do(req)
}

func checkStatus(resp *http.Response, what string) provider.FailureClass {
	switch resp.StatusCode {
	case 200, 201, 204:
		return ""
	case 401, 403:
		return provider.FailAuth
	case 404:
		return provider.FailInvalid
	case 429:
		return provider.FailRateLimited
	default:
		if resp.StatusCode >= 500 {
			return provider.FailServer
		}
		return provider.FailInvalid
	}
}

type apiEvent struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	HTMLLink string `json:"htmlLink"`
	Start   struct {
		DateTime string `json:"dateTime"`
		Date     string `json:"date"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
		Date     string `json:"date"`
	} `json:"end"`
}

func toEvent(e apiEvent) Event {
	start := e.Start.DateTime
	if start == "" {
		start = e.Start.Date
	}
	end := e.End.DateTime
	if end == "" {
		end = e.End.Date
	}
	return Event{ID: e.ID, Summary: e.Summary, Start: start, End: end, Link: e.HTMLLink}
}

// Agenda lists upcoming events on the primary calendar, soonest first.
func (s *Service) Agenda(ctx context.Context, max int) ([]Event, provider.Result[[]Event]) {
	fail := func(class provider.FailureClass, err error) ([]Event, provider.Result[[]Event]) {
		return nil, provider.Result[[]Event]{Failure: class, Err: err}
	}
	if !s.Configured() {
		return fail(provider.FailNotConfigured, fmt.Errorf("calendar not connected"))
	}
	if max <= 0 || max > 20 {
		max = 10
	}
	if cached, ok, _, _ := s.cache.Get("agenda"); ok {
		_ = cached
	}
	if !s.break_.Allow() {
		return fail(provider.FailUnavailable, fmt.Errorf("calendar temporarily unavailable"))
	}
	path := "/calendars/primary/events?singleEvents=true&orderBy=startTime&timeMin=" +
		url.QueryEscape(time.Now().UTC().Format(time.RFC3339)) + "&maxResults=" + itoa(max)
	resp, err := s.authed(ctx, "GET", path, false)
	if err != nil {
		if strings.Contains(err.Error(), "calendar_oauth_") {
			return fail(provider.FailAuth, err)
		}
		s.break_.RecordFailure(provider.FailNetwork)
		return fail(provider.FailNetwork, err)
	}
	defer resp.Body.Close()
	if class := checkStatus(resp, "agenda"); class != "" {
		s.break_.RecordFailure(class)
		return fail(class, fmt.Errorf("calendar agenda status %d", resp.StatusCode))
	}
	var list struct {
		Items []apiEvent `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return fail(provider.FailMalformed, err)
	}
	out := make([]Event, 0, len(list.Items))
	for _, e := range list.Items {
		out = append(out, toEvent(e))
	}
	s.break_.RecordSuccess()
	s.cache.Set("agenda", out, "calendar")
	return out, provider.Result[[]Event]{Value: out, Provider: "calendar"}
}

// QuickAdd creates an event from natural language ("Dentist tomorrow at
// 2pm"), replacing gcalcli's quick command with the API equivalent.
func (s *Service) QuickAdd(ctx context.Context, text string) (Event, provider.Result[Event]) {
	fail := func(class provider.FailureClass, err error) (Event, provider.Result[Event]) {
		return Event{}, provider.Result[Event]{Failure: class, Err: err}
	}
	if !s.Configured() {
		return fail(provider.FailNotConfigured, fmt.Errorf("calendar not connected"))
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fail(provider.FailInvalid, fmt.Errorf("event text required"))
	}
	if !s.break_.Allow() {
		return fail(provider.FailUnavailable, fmt.Errorf("calendar temporarily unavailable"))
	}
	resp, err := s.authed(ctx, "POST", "/calendars/primary/events/quickAdd?text="+url.QueryEscape(text), true)
	if err != nil {
		if strings.Contains(err.Error(), "calendar_oauth_") {
			return fail(provider.FailAuth, err)
		}
		s.break_.RecordFailure(provider.FailNetwork)
		return fail(provider.FailNetwork, err)
	}
	defer resp.Body.Close()
	if class := checkStatus(resp, "quickAdd"); class != "" {
		s.break_.RecordFailure(class)
		return fail(class, fmt.Errorf("calendar quickAdd status %d", resp.StatusCode))
	}
	var created apiEvent
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return fail(provider.FailMalformed, err)
	}
	s.break_.RecordSuccess()
	ev := toEvent(created)
	return ev, provider.Result[Event]{Value: ev, Provider: "calendar"}
}

// DeleteByQuery deletes the first event matching q, mirroring the legacy
// delete-by-title behaviour. Returns the deleted summary for evidence.
func (s *Service) DeleteByQuery(ctx context.Context, q string) (Event, provider.Result[Event]) {
	fail := func(class provider.FailureClass, err error) (Event, provider.Result[Event]) {
		return Event{}, provider.Result[Event]{Failure: class, Err: err}
	}
	if !s.Configured() {
		return fail(provider.FailNotConfigured, fmt.Errorf("calendar not connected"))
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return fail(provider.FailInvalid, fmt.Errorf("event query required"))
	}
	if !s.break_.Allow() {
		return fail(provider.FailUnavailable, fmt.Errorf("calendar temporarily unavailable"))
	}
	resp, err := s.authed(ctx, "GET", "/calendars/primary/events?q="+url.QueryEscape(q)+"&maxResults=5&singleEvents=true", false)
	if err != nil {
		if strings.Contains(err.Error(), "calendar_oauth_") {
			return fail(provider.FailAuth, err)
		}
		s.break_.RecordFailure(provider.FailNetwork)
		return fail(provider.FailNetwork, err)
	}
	var list struct {
		Items []apiEvent `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		resp.Body.Close()
		return fail(provider.FailMalformed, err)
	}
	resp.Body.Close()
	if len(list.Items) == 0 {
		return fail(provider.FailInvalid, fmt.Errorf("no event matches %q", q))
	}
	target := toEvent(list.Items[0])
	del, err := s.authed(ctx, "DELETE", "/calendars/primary/events/"+url.PathEscape(target.ID), true)
	if err != nil {
		if strings.Contains(err.Error(), "calendar_oauth_") {
			return fail(provider.FailAuth, err)
		}
		s.break_.RecordFailure(provider.FailNetwork)
		return fail(provider.FailNetwork, err)
	}
	defer del.Body.Close()
	if class := checkStatus(del, "delete"); class != "" {
		s.break_.RecordFailure(class)
		return fail(class, fmt.Errorf("calendar delete status %d", del.StatusCode))
	}
	s.break_.RecordSuccess()
	return target, provider.Result[Event]{Value: target, Provider: "calendar"}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
