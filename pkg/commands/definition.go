package commands

import "context"

type HandlerFunc func(ctx context.Context, req Request, rt *Runtime) error

type Definition struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
	SubCommands []Definition
	Handler     HandlerFunc
}

func (d Definition) EffectiveUsage() string {
	if d.Usage != "" {
		return d.Usage
	}
	if len(d.SubCommands) > 0 {
		return d.Name + " <option>"
	}
	return d.Name
}
