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
// The Feed value is the canonical, hyphenated feed id (e.g. "timing.data",
// "events.race", "weather.tire-mismatch", "analytics.team-points"). Every delivery
// carries the fixed envelope { feed, sessionId, utc, <body> }; analytics feeds add a
// separate raceId (see RaceID) and lap.
//
// Envelope shapes by feed family:
//   - Most feeds: RawData
//   - Per-driver feeds (timing.data, analytics.strategy, analytics.race-odds,
//     analytics.gap-projection, analytics.tire-strategy): RawDrivers
//   - analytics.team-points: RawConstructors
//   - analytics.qualifying: RawSectors
//   - analytics.race-odds also sets RawPairs (same-constructor H2H pairs)
//   - Live-alert analytics feeds (analytics.sector-pace, .battle, .pit-window,
//     .track-conditions, .pit-quality) and the data-wrapped weather feeds
//     (weather.rain-onset, .rain-cleared, .strategy-divergence): RawData
//   - Flat weather feeds (weather.forecast-update, .tire-mismatch,
//     .compound-crossover) put their fields at the top level — unmarshal
//     Raw (the full body) into your own struct.
type WebhookPayload struct {
	// Feed is the canonical feed identifier, e.g. "timing.data", "events.race",
	// "analytics.strategy".
	Feed string
	// SessionID is the active session identifier, or nil between sessions.
	SessionID *string
	// RaceID is the canonical race identifier on analytics feeds (e.g.
	// "2026-bahrain-r1"); nil when the feed carries no race id (e.g. qualifying,
	// or between sessions). It is distinct from SessionID: analytics payloads
	// carry both a session id and a separate, nullable race id.
	RaceID *string
	// Event is the event slug carried by events.race / events.qualifying and the
	// weather feeds (e.g. "overtake"); empty for feeds without one.
	Event string
	// Lap is the race lap on analytics and weather feeds; nil when absent.
	Lap *int
	// Utc is the ISO-8601 emit timestamp on analytics, events, and weather feeds.
	Utc string
	// RawData holds the payload for session-wide feeds (e.g. "events.race", "weather.data").
	RawData json.RawMessage
	// RawDrivers holds the per-driver array. Present for "timing.data" and
	// all analytics feeds whose top-level payload key is "drivers".
	RawDrivers json.RawMessage
	// RawConstructors holds the constructor array for "analytics.team-points".
	RawConstructors json.RawMessage
	// RawPairs holds the same-constructor H2H pairs array for "analytics.race-odds".
	RawPairs json.RawMessage
	// RawSectors holds the sector-probability object for "analytics.qualifying".
	RawSectors json.RawMessage
	// Raw is the complete, unparsed delivery body. Use it to unmarshal flat feeds
	// (e.g. weather.forecast-update) whose fields live at the top level.
	Raw json.RawMessage
	// IsReplay is true when this delivery is part of a simulated session replay.
	IsReplay bool
	// RawReplay is the raw JSON replay metadata.
	RawReplay json.RawMessage
}

// DriverRef is the stable driver-identity block embedded in every driver entry of
// a per-driver feed's drivers array (RawDrivers). Unmarshal RawDrivers into a slice
// whose elements embed or contain these fields to read driver identity. Feeds never
// deliver a reduced identity — the full DriverRef is always present.
type DriverRef struct {
	DriverID      string `json:"driverId"`
	ConstructorID string `json:"constructorId"`
	Number        string `json:"number"`
	Tla           string `json:"tla"`
	Name          string `json:"name"`
	Team          string `json:"team"`
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
//	case "timing.data":
//	    // per-driver payload in event.RawDrivers
//	case "analytics.strategy":
//	    // per-driver strategy signals in event.RawDrivers
//	case "analytics.team-points":
//	    // constructor rows in event.RawConstructors
//	case "events.race":
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
		RaceID       *string         `json:"raceId"`
		Event        string          `json:"event,omitempty"`
		Lap          *int            `json:"lap,omitempty"`
		Utc          string          `json:"utc,omitempty"`
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
		RaceID:    raw.RaceID,
		Event:     raw.Event,
		Lap:       raw.Lap,
		Utc:       raw.Utc,
		RawData:   raw.Data,
		Raw:       append(json.RawMessage(nil), rawBody...),
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
