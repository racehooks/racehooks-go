package racehooks_test

import (
	"encoding/json"
	"testing"

	"github.com/racehooks/racehooks-go/racehooks"
)

const testSecret = "test_webhook_secret_abc123"

// The body RaceHooks would have sent (uses "feed" key, not "type")
var sampleBody = []byte(`{"feed":"timingdata","sessionId":"session_monaco_r","data":{"1":{"LastLapTime":{"Value":"1:12.345"}}}}`)

func TestComputeSignature(t *testing.T) {
	sig := racehooks.ComputeSignature(testSecret, sampleBody)
	if len(sig) == 0 {
		t.Fatal("expected non-empty signature")
	}
	if sig[:7] != "sha256=" {
		t.Errorf("signature should start with sha256=, got %s", sig[:7])
	}
	// Verify determinism
	sig2 := racehooks.ComputeSignature(testSecret, sampleBody)
	if sig != sig2 {
		t.Error("ComputeSignature is not deterministic")
	}
}

func TestVerifySignature_ValidSignature(t *testing.T) {
	sig := racehooks.ComputeSignature(testSecret, sampleBody)
	if !racehooks.VerifySignature(testSecret, sampleBody, sig) {
		t.Error("expected valid signature to pass")
	}
}

func TestVerifySignature_InvalidSignature(t *testing.T) {
	if racehooks.VerifySignature(testSecret, sampleBody, "sha256=badhex") {
		t.Error("expected invalid signature to fail")
	}
}

func TestVerifySignature_WrongSecret(t *testing.T) {
	sig := racehooks.ComputeSignature("other_secret", sampleBody)
	if racehooks.VerifySignature(testSecret, sampleBody, sig) {
		t.Error("expected wrong-secret signature to fail")
	}
}

func TestVerifySignature_TamperedBody(t *testing.T) {
	sig := racehooks.ComputeSignature(testSecret, sampleBody)
	tampered := append(sampleBody, '!')
	if racehooks.VerifySignature(testSecret, tampered, sig) {
		t.Error("expected tampered body to fail verification")
	}
}

func TestConstructEvent_ValidSignature(t *testing.T) {
	sig := racehooks.ComputeSignature(testSecret, sampleBody)
	event, err := racehooks.ConstructEvent(sampleBody, sig, testSecret)
	if err != nil {
		t.Fatalf("ConstructEvent: %v", err)
	}
	if event.Feed != "timingdata" {
		t.Errorf("expected feed timingdata, got %s", event.Feed)
	}
	if event.SessionID == nil || *event.SessionID != "session_monaco_r" {
		t.Errorf("unexpected sessionId: %v", event.SessionID)
	}
	if len(event.RawData) == 0 {
		t.Error("expected non-empty RawData")
	}
}

