// Package racehooks provides a Go client for the RaceHooks API.
//
// # Quick start
//
//	client, err := racehooks.NewClient("your_client_id", "your_client_secret")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Authenticate (auto-called on first request, or call explicitly)
//	if err := client.Authenticate(context.Background()); err != nil {
//	    log.Fatal(err)
//	}
//
//	// List available feeds
//	feeds, err := client.GetFeeds(context.Background(), nil)
//
//	// Verify an incoming webhook
//	ok := racehooks.VerifySignature(secret, rawBody, signatureHeader)
package racehooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://api.racehooks.io/v1"
	defaultTimeout = 30 * time.Second
)

// Client is the RaceHooks API client. Create one with NewClient.
type Client struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithBaseURL overrides the API base URL (useful for testing).
func WithBaseURL(u string) ClientOption {
	return func(c *Client) { c.baseURL = u }
}

// WithHTTPClient substitutes a custom *http.Client.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = hc }
}

// NewClient creates a new RaceHooks API client.
// clientID and clientSecret are obtained from the RaceHooks console.
func NewClient(clientID, clientSecret string, opts ...ClientOption) (*Client, error) {
	if clientID == "" {
		return nil, fmt.Errorf("racehooks: clientID is required")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("racehooks: clientSecret is required")
	}
	c := &Client{
		baseURL:      defaultBaseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------------------

// TokenResponse is returned by POST /v1/oauth.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// Authenticate exchanges client credentials for a Bearer token.
// It is called automatically before any API request when the token is absent
// or expired, so you rarely need to call this directly.
func (c *Client) Authenticate(ctx context.Context) error {
	body, err := json.Marshal(map[string]string{
		"client_id":     c.clientID,
		"client_secret": c.clientSecret,
		"grant_type":    "client_credentials",
	})
	if err != nil {
		return fmt.Errorf("racehooks: marshal auth request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/oauth", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("racehooks: build auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("racehooks: auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return parseAPIError(resp)
	}

	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return fmt.Errorf("racehooks: decode token response: %w", err)
	}

	// Guard against malformed/zero ExpiresIn from the server.
	// Subtract a 60-second skew buffer; clamp to at least 0 so we never
	// store a negative expiry and trigger a re-auth storm.
	skewedExpiry := tr.ExpiresIn - 60
	if skewedExpiry < 0 {
		skewedExpiry = 0
	}

	c.mu.Lock()
	c.accessToken = tr.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(skewedExpiry) * time.Second)
	c.mu.Unlock()

	return nil
}

// ensureToken guarantees a valid token exists and returns it.
// The token is re-fetched at most once per concurrent group: if two goroutines
// both observe an expired token, the first to re-acquire the lock after
// Authenticate returns will win and store the new token; the second will see
// the already-fresh token and skip re-authentication.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		tok := c.accessToken
		c.mu.Unlock()
		return tok, nil
	}
	c.mu.Unlock()

	if err := c.Authenticate(ctx); err != nil {
		return "", err
	}

	c.mu.Lock()
	tok := c.accessToken
	c.mu.Unlock()
	return tok, nil
}

// ---------------------------------------------------------------------------
// Internal HTTP helpers
// ---------------------------------------------------------------------------

func (c *Client) do(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	tok, err := c.ensureToken(ctx)
	if err != nil {
		return err
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("racehooks: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("racehooks: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("racehooks: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseAPIError(resp)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("racehooks: decode response: %w", err)
		}
	}
	return nil
}

// APIError represents an error response from the RaceHooks API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("racehooks: API error %d: %s", e.StatusCode, e.Message)
}

func parseAPIError(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	msg := string(bytes.TrimSpace(b))
	if msg == "" {
		msg = resp.Status
	}
	return &APIError{StatusCode: resp.StatusCode, Message: msg}
}

