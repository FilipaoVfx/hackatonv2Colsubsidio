package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// ---------- parseKapsoWebhook / parseKapsoInbound (pure webhook parsing) ----------

const kapsoReceivedBody = `{
  "message": {
    "id": "wamid.123",
    "timestamp": "1730092800",
    "type": "text",
    "from": "573001234567",
    "text": { "body": "Hola, cuánto cuesta?" },
    "kapso": { "direction": "inbound", "status": "received", "content": "Hola, cuánto cuesta?" }
  },
  "conversation": { "id": "conv_123", "phone_number": "573001234567", "status": "active" },
  "is_new_conversation": true,
  "phone_number_id": "123456789012345"
}`

func TestParseKapsoWebhookSingle(t *testing.T) {
	payloads := parseKapsoWebhook([]byte(kapsoReceivedBody))
	if len(payloads) != 1 {
		t.Fatalf("payloads = %d, want 1", len(payloads))
	}
	from, text, id := parseKapsoInbound(payloads[0])
	if from != "+573001234567" {
		t.Errorf("from = %q, want +573001234567 (E.164 normalizado)", from)
	}
	if text != "Hola, cuánto cuesta?" {
		t.Errorf("text = %q", text)
	}
	if id != "wamid.123" {
		t.Errorf("msgID = %q", id)
	}
}

func TestParseKapsoWebhookBatch(t *testing.T) {
	body := `{"type":"whatsapp.message.received","batch":true,"batch_info":{"size":2},
		"data":[` + kapsoReceivedBody + `,` + kapsoReceivedBody + `]}`
	payloads := parseKapsoWebhook([]byte(body))
	if len(payloads) != 2 {
		t.Fatalf("batch payloads = %d, want 2", len(payloads))
	}
	if from, _, _ := parseKapsoInbound(payloads[1]); from != "+573001234567" {
		t.Errorf("batch from = %q", from)
	}
}

func TestParseKapsoInboundNonText(t *testing.T) {
	// image/audio/reaction: from válido pero texto vacío (la ruta lo ignora)
	body := `{"message":{"id":"wamid.9","type":"image","from":"57300","image":{"id":"m1"}},
		"conversation":{"id":"c1","phone_number":"57300"}}`
	payloads := parseKapsoWebhook([]byte(body))
	if len(payloads) != 1 {
		t.Fatalf("payloads = %d", len(payloads))
	}
	from, text, _ := parseKapsoInbound(payloads[0])
	if from != "+57300" {
		t.Errorf("from = %q", from)
	}
	if text != "" {
		t.Errorf("non-text should yield empty text, got %q", text)
	}
}

func TestParseKapsoWebhookInvalid(t *testing.T) {
	for _, body := range []string{"", "not json", "{}", `{"batch":true,"data":[]}`} {
		if got := parseKapsoWebhook([]byte(body)); len(got) != 0 {
			t.Errorf("body %q: payloads = %d, want 0", body, len(got))
		}
	}
}

// ---------- KapsoAdapter ----------

func TestKapsoDigits(t *testing.T) {
	if got := kapsoDigits("+57 300 123 4567"); got != "57 300 123 4567" {
		t.Errorf("kapsoDigits strip + = %q", got)
	}
	if got := kapsoDigits("57300"); got != "57300" {
		t.Errorf("kapsoDigits digits-only = %q", got)
	}
}

func TestKapsoEnabled(t *testing.T) {
	empty := &KapsoAdapter{}
	if empty.Enabled() {
		t.Error("empty adapter should be disabled")
	}
	full := &KapsoAdapter{apiKey: "k", phoneNumberID: "123"}
	if !full.Enabled() {
		t.Error("fully configured adapter should be enabled")
	}
}

func TestNewKapsoAdapterDefaults(t *testing.T) {
	a := NewKapsoAdapter()
	if a.base != "https://api.kapso.ai" {
		t.Errorf("default base = %q", a.base)
	}
	t.Setenv("KAPSO_BASE_URL", "http://mock:9000/")
	if b := NewKapsoAdapter(); b.base != "http://mock:9000" {
		t.Errorf("KAPSO_BASE_URL override (trailing slash trimmed) = %q", b.base)
	}
}

// ---------- verifyKapsoSignature ----------

