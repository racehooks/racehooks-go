package racehooks_test

import (
	"encoding/json"
	"testing"

	"github.com/racehooks/racehooks-go/racehooks"
)

const testSecret = "test_webhook_secret_abc123"

// The body RaceHooks would have sent
var sampleBody = []byte(`{"type":"timingdata","sessionId":"session_monaco_r","data":{"1":{"LastLapTime":{"Value":"1:12.345"}}},"analytics":null}`)

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
	if event.Type != "timingdata" {
		t.Errorf("expected type timingdata, got %s", event.Type)
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
	if event.Type != "timingdata" {
		t.Errorf("expected timingdata, got %s", event.Type)
	}
}

func TestParsePayload_WithAnalytics(t *testing.T) {
	body := []byte(`{"type":"timingdata","sessionId":"s1","data":{},"analytics":{"overtakeProb":0.82}}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if !event.HasAnalytics {
		t.Error("expected HasAnalytics=true")
	}
	if len(event.RawAnalytics) == 0 {
		t.Error("expected non-empty RawAnalytics")
	}
}

func TestParsePayload_WithReplay(t *testing.T) {
	body := []byte(`{"type":"cardata","sessionId":"s1","data":{},"_replay":{"sourcePath":"2025/race/","originalUtc":"2025-05-25T14:00:00Z","speed":60,"isReplay":true}}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if !event.IsReplay {
		t.Error("expected IsReplay=true")
	}
}

func TestParsePayload_NullSessionId(t *testing.T) {
	body := []byte(`{"type":"trackstatus","sessionId":null,"data":{"Status":"1"}}`)
	event, err := racehooks.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if event.SessionID != nil {
		t.Errorf("expected nil sessionId, got %v", event.SessionID)
	}
}

func TestParsePayload_RawDataAccessible(t *testing.T) {
	body := []byte(`{"type":"weatherdata","sessionId":"s1","data":{"AirTemp":"23.5","Humidity":"55"}}`)
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

func TestParsePayload_InvalidJSON(t *testing.T) {
	_, err := racehooks.ParsePayload([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
