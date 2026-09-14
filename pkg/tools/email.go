package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/product"
	gmailprov "github.com/ianclemence/ghost/pkg/providers/gmail"
	outlookprov "github.com/ianclemence/ghost/pkg/providers/outlook"
)

// EmailSearchTool lists recent messages matching a query. Read-only:
// no evidence required. Gmail is tried first, Outlook second; bodies are
// never fetched (metadata + snippet only) and sensitive content is
// filtered by the provider.
type EmailSearchTool struct {
	newSvc func() *gmailprov.Service
	newOut func() *outlookprov.Service
}

func NewEmailSearchTool() *EmailSearchTool {
	return &EmailSearchTool{
		newSvc: func() *gmailprov.Service { return gmailprov.New(gmailprov.Config{}) },
		newOut: func() *outlookprov.Service { return outlookprov.New(outlookprov.Config{}) },
	}
}

func (t *EmailSearchTool) Name() string { return "email_search" }

func (t *EmailSearchTool) Description() string {
	return "Search the user's email inbox (Gmail or Outlook). Use for \"check my email\", \"any mail from X\", \"unread emails\". Returns sender, subject, date, and snippet. Never ask for provider details."
}

func (t *EmailSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "Gmail search query, e.g. 'is:unread', 'from:boss', 'newer_than:7d'"},
			"max":   map[string]interface{}{"type": "integer", "description": "Max messages (1-20, default 10)"},
		},
	}
}

func (t *EmailSearchTool) Timeout() time.Duration { return providerToolTimeout }

func (t *EmailSearchTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	q := strings.TrimSpace(sarg(args, "query"))
	max := iarg(args, "max", 10)
	cctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	if svc := t.newSvc(); svc.Configured() {
		msgs, r := svc.Search(cctx, q, max)
		if r.Err != nil {
			o := product.OutcomeForProviderFailure("email", r.Failure, r.Err)
			return providerError(o.UserMessage)
		}
		return renderEmailList(msgs)
	}
	if out := t.newOut(); out.Configured() {
		msgs, r := out.Search(cctx, q, max)
		if r.Err != nil {
			o := product.OutcomeForProviderFailure("email", r.Failure, r.Err)
			return providerError(o.UserMessage)
		}
		return renderEmailList(msgs)
	}
	return ErrorResult("No mailbox connected. Connect Gmail or Outlook in Ghost settings under Connected Apps, then try again.")
}

func renderEmailList(msgs []gmailprov.Message) *ToolResult {
	if len(msgs) == 0 {
		return NewToolResult("No matching emails found.")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d email(s):\n", len(msgs))
	for _, m := range msgs {
		fmt.Fprintf(&sb, "- %s | %s | %s\n  %s\n", m.Date, m.From, m.Subject, m.Snippet)
	}
	return NewToolResult(strings.TrimSpace(sb.String()))
}

// EmailSendTool sends a plain-text message via Gmail (preferred) or
// Outlook. Consequential: the permission broker must approve before Execute
// is ever called, and success carries acknowledgement evidence so the model
// can never claim delivery without runtime proof.
type EmailSendTool struct {
	newSvc func() *gmailprov.Service
	newOut func() *outlookprov.Service
}

func NewEmailSendTool() *EmailSendTool {
	return &EmailSendTool{
		newSvc: func() *gmailprov.Service { return gmailprov.New(gmailprov.Config{}) },
		newOut: func() *outlookprov.Service { return outlookprov.New(outlookprov.Config{}) },
	}
}

func (t *EmailSendTool) Name() string { return "email_send" }

func (t *EmailSendTool) Description() string {
	return "Send a plain-text email via the user's connected mailbox. Use for \"send an email to X\", \"reply\". Requires explicit user approval first."
}

func (t *EmailSendTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"to":      map[string]interface{}{"type": "string", "description": "Recipient email address"},
			"subject": map[string]interface{}{"type": "string", "description": "Subject line"},
			"body":    map[string]interface{}{"type": "string", "description": "Plain-text body"},
		},
		"required": []string{"to", "subject", "body"},
	}
}

func (t *EmailSendTool) Timeout() time.Duration { return providerToolTimeout }

func (t *EmailSendTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	to := strings.TrimSpace(sarg(args, "to"))
	subject := strings.TrimSpace(sarg(args, "subject"))
	body, _ := args["body"].(string)
	if to == "" || subject == "" || strings.TrimSpace(body) == "" {
		return ErrorResult("email_send needs to, subject, and body.")
	}
	cctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	if svc := t.newSvc(); svc.Configured() {
		sent, r := svc.Send(cctx, to, subject, body)
		if r.Err != nil {
			o := product.OutcomeForProviderFailure("email", r.Failure, r.Err)
			return providerError(o.UserMessage)
		}
		return emailSentEvidence(to, subject, sent.ID, "gmail")
	}
	if out := t.newOut(); out.Configured() {
		_, r := out.Send(cctx, to, subject, body)
		if r.Err != nil {
			o := product.OutcomeForProviderFailure("email", r.Failure, r.Err)
			return providerError(o.UserMessage)
		}
		return emailSentEvidence(to, subject, "", "outlook")
	}
	return ErrorResult("No mailbox connected. Connect Gmail or Outlook in Ghost settings under Connected Apps, then try again.")
}

func emailSentEvidence(to, subject, messageID, provider string) *ToolResult {
	res := NewToolResult(fmt.Sprintf("Email sent to %s: %s.", to, subject))
	ev := map[string]interface{}{
		"type":      "acknowledgement",
		"operation": "send",
		"recipient": to,
		"provider":  provider,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	if messageID != "" {
		ev["message_id"] = messageID
	}
	res.Evidence = ev
	return res
}
