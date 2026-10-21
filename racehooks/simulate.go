package racehooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// ---------------------------------------------------------------------------
// Session replay (simulate)
//
// Prepare a completed session's recorded-playback artifact, then stream it from
// the simulate-stream service as if it were live. Any authenticated tier can
// list and prepare real sessions; tier controls the artifact profile ("lite" vs
// "full") and the replay quota.
// ---------------------------------------------------------------------------

// ReplayableSession is a completed session that can be replayed
// (GET /v1/simulate/sessions).
type ReplayableSession struct {
	// SessionID is the archive id, e.g. "2026-great-britain_r".
	SessionID   string `json:"sessionId"`
	SessionName string `json:"sessionName,omitempty"`
	EventID     string `json:"eventId"`
	EventName   string `json:"eventName"`
	// CircuitID is nil when the circuit is unresolved.
	CircuitID      *string         `json:"circuitId,omitempty"`
	CircuitOutline json.RawMessage `json:"circuitOutline,omitempty"`
	Year           int             `json:"year"`
	// Type is the raw session type code, e.g. "r", "q", "ss", "p1".
	Type      string `json:"type"`
	StartDate string `json:"startDate"`
	// AnalyticsPreview is true when replaying delivers the full Analytics feeds
	// on any plan without using a replay slot.
	AnalyticsPreview bool `json:"analyticsPreview"`
}

// ReplayableSessionList is the response from GET /v1/simulate/sessions.
type ReplayableSessionList struct {
	Sessions []ReplayableSession `json:"sessions"`
	Total    int                 `json:"total"`
	Limit    int                 `json:"limit"`
	Offset   int                 `json:"offset"`
	// RestrictedToSeason is the season the listing is clamped to when the tier
	// limits the archive; nil otherwise.
	RestrictedToSeason *string `json:"restrictedToSeason"`
}

// ListReplayableSessionsParams filters and paginates GetReplayableSessions.
// Zero-valued fields are omitted so the server can apply its own defaults.
type ListReplayableSessionsParams struct {
	Year int
	// Type is one of "race", "qualifying", "sprint" or "practice".
	Type   string
	Limit  int // 1-500, default 200
	Offset int
}

// SimulateArtifact is the recorded-playback artifact state for a session
// (GET /v1/simulate/status, POST /v1/simulate/prepare).
type SimulateArtifact struct {
	SessionID    string `json:"sessionId"`
	SessionName  string `json:"sessionName,omitempty"`
	ModelVersion string `json:"modelVersion"`
	// Profile is "lite" for self-serve tiers, "full" for analytics-grade tiers
	// and the demo session.
	Profile string `json:"profile"`
	// Status is "ready" (stream it now), "preparing" (generation was triggered;
	// poll GetReplayStatus), "missing" (call PrepareReplay) or "unknown" (no such
	// session).
	Status string `json:"status"`
	// FormatFallback is true when served from a compatible prior format while the
	// current one regenerates.
	FormatFallback bool `json:"formatFallback,omitempty"`
}

// DemoSession is the curated demo session (GET /v1/simulate/demo).
type DemoSession struct {
	SessionID   string `json:"sessionId"`
	SessionName string `json:"sessionName,omitempty"`
	// Status is "ready" or "missing".
	Status string `json:"status"`
}

// ReplayQuota is the distinct-session replay-delivery quota
// (GET /v1/simulate/quota). Watching is unlimited; -1 means unlimited.
type ReplayQuota struct {
	Limit     int `json:"limit"`
	Used      int `json:"used"`
	Remaining int `json:"remaining"`
	// Unlocked lists session ids already unlocked; re-running them is free.
	Unlocked []string `json:"unlocked"`
	// CurrentSeasonOnly is true when the tier may only replay the current season.
	CurrentSeasonOnly bool `json:"currentSeasonOnly"`
}

// GetReplayableSessions lists completed sessions available for replay
// (GET /v1/simulate/sessions). Self-serve tiers see the current season plus the
// demo session; see ReplayableSessionList.RestrictedToSeason. Pass nil for defaults.
func (c *Client) GetReplayableSessions(ctx context.Context, params *ListReplayableSessionsParams) (*ReplayableSessionList, error) {
	path := "/simulate/sessions"
	if params != nil {
		q := url.Values{}
		if params.Year > 0 {
			q.Set("year", strconv.Itoa(params.Year))
		}
		if params.Type != "" {
			q.Set("type", params.Type)
		}
		if params.Limit > 0 {
			q.Set("limit", strconv.Itoa(params.Limit))
		}
		if params.Offset > 0 {
			q.Set("offset", strconv.Itoa(params.Offset))
		}
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
	}

	var env struct {
		Data ReplayableSessionList `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// GetDemoSession returns the curated demo session (GET /v1/simulate/demo).
func (c *Client) GetDemoSession(ctx context.Context) (*DemoSession, error) {
	var env struct {
		Data DemoSession `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/simulate/demo", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// GetReplayQuota returns the distinct-session replay quota for the client's
// tier (GET /v1/simulate/quota).
func (c *Client) GetReplayQuota(ctx context.Context) (*ReplayQuota, error) {
	var env struct {
		Data ReplayQuota `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/simulate/quota", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// PrepareReplay ensures a session's playback artifact exists, triggering
// (deduplicated) generation if it is cold (POST /v1/simulate/prepare).
func (c *Client) PrepareReplay(ctx context.Context, sessionID string) (*SimulateArtifact, error) {
	var env struct {
		Data SimulateArtifact `json:"data"`
	}
	body := map[string]string{"sessionId": sessionID}
	if err := c.do(ctx, http.MethodPost, "/simulate/prepare", body, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// GetReplayStatus reports whether a session's playback artifact is ready to
// stream (GET /v1/simulate/status).
func (c *Client) GetReplayStatus(ctx context.Context, sessionID string) (*SimulateArtifact, error) {
	var env struct {
		Data SimulateArtifact `json:"data"`
	}
	path := "/simulate/status?session=" + url.QueryEscape(sessionID)
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}
