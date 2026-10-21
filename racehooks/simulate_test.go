package racehooks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/racehooks/racehooks-go/racehooks"
)

// replayServer serves the OAuth endpoint plus one handler, recording the last
// non-OAuth request.
func replayServer(t *testing.T, status int, payload interface{}, got **http.Request, gotBody *map[string]string) *racehooks.Client {
	t.Helper()
	_, c := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth" {
			writeJSON(w, map[string]interface{}{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		*got = r
		if gotBody != nil && r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(gotBody)
		}
		w.WriteHeader(status)
		writeJSON(w, payload)
	}))
	return c
}

func TestGetReplayableSessions(t *testing.T) {
	var req *http.Request
	c := replayServer(t, 200, map[string]interface{}{"data": map[string]interface{}{
		"sessions": []map[string]interface{}{{
			"sessionId": "2026-great-britain_r", "eventId": "e1", "eventName": "British Grand Prix",
			"circuitId": nil, "year": 2026, "type": "r", "startDate": "2026-07-05T14:00:00Z",
			"analyticsPreview": true,
		}},
		"total": 1, "limit": 20, "offset": 0, "restrictedToSeason": "2026",
	}}, &req, nil)

	res, err := c.GetReplayableSessions(context.Background(), &racehooks.ListReplayableSessionsParams{Year: 2026, Type: "race", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "GET" || req.URL.Path != "/v1/simulate/sessions" {
		t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
	}
	q := req.URL.Query()
	if q.Get("year") != "2026" || q.Get("type") != "race" || q.Get("limit") != "20" || q.Has("offset") {
		t.Fatalf("unexpected query %v", q)
	}
	if res.RestrictedToSeason == nil || *res.RestrictedToSeason != "2026" {
		t.Fatalf("restrictedToSeason = %v", res.RestrictedToSeason)
	}
	s := res.Sessions[0]
	if s.SessionID != "2026-great-britain_r" || s.CircuitID != nil || !s.AnalyticsPreview {
		t.Fatalf("unexpected session %+v", s)
	}
}

func TestGetReplayableSessions_NilParamsSendsNoQuery(t *testing.T) {
	var req *http.Request
	c := replayServer(t, 200, map[string]interface{}{"data": map[string]interface{}{
		"sessions": []interface{}{}, "total": 0, "restrictedToSeason": nil,
	}}, &req, nil)
	res, err := c.GetReplayableSessions(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.RawQuery != "" || res.RestrictedToSeason != nil {
		t.Fatalf("query=%q restricted=%v", req.URL.RawQuery, res.RestrictedToSeason)
	}
}

func TestPrepareAndStatus(t *testing.T) {
	art := map[string]interface{}{"data": map[string]interface{}{
		"sessionId": "2026-great-britain_r", "modelVersion": "v1", "profile": "lite",
		"status": "preparing", "formatFallback": true,
	}}
	var req *http.Request
	body := map[string]string{}
	c := replayServer(t, 200, art, &req, &body)

	p, err := c.PrepareReplay(context.Background(), "2026-great-britain_r")
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "POST" || req.URL.Path != "/v1/simulate/prepare" || body["sessionId"] != "2026-great-britain_r" {
		t.Fatalf("unexpected request %s %s body=%v", req.Method, req.URL.Path, body)
	}
	if p.Status != "preparing" || p.Profile != "lite" || !p.FormatFallback {
		t.Fatalf("unexpected artifact %+v", p)
	}

	s, err := c.GetReplayStatus(context.Background(), "2026-great-britain_r")
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "GET" || req.URL.Path != "/v1/simulate/status" || req.URL.Query().Get("session") != "2026-great-britain_r" {
		t.Fatalf("unexpected request %s %s", req.Method, req.URL.String())
	}
	if s.ModelVersion != "v1" {
		t.Fatalf("unexpected artifact %+v", s)
	}
}

func TestGetDemoSessionAndQuota(t *testing.T) {
	var req *http.Request
	c := replayServer(t, 200, map[string]interface{}{"data": map[string]interface{}{"sessionId": "2026-demo_r", "status": "ready"}}, &req, nil)
	d, err := c.GetDemoSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/v1/simulate/demo" || d.SessionID != "2026-demo_r" || d.Status != "ready" {
		t.Fatalf("unexpected %s %+v", req.URL.Path, d)
	}

	var req2 *http.Request
	c2 := replayServer(t, 200, map[string]interface{}{"data": map[string]interface{}{
		"limit": -1, "used": 2, "remaining": -1, "unlocked": []string{"a", "b"}, "currentSeasonOnly": true,
	}}, &req2, nil)
	q, err := c2.GetReplayQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if req2.URL.Path != "/v1/simulate/quota" || q.Limit != -1 || len(q.Unlocked) != 2 || !q.CurrentSeasonOnly {
		t.Fatalf("unexpected %s %+v", req2.URL.Path, q)
	}
}

func TestReplayErrorIsAPIError(t *testing.T) {
	var req *http.Request
	c := replayServer(t, 403, map[string]string{"error": "forbidden"}, &req, nil)
	_, err := c.GetReplayQuota(context.Background())
	apiErr, ok := err.(*racehooks.APIError)
	if !ok || apiErr.StatusCode != 403 {
		t.Fatalf("expected *APIError 403, got %v", err)
	}
}
