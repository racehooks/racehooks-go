# racehooks-go

Official Go client for RaceHooks — F1 live timing webhook delivery

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
        FeedID:        "timingdata",
        WebhookURL:    "https://yourapp.com/hooks/f1",
        WebhookMethod: "POST",
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

    switch event.Type {
    case "timingdata":
        // handle timing data
    case "cardata":
        // handle car telemetry
    }

    w.WriteHeader(http.StatusOK)
}
```

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
| `CreateWebhook(ctx, req)` | Create a webhook subscription. Returns the signing secret (once only). |
| `ListWebhooks(ctx, params)` | List all webhook subscriptions. |
| `GetWebhook(ctx, webhookID)` | Retrieve a single webhook. |
| `UpdateWebhook(ctx, webhookID, req)` | Patch a webhook (URL, method, filters, active state). |
| `DeleteWebhook(ctx, webhookID)` | Delete a webhook subscription. |
| `TestWebhook(ctx, webhookID)` | Fire a test payload to the webhook URL immediately. |
| `GetWebhookSecret(ctx, webhookID)` | Retrieve the HMAC signing secret. Requires Live plan or above. |
| `RotateWebhookSecret(ctx, webhookID)` | Rotate the signing secret and return the new value. |
| `GetWebhookLogs(ctx, webhookID, params)` | Retrieve paginated delivery logs for a webhook. |

## Requirements

- Go 1.20+
- No external dependencies

## License

MIT
