package browser

import (
	"testing"
)

func TestDetectCheckoutFindsCartTotal(t *testing.T) {
	q := DetectCheckout("https://shop.example.com/checkout",
		"Your cart\nOrder summary\nTotal $42.50\nPlace order")
	if !q.IsCheckout {
		t.Fatal("checkout page must be detected")
	}
	if q.Total != "42.50" {
		t.Fatalf("largest total must win, got %q", q.Total)
	}
	if q.Merchant != "shop.example.com" {
		t.Fatalf("merchant must be page host, got %q", q.Merchant)
	}
}

func TestDetectCheckoutIgnoresArticle(t *testing.T) {
	q := DetectCheckout("https://news.example.com/a",
		"Markets rose today as analysts discussed totals and summaries.")
	if q.IsCheckout {
		t.Fatal("article must not be detected as checkout")
	}
}

func TestDetectCheckoutLargestAmountWins(t *testing.T) {
	q := DetectCheckout("https://shop.example.com/cart",
		"Shopping cart\nItem $12.00\nShipping $3.00\nGrand total $15.00\nCheckout")
	if !q.IsCheckout || q.Total != "15.00" {
		t.Fatalf("grand total must win, got %+v", q)
	}
}

func TestConfirmationKeywords(t *testing.T) {
	if !ConfirmationKeywords("Thank you for your order! Order number 123.") {
		t.Fatal("receipt text must confirm")
	}
	if ConfirmationKeywords("Your cart has 2 items.") {
		t.Fatal("cart text must not confirm")
	}
}
