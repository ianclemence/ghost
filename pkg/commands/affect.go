package commands

import "context"

// affectHandler answers /affect. Ghost used to keep running totals of the tone
// of the owner's messages and show them here; it no longer scores anything, so
// the honest answer is that there is nothing to show.
func affectHandler(ctx context.Context, req Request, rt *Runtime) error {
	return req.Reply("Ghost does not score the tone of your messages and keeps no mood or affinity total. " +
		"An older version did; its file was deleted when this version started.")
}
