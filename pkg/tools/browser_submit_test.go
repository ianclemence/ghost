package tools

import (
	"context"
	"strings"
	"testing"
)

// submitBare with a stubbed CLI: merchant/amount mismatch must refuse
// without clicking; match must click and carry receipt evidence.
func TestBrowserSubmitQuoteBinding(t *testing.T) {
	page := `{"url":"https://shop.example.com/checkout","title":"Checkout","text":"Your cart\nOrder summary\nTotal $42.50\nPlace order"}`
	receipt := `{"url":"https://shop.example.com/order/1","title":"Done","text":"Thank you for your order!"}`

	newTool := func() (*BrowserTool, *[]string) {
		var ops []string
		tool := NewBrowserTool(t.TempDir(), "submit")
		tool.run = func(ctx context.Context, action string, args ...string) *ToolResult {
			ops = append(ops, action)
			if action == "snapshot" && len(ops) == 1 {
				return NewToolResult(page)
			}
			if action == "click" {
				return NewToolResult("clicked")
			}
			return NewToolResult(receipt)
		}
		return tool, &ops
	}

	// Wrong amount: refuse, never click.
	tool, ops := newTool()
	res := tool.submitBare(context.Background(), map[string]interface{}{
		"ref": "@e9", "merchant": "shop.example.com", "amount": "9.99",
	})
	if !res.IsError || !strings.Contains(res.ForLLM, "does not match") {
		t.Fatalf("amount mismatch must refuse, got %+v", res)
	}
	for _, op := range *ops {
		if op == "click" {
			t.Fatal("refused submit must never click")
		}
	}

	// Wrong merchant: refuse, never click.
	tool2, ops2 := newTool()
	res2 := tool2.submitBare(context.Background(), map[string]interface{}{
		"ref": "@e9", "merchant": "evil.example.com", "amount": "42.50",
	})
	if !res2.IsError || !strings.Contains(res2.ForLLM, "does not match") {
		t.Fatalf("merchant mismatch must refuse, got %+v", res2)
	}
	for _, op := range *ops2 {
		if op == "click" {
			t.Fatal("refused submit must never click")
		}
	}

	// Match: click once, receipt evidence attached.
	tool3, ops3 := newTool()
	res3 := tool3.submitBare(context.Background(), map[string]interface{}{
		"ref": "@e9", "merchant": "shop.example.com", "amount": "42.50",
	})
	if res3.IsError {
		t.Fatalf("matching submit must proceed, got %+v", res3)
	}
	clicks := 0
	for _, op := range *ops3 {
		if op == "click" {
			clicks++
		}
	}
	if clicks != 1 {
		t.Fatalf("matching submit must click exactly once, ops=%v", *ops3)
	}
	if res3.Evidence["op"] != "browser.submit" || res3.Evidence["merchant"] != "shop.example.com" ||
		res3.Evidence["amount"] != "42.50" || res3.Evidence["confirmed"] != true {
		t.Fatalf("receipt evidence must carry quote + confirmation: %+v", res3.Evidence)
	}
}

func TestBrowserNavigateURLPolicy(t *testing.T) {
	tool := NewBrowserTool(t.TempDir(), "navigate")
	res := tool.executeBare(context.Background(), map[string]interface{}{"url": "file:///etc/passwd"})
	if !res.IsError || !strings.Contains(res.ForLLM, "refused") {
		t.Fatalf("file:// navigation must be refused, got %+v", res)
	}
}

func TestMatchAmountNormalization(t *testing.T) {
	if !matchAmount("$42.50", "42.50") {
		t.Fatal("$42.50 must equal 42.50")
	}
	if matchAmount("9.99", "42.50") {
		t.Fatal("different totals must not match")
	}
	if matchAmount("", "42.50") || matchAmount("42.50", "") {
		t.Fatal("empty amounts must never match")
	}
}
