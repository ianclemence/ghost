package channels

import (
	"context"
	"fmt"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/logger"
)

type EmailChannel struct {
	*BaseChannel
	config config.EmailConfig
}

func NewEmailChannel(cfg config.EmailConfig, messageBus *bus.MessageBus) (*EmailChannel, error) {
	if cfg.SMTPHost == "" || cfg.SMTPPort <= 0 || cfg.From == "" || cfg.To == "" {
		return nil, fmt.Errorf("invalid email config: smtp_host, smtp_port, from, and to are required")
	}
	return &EmailChannel{
		BaseChannel: NewBaseChannel("email", cfg, messageBus, cfg.AllowFrom),
		config:      cfg,
	}, nil
}

func (c *EmailChannel) Start(ctx context.Context) error {
	c.setRunning(true)
	logger.InfoC("email", "Email channel started")
	return nil
}

func (c *EmailChannel) Stop(ctx context.Context) error {
	c.setRunning(false)
	logger.InfoC("email", "Email channel stopped")
	return nil
}

func (c *EmailChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("email channel not running")
	}
	addr := c.config.SMTPHost + ":" + strconv.Itoa(c.config.SMTPPort)
	subject := "Ghost Response"
	if msg.ChatID != "" {
		subject = "Ghost Response [" + msg.ChatID + "]"
	}
	body := "From channel: " + msg.Channel + "\n\n" + msg.Content
	raw := "From: " + c.config.From + "\r\n" +
		"To: " + c.config.To + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		body + "\r\n"

	var auth smtp.Auth
	if strings.TrimSpace(c.config.Username) != "" {
		auth = smtp.PlainAuth("", c.config.Username, c.config.Password, c.config.SMTPHost)
	}
	recipients := []string{c.config.To}
	if err := smtp.SendMail(addr, auth, c.config.From, recipients, []byte(raw)); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	return nil
}
