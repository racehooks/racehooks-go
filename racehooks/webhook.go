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
// Raw* fields contain JSON that callers unmarshal into their own structs based on Feed.
//
// Envelope shapes by feed family:
//   - Most feeds: RawData
//   - Per-driver feeds (timingdata, analytics.strategy, analytics.race-odds,
//     analytics.gap-projection, analytics.tire-strategy): RawDrivers
//   - analytics.constructor: RawConstructors
//   - analytics.qualifying: RawSectors
//   - analytics.race-odds also sets RawPairs (same-constructor H2H pairs)
type WebhookPayload struct {
	// Feed is the feed identifier, e.g. "timingdata", "raceevent", "analytics.strategy".
	Feed string
	// SessionID is the active session identifier, or nil between sessions.
	SessionID *string
	// RawData holds the payload for session-wide feeds (e.g. "raceevent", "weatherdata").
	RawData json.RawMessage
	// RawDrivers holds the per-driver array. Present for "timingdata", "cardata", and
	// all analytics feeds whose top-level payload key is "drivers".
	RawDrivers json.RawMessage
	// RawConstructors holds the constructor array for "analytics.constructor".
	RawConstructors json.RawMessage
	// RawPairs holds the same-constructor H2H pairs array for "analytics.race-odds".
	RawPairs json.RawMessage
	// RawSectors holds the sector-probability object for "analytics.qualifying".
	RawSectors json.RawMessage
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
//	switch event.Feed {
//	case "timingdata":
//	    // per-driver payload in event.RawDrivers
//	case "analytics.strategy":
//	    // per-driver strategy signals in event.RawDrivers
//	case "analytics.constructor":
//	    // constructor rows in event.RawConstructors
//	case "raceevent":
//	    // session-wide payload in event.RawData
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
		Feed         string          `json:"feed"`
		SessionID    *string         `json:"sessionId"`
		Data         json.RawMessage `json:"data"`
		Drivers      json.RawMessage `json:"drivers,omitempty"`
		Constructors json.RawMessage `json:"constructors,omitempty"`
		Pairs        json.RawMessage `json:"pairs,omitempty"`
		Sectors      json.RawMessage `json:"sectors,omitempty"`
		Replay       json.RawMessage `json:"_replay,omitempty"`
	}
	if err := json.Unmarshal(rawBody, &raw); err != nil {
		return nil, fmt.Errorf("racehooks: parse webhook payload: %w", err)
	}

	notNull := func(b json.RawMessage) bool {
		return len(b) > 0 && !bytes.Equal(b, []byte("null"))
	}

	p := &WebhookPayload{
		Feed:      raw.Feed,
		SessionID: raw.SessionID,
		RawData:   raw.Data,
	}
	if notNull(raw.Drivers) {
		p.RawDrivers = raw.Drivers
	}
	if notNull(raw.Constructors) {
		p.RawConstructors = raw.Constructors
	}
	if notNull(raw.Pairs) {
		p.RawPairs = raw.Pairs
	}
	if notNull(raw.Sectors) {
		p.RawSectors = raw.Sectors
	}
	if notNull(raw.Replay) {
		p.IsReplay = true
		p.RawReplay = raw.Replay
	}

	return p, nil
}
