// Package interaction defines the unified inbound request envelope.
//
// Every channel (Web, Mobile, Voice, Telegram, Email, CLI) funnels into
// ONE runtime through this envelope. Each request knows: channel,
// channel identity, conversation, context, owner, ghost, agent. The
// runtime never branches into per-channel agent implementations.
package interaction

import (
	"errors"
	"strings"

	"github.com/ianclemence/ghost/pkg/bus"
)

// Request is the canonical inbound envelope.
type Request struct {
	RequestID       string `json:"request_id"`
	Channel         string `json:"channel"`
	ChannelIdentity string `json:"channel_identity"`
	ConversationID  string `json:"conversation_id"`
	ContextID       string `json:"context_id,omitempty"`
	OwnerID         string `json:"owner_id"`
	GhostID         string `json:"ghost_id"`
	AgentID         string `json:"agent_id"`
	Content         string `json:"content"`
	SessionKey      string `json:"session_key"`
}

// FromInbound builds the envelope from a bus message plus resolved
// ownership. Empty channel/content fails closed; context defaults to
// "personal" (the V1 default scope).
func FromInbound(msg bus.InboundMessage, requestID, conversationID, contextID, ownerID, ghostID, agentID string) (Request, error) {
	if strings.TrimSpace(msg.Channel) == "" {
		return Request{}, errors.New("channel required")
	}
	if strings.TrimSpace(msg.Content) == "" && len(msg.Media) == 0 {
		return Request{}, errors.New("empty inbound message")
	}
	if strings.TrimSpace(requestID) == "" || strings.TrimSpace(ghostID) == "" || strings.TrimSpace(ownerID) == "" {
		return Request{}, errors.New("request, ghost, and owner identity required")
	}
	if contextID == "" {
		contextID = "personal"
	}
	if agentID == "" {
		agentID = "agent-main"
	}
	return Request{
		RequestID: requestID, Channel: msg.Channel,
		ChannelIdentity: msg.SenderID, ConversationID: conversationID,
		ContextID: contextID, OwnerID: ownerID, GhostID: ghostID,
		AgentID: agentID, Content: msg.Content, SessionKey: msg.SessionKey,
	}, nil
}

// ChannelOf distinguishes input source for product behavior
// ("this came from Telegram" vs "this came from Web") without
// branching the runtime.
func (r Request) ChannelOf() string { return r.Channel }
