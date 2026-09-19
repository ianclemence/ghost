package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Executor runs one connector capability and returns a bounded, human-readable
// result. It never touches the model: a connector either returns real data or
// an honest error.
type Executor interface {
	Execute(ctx context.Context, cap Capability, args map[string]interface{}) (string, error)
}

// maxResponseBytes bounds a connector response so a hostile or huge API cannot
// exhaust memory. The caller sees a truncated result, never an OOM.
const maxResponseBytes = 1 << 20 // 1 MiB

// OpenAPIExecutor performs OpenAPI operations over HTTP. Path placeholders
// ({id}) are substituted; GET/DELETE/HEAD send remaining args as query
// parameters; other methods send them as a JSON body. Headers are supplied by
// the caller (the vault), never read here.
type OpenAPIExecutor struct {
	BaseURL string
	Client  *http.Client
	Headers func() (map[string]string, error)
}

func (e *OpenAPIExecutor) Execute(ctx context.Context, cap Capability, args map[string]interface{}) (string, error) {
	if e == nil || strings.TrimSpace(e.BaseURL) == "" {
		return "", fmt.Errorf("connector has no base URL")
	}
	if cap.Operation == nil || cap.Operation.Method == "" || cap.Operation.Path == "" {
		return "", fmt.Errorf("capability %q has no operation", cap.ID)
	}
	method := strings.ToUpper(cap.Operation.Method)
	remaining := map[string]interface{}{}
	for k, v := range args {
		remaining[k] = v
	}

	segs := strings.Split(cap.Operation.Path, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			name := strings.Trim(s, "{}")
			if v, ok := remaining[name]; ok {
				segs[i] = url.PathEscape(fmt.Sprint(v))
				delete(remaining, name)
			}
		}
	}
	full := strings.TrimRight(e.BaseURL, "/") + strings.Join(segs, "/")

	var body io.Reader
	switch method {
	case http.MethodGet, http.MethodDelete, http.MethodHead:
		q := url.Values{}
		for k, v := range remaining {
			q.Set(k, fmt.Sprint(v))
		}
		if enc := q.Encode(); enc != "" {
			full += "?" + enc
		}
	default:
		raw, err := json.Marshal(remaining)
		if err != nil {
			return "", err
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.Headers != nil {
		hs, herr := e.Headers()
		if herr != nil {
			return "", herr
		}
		for k, v := range hs {
			req.Header.Set(k, v)
		}
	}

	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	text := strings.TrimSpace(string(data))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("connector returned HTTP %d: %s", resp.StatusCode, truncate(text, 300))
	}
	if text == "" {
		return "(empty response)", nil
	}
	return text, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
