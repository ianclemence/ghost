package outlook

// Outlook provider (Microsoft Graph): search/list messages and send mail.
// Shares the email tool surface with Gmail: tools try Gmail first, then
// fall back here. Message shape reuses the Gmail redacted projection so
// tools render one format. Auth is OAuth via pkg/skills (sealed refresh
// token); secrets never reach the model.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
	gmailprov "github.com/ianclemence/ghost/pkg/providers/gmail"
	"github.com/ianclemence/ghost/pkg/skills"
)

// Config mirrors the Gmail provider: injectable client + base for tests.
type Config struct {
	HTTPClient      *http.Client
	Base            string
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	if c.Base == "" {
		c.Base = "https://graph.microsoft.com/v1.0"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if c.BreakerCooldown == 0 {
		c.BreakerCooldown = 60 * time.Second
	}
	return c
}

// Service fronts Microsoft Graph mail.
type Service struct {
	cfg    Config
	break_ *provider.Breaker
}

func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, break_: provider.NewBreaker(3, cfg.BreakerCooldown)}
}

// Configured reports whether Outlook OAuth is connected.
func (s *Service) Configured() bool {
	return skills.OutlookWebStatus().Connected
}

type graphListResp struct {
	Value []struct {
		ID               string `json:"id"`
		Subject          string `json:"subject"`
		ReceivedDateTime string `json:"receivedDateTime"`
		BodyPreview      string `json:"bodyPreview"`
		From             struct {
			EmailAddress struct {
				Address string `json:"address"`
				Name    string `json:"name"`
			} `json:"emailAddress"`
		} `json:"from"`
	} `json:"value"`
}

// Search lists recent messages. q uses Graph $search syntax when non-empty,
// otherwise newest-first inbox.
func (s *Service) Search(ctx context.Context, q string, max int) ([]gmailprov.Message, provider.Result[[]gmailprov.Message]) {
	if !s.Configured() {
		return nil, provider.Result[[]gmailprov.Message]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("outlook not connected")}
	}
	if max <= 0 || max > 20 {
		max = 10
	}
	access, err := skills.OutlookAccessToken(ctx, false)
	if err != nil {
		return nil, provider.Result[[]gmailprov.Message]{Failure: provider.FailAuth, Err: err}
	}
	if !s.break_.Allow() {
		return nil, provider.Result[[]gmailprov.Message]{Failure: provider.FailUnavailable, Err: fmt.Errorf("outlook temporarily unavailable")}
	}
	path := "/me/messages?$top=" + itoa(max) + "&$orderby=receivedDateTime desc&$select=id,subject,receivedDateTime,bodyPreview,from"
	if strings.TrimSpace(q) != "" {
		path += "&$search=" + url.QueryEscape(`"`+strings.TrimSpace(q)+`"`)
		req, err := s.authed(ctx, access, "GET", path, nil, "ConsistencyLevel: eventual")
		if err != nil {
			return nil, provider.Result[[]gmailprov.Message]{Failure: provider.FailInvalid, Err: err}
		}
		return s.doList(req)
	}
	req, err := s.authed(ctx, access, "GET", path, nil, "")
	if err != nil {
		return nil, provider.Result[[]gmailprov.Message]{Failure: provider.FailInvalid, Err: err}
	}
	return s.doList(req)
}

func (s *Service) authed(ctx context.Context, access, method, path string, body io.Reader, extraHeader string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.Base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	if extraHeader != "" {
		parts := strings.SplitN(extraHeader, ": ", 2)
		if len(parts) == 2 {
			req.Header.Set(parts[0], parts[1])
		}
	}
	return req, nil
}

func (s *Service) doList(req *http.Request) ([]gmailprov.Message, provider.Result[[]gmailprov.Message]) {
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		s.break_.RecordFailure(provider.FailNetwork)
		return nil, provider.Result[[]gmailprov.Message]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		s.break_.RecordFailure(provider.FailAuth)
		return nil, provider.Result[[]gmailprov.Message]{Failure: provider.FailAuth, Err: fmt.Errorf("outlook unauthorized")}
	}
	if resp.StatusCode != 200 {
		s.break_.RecordFailure(provider.FailServer)
		return nil, provider.Result[[]gmailprov.Message]{Failure: provider.FailServer, Err: fmt.Errorf("outlook status %d", resp.StatusCode)}
	}
	var list graphListResp
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, provider.Result[[]gmailprov.Message]{Failure: provider.FailMalformed, Err: err}
	}
	out := make([]gmailprov.Message, 0, len(list.Value))
	for _, m := range list.Value {
		from := m.From.EmailAddress.Address
		if m.From.EmailAddress.Name != "" {
			from = m.From.EmailAddress.Name + " <" + from + ">"
		}
		out = append(out, gmailprov.Message{
			ID:      m.ID,
			From:    from,
			Subject: m.Subject,
			Date:    m.ReceivedDateTime,
			Snippet: gmailprov.FilterSensitive(m.BodyPreview),
		})
	}
	s.break_.RecordSuccess()
	return out, provider.Result[[]gmailprov.Message]{Value: out, Provider: "outlook"}
}

// Send transmits a plain-text message via Graph sendMail. Returns the
// provider tag for acknowledgement evidence (Graph gives no stable ID
// synchronously, so evidence cites recipient + timestamp).
func (s *Service) Send(ctx context.Context, to, subject, body string) (string, provider.Result[string]) {
	if !s.Configured() {
		return "", provider.Result[string]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("outlook not connected")}
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return "", provider.Result[string]{Failure: provider.FailInvalid, Err: fmt.Errorf("recipient required")}
	}
	access, err := skills.OutlookAccessToken(ctx, true)
	if err != nil {
		return "", provider.Result[string]{Failure: provider.FailAuth, Err: err}
	}
	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"subject": subject,
			"body":    map[string]interface{}{"contentType": "Text", "content": body},
			"toRecipients": []map[string]interface{}{
				{"emailAddress": map[string]interface{}{"address": to}},
			},
		},
		"saveToSentItems": true,
	}
	data, _ := json.Marshal(payload)
	req, err := s.authed(ctx, access, "POST", "/me/sendMail", bytes.NewReader(data), "")
	if err != nil {
		return "", provider.Result[string]{Failure: provider.FailInvalid, Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", provider.Result[string]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 16*1024))
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return "", provider.Result[string]{Failure: provider.FailAuth, Err: fmt.Errorf("outlook unauthorized")}
	}
	if resp.StatusCode != 202 && resp.StatusCode != 200 {
		return "", provider.Result[string]{Failure: provider.FailServer, Err: fmt.Errorf("outlook status %d", resp.StatusCode)}
	}
	return "outlook", provider.Result[string]{Value: "outlook", Provider: "outlook"}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
