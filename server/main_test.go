package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func calculateTotal(price, quantity int) int {
	if price < 0 || quantity < 1 || quantity > 10 {
		return 0
	}
	return price * quantity
}

func verifyWebhookSignature(body []byte, signature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal([]byte(signature), []byte(hex.EncodeToString(mac.Sum(nil))))
}

func TestCalculateTotal(t *testing.T) {
	if got := calculateTotal(2000, 3); got != 6000 {
		t.Fatalf("got %d, want 6000", got)
	}
	if got := calculateTotal(2000, 11); got != 0 {
		t.Fatalf("oversized quantity returned %d", got)
	}
}

func TestWebhookSignature(t *testing.T) {
	body, secret := []byte(`{"reference":"VF3-ABC","status":"paid"}`), "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !verifyWebhookSignature(body, hex.EncodeToString(mac.Sum(nil)), secret) {
		t.Fatal("valid signature was rejected")
	}
	if verifyWebhookSignature(body, "bad", secret) {
		t.Fatal("invalid signature was accepted")
	}
}

func TestTicketCodesAreUnpredictableLength(t *testing.T) {
	code := newTicketCode()
	if len(code) != 14 || code[:4] != "VF3-" {
		t.Fatalf("unexpected ticket code %q", code)
	}
}
