package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/documents"
)

// DocumentTool makes a document the owner can keep, print, send or sign: a CV,
// a cover letter, an invoice, an itinerary, a one-pager, a letter. Ghost
// writes Markdown; the Pod lays it out as a printed page (PDF) in Ghost's own
// type, keeps the source so it can also be had as Word, and puts it in the
// conversation with its pages to look through. Each call with the same title
// is the next version.
type DocumentTool struct {
	workspace string
	pub       CanvasPublisher
}

func NewDocumentTool(workspace string, pub CanvasPublisher) *DocumentTool {
	return &DocumentTool{workspace: workspace, pub: pub}
}

func (t *DocumentTool) Name() string { return "document" }

func (t *DocumentTool) Description() string {
	return `Make a document the owner can keep, print, send or sign: a CV or resume, a cover letter, a letter, an invoice or quote, an itinerary, a one-pager, a report, a plan, meeting notes, a recipe card. Write it in Markdown; it is laid out as a clean printed PDF (A4) and shown in the chat with its pages, and the owner can share it or have it as Word.

Write the document itself, ready to use, not a description of one: start with "# Title", put a one-line subtitle under it (who, where, a date), use ## for sections, lists, and tables (| a | b |) for anything tabular such as invoice lines. A single line break is kept as a line break (addresses, sign-offs). No HTML. Use the owner's real details from memory where you have them and leave a clear [placeholder] where you do not; never invent facts about them.

To change a document, call document again with the WHOLE updated Markdown and the same title: it becomes the next version. Do not paste the document into your reply; say in a sentence what you made.`
}

func (t *DocumentTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"title", "markdown"},
		"properties": map[string]interface{}{
			"title":    map[string]interface{}{"type": "string", "description": "A short name (\"Amina's CV\", \"Invoice 0042\"). Reuse it for a new version."},
			"markdown": map[string]interface{}{"type": "string", "description": "The whole document in Markdown, under 200 KB."},
			"summary":  map[string]interface{}{"type": "string", "description": "Optional: one line on what this is, or what changed in this version."},
		},
	}
}

func (t *DocumentTool) Timeout() time.Duration { return 90 * time.Second }

func (t *DocumentTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	title := strings.TrimSpace(sarg(args, "title"))
	md, _ := args["markdown"].(string)
	summary := strings.TrimSpace(sarg(args, "summary"))
	r, err := documents.Render(ctx, t.workspace, title, md)
	if err != nil {
		if errors.Is(err, documents.ErrUnavailable) {
			return ErrorResult("Documents can't be made on this Pod yet: " + err.Error() + ". Tell the owner, and offer the text in the chat instead.")
		}
		return ErrorResult(fmt.Sprintf("The document was not made: %v. Fix it and call document again.", err))
	}
	if t.pub == nil {
		return NewToolResult(fmt.Sprintf("The document %q was saved as %s, but the chat can't show it right now.", title, r.PDF))
	}
	pages := "1 page"
	if r.Pages != 1 {
		pages = fmt.Sprintf("%d pages", r.Pages)
	}
	if summary == "" {
		summary = pages
		if r.Version > 1 {
			summary = fmt.Sprintf("Version %d · %s", r.Version, pages)
		}
	}
	a, err := t.pub.Publish(artifacts.Input{
		SessionKey: SessionKeyFromContext(ctx),
		Kind:       artifacts.KindFile,
		Title:      title,
		Summary:    summary,
		Path:       r.PDF,
	})
	if err != nil {
		return ErrorResult("The document was made but could not be shown in the chat: " + err.Error())
	}
	return &ToolResult{
		ForLLM: fmt.Sprintf("The document %q (%s, version %d) is in the owner's chat as artifact %s, where they can look through it, share it as PDF or have it as Word. Do not paste it. Say in one sentence what you made; to change it, call document again with the whole updated Markdown and the same title.", title, pages, r.Version, a.ID),
		Silent: true,
	}
}
