package channels

import (
	"context"
	"strings"

	"github.com/ianclemence/ghost/pkg/bus"
)

type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, msg bus.OutboundMessage) error
	IsRunning() bool
	IsAllowed(senderID string) bool
}

type BaseChannel struct {
	config    interface{}
	bus       *bus.MessageBus
	running   bool
	name      string
	allowList []string
}

func NewBaseChannel(name string, config interface{}, bus *bus.MessageBus, allowList []string) *BaseChannel {
	return &BaseChannel{
		config:    config,
		bus:       bus,
		name:      name,
		allowList: allowList,
		running:   false,
	}
}

func (c *BaseChannel) Name() string {
	return c.name
}

func (c *BaseChannel) IsRunning() bool {
	return c.running
}

// OnUnknownSender is told when someone who is not on a channel's allowlist
// writes to Ghost, so the owner can hear that a stranger knocked (and, if it
// was a friend, allow them). It is set by the daemon; nil in tests.
var OnUnknownSender func(channel, senderID string)

// IsAllowed reports whether a sender may talk to Ghost through this channel.
// An empty allowlist allows nobody: a messaging channel reaches the owner's own
// conversation, memory and tools, so it must be opened to named people on
// purpose. (It used to allow everyone, which meant anyone who found the bot's
// name could talk to Ghost.)
func (c *BaseChannel) IsAllowed(senderID string) bool {
	if len(c.allowList) == 0 {
		return false
	}

	// Extract parts from compound senderID like "123456|username"
	idPart := senderID
	userPart := ""
	if idx := strings.Index(senderID, "|"); idx > 0 {
		idPart = senderID[:idx]
		userPart = senderID[idx+1:]
	}

	for _, allowed := range c.allowList {
		// Strip leading "@" from allowed value for username matching
		trimmed := strings.TrimPrefix(allowed, "@")
		allowedID := trimmed
		allowedUser := ""
		if idx := strings.Index(trimmed, "|"); idx > 0 {
			allowedID = trimmed[:idx]
			allowedUser = trimmed[idx+1:]
		}

		// Support either side using "id|username" compound form.
		// This keeps backward compatibility with legacy Telegram allowlist entries.
		if senderID == allowed ||
			idPart == allowed ||
			senderID == trimmed ||
			idPart == trimmed ||
			idPart == allowedID ||
			(allowedUser != "" && senderID == allowedUser) ||
			(userPart != "" && (userPart == allowed || userPart == trimmed || userPart == allowedUser)) {
			return true
		}
	}

	return false
}

// SharedConversationKey is the one conversation every surface talks into:
// the terminal, the mobile app, the web console, and the owner's messaging
// channels all read and write the same thread. The *surface* a message came
// from is provenance (recorded on the turn and in message metadata), never
// a different conversation.
const SharedConversationKey = "main"

func (c *BaseChannel) HandleMessage(senderID, chatID, content string, media []string, metadata map[string]string) {
	if !c.IsAllowed(senderID) {
		if hook := OnUnknownSender; hook != nil {
			hook(c.name, senderID)
		}
		return
	}

	// One conversation, many surfaces. An allowlisted sender — Ghost's
	// owner — talks into the shared conversation no matter which channel
	// they used. The channel, chat and sender ride along on the message so
	// the reply still goes back to the right place and the turn keeps its
	// provenance; they are not part of the conversation identity.
	sessionKey := SharedConversationKey

	// Record where this turn came from without changing the conversation.
	if metadata == nil {
		metadata = map[string]string{}
	}
	if metadata["source_channel"] == "" {
		metadata["source_channel"] = c.name
	}

	msg := bus.InboundMessage{
		Channel:    c.name,
		SenderID:   senderID,
		ChatID:     chatID,
		Content:    content,
		Media:      media,
		SessionKey: sessionKey,
		Metadata:   metadata,
	}

	c.bus.PublishInbound(msg)
}

func (c *BaseChannel) setRunning(running bool) {
	c.running = running
}
