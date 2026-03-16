package channels

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/bus"
)

type DeliveryRouter struct{}

func NewDeliveryRouter() *DeliveryRouter {
	return &DeliveryRouter{}
}

func (r *DeliveryRouter) ResolveTarget(msg bus.OutboundMessage) string {
	target := strings.ToLower(strings.TrimSpace(msg.Channel))
	if msg.Metadata == nil {
		return target
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
