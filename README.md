# racehooks-go

Official Go client for [RaceHooks](https://racehooks.io) — the motorsports analytics platform. Verify webhooks and parse the full catalog of live F1 feeds, including the derived analytics feeds backed by production ML models.

[![Go Reference](https://pkg.go.dev/badge/github.com/racehooks/racehooks-go.svg)](https://pkg.go.dev/github.com/racehooks/racehooks-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> RaceHooks is an independent service and is not affiliated with or endorsed by Formula One Management or the FIA. "Formula 1," "F1," and related marks are trademarks of Formula One Licensing BV.

## Installation

```bash
go get github.com/racehooks/racehooks-go
```

No external dependencies. Requires Go 1.20+.

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/racehooks/racehooks-go/racehooks"
)

func main() {
    client, err := racehooks.NewClient("your_client_id", "your_client_secret")
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // Create a webhook subscription
    resp, err := client.CreateWebhook(ctx, &racehooks.CreateWebhookRequest{
        FeedID:        "timing.data",
        WebhookURL:    "https://yourapp.com/hooks/f1",
        WebhookMethod: "post",
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Webhook created: %s\n", resp.Webhook.WebhookID)
    // Store resp.WebhookSecret — it is returned only at creation time.
    fmt.Printf("Signing secret: %s\n", resp.WebhookSecret)
}
```

## Webhook verification

Every delivery includes an `X-RaceHooks-Signature` header. Verify it before processing:

```go
import (
    "io"
    "net/http"

    "github.com/racehooks/racehooks-go/racehooks"
)

func handleWebhook(w http.ResponseWriter, r *http.Request) {
    body, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "read error", http.StatusBadRequest)
        return
    }

    sig := r.Header.Get("X-RaceHooks-Signature")
    if !racehooks.VerifySignature(secret, body, sig) {
        http.Error(w, "invalid signature", http.StatusUnauthorized)
        return
    }

    // Parse and dispatch
    event, err := racehooks.ConstructEvent(body, sig, secret)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    switch event.Feed {
    case "timing.data":
        // per-driver payload in event.RawDrivers
    case "events.race", "events.qualifying":
        // session-wide event in event.RawData (event.Event, event.Lap)
    case "analytics.strategy", "analytics.race-odds":
        // per-driver ML analytics in event.RawDrivers (event.SessionID, event.RaceID, event.Lap)
    case "analytics.battle", "analytics.pit-window", "analytics.track-conditions":
        // live-alert payload in event.RawData (event.Lap, event.Utc)
    case "weather.forecast-update", "weather.tire-mismatch":
        // flat weather feed — fields at top level; unmarshal event.Raw
    }

    w.WriteHeader(http.StatusOK)
}
```

## Analytics tier

On the Analytics tier, RaceHooks delivers the derived `analytics.*` and `weather.*`
feeds — backed by production ML models and an algorithmic intelligence layer (CTMC
win/podium, ECP/ECPA, EKF tyre health). Analytics envelopes carry `SessionID`, a
separate nullable `RaceID`, `Lap`, and `Utc`. Data-wrapped feeds expose their payload in
`RawData`; the flat weather feeds (`weather.forecast-update`, `weather.tire-mismatch`,
`weather.compound-crossover`) put fields at the top level — unmarshal `event.Raw`
into your own struct.

### Signature API

| Function | Description |
|---|---|
| `VerifySignature(secret, rawBody, signatureHeader)` | Returns `true` if the `X-RaceHooks-Signature` header is valid. Uses constant-time comparison. |
| `ComputeSignature(secret, rawBody)` | Returns the expected `sha256=<hex>` signature value. |
| `ConstructEvent(rawBody, signature, secret)` | Verifies signature and parses the body into a `WebhookPayload`. |
| `ParsePayload(rawBody)` | Parses without verifying — prefer `ConstructEvent` when you have a secret. |

## API reference

| Method | Description |
|---|---|
| `GetFeeds(ctx, params)` | List the F1 timing feed catalog. |
| `GetEvents(ctx, params)` | List the event calendar (filter by `Year`, `Upcoming`; paginate with `Limit`/`Offset`). |
| `CreateWebhook(ctx, req)` | Create a webhook subscription. Returns the signing secret (once only). |
| `ListWebhooks(ctx, params)` | List all webhook subscriptions. |
| `GetWebhook(ctx, webhookID)` | Retrieve a single webhook. |
| `UpdateWebhook(ctx, webhookID, req)` | Patch a webhook (URL, method, filters, active state). |
| `DeleteWebhook(ctx, webhookID)` | Delete a webhook subscription. |
| `TestWebhook(ctx, webhookID)` | Fire a test payload to the webhook URL immediately. |
| `GetWebhookSecret(ctx, webhookID)` | Retrieve the HMAC signing secret. Available on all paid plans. |
| `RotateWebhookSecret(ctx, webhookID)` | Rotate the signing secret and return the new value. |
| `GetWebhookLogs(ctx, webhookID, params)` | Retrieve paginated delivery logs for a webhook. |
| `GetRaceScores(ctx, raceID, params)` | Estimated fantasy points for a completed race. Pass `&GetRaceScoresParams{Session: "sprint"}` to score the Sprint. Requires a paid plan. |
| `GetSessionScores(ctx, sessionID)` | Running (provisional) fantasy score while a session is live. Requires a paid plan. |
| `GetSessionPitTimes(ctx, sessionID)` | Pit stop time leaderboard for a session (`PitStopTimeSec` per driver). Requires a paid plan. |

## Requirements

- Go 1.20+
- No external dependencies

## Releasing

Go modules are published to [pkg.go.dev](https://pkg.go.dev/github.com/racehooks/racehooks-go)
by pushing a semver tag — there is no publish step or registry token:

```bash
git tag v0.1.0
git push origin v0.1.0
```

pkg.go.dev indexes the new version automatically on first request. CI
(`.github/workflows/ci.yml`) runs `go build`, `go vet`, and `go test` on every
push and pull request to `main`.

## License

MIT
