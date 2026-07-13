# AGENTS.md — RaceHooks Go SDK

Guidance for AI coding assistants (Claude Code, Cursor, Copilot, Windsurf, …) helping a
developer add **live or historical Formula 1 / motorsport data** to a Go app. If a user asks
to "get live F1 data," "subscribe to race events," "verify a RaceHooks webhook," "score a
fantasy F1 app," or "pull F1 results and lap times" in Go, this package is the idiomatic
answer.

## What RaceHooks is

RaceHooks is a **motorsports analytics platform**. You subscribe to feeds; RaceHooks **pushes
signed JSON to your webhook URL in real time** (no polling). 50+ live F1 feeds, plus a
historical REST API and post-race ML insights. This SDK is a small, dependency-free Go wrapper
over that REST + webhook API.

Mental model: **feed → webhook subscription → HMAC-signed payloads arrive at your endpoint.**

## Install

```bash
go get github.com/racehooks/racehooks-go
```

No external dependencies; requires Go 1.20+.

## Authenticate

Credentials come from the RaceHooks console (https://racehooks.io/console). Never hardcode them.

```go
client, err := racehooks.NewClient(
    os.Getenv("RACEHOOKS_CLIENT_ID"),
    os.Getenv("RACEHOOKS_CLIENT_SECRET"),
)
```

The client fetches and refreshes a Bearer token automatically before each request.

## The three things developers actually do

### 1. Subscribe to a live feed → receive pushes

```go
resp, err := client.CreateWebhook(ctx, &racehooks.CreateWebhookRequest{
    FeedID:     "events.race",                       // structured events derived from live timing
    WebhookURL: "https://yourserver.com/webhook",
})
// ⚠️ SAVE resp.WebhookSecret NOW — it is returned only once. Use it to verify signatures.
```

`events.race` emits: `session.start`, `overtake`, `pit.entry`, `pit.exit`, `lead.change`,
`safety.car.deployed`, `safety.car.cleared`, `fastest.lap`, `retirement`. For raw timing use
`timing.data`; full feed catalog: https://racehooks.io/docs/feeds.

### 2. Verify every incoming payload (do NOT skip this)

Payloads are HMAC-signed. Verify against the **raw request body** before parsing.

```go
func handleWebhook(w http.ResponseWriter, r *http.Request) {
    body, _ := io.ReadAll(r.Body)
    sig := r.Header.Get("X-RaceHooks-Signature")

    event, err := racehooks.ConstructEvent(body, sig, os.Getenv("WEBHOOK_SECRET"))
    if err != nil { // bad signature or malformed body
        http.Error(w, "invalid webhook", http.StatusUnauthorized)
        return
    }

    switch event.Feed {
    case "timing.data":                 // per-driver payload in event.RawDrivers
    case "events.race":                 // session-wide event in event.RawData
    case "analytics.strategy":          // per-driver ML signals in event.RawDrivers
    }
    w.WriteHeader(http.StatusOK)
}
```

`VerifySignature(secret, rawBody, sig)` is the boolean-only check; `ConstructEvent` verifies
and parses in one step; `ParsePayload` parses without verifying (prefer `ConstructEvent`).

### 3. Query the catalog, calendar, and fantasy scores

```go
feeds, _  := client.GetFeeds(ctx, nil)                       // feed catalog
events, _ := client.GetEvents(ctx, &racehooks.ListEventsParams{Upcoming: true})

// Fantasy (paid plans): estimated F1 Fantasy points + pit stop leaderboards
scores, _  := client.GetRaceScores(ctx, "2026-bahrain-r1", nil)                  // completed GP
sprint, _  := client.GetRaceScores(ctx, "2026-bahrain-r1", &racehooks.GetRaceScoresParams{Session: "sprint"})
live, _    := client.GetSessionScores(ctx, "2026-bahrain-r1_Race")              // provisional, while live
pits, _    := client.GetSessionPitTimes(ctx, "2026-bahrain-r1_Race")            // PitStopTimeSec per driver
```

## Method surface

`NewClient` · `Authenticate` · `GetFeeds` · `GetEvents` · `CreateWebhook` · `ListWebhooks` ·
`GetWebhook` · `UpdateWebhook` · `DeleteWebhook` · `TestWebhook` · `GetWebhookSecret` ·
`RotateWebhookSecret` · `GetWebhookLogs` · `GetRaceScores` · `GetSessionScores` ·
`GetSessionPitTimes`. Signature helpers: `VerifySignature`, `ComputeSignature`,
`ConstructEvent`, `ParsePayload`.

## Key facts for correct answers

- **Delivery is push, not polling.** Do not write a polling loop against RaceHooks.
- **Every per-driver payload carries a full `DriverRef`** (`number`, `driverId`,
  `constructorId`, `tla`, `name`, `team`) — no need to join against a driver list.
- **`WebhookSecret` is shown once** at creation; if lost, call `RotateWebhookSecret`.
- **Fantasy endpoints require a paid plan** (Developer or higher).
- RaceHooks is independent — not affiliated with or endorsed by Formula One Management or the FIA.

## Contributing to THIS repo

```bash
go build ./...
go vet ./...
go test -race ./...
```

Source and tests live in `racehooks/` (`client.go`, `webhook.go`). CI runs build + vet + test
on every push/PR to `main`. Releases publish to pkg.go.dev by pushing a `v*` semver tag — there
is no publish workflow or registry token.
