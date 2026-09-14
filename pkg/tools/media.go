package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/product"
	spotifyprov "github.com/ianclemence/ghost/pkg/providers/spotify"
)

// MediaPlayTool reports Spotify now-playing status and controls playback
// (play/pause/next/previous) on the active device. Status is read-only;
// control actions are low-risk and broker-governed like other media acts.
type MediaPlayTool struct {
	newSvc func() *spotifyprov.Service
}

func NewMediaPlayTool() *MediaPlayTool {
	return &MediaPlayTool{newSvc: func() *spotifyprov.Service { return spotifyprov.New(spotifyprov.Config{}) }}
}

func (t *MediaPlayTool) Name() string { return "media_play" }

func (t *MediaPlayTool) Description() string {
	return "Check Spotify now-playing status or control playback (play, pause, next, previous) on the user's active device. Use for \"what's playing\", \"pause the music\", \"next song\"."
}

func (t *MediaPlayTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"status", "play", "pause", "next", "previous"}},
		},
	}
}

func (t *MediaPlayTool) Timeout() time.Duration { return providerToolTimeout }

func (t *MediaPlayTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	svc := t.newSvc()
	if !svc.Configured() {
		return ErrorResult("Spotify isn't connected. Connect Spotify in Ghost settings under Connected Apps, then try again.")
	}
	action := strings.ToLower(strings.TrimSpace(sarg(args, "action")))
	if action == "" {
		action = "status"
	}
	cctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	if action == "status" {
		st, r := svc.NowPlaying(cctx)
		if r.Err != nil {
			o := product.OutcomeForProviderFailure("media", r.Failure, r.Err)
			return providerError(o.UserMessage)
		}
		if !st.Playing && st.Track == "" {
			return NewToolResult("Nothing is playing on Spotify right now.")
		}
		verb := "Paused"
		if st.Playing {
			verb = "Now playing"
		}
		detail := st.Track
		if st.Artist != "" {
			detail += " by " + st.Artist
		}
		if st.Device != "" {
			detail += " on " + st.Device
		}
		return NewToolResult(fmt.Sprintf("%s: %s.", verb, detail))
	}
	r := svc.Control(cctx, action, "")
	if r.Err != nil {
		o := product.OutcomeForProviderFailure("media", r.Failure, r.Err)
		return providerError(o.UserMessage)
	}
	return NewToolResult(fmt.Sprintf("Spotify: %s.", action))
}
