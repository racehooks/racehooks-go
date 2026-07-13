package racehooks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/racehooks/racehooks-go/racehooks"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestServer(t *testing.T, handler http.Handler) (*httptest.Server, *racehooks.Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := racehooks.NewClient("test_id", "test_secret",
		racehooks.WithBaseURL(srv.URL+"/v1"),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return srv, client
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// NewClient
// ---------------------------------------------------------------------------

func TestNewClient_RequiresClientID(t *testing.T) {
	_, err := racehooks.NewClient("", "secret")
	if err == nil {
		t.Fatal("expected error for empty clientID")
	}
}

func TestNewClient_RequiresClientSecret(t *testing.T) {
	_, err := racehooks.NewClient("id", "")
	if err == nil {
		t.Fatal("expected error for empty clientSecret")
	}
}

func TestNewClient_Success(t *testing.T) {
	c, err := racehooks.NewClient("id", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

// ---------------------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------------------

func TestAuthenticate_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		writeJSON(w, map[string]interface{}{
			"access_token": "tok_abc123",
			"token_type":   "Bearer",
			"expires_in":   43200,
		})
	})

	_, client := newTestServer(t, mux)
	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestAuthenticate_InvalidCredentials(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})

	_, client := newTestServer(t, mux)
	err := client.Authenticate(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid credentials")
	}
	apiErr, ok := err.(*racehooks.APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("expected status 401, got %d", apiErr.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Feeds
// ---------------------------------------------------------------------------

func TestGetFeeds_Success(t *testing.T) {
	mux := http.NewServeMux()
	// Serve the auth endpoint so ensureToken passes
	mux.HandleFunc("/v1/oauth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"access_token": "tok_feeds",
			"token_type":   "Bearer",
			"expires_in":   43200,
		})
	})
	mux.HandleFunc("/v1/feeds", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"feeds": []map[string]interface{}{
					{"feedId": "timing.data", "name": "Timing Data", "path": "TimingData", "availability": "free"},
					{"feedId": "weather.data", "name": "Weather Data", "path": "WeatherData", "availability": "free"},
				},
				"total":  2,
				"limit":  50,
				"offset": 0,
			},
		})
	})

	_, client := newTestServer(t, mux)
	feeds, err := client.GetFeeds(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetFeeds: %v", err)
	}
	if len(feeds.Feeds) != 2 {
		t.Errorf("expected 2 feeds, got %d", len(feeds.Feeds))
	}
	if feeds.Feeds[0].FeedID != "timing.data" {
		t.Errorf("expected feedId timing.data, got %s", feeds.Feeds[0].FeedID)
	}
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

func TestGetEvents_Success(t *testing.T) {
	mux := setupAuthMux(t)
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Query().Get("year") != "2026" {
			t.Errorf("expected year=2026, got %s", r.URL.Query().Get("year"))
		}
		if r.URL.Query().Get("upcoming") != "1" {
			t.Errorf("expected upcoming=1, got %s", r.URL.Query().Get("upcoming"))
		}
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"events": []map[string]interface{}{
					{
						"eventId":      "2026-bahrain",
						"name":         "Bahrain Grand Prix",
						"officialName": "FORMULA 1 GULF AIR BAHRAIN GRAND PRIX 2026",
						"countryCode":  "BHR",
						"countryName":  "Bahrain",
						"location":     "Sakhir",
						"circuitName":  "Bahrain International Circuit",
						"state":        "upcoming",
						"type":         "race",
						"roundText":    "Round 1",
						"sessions": []map[string]interface{}{
							{
								"sessionId": "2026-bahrain_Race",
								"type":      "Race",
								"name":      "Race",
								"status":    "scheduled",
								"startDate": "2026-03-16T15:00:00",
								"endDate":   "2026-03-16T17:00:00",
								"gmtOffset": "+03:00",
								"path":      "2026/2026-03-16_Bahrain_Grand_Prix/2026-03-16_Race/",
							},
						},
					},
				},
				"total": 1,
			},
		})
	})

	_, client := newTestServer(t, mux)
	list, err := client.GetEvents(context.Background(), &racehooks.ListEventsParams{Year: "2026", Upcoming: true})
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if list.Total != 1 || len(list.Events) != 1 {
		t.Fatalf("expected 1 event, got total=%d len=%d", list.Total, len(list.Events))
	}
	ev := list.Events[0]
	if ev.EventID != "2026-bahrain" || ev.CircuitName != "Bahrain International Circuit" {
		t.Errorf("event fields not decoded: %+v", ev)
	}
	if len(ev.Sessions) != 1 || ev.Sessions[0].SessionID != "2026-bahrain_Race" ||
		ev.Sessions[0].GmtOffset != "+03:00" {
		t.Errorf("event session not decoded: %+v", ev.Sessions)
	}
}

