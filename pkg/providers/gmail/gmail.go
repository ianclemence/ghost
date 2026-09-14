package gmail

// Gmail API provider: search/list messages and send mail on the user's
// behalf. Authentication is OAuth via pkg/skills (sealed refresh token,
// in-memory access token); the raw secret never reaches the model, tools
// receive only redacted message content. One-time codes, password-reset
// links, and magic links are filtered from bodies before display.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/skills"
)

// Config mirrors the other data providers (flight/weather): injectable HTTP
// client + base URL for tests, breaker + cache TTLs.
type Config struct {
	HTTPClient      *http.Client
	Base            string
	CacheTTL        time.Duration
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	if c.Base == "" {
		c.Base = "https://gmail.googleapis.com/gmail/v1"
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
	return c
}

// Message is the redacted projection handed to the model/tools.
type Message struct {
	ID      string `json:"id"`
	Thread  string `json:"thread_id,omitempty"`
	From    string `json:"from,omitempty"`
	Subject string `json:"subject,omitempty"`
	Date    string `json:"date,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Body    string `json:"body,omitempty"`
}

// Service fronts the Gmail API with strategy fallback (single provider;
// breaker + cache still apply for honest failure semantics).
type Service struct {
	cfg    Config
	cache  *provider.Cache[[]Message]
	break_ *provider.Breaker
}

func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{
		cfg:    cfg,
		cache:  provider.NewCache[[]Message](cfg.CacheTTL),
		break_: provider.NewBreaker(3, cfg.BreakerCooldown),
	}
}

// Configured reports whether Gmail OAuth is connected.
func (s *Service) Configured() bool {
	return skills.GmailWebStatus().Connected
}

type messageListResp struct {
	Messages []struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	} `json:"messages"`
}

type messageGetResp struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
	Snippet  string `json:"snippet"`
	Payload  struct {
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		Body struct {
			Data string `json:"data"`
		} `json:"body"`
		Parts []struct {
			MimeType string `json:"mimeType"`
			Body     struct {
				Data string `json:"data"`
			} `json:"body"`
		} `json:"parts"`
	} `json:"payload"`
}

func headerOf(m messageGetResp, name string) string {
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// FilterSensitive strips one-time codes, password-reset links, and magic
// login links from body text so inbox access can't become account takeover.
func FilterSensitive(body string) string {
	lines := strings.Split(body, "\n")
	kept := lines[:0]
	for _, ln := range lines {
		l := strings.ToLower(ln)
		if strings.Contains(l, "one-time") || strings.Contains(l, "one time") ||
			strings.Contains(l, "verification code") || strings.Contains(l, "verify your") && strings.Contains(l, "code") {
			kept = append(kept, "[verification code redacted]")
			continue
		}
		if strings.Contains(l, "password reset") || strings.Contains(l, "reset your password") ||
			strings.Contains(l, "magic link") || strings.Contains(l, "sign-in link") || strings.Contains(l, "signin link") {
			kept = append(kept, "[sign-in link redacted]")
			continue
		}
		kept = append(kept, ln)
	}
	return strings.Join(kept, "\n")
}

func (s *Service) authedGET(ctx context.Context, access, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.cfg.Base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	return s.cfg.HTTPClient.Do(req)
}

// Search lists recent messages matching q (Gmail query syntax), newest
// first, with redacted metadata + snippet. No bodies (cheap + safe).
func (s *Service) Search(ctx context.Context, q string, max int) ([]Message, provider.Result[[]Message]) {
	if !s.Configured() {
		return nil, provider.Result[[]Message]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("gmail not connected")}
	}
	if max <= 0 || max > 20 {
		max = 10
	}
	if cached, ok, _, _ := s.cache.Get("search:" + q); ok {
		_ = cached
	}
	access, err := skills.GmailAccessToken(ctx, false)
	if err != nil {
		return nil, provider.Result[[]Message]{Failure: provider.FailAuth, Err: err}
	}
	if !s.break_.Allow() {
		return nil, provider.Result[[]Message]{Failure: provider.FailUnavailable, Err: fmt.Errorf("gmail temporarily unavailable")}
	}
	resp, err := s.authedGET(ctx, access, "/users/me/messages?maxResults="+itoa(max)+"&q="+url.QueryEscape(q))
	if err != nil {
		s.break_.RecordFailure(provider.FailNetwork)
		return nil, provider.Result[[]Message]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		s.break_.RecordFailure(provider.FailAuth)
		return nil, provider.Result[[]Message]{Failure: provider.FailAuth, Err: fmt.Errorf("gmail unauthorized")}
	}
	if resp.StatusCode != 200 {
		s.break_.RecordFailure(provider.FailServer)
		return nil, provider.Result[[]Message]{Failure: provider.FailServer, Err: fmt.Errorf("gmail status %d", resp.StatusCode)}
	}
	var list messageListResp
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, provider.Result[[]Message]{Failure: provider.FailMalformed, Err: err}
	}
	out := make([]Message, 0, len(list.Messages))
	for _, m := range list.Messages {
		detail, err := s.fetchMeta(ctx, access, m.ID)
		if err != nil {
			continue
		}
		detail.Thread = m.ThreadID
		out = append(out, detail)
	}
	s.break_.RecordSuccess()
	s.cache.Set("search:"+q, out, "gmail")
	return out, provider.Result[[]Message]{Value: out, Provider: "gmail"}
}

func (s *Service) fetchMeta(ctx context.Context, access, id string) (Message, error) {
	resp, err := s.authedGET(ctx, access, "/users/me/messages/"+id+"?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date")
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Message{}, fmt.Errorf("gmail status %d", resp.StatusCode)
	}
	var m messageGetResp
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return Message{}, err
	}
	return Message{
		ID:      m.ID,
		From:    headerOf(m, "From"),
		Subject: headerOf(m, "Subject"),
		Date:    headerOf(m, "Date"),
		Snippet: FilterSensitive(m.Snippet),
	}, nil
}

type sendResp struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

// Send transmits a plain-text message. Returns the Gmail message ID for
// acknowledgement evidence. The permission broker must approve before this
// is ever called.
func (s *Service) Send(ctx context.Context, to, subject, body string) (sendResp, provider.Result[sendResp]) {
	if !s.Configured() {
		return sendResp{}, provider.Result[sendResp]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("gmail not connected")}
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return sendResp{}, provider.Result[sendResp]{Failure: provider.FailInvalid, Err: fmt.Errorf("recipient required")}
	}
	access, err := skills.GmailAccessToken(ctx, true)
	if err != nil {
		return sendResp{}, provider.Result[sendResp]{Failure: provider.FailAuth, Err: err}
	}
	raw := "To: " + to + "\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body
	encoded := base64.URLEncoding.EncodeToString([]byte(raw))
	payload := `{"raw":"` + encoded + `"}`
	req, err := http.NewRequestWithContext(ctx, "POST", s.cfg.Base+"/users/me/messages/send", strings.NewReader(payload))
	if err != nil {
		return sendResp{}, provider.Result[sendResp]{Failure: provider.FailInvalid, Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return sendResp{}, provider.Result[sendResp]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return sendResp{}, provider.Result[sendResp]{Failure: provider.FailAuth, Err: fmt.Errorf("gmail unauthorized")}
	}
	if resp.StatusCode != 200 {
		return sendResp{}, provider.Result[sendResp]{Failure: provider.FailServer, Err: fmt.Errorf("gmail status %d", resp.StatusCode)}
	}
	var out sendResp
	if err := json.Unmarshal(rawBody, &out); err != nil {
		return sendResp{}, provider.Result[sendResp]{Failure: provider.FailMalformed, Err: err}
	}
	return out, provider.Result[sendResp]{Value: out, Provider: "gmail"}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
