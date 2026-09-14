package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/product"
	githubprov "github.com/ianclemence/ghost/pkg/providers/github"
)

// CodeSearchTool searches code across repositories the connected GitHub
// account can see. Read-only: no evidence required. Trust-user model —
// Ghost documents read-only PAT scopes; the token's own scopes govern.
type CodeSearchTool struct {
	cfg *githubprov.Config
}

func NewCodeSearchTool() *CodeSearchTool { return &CodeSearchTool{} }

func (t *CodeSearchTool) Name() string { return "code_search" }

func (t *CodeSearchTool) Description() string {
	return "Search code in the user's connected GitHub repositories. Use for \"find where X is defined\", \"search the codebase for Y\". Optionally constrain to one repo (owner/name)."
}

func (t *CodeSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "Code search query"},
			"repo":  map[string]interface{}{"type": "string", "description": "Constrain to one repo (owner/name), optional"},
		},
		"required": []string{"query"},
	}
}

func (t *CodeSearchTool) Timeout() time.Duration { return providerToolTimeout }

func (t *CodeSearchTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	q := strings.TrimSpace(sarg(args, "query"))
	if q == "" {
		return ErrorResult("code_search needs a query.")
	}
	cfg := githubprov.Config{PAT: credentials.GithubKey()}
	if t.cfg != nil {
		cfg = *t.cfg
	}
	svc := githubprov.New(cfg)
	if !svc.Configured() {
		return ErrorResult(product.FriendlyFor("code", product.ErrConfigRequired))
	}
	cctx, cancel := context.WithTimeout(ctx, providerToolTimeout)
	defer cancel()
	hits, r := svc.SearchCode(cctx, q, strings.TrimSpace(sarg(args, "repo")))
	if r.Err != nil {
		o := product.OutcomeForProviderFailure("code", r.Failure, r.Err)
		return providerError(o.UserMessage)
	}
	if len(hits) == 0 {
		return NewToolResult("No matching code found.")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d match(es):\n", len(hits))
	for _, h := range hits {
		fmt.Fprintf(&sb, "- %s %s\n  %s\n", h.Repo, h.Path, h.URL)
	}
	return NewToolResult(strings.TrimSpace(sb.String()))
}
