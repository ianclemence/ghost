package channels

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/bus"
)

type ActiveSessionProvider interface {
	GetLastActiveSession() (string, string)
}

type DeliveryRouter struct {
	sessionProvider ActiveSessionProvider
}

func NewDeliveryRouter(sp ActiveSessionProvider) *DeliveryRouter {
	return &DeliveryRouter{sessionProvider: sp}
}

func (r *DeliveryRouter) ResolveTarget(msg bus.OutboundMessage) string {
	target := strings.ToLower(strings.TrimSpace(msg.Channel))
	if msg.Metadata == nil {
		return target
	}

	// Smart routing: follow the user to their last active session if requested
	if mode, ok := msg.Metadata["delivery_mode"].(string); ok && mode == "smart" {
		if r.sessionProvider != nil {
			activeChan, _ := r.sessionProvider.GetLastActiveSession()
			if activeChan != "" {
				return activeChan
			}
		}
	}

	if explicit, ok := msg.Metadata["delivery_target"].(string); ok {
		explicit = strings.ToLower(strings.TrimSpace(explicit))
		if explicit != "" {
			return explicit
		}
	}
	if mode, ok := msg.Metadata["delivery_mode"].(string); ok {
		mode = strings.ToLower(strings.TrimSpace(mode))
		switch mode {
		case "origin":
			return target
		case "home":
			if home, ok := msg.Metadata["home_channel"].(string); ok {
				home = strings.ToLower(strings.TrimSpace(home))
				if home != "" {
					return home
				}
			}
		case "explicit":
			if exp, ok := msg.Metadata["explicit_channel"].(string); ok {
				exp = strings.ToLower(strings.TrimSpace(exp))
				if exp != "" {
					return exp
				}
			}
		}
	}
	return target
}
