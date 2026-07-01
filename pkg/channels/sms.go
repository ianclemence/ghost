package channels

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/logger"
)

type SMSChannel struct {
	*BaseChannel
	config     config.SMSConfig
	httpClient *http.Client
	server     *http.Server
	mu         sync.Mutex
}

func NewSMSChannel(cfg config.SMSConfig, bus *bus.MessageBus) (*SMSChannel, error) {
	if cfg.AccountSID == "" || cfg.AuthToken == "" {
		return nil, fmt.Errorf("SMS requires account_sid and auth_token")
	}

	if cfg.From == "" {
		return nil, fmt.Errorf("SMS requires a phone number (from)")
	}

	base := NewBaseChannel("sms", cfg, bus, cfg.AllowFrom)

	return &SMSChannel{
		BaseChannel: base,
		config:      cfg,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *SMSChannel) Start(ctx context.Context) error {
	if c.config.WebhookURL != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/sms/webhook", c.handleWebhook)

		c.server = &http.Server{
			Addr:    ":8081",
			Handler: mux,
		}

		go func() {
			logger.InfoC("sms", "Starting webhook server on :8081")
			if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.ErrorCF("sms", "Webhook server error: %v", map[string]interface{}{"error": err.Error()})
			}
		}()
	}

	c.setRunning(true)
	logger.InfoC("sms", "Channel started")
	return nil
}

func (c *SMSChannel) Stop(ctx context.Context) error {
	c.setRunning(false)

	if c.server != nil {
		if err := c.server.Shutdown(ctx); err != nil {
			logger.ErrorCF("sms", "Error stopping webhook server: %v", map[string]interface{}{"error": err.Error()})
		}
	}

	logger.InfoC("sms", "Channel stopped")
	return nil
}

func (c *SMSChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("sms channel is not running")
	}

	to := msg.ChatID
	if to == "" {
		return fmt.Errorf("no recipient specified")
	}

	return c.sendViaAPI(ctx, to, msg.Content)
}

func (c *SMSChannel) sendViaAPI(ctx context.Context, to, body string) error {
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", c.config.AccountSID)

	data := url.Values{}
	data.Set("To", to)
	data.Set("From", c.config.From)
	data.Set("Body", body)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.config.AccountSID, c.config.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send SMS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Twilio API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

func (c *SMSChannel) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	from := r.FormValue("From")
	to := r.FormValue("To")
	body := r.FormValue("Body")
	messageSID := r.FormValue("MessageSid")

	logger.InfoCF("sms", "Received SMS", map[string]interface{}{
		"from":    from,
		"to":      to,
		"sid":     messageSID,
	})

	metadata := map[string]string{
		"message_sid": messageSID,
		"from":        from,
		"to":          to,
	}

	c.HandleMessage(from, from, body, nil, metadata)

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, "OK")
}

func (c *SMSChannel) SendSMS(ctx context.Context, to, body string) error {
	return c.sendViaAPI(ctx, to, body)
}

func (c *SMSChannel) parseMetadata(msg bus.OutboundMessage) map[string]string {
	metadata := make(map[string]string)
	if msg.Metadata != nil {
		for k, v := range msg.Metadata {
			if s, ok := v.(string); ok {
				metadata[k] = s
			}
		}
	}
	return metadata
}

func (c *SMSChannel) formatMessage(content string) string {
	if len(content) > 1600 {
		return content[:1600] + "..."
	}
	return content
}