// ---------------------------------------------------------------------------
// Webhooks — CRUD
// ---------------------------------------------------------------------------

func setupAuthMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"access_token": "tok_wh",
			"token_type":   "Bearer",
			"expires_in":   43200,
		})
	})
	return mux
}

func TestCreateWebhook_Success(t *testing.T) {
	mux := setupAuthMux(t)
	mux.HandleFunc("/v1/webhooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"webhook": map[string]interface{}{
					"webhookId":        "wh_abc123",
					"feedId":           "timing.data",
					"webhookUrl":       "https://example.com/hook",
					"webhookMethod":    "post",
					"active":           true,
					"subscriptionTier": "starter",
					"createdAt":        time.Now().Format(time.RFC3339),
				},
				"webhookSecret": "abc123secret",
				"tier":          "starter",
			},
		})
	})

	_, client := newTestServer(t, mux)
	resp, err := client.CreateWebhook(context.Background(), &racehooks.CreateWebhookRequest{
		FeedID:        "timing.data",
		WebhookURL:    "https://example.com/hook",
		WebhookMethod: "post",
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if resp.Webhook.WebhookID != "wh_abc123" {
		t.Errorf("expected webhookId wh_abc123, got %s", resp.Webhook.WebhookID)
	}
	if resp.WebhookSecret != "abc123secret" {
		t.Errorf("expected webhookSecret, got %q", resp.WebhookSecret)
	}
}

func TestListWebhooks_Success(t *testing.T) {
	mux := setupAuthMux(t)
	mux.HandleFunc("/v1/webhooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"webhooks": []map[string]interface{}{
					{
						"webhookId":        "wh_1",
						"feedId":           "timing.data",
						"webhookUrl":       "https://example.com/a",
						"webhookMethod":    "post",
						"active":           true,
						"subscriptionTier": "pro",
						"createdAt":        time.Now().Format(time.RFC3339),
					},
				},
				"total":  1,
				"limit":  20,
				"offset": 0,
			},
		})
	})

	_, client := newTestServer(t, mux)
	list, err := client.ListWebhooks(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(list.Webhooks) != 1 {
		t.Errorf("expected 1 webhook, got %d", len(list.Webhooks))
	}
}

func TestGetWebhook_Success(t *testing.T) {
	mux := setupAuthMux(t)
	mux.HandleFunc("/v1/webhooks/wh_xyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"webhook": map[string]interface{}{
					"webhookId":        "wh_xyz",
					"feedId":           "weather.data",
					"webhookUrl":       "https://example.com/wx",
					"webhookMethod":    "post",
					"active":           true,
					"subscriptionTier": "free",
					"createdAt":        time.Now().Format(time.RFC3339),
				},
			},
		})
	})

	_, client := newTestServer(t, mux)
	wh, err := client.GetWebhook(context.Background(), "wh_xyz")
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if wh.FeedID != "weather.data" {
		t.Errorf("expected feedId weather.data, got %s", wh.FeedID)
	}
}

func TestDeleteWebhook_Success(t *testing.T) {
	mux := setupAuthMux(t)
	mux.HandleFunc("/v1/webhooks/wh_del", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		writeJSON(w, map[string]interface{}{"data": map[string]interface{}{"success": true}})
	})

	_, client := newTestServer(t, mux)
	if err := client.DeleteWebhook(context.Background(), "wh_del"); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
}

func TestGetWebhookSecret_Success(t *testing.T) {
	mux := setupAuthMux(t)
	mux.HandleFunc("/v1/webhooks/wh_sec/secret", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"webhookId":     "wh_sec",
				"webhookSecret": "supersecret",
			},
		})
	})

	_, client := newTestServer(t, mux)
	secret, err := client.GetWebhookSecret(context.Background(), "wh_sec")
	if err != nil {
		t.Fatalf("GetWebhookSecret: %v", err)
	}
	if secret != "supersecret" {
		t.Errorf("expected supersecret, got %s", secret)
	}
}

func TestRotateWebhookSecret_Success(t *testing.T) {
	mux := setupAuthMux(t)
	mux.HandleFunc("/v1/webhooks/wh_rot/rotate-secret", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"webhookId":     "wh_rot",
				"webhookSecret": "newrotatedsecret",
			},
		})
	})

	_, client := newTestServer(t, mux)
	secret, err := client.RotateWebhookSecret(context.Background(), "wh_rot")
	if err != nil {
		t.Fatalf("RotateWebhookSecret: %v", err)
	}
	if secret != "newrotatedsecret" {
		t.Errorf("expected newrotatedsecret, got %s", secret)
	}
}

