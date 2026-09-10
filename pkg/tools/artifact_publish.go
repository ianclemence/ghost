package tools

import (
	"context"

	"github.com/ianclemence/ghost/pkg/artifacts"
)

// PublishArtifactTool lets the model propose a handoff ("I put together
// the report"). The proposal carries no authority: artifacts.Publish
// validates existence, bounds, and estate membership, and only a
// validated record becomes an artifact. Anything invalid fails here, so
// the model can never claim a file it did not actually produce.
type PublishArtifactTool struct {
	store     *artifacts.Store
	workspace string
}

// NewPublishArtifactTool binds the tool to one artifact store. A nil
// store refuses every call (fail-closed for unwired loops).
func NewPublishArtifactTool(store *artifacts.Store, workspace string) *PublishArtifactTool {
	return &PublishArtifactTool{store: store, workspace: workspace}
}

func (t *PublishArtifactTool) Name() string {
	return "publish_artifact"
}

func (t *PublishArtifactTool) Description() string {
	return "Hand the user a result: a workspace file, a short text result, or a link. " +
		"Use after the file actually exists — the runtime validates existence and " +
		"rejects anything it cannot prove. Title is required; give exactly one of path, text, or url."
}

func (t *PublishArtifactTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title":   map[string]interface{}{"type": "string"},
			"kind":    map[string]interface{}{"type": "string", "enum": []string{"file", "text", "link"}},
			"path":    map[string]interface{}{"type": "string"},
			"text":    map[string]interface{}{"type": "string"},
			"url":     map[string]interface{}{"type": "string"},
			"summary": map[string]interface{}{"type": "string"},
		},
		"required": []string{"title", "kind"},
	}
}

func (t *PublishArtifactTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.store == nil {
		return ErrorResult("Artifact publishing is unavailable: no artifact store is wired. Nothing was published.")
	}
	str := func(key string) string {
		s, _ := args[key].(string)
		return s
	}
	a, err := t.store.Publish(artifacts.Input{
		SessionKey: SessionKeyFromContext(ctx),
		Kind:       str("kind"),
		Title:      str("title"),
		Summary:    str("summary"),
		Path:       str("path"),
		Text:       str("text"),
		URL:        str("url"),
	})
	if err != nil {
		return ErrorResult("Artifact rejected: " + err.Error() + " Nothing was published.")
	}
	return &ToolResult{
		ForLLM: "Published artifact " + a.ID + " (" + a.Kind + ": " + a.Title + "). It is now available to the user.",
	}
}
