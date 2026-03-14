package commands

import (
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/session"
	"github.com/ianclemence/ghost/pkg/tools"
)

type Runtime struct {
	Tools    *tools.ToolRegistry
	Sessions *session.SessionManager
	Bus      *bus.MessageBus
	Commands *Registry
}