func TestConstructEvent_InvalidSignature(t *testing.T) {
	_, err := racehooks.ConstructEvent(sampleBody, "sha256=bad", testSecret)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestParsePayload_NoSignature(t *testing.T) {
	event, err := racehooks.ParsePayload(sampleBody)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if event.Feed != "timingdata" {
		t.Errorf("expected timingdata, got %s", event.Feed)
	}
}

func TestParsePayload_AnalyticsStrategyFeed(t *testing.T) {
	body := []byte(`{"feed":"analytics.strategy","sessionId":"s1","raceId":"2026-bahrain-r1","lap":22,"drivers":[{"tla":"VER","pitStopProbability":0.82}]}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if event.Feed != "analytics.strategy" {
		t.Errorf("expected analytics.strategy, got %s", event.Feed)
	}
	if len(event.RawDrivers) == 0 {
		t.Error("expected non-empty RawDrivers for analytics.strategy")
	}
}

func TestParsePayload_AnalyticsConstructorFeed(t *testing.T) {
	body := []byte(`{"feed":"analytics.constructor","sessionId":"s1","raceId":"2026-bahrain-r1","lap":22,"constructors":[{"constructorId":"red_bull","expectedConstructorPoints":31.2}]}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if len(event.RawConstructors) == 0 {
		t.Error("expected non-empty RawConstructors for analytics.constructor")
	}
}

func TestParsePayload_AnalyticsRaceOddsIncludesPairs(t *testing.T) {
	body := []byte(`{"feed":"analytics.race-odds","sessionId":"s1","raceId":"2026-bahrain-r1","lap":22,"drivers":[],"pairs":[{"constructorId":"red_bull","driverA":"1","driverB":"11","h2hAWinsProbability":0.61}]}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if len(event.RawPairs) == 0 {
		t.Error("expected non-empty RawPairs for analytics.race-odds")
	}
}

func TestParsePayload_AnalyticsQualifyingFeed(t *testing.T) {
	body := []byte(`{"feed":"analytics.qualifying","sessionId":"s1","raceId":null,"lap":3,"sectors":{"poleMarginMs":142},"drivers":[]}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if len(event.RawSectors) == 0 {
		t.Error("expected non-empty RawSectors for analytics.qualifying")
	}
}

func TestParsePayload_WithReplay(t *testing.T) {
	body := []byte(`{"feed":"cardata","sessionId":"s1","data":{},"_replay":{"sourcePath":"2025/race/","originalUtc":"2025-05-25T14:00:00Z","speed":60,"isReplay":true}}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if !event.IsReplay {
		t.Error("expected IsReplay=true")
	}
}

func TestParsePayload_NullSessionId(t *testing.T) {
	body := []byte(`{"feed":"trackstatus","sessionId":null,"data":{"Status":"1"}}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if event.SessionID != nil {
		t.Errorf("expected nil sessionId, got %v", event.SessionID)
	}
}

func TestParsePayload_RawDataAccessible(t *testing.T) {
	body := []byte(`{"feed":"weatherdata","sessionId":"s1","data":{"AirTemp":"23.5","Humidity":"55"}}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}

	var wx map[string]string
	if err := json.Unmarshal(event.RawData, &wx); err != nil {
		t.Fatalf("unmarshal RawData: %v", err)
	}
	if wx["AirTemp"] != "23.5" {
		t.Errorf("expected AirTemp 23.5, got %s", wx["AirTemp"])
	}
}

func TestParsePayload_DriversEnvelope(t *testing.T) {
	// Per-driver feeds (timingdata, cardata) use the "drivers" array, not "data"
	body := []byte(`{"feed":"timingdata","sessionId":"s1","drivers":[{"number":"44","tla":"HAM","pos":1}]}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if event.Feed != "timingdata" {
		t.Errorf("expected timingdata, got %s", event.Feed)
	}
	if len(event.RawDrivers) == 0 {
		t.Error("expected non-empty RawDrivers for per-driver feed")
	}
}

func TestParsePayload_InvalidJSON(t *testing.T) {
	_, err := racehooks.ParsePayload([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParsePayload_AnalyticsEnvelopeFields(t *testing.T) {
	// Live-alert analytics feed: lap/utc on the envelope, payload in RawData.
	body := []byte(`{"feed":"analytics.battle","sessionId":"9724","lap":30,"utc":"2026-06-01T14:30:00Z","data":{"battleStatus":"APPROACHING"}}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if event.Lap == nil || *event.Lap != 30 {
		t.Errorf("expected lap 30, got %v", event.Lap)
	}
	if event.Utc != "2026-06-01T14:30:00Z" {
		t.Errorf("expected utc, got %q", event.Utc)
	}
	if len(event.RawData) == 0 {
		t.Error("expected non-empty RawData for analytics.battle")
	}
}

func TestParsePayload_FlatWeatherFeedViaRaw(t *testing.T) {
	// Flat weather feed: fields at the top level (no data wrapper) — reachable via Raw.
	body := []byte(`{"feed":"weather.forecast_update","sessionId":"9724","event":"weather.forecast_update","lap":10,"utc":"t","source":"open-meteo","model":"ukmo_seamless","currentPrecipRateMmhr":0.0,"precipProbabilityPct":65,"next60min":[]}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if event.Event != "weather.forecast_update" {
		t.Errorf("expected event slug, got %q", event.Event)
	}
	var forecast struct {
		Source               string  `json:"source"`
		Model                string  `json:"model"`
		PrecipProbabilityPct float64 `json:"precipProbabilityPct"`
	}
	if err := json.Unmarshal(event.Raw, &forecast); err != nil {
		t.Fatalf("unmarshal Raw: %v", err)
	}
	if forecast.Model != "ukmo_seamless" || forecast.PrecipProbabilityPct != 65 {
		t.Errorf("flat fields not reachable via Raw: %+v", forecast)
	}
}