func TestTestWebhook_Success(t *testing.T) {
	mux := setupAuthMux(t)
	mux.HandleFunc("/v1/webhooks/wh_test/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"delivered":    true,
				"statusCode":   200,
				"errorMessage": nil,
			},
		})
	})

	_, client := newTestServer(t, mux)
	resp, err := client.TestWebhook(context.Background(), "wh_test")
	if err != nil {
		t.Fatalf("TestWebhook: %v", err)
	}
	if !resp.Delivered {
		t.Error("expected delivered=true")
	}
}

// ---------------------------------------------------------------------------
// Token refresh — regression tests for race condition and ExpiresIn clamp
// ---------------------------------------------------------------------------

// TestAuthenticate_ConcurrentTokenRefresh verifies that multiple goroutines
// calling a method simultaneously do not race on the token fields.
// Run with: go test -race ./racehooks/...
func TestAuthenticate_ConcurrentTokenRefresh(t *testing.T) {
	var authCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth", func(w http.ResponseWriter, r *http.Request) {
		authCalls.Add(1)
		writeJSON(w, map[string]interface{}{
			"access_token": "tok_concurrent",
			"token_type":   "Bearer",
			"expires_in":   43200,
		})
	})
	mux.HandleFunc("/v1/feeds", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"feeds": []interface{}{},
				"total": 0, "limit": 50, "offset": 0,
			},
		})
	})

	_, client := newTestServer(t, mux)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, errs[i] = client.GetFeeds(context.Background(), nil)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}

// TestAuthenticate_ExpiresInBelowSkew verifies that a server returning a very
// short ExpiresIn does not produce a negative token expiry that causes an
// infinite re-auth loop.
func TestAuthenticate_ExpiresInBelowSkew(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"access_token": "tok_short",
			"token_type":   "Bearer",
			"expires_in":   30, // less than the 60-second skew buffer
		})
	})
	mux.HandleFunc("/v1/feeds", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"feeds": []interface{}{},
				"total": 0, "limit": 50, "offset": 0,
			},
		})
	})

	_, client := newTestServer(t, mux)
	// Should not error — clamped expiry means token is considered immediately
	// expired but a fresh one is fetched and used within the same call.
	if _, err := client.GetFeeds(context.Background(), nil); err != nil {
		t.Fatalf("GetFeeds with short ExpiresIn: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetWebhookLogs
// ---------------------------------------------------------------------------

func TestGetWebhookLogs_Success(t *testing.T) {
	mux := setupAuthMux(t)
	mux.HandleFunc("/v1/webhooks/wh_log/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"logs": []map[string]interface{}{
					{
						"logId":        "log_1",
						"webhookId":    "wh_log",
						"statusCode":   200,
						"retryCount":   0,
						"errorMessage": nil,
						"payload":      nil,
						"durationMs":   42,
						"createdAt":    time.Now().Format(time.RFC3339),
					},
				},
				"total": 1, "limit": 50, "offset": 0,
			},
		})
	})

	_, client := newTestServer(t, mux)
	logs, err := client.GetWebhookLogs(context.Background(), "wh_log", nil)
	if err != nil {
		t.Fatalf("GetWebhookLogs: %v", err)
	}
	if len(logs.Logs) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(logs.Logs))
	}
	if logs.Logs[0].LogID != "log_1" {
		t.Errorf("expected logId log_1, got %s", logs.Logs[0].LogID)
	}
}

// TestGetWebhookLogs_Pagination verifies that limit/offset are forwarded as
// query parameters.
func TestGetWebhookLogs_Pagination(t *testing.T) {
	mux := setupAuthMux(t)
	mux.HandleFunc("/v1/webhooks/wh_pg/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("expected limit=10, got %s", r.URL.Query().Get("limit"))
		}
		if r.URL.Query().Get("offset") != "20" {
			t.Errorf("expected offset=20, got %s", r.URL.Query().Get("offset"))
		}
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"logs":  []interface{}{},
				"total": 0, "limit": 10, "offset": 20,
			},
		})
	})

	_, client := newTestServer(t, mux)
	if _, err := client.GetWebhookLogs(context.Background(), "wh_pg",
		&racehooks.ListWebhooksParams{Limit: 10, Offset: 20}); err != nil {
		t.Fatalf("GetWebhookLogs pagination: %v", err)
	}
}
