package racehooks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
					{"feedId": "cardata", "name": "Car Data", "path": "CarData.z", "availability": "pro"},
					{"feedId": "timingdata", "name": "Timing Data", "path": "TimingData", "availability": "free"},
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
	if feeds.Feeds[0].FeedID != "cardata" {
		t.Errorf("expected feedId cardata, got %s", feeds.Feeds[0].FeedID)
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
					"feedId":           "timingdata",
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
		FeedID:        "timingdata",
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
						"feedId":           "cardata",
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
					"feedId":           "weatherdata",
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
	if wh.FeedID != "weatherdata" {
		t.Errorf("expected feedId weatherdata, got %s", wh.FeedID)
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
