package racehooks

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// VerifySignature verifies the HMAC-SHA256 signature on an incoming webhook.
//
//   - secret is the webhook's signing secret (from CreateWebhook or GetWebhookSecret).
//   - rawBody is the raw request body bytes (do not parse before verifying).
//   - signature is the value of the X-RaceHooks-Signature header.
//
// Returns true if the signature is valid.
//
// Example (net/http handler):
//
//	body, _ := io.ReadAll(r.Body)
//	ok := racehooks.VerifySignature(secret, body, r.Header.Get("X-RaceHooks-Signature"))
//	if !ok {
//	    http.Error(w, "invalid signature", http.StatusUnauthorized)
//	    return
//	}
func VerifySignature(secret string, rawBody []byte, signature string) bool {
	expected := ComputeSignature(secret, rawBody)
	// constant-time compare via hmac.Equal to prevent timing attacks
	return hmac.Equal([]byte(expected), []byte(signature))
}

// ComputeSignature returns the expected X-RaceHooks-Signature value for a given
// secret and raw body. The format is "sha256=<hex>".
func ComputeSignature(secret string, rawBody []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// WebhookPayload is the parsed envelope for every incoming RaceHooks webhook delivery.
// RawData, RawAnalytics, and RawReplay are raw JSON — use json.Unmarshal into
// your own structs for type-safe access to feed-specific fields.
type WebhookPayload struct {
	// Type is the feed identifier, e.g. "timingdata", "cardata", "raceevent".
	Type string
	// SessionID is the active session identifier, or nil between sessions.
	SessionID *string
	// RawData is the raw JSON for the feed payload.
	// Unmarshal into the appropriate struct based on Type.
	RawData json.RawMessage
	// HasAnalytics is true when analytics enrichment data is present.
	HasAnalytics bool
	// RawAnalytics is the raw JSON analytics enrichment (Analytics tier only).
	RawAnalytics json.RawMessage
	// IsReplay is true when this delivery is part of a simulated session replay.
	IsReplay bool
	// RawReplay is the raw JSON replay metadata.
	RawReplay json.RawMessage
}

// ConstructEvent parses the raw webhook body into a WebhookPayload after
// verifying the HMAC-SHA256 signature. Returns an error if the signature is
// invalid or the body cannot be decoded.
//
// Use this as the primary entry point in webhook handlers:
//
//	body, _ := io.ReadAll(r.Body)
//	event, err := racehooks.ConstructEvent(body, r.Header.Get("X-RaceHooks-Signature"), secret)
//	if err != nil {
//	    http.Error(w, err.Error(), http.StatusBadRequest)
//	    return
//	}
//	switch event.Type {
//	case "timingdata":
//	    // handle timing data
//	}
func ConstructEvent(rawBody []byte, signature, secret string) (*WebhookPayload, error) {
	if !VerifySignature(secret, rawBody, signature) {
		return nil, fmt.Errorf("racehooks: webhook signature verification failed")
	}
	return ParsePayload(rawBody)
}

// ParsePayload decodes raw JSON bytes into a WebhookPayload without verifying
// the signature. Prefer ConstructEvent when you have a signing secret.
func ParsePayload(rawBody []byte) (*WebhookPayload, error) {
	var raw struct {
		Type      string          `json:"type"`
		SessionID *string         `json:"sessionId"`
		Data      json.RawMessage `json:"data"`
		Analytics json.RawMessage `json:"analytics,omitempty"`
		Replay    json.RawMessage `json:"_replay,omitempty"`
	}
	if err := json.Unmarshal(rawBody, &raw); err != nil {
		return nil, fmt.Errorf("racehooks: parse webhook payload: %w", err)
	}

	p := &WebhookPayload{
		Type:      raw.Type,
		SessionID: raw.SessionID,
		RawData:   raw.Data,
	}
	if len(raw.Analytics) > 0 && !bytes.Equal(raw.Analytics, []byte("null")) {
		p.HasAnalytics = true
		p.RawAnalytics = raw.Analytics
	}
	if len(raw.Replay) > 0 && !bytes.Equal(raw.Replay, []byte("null")) {
		p.IsReplay = true
		p.RawReplay = raw.Replay
	}

	return p, nil
}
