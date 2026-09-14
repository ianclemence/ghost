package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/product"
	notionprov "github.com/ianclemence/ghost/pkg/providers/notion"
)

// DocsSearchTool searches Notion pages shared with the connected
// integration. Read-only: no evidence required. Sharing is the access
// control — only pages the user shared with the integration are visible.
type DocsSearchTool struct {
	cfg *notionprov.Config
}

func NewDocsSearchTool() *DocsSearchTool { return &DocsSearchTool{} }

func (t *DocsSearchTool) Name() string { return "docs_search" }

func (t *DocsSearchTool) Description() string {
	return "Search the user's connected Notion workspace. Use for \"find the doc about X\", \"search my notes for Y\". Only pages shared with the Ghost integration are visible."
}

func (t *DocsSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "Docs search query"},
		},
		"required": []string{"query"},
	}
}

func (t *DocsSearchTool) Timeout() time.Duration { return providerToolTimeout }

func (t *DocsSearchTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	q := strings.TrimSpace(sarg(args, "query"))
	if q == "" {
		return ErrorResult("docs_search needs a query.")
	}
	cfg := notionprov.Config{Token: credentials.NotionKey()}
	if t.cfg != nil {
		cfg = *t.cfg
	}
	svc := notionprov.New(cfg)
	if !svc.Configured() {
		return ErrorResult(product.FriendlyFor("docs", product.ErrConfigRequired))
	}
	cctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	hits, r := svc.Search(cctx, q)
	if r.Err != nil {
		o := product.OutcomeForProviderFailure("docs", r.Failure, r.Err)
		return providerError(o.UserMessage)
	}
	if len(hits) == 0 {
		return NewToolResult("No matching docs found.")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d doc(s):\n", len(hits))
	for _, h := range hits {
		fmt.Fprintf(&sb, "- %s\n  %s\n", h.Title, h.URL)
	}
	return NewToolResult(strings.TrimSpace(sb.String()))
}