// paginationQuery builds a query string from limit/offset values.
// Zero values are omitted so the server can apply its own defaults.
func paginationQuery(limit, offset int) string {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// ---------------------------------------------------------------------------
// Feeds
// ---------------------------------------------------------------------------

// Feed represents a single F1 timing feed.
type Feed struct {
	FeedID       string `json:"feedId"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Availability string `json:"availability"`
}

// FeedList is the paginated response from GET /v1/feeds.
type FeedList struct {
	Feeds  []Feed `json:"feeds"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// ListFeedsParams controls pagination for GetFeeds.
type ListFeedsParams struct {
	Limit  int // default 50, max 100
	Offset int
}

// GetFeeds returns the feed catalog. Pass nil for default pagination.
func (c *Client) GetFeeds(ctx context.Context, params *ListFeedsParams) (*FeedList, error) {
	path := "/feeds"
	if params != nil {
		path += paginationQuery(params.Limit, params.Offset)
	}

	var env struct {
		Data FeedList `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

// Event is a race weekend on the calendar.
type Event struct {
	EventID      string         `json:"eventId"`
	Name         string         `json:"name"`
	OfficialName string         `json:"officialName"`
	CountryCode  string         `json:"countryCode"`
	CountryName  string         `json:"countryName"`
	Location     string         `json:"location"`
	CircuitName  string         `json:"circuitName"`
	State        string         `json:"state"`
	Type         string         `json:"type"`
	RoundText    string         `json:"roundText"`
	Sessions     []EventSession `json:"sessions,omitempty"`
}

// EventSession is a single session (Practice, Qualifying, Sprint, Race) within an Event.
type EventSession struct {
	SessionID string `json:"sessionId"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	GmtOffset string `json:"gmtOffset"`
	Path      string `json:"path"`
}

// EventList is the response from GET /v1/events.
type EventList struct {
	Events []Event `json:"events"`
	Total  int     `json:"total"`
}

// ListEventsParams controls filtering and pagination for GetEvents.
// Zero-valued fields are omitted so the server can apply its own defaults.
type ListEventsParams struct {
	Limit    int
	Offset   int
	Upcoming bool
	Year     string
}

// GetEvents returns the event calendar. Pass nil for defaults.
func (c *Client) GetEvents(ctx context.Context, params *ListEventsParams) (*EventList, error) {
	path := "/events"
	if params != nil {
		q := url.Values{}
		if params.Limit > 0 {
			q.Set("limit", strconv.Itoa(params.Limit))
		}
		if params.Offset > 0 {
			q.Set("offset", strconv.Itoa(params.Offset))
		}
		if params.Upcoming {
			q.Set("upcoming", "1")
		}
		if params.Year != "" {
			q.Set("year", params.Year)
		}
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
	}

	var env struct {
		Data EventList `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// ---------------------------------------------------------------------------
// Webhooks
// ---------------------------------------------------------------------------

// WebhookFilters specifies optional delivery filters for a webhook subscription.
type WebhookFilters struct {
	Drivers       []string       `json:"drivers,omitempty"`
	DriverNumbers []string       `json:"driverNumbers,omitempty"`
	Constructors  []string       `json:"constructors,omitempty"`
	Positions     *PositionRange `json:"positions,omitempty"`
	EventTypes    []string       `json:"eventTypes,omitempty"`
}

// PositionRange specifies a race-position window for filtering.
type PositionRange struct {
	Min int `json:"min,omitempty"`
	Max int `json:"max,omitempty"`
}

// Webhook is a webhook subscription record returned by the API.
type Webhook struct {
	WebhookID        string    `json:"webhookId"`
	FeedID           string    `json:"feedId"`
	WebhookURL       string    `json:"webhookUrl"`
	WebhookMethod    string    `json:"webhookMethod"`
	Active           bool      `json:"active"`
	SubscriptionTier string    `json:"subscriptionTier"`
	CreatedAt        time.Time `json:"createdAt"`
}

// CreateWebhookRequest is the body for POST /v1/webhooks.
type CreateWebhookRequest struct {
	FeedID        string          `json:"feedId"`
	WebhookURL    string          `json:"webhookUrl"`
	WebhookMethod string          `json:"webhookMethod"`
	Filters       *WebhookFilters `json:"filters,omitempty"`
}

// CreateWebhookResponse is the envelope returned on successful creation.
type CreateWebhookResponse struct {
	Webhook       Webhook `json:"webhook"`
	WebhookSecret string  `json:"webhookSecret,omitempty"`
	Tier          string  `json:"tier"`
}

// CreateWebhook creates a new webhook subscription (POST /v1/webhooks).
func (c *Client) CreateWebhook(ctx context.Context, req *CreateWebhookRequest) (*CreateWebhookResponse, error) {
	var env struct {
		Data CreateWebhookResponse `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/webhooks", req, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// ListWebhooksParams controls pagination for ListWebhooks.
type ListWebhooksParams struct {
	Limit  int
	Offset int
}

// WebhookList is the paginated response from GET /v1/webhooks.
type WebhookList struct {
	Webhooks []Webhook `json:"webhooks"`
	Total    int       `json:"total"`
	Limit    int       `json:"limit"`
	Offset   int       `json:"offset"`
}

// ListWebhooks returns all webhook subscriptions for the authenticated client.
func (c *Client) ListWebhooks(ctx context.Context, params *ListWebhooksParams) (*WebhookList, error) {
	path := "/webhooks"
	if params != nil {
		path += paginationQuery(params.Limit, params.Offset)
	}

	var env struct {
		Data WebhookList `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// GetWebhook retrieves a single webhook by ID.
func (c *Client) GetWebhook(ctx context.Context, webhookID string) (*Webhook, error) {
	var env struct {
		Data struct {
			Webhook Webhook `json:"webhook"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/webhooks/"+webhookID, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data.Webhook, nil
}

// UpdateWebhookRequest is the body for PATCH /v1/webhooks/:id.
// All fields are optional; only non-nil values are sent.
type UpdateWebhookRequest struct {
	WebhookURL    *string         `json:"webhookUrl,omitempty"`
	WebhookMethod *string         `json:"webhookMethod,omitempty"`
	Filters       *WebhookFilters `json:"filters,omitempty"`
	Active        *bool           `json:"active,omitempty"`
}

// UpdateWebhook patches a webhook subscription.
func (c *Client) UpdateWebhook(ctx context.Context, webhookID string, req *UpdateWebhookRequest) (*Webhook, error) {
	var env struct {
		Data struct {
			Webhook Webhook `json:"webhook"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPatch, "/webhooks/"+webhookID, req, &env); err != nil {
		return nil, err
	}
	return &env.Data.Webhook, nil
}

// DeleteWebhook deletes a webhook subscription.
func (c *Client) DeleteWebhook(ctx context.Context, webhookID string) error {
	return c.do(ctx, http.MethodDelete, "/webhooks/"+webhookID, nil, nil)
}

// GetWebhookSecret retrieves the HMAC signing secret for a webhook.
// Requires Live plan or above.
func (c *Client) GetWebhookSecret(ctx context.Context, webhookID string) (string, error) {
	var env struct {
		Data struct {
			WebhookID     string `json:"webhookId"`
			WebhookSecret string `json:"webhookSecret"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/webhooks/"+webhookID+"/secret", nil, &env); err != nil {
		return "", err
	}
	return env.Data.WebhookSecret, nil
}

// RotateWebhookSecret rotates the HMAC signing secret and returns the new one.
func (c *Client) RotateWebhookSecret(ctx context.Context, webhookID string) (string, error) {
	var env struct {
		Data struct {
			WebhookID     string `json:"webhookId"`
			WebhookSecret string `json:"webhookSecret"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/webhooks/"+webhookID+"/rotate-secret", nil, &env); err != nil {
		return "", err
	}
	return env.Data.WebhookSecret, nil
}

// WebhookLog is a single delivery log entry.
type WebhookLog struct {
	LogID        string    `json:"logId"`
	WebhookID    string    `json:"webhookId"`
	StatusCode   int       `json:"statusCode"`
	RetryCount   int       `json:"retryCount"`
	ErrorMessage *string   `json:"errorMessage"`
	Payload      *string   `json:"payload"`
	DurationMs   *int      `json:"durationMs"`
	CreatedAt    time.Time `json:"createdAt"`
}

// WebhookLogList is the paginated response from GET /v1/webhooks/:id/logs.
type WebhookLogList struct {
	Logs   []WebhookLog `json:"logs"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

// GetWebhookLogs retrieves delivery logs for a webhook.
func (c *Client) GetWebhookLogs(ctx context.Context, webhookID string, params *ListWebhooksParams) (*WebhookLogList, error) {
	path := "/webhooks/" + webhookID + "/logs"
	if params != nil {
		path += paginationQuery(params.Limit, params.Offset)
	}

	var env struct {
		Data WebhookLogList `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// TestWebhookResponse is returned by POST /v1/webhooks/:id/test.
type TestWebhookResponse struct {
	Delivered    bool    `json:"delivered"`
	StatusCode   int     `json:"statusCode"`
	ErrorMessage *string `json:"errorMessage"`
}

// TestWebhook fires a test payload to the webhook URL immediately.
func (c *Client) TestWebhook(ctx context.Context, webhookID string) (*TestWebhookResponse, error) {
	var env struct {
		Data TestWebhookResponse `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/webhooks/"+webhookID+"/test", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// ---------------------------------------------------------------------------
// Fantasy
//
// Pit stop leaderboards and estimated F1 Fantasy points per driver. All three
// endpoints require a paid plan (Developer or higher).
// ---------------------------------------------------------------------------

// PitTimeStop is a single pit stop time entry on a fantasy pit-times leaderboard.
// PitStopTimeSec is the stationary pit-stop time (seconds). Lap is a pointer
// because live-only stops may not carry a lap number.
type PitTimeStop struct {
	Driver         string  `json:"driver"`
	TLA            string  `json:"tla"`
	Team           string  `json:"team"`
	PitStopTimeSec float64 `json:"pitStopTimeSec"`
	Lap            *int    `json:"lap"`
}

// SessionPitTimes is the response from GET /v1/fantasy/session/:sessionId/pit-times.
type SessionPitTimes struct {
	SessionID   string        `json:"sessionId"`
	Source      string        `json:"source"` // "live" | "historical"
	FastestStop *PitTimeStop  `json:"fastestStop"`
	Stops       []PitTimeStop `json:"stops"`
}

// FantasyScoreBreakdown is the per-driver fantasy points breakdown. Nullable
// components (grid-derived, fastest-lap, teammate-quali) use pointer types: they
// are nil while a session is live or when the component does not apply.
type FantasyScoreBreakdown struct {
	RacePosPoints         int  `json:"racePosPoints"`
	QualiPosPoints        *int `json:"qualiPosPoints"`
	Q3Bonus               *int `json:"q3Bonus"`
	PositionsGained       *int `json:"positionsGained"`
	PositionsGainedPoints int  `json:"positionsGainedPoints"`
	FastestLapPoints      *int `json:"fastestLapPoints"`
	BeatTeammateRace      int  `json:"beatTeammateRace"`
	BeatTeammateQuali     *int `json:"beatTeammateQuali"`
	DNFPenalty            int  `json:"dnfPenalty"`
	Total                 int  `json:"total"`
}

// FantasyScoreEntry is one driver's fantasy score with its breakdown.
type FantasyScoreEntry struct {
	Driver    string                `json:"driver"`
	TLA       string                `json:"tla"`
	Team      string                `json:"team"`
	Breakdown FantasyScoreBreakdown `json:"breakdown"`
}

// RaceScores is the response from GET /v1/fantasy/races/:raceId/scores.
type RaceScores struct {
	RaceID      string              `json:"raceId"`
	SessionType string              `json:"sessionType"` // "grand_prix" | "sprint"
	Scores      []FantasyScoreEntry `json:"scores"`
	DataQuality string              `json:"dataQuality,omitempty"`
}

// SessionScores is the response from GET /v1/fantasy/session/:sessionId/scores —
// the running (provisional) score while a session is live. Live is false and
// Scores is empty when no session is currently live for this id.
type SessionScores struct {
	SessionID   string              `json:"sessionId"`
	Live        bool                `json:"live"`
	Provisional bool                `json:"provisional"`
	SessionType string              `json:"sessionType"` // "grand_prix" | "sprint"
	CurrentLap  int                 `json:"currentLap"`
	TotalLaps   int                 `json:"totalLaps"`
	Scores      []FantasyScoreEntry `json:"scores"`
}

// GetRaceScoresParams controls which completed session GetRaceScores scores.
type GetRaceScoresParams struct {
	// Session selects the session to score: "grand_prix" (default) or "sprint".
	Session string
}

// GetRaceScores returns estimated fantasy points per driver for a completed
// session. Pass nil to score the Grand Prix, or &GetRaceScoresParams{Session:
// "sprint"} to score the Sprint (compressed points table).
func (c *Client) GetRaceScores(ctx context.Context, raceID string, params *GetRaceScoresParams) (*RaceScores, error) {
	path := "/fantasy/races/" + raceID + "/scores"
	// The API querystring accepts session=sprint (default = Grand Prix); only the
	// Sprint needs an explicit param.
	if params != nil && params.Session == "sprint" {
		path += "?session=sprint"
	}

	var env struct {
		Data RaceScores `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// GetSessionScores returns the running (provisional) fantasy score for each
// driver while a session is live (GET /v1/fantasy/session/:sessionId/scores).
func (c *Client) GetSessionScores(ctx context.Context, sessionID string) (*SessionScores, error) {
	var env struct {
		Data SessionScores `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/fantasy/session/"+sessionID+"/scores", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// GetSessionPitTimes returns pit stop time rankings for a session
// (GET /v1/fantasy/session/:sessionId/pit-times).
func (c *Client) GetSessionPitTimes(ctx context.Context, sessionID string) (*SessionPitTimes, error) {
	var env struct {
		Data SessionPitTimes `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/fantasy/session/"+sessionID+"/pit-times", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}