func TestVerifyKapsoSignature(t *testing.T) {
	body := []byte(kapsoReceivedBody)
	secret := "whsec_test"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := hex.EncodeToString(mac.Sum(nil))

	if !verifyKapsoSignature(body, good, secret) {
		t.Error("valid signature rejected")
	}
	if verifyKapsoSignature(body, good, "otro-secret") {
		t.Error("signature accepted with wrong secret")
	}
	if verifyKapsoSignature(body, "", secret) {
		t.Error("empty signature accepted")
	}
	if verifyKapsoSignature(body, "zzzz", secret) {
		t.Error("non-hex signature accepted")
	}
	if verifyKapsoSignature(append(body, ' '), good, secret) {
		t.Error("tampered body accepted")
	}
}

// ---------- processKapsoWebhook (route logic, transport-agnostic) ----------

func sign(t *testing.T, body []byte, secret string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestProcessKapsoWebhookHappyPath(t *testing.T) {
	var got []string
	status, debug := processKapsoWebhook([]byte(kapsoReceivedBody),
		"whatsapp.message.received", "", "unused-secret-is-ignored-when-empty",
		func(from, text string) { got = append(got, from+"|"+text) })
	// nota: con secret configurado se exige firma; aquí va sin secret:
	if status != 401 { // secret set -> firma requerida
		t.Fatalf("status = %d, want 401 (firma requerida)", status)
	}
	status, debug = processKapsoWebhook([]byte(kapsoReceivedBody),
		"whatsapp.message.received", "", "",
		func(from, text string) { got = append(got, from+"|"+text) })
	if status != 200 {
		t.Fatalf("status = %d, want 200 (%s)", status, debug)
	}
	if len(got) != 1 || got[0] != "+573001234567|Hola, cuánto cuesta?" {
		t.Errorf("processed = %v", got)
	}
}

func TestProcessKapsoWebhookSignatureEnforced(t *testing.T) {
	body := []byte(kapsoReceivedBody)
	secret := "whsec_test"
	calls := 0
	process := func(from, text string) { calls++ }

	// firma válida -> procesa
	status, _ := processKapsoWebhook(body, "whatsapp.message.received", sign(t, body, secret), secret, process)
	if status != 200 || calls != 1 {
		t.Errorf("valid sig: status=%d calls=%d", status, calls)
	}
	// firma inválida -> 401 y NO procesa
	status, _ = processKapsoWebhook(body, "whatsapp.message.received", "deadbeef", secret, process)
	if status != 401 || calls != 1 {
		t.Errorf("bad sig: status=%d calls=%d (want 401, still 1)", status, calls)
	}
}

func TestProcessKapsoWebhookEventFilter(t *testing.T) {
	calls := 0
	status, debug := processKapsoWebhook([]byte(kapsoReceivedBody),
		"whatsapp.message.delivered", "", "", func(from, text string) { calls++ })
	if status != 200 || calls != 0 {
		t.Errorf("status=%d calls=%d, want ack 200 sin procesar (%s)", status, calls, debug)
	}
}

func TestProcessKapsoWebhookSkipsNonTextAndMalformed(t *testing.T) {
	calls := 0
	process := func(from, text string) { calls++ }
	// imagen: ack sin procesar
	img := `{"message":{"id":"w1","type":"image","from":"57300"},"conversation":{"phone_number":"57300"}}`
	if status, _ := processKapsoWebhook([]byte(img), "whatsapp.message.received", "", "", process); status != 200 || calls != 0 {
		t.Errorf("image: status=%d calls=%d", status, calls)
	}
	// basura: ack 200 sin crash
	if status, _ := processKapsoWebhook([]byte("not json"), "whatsapp.message.received", "", "", process); status != 200 || calls != 0 {
		t.Errorf("garbage: status=%d calls=%d", status, calls)
	}
	// batch de 2: procesa ambos
	batch := `{"batch":true,"data":[` + kapsoReceivedBody + `,` + kapsoReceivedBody + `]}`
	if status, debug := processKapsoWebhook([]byte(batch), "whatsapp.message.received", "", "", process); status != 200 || calls != 2 {
		t.Errorf("batch: status=%d calls=%d (%s)", status, calls, debug)
	}
}
