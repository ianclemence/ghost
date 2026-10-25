package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/credentials"
)

// VaultItemsTool lets Ghost see what the owner keeps in their Vault, as much as
// it is allowed to and no more. The line it reads for each item is built in the
// credentials package (ModelItems), which is where "Ghost never sees a card's
// number" is decided; this tool only prints it.
type VaultItemsTool struct{}

func NewVaultItemsTool() *VaultItemsTool { return &VaultItemsTool{} }

func (t *VaultItemsTool) Name() string { return "vault_items" }

func (t *VaultItemsTool) Description() string {
	return "See what the owner has saved in their Vault that you can use: logins (the site and the username), contacts (their details in full), cards (a nickname, the brand and the last four digits) and keys (their names). " +
		"Passwords, card numbers, security codes and key values are never shown to you and cannot be. Use it before signing in to a site or filling in a form with the owner's details. " +
		"To sign in to a site that has a login, use browser_login. If it has none, use request_credentials."
}

func (t *VaultItemsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"kind": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"login", "card", "contact", "key"},
				"description": "Only this kind. Leave out for everything.",
			},
		},
	}
}

func (t *VaultItemsTool) Timeout() time.Duration { return 5 * time.Second }

func (t *VaultItemsTool) Execute(_ context.Context, args map[string]interface{}) *ToolResult {
	kind := credentials.ItemKind(strings.TrimSpace(sarg(args, "kind")))
	if kind != "" && !kind.Valid() {
		return ErrorResult("kind is login, card, contact or key")
	}
	items := credentials.ModelItems(kind)
	if len(items) == 0 {
		if kind != "" {
			return NewToolResult(fmt.Sprintf("The Vault has no %ss saved.", kind))
		}
		return NewToolResult("The Vault is empty.")
	}
	var sb strings.Builder
	var cur credentials.ItemKind
	for _, it := range items {
		if it.Kind != cur {
			cur = it.Kind
			fmt.Fprintf(&sb, "%ss:\n", strings.Title(string(cur))) //nolint:staticcheck // ASCII kind names
		}
		fmt.Fprintf(&sb, "- %s: %s\n", it.Title, it.Line)
	}
	sb.WriteString("\nPasswords, card numbers, security codes and key values are not shown to you. Logins are used with browser_login.")
	return NewToolResult(strings.TrimSpace(sb.String()))
}

// RequestCredentialsTool asks the owner for a sign-in Ghost does not have, with
// a private form on their phone. The password goes from the phone to the Vault;
// it is never typed into the conversation and the model never reads it. The tool
// returns at once: the owner fills the form in their own time and tells Ghost to
// carry on.
type RequestCredentialsTool struct {
	channel string
	chatID  string
	publish func(channel, chatID, sessionID string, c cards.Card)
}

func NewRequestCredentialsTool() *RequestCredentialsTool { return &RequestCredentialsTool{} }

// SetContext implements ContextualTool.
func (t *RequestCredentialsTool) SetContext(channel, chatID string) {
	t.channel, t.chatID = channel, chatID
}

// SetPublisher wires the card emitter.
func (t *RequestCredentialsTool) SetPublisher(fn func(channel, chatID, sessionID string, c cards.Card)) {
	t.publish = fn
}

func (t *RequestCredentialsTool) Name() string { return "request_credentials" }

func (t *RequestCredentialsTool) Description() string {
	return "Ask the owner for a sign-in you need and do not have, with a private form in their app. Use it when browser_login says there is no saved login for a site, or you reach a sign-in page you have no login for. " +
		"Never ask for a username or password in the chat, and never ask them to paste one: this form is the only way. Say in one short sentence which site you need to sign in to and what for, then stop and wait; they will tell you when it is done. " +
		"If a login for the site already exists this tells you so; use browser_login."
}

func (t *RequestCredentialsTool) Parameters() map[string]interface{} {
	s := func(d string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": d}
	}
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"site"},
		"properties": map[string]interface{}{
			"site":    s("The site to sign in to: its address or host, such as ads.google.com."),
			"service": s("The name the owner knows it by, such as Google Ads Manager."),
			"reason":  s("One short sentence on what you will do once signed in, such as: Pause the campaigns that are over budget."),
		},
	}
}

func (t *RequestCredentialsTool) Timeout() time.Duration { return 5 * time.Second }

func (t *RequestCredentialsTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	host := cards.CredentialHost(sarg(args, "site"))
	if host == "" {
		return ErrorResult("site must be a website address such as ads.google.com")
	}
	if _, ok := credentials.WebLoginFor(host); ok {
		return NewToolResult(fmt.Sprintf("A sign-in for %s is already saved. Use browser_login with host %s; do not ask the owner for it again.", host, host))
	}
	if t.publish == nil || t.channel != "mobile" || t.chatID == "" {
		return NewToolResult(fmt.Sprintf("This surface cannot show a private form. Tell the owner you need to sign in to %s and that they can add the login in the Ghost app under Settings, Vault, then ask you again. Never ask for the password here.", host))
	}
	// One form per site at a time: asking twice in a turn is noise, and a form
	// still open is the answer.
	for _, c := range cards.DefaultStore.List(t.channel) {
		if c.Kind == cards.KindCredentials && c.Resolved == nil && cards.CredentialHostOf(c) == host {
			return NewToolResult(fmt.Sprintf("A private sign-in form for %s is already open on the owner's screen. Wait for them to fill it in.", host))
		}
	}
	card, err := cards.NewCredentialRequest(host, sarg(args, "service"), sarg(args, "reason"))
	if err != nil {
		return ErrorResult(fmt.Sprintf("The form was not made: %v", err))
	}
	t.publish(t.channel, t.chatID, SessionKeyFromContext(ctx), card)
	return NewToolResult(fmt.Sprintf("A private sign-in form for %s is on the owner's screen. In one short sentence, say you need to sign in to %s and why, and that you'll carry on once they've entered it. Then stop. You will never see what they type.", cards.CredentialServiceOf(card), host))
}
