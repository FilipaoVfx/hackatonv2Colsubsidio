package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// KapsoAdapter is the real WhatsApp I/O adapter over Kapso (https://kapso.ai),
// a developer proxy for the Meta WhatsApp Cloud API. Outbound: sends a text
// message through the Meta-compatible messages endpoint (X-API-Key auth).
// Inbound: parses the JSON payload Kapso POSTs to our webhook. Same graceful-
// degradation contract as the other adapters: without credentials it is a
// no-op and the demo runs offline (replies still stream to the UI over /ws).
//
// WhatsApp business rule (24h service window) applies unchanged: free-form
// text only inside the 24h window from the customer's last inbound message;
// outside it, a pre-approved template is required. The Kapso Sandbox (free,
// shared number, activation by a 6-char code) relaxes this for testing, like
// code. Templates are NOT available in the sandbox either.
type KapsoAdapter struct {
	apiKey        string
	phoneNumberID string // Meta phone_number_id of the Kapso (sandbox) number
	base          string // default https://api.kapso.ai (overridable for tests)
	http          *http.Client
}

func NewKapsoAdapter() *KapsoAdapter {
	base := strings.TrimRight(os.Getenv("KAPSO_BASE_URL"), "/")
	if base == "" {
		base = "https://api.kapso.ai"
	}
	return &KapsoAdapter{
		apiKey:        os.Getenv("KAPSO_API_KEY"),
		phoneNumberID: os.Getenv("KAPSO_PHONE_NUMBER_ID"),
		base:          base,
		http:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (k *KapsoAdapter) Enabled() bool { return k.apiKey != "" && k.phoneNumberID != "" }

// kapsoDigits normalizes a phone to the digits-only form the Meta Cloud API
// expects in "to" ("+57 300 123..." -> "57300123..."). Spaces/dashes dropped.
func kapsoDigits(phone string) string {
	return strings.TrimPrefix(canonPhone(phone), "+")
}

// Send delivers a free-form text message to `to` (E.164) and returns the
// WhatsApp message id (wamid). Only valid inside the 24h service window (or
// Sandbox). Mirrors the error/status handling of the previous adapters.
func (k *KapsoAdapter) Send(ctx context.Context, to, body string) (string, error) {
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                kapsoDigits(to),
		"type":              "text",
		"text":              map[string]string{"body": body},
	}
	raw, _ := json.Marshal(payload)
	endpoint := k.base + "/meta/whatsapp/v24.0/" + k.phoneNumberID + "/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-Key", k.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := k.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("kapso: read body: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("kapso %d: %s", resp.StatusCode, string(data[:min(len(data), 300)]))
	}
	// Meta shape: {"messaging_product":"whatsapp","messages":[{"id":"wamid..."}]}
	var out struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &out); err != nil || len(out.Messages) == 0 {
		return "", nil // envío aceptado; id no disponible (no crítico)
	}
	return out.Messages[0].ID, nil
}

// SendButtons delivers an interactive quick-reply button message (Meta Cloud
// API "interactive"/"button"). WhatsApp allows 1-3 buttons per message and
// titles of max 20 chars — callers must enforce both (buttonsForQuestion
// does). The button title comes back as inbound text when tapped (see
// parseKapsoInbound), so no extra plumbing is needed downstream.
func (k *KapsoAdapter) SendButtons(ctx context.Context, to, body string, buttons []string) (string, error) {
	if len(buttons) == 0 || len(buttons) > 3 {
		return k.Send(ctx, to, body) // fuera de rango: degrada a texto plano
	}
	replies := make([]map[string]interface{}, len(buttons))
	for i, title := range buttons {
		replies[i] = map[string]interface{}{
			"type":  "reply",
			"reply": map[string]string{"id": fmt.Sprintf("btn_%d", i), "title": title},
		}
	}
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                kapsoDigits(to),
		"type":              "interactive",
		"interactive": map[string]interface{}{
			"type":   "button",
			"body":   map[string]string{"text": body},
			"action": map[string]interface{}{"buttons": replies},
		},
	}
	raw, _ := json.Marshal(payload)
	endpoint := k.base + "/meta/whatsapp/v24.0/" + k.phoneNumberID + "/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-Key", k.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := k.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("kapso: read body: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("kapso %d: %s", resp.StatusCode, string(data[:min(len(data), 300)]))
	}
	return "", nil
}

// ---- inbound webhook parsing (pure functions — unit-testable, no network) ----

type kapsoText struct {
	Body string `json:"body"`
}

// kapsoButtonReply mirrors interactive.button_reply: user tapped a quick-reply
// button. Title is the visible label (what we treat as the user's answer).
type kapsoButtonReply struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type kapsoInteractive struct {
	Type        string            `json:"type"`
	ButtonReply *kapsoButtonReply `json:"button_reply"`
}

type kapsoMessage struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	From        string            `json:"from"`
	Text        *kapsoText        `json:"text"`
	Interactive *kapsoInteractive `json:"interactive"`
}

type kapsoConversation struct {
	ID          string `json:"id"`
	PhoneNumber string `json:"phone_number"`
}

// kapsoWebhookPayload mirrors the v2 payload of `whatsapp.message.received`.
type kapsoWebhookPayload struct {
	Message       kapsoMessage      `json:"message"`
	Conversation  kapsoConversation `json:"conversation"`
	PhoneNumberID string            `json:"phone_number_id"`
}

// kapsoBatch mirrors the buffered envelope (X-Webhook-Batch: true).
type kapsoBatch struct {
	Batch bool                  `json:"batch"`
	Data  []kapsoWebhookPayload `json:"data"`
}

// parseKapsoWebhook decodes a webhook request body into its payloads: a single
// event, or the batch envelope when buffering is enabled. Unknown/invalid
// bodies yield zero payloads (the route acks and ignores them).
func parseKapsoWebhook(body []byte) []kapsoWebhookPayload {
	var batch kapsoBatch
	if err := json.Unmarshal(body, &batch); err == nil && batch.Batch && len(batch.Data) > 0 {
		return batch.Data
	}
	var single kapsoWebhookPayload
	if err := json.Unmarshal(body, &single); err != nil {
		return nil
	}
	if single.Message.ID == "" && single.Conversation.ID == "" {
		return nil
	}
	return []kapsoWebhookPayload{single}
}

// parseKapsoInbound extracts the sender phone (E.164 with "+"), the text body
// and the message id from one payload. Non-text messages (image, audio,
// reaction, ...) return empty text — the demo flow is text-only.
func parseKapsoInbound(p kapsoWebhookPayload) (from, text, msgID string) {
	from = p.Message.From
	if from == "" {
		from = p.Conversation.PhoneNumber
	}
	if from != "" && !strings.HasPrefix(from, "+") {
		from = "+" + from
	}
	if p.Message.Type == "text" && p.Message.Text != nil {
		text = strings.TrimSpace(p.Message.Text.Body)
	}
	// Quick-reply button tap: the title becomes the inbound text so it flows
	// through the exact same pipeline as a typed answer.
	if p.Message.Type == "interactive" && p.Message.Interactive != nil &&
		p.Message.Interactive.ButtonReply != nil {
		text = strings.TrimSpace(p.Message.Interactive.ButtonReply.Title)
	}
	return from, text, p.Message.ID
}

// verifyKapsoSignature checks the X-Webhook-Signature HMAC-SHA256 (hex) of the
// RAW request body against the webhook secret. Pure — testable.
func verifyKapsoSignature(body []byte, signature, secret string) bool {
	if signature == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	got, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return hmac.Equal(got, expected)
}

// inboundDedupe suppresses repeated deliveries of the SAME WhatsApp message.
// Kapso reintenta cuando no recibe el 200 a tiempo y también entrega lotes con
// reintentos dentro: sin esto el mismo mensaje corre dos turnos completos
// (doble llamada al LLM, variables guardadas dos veces, respuesta duplicada al
// cliente). Ventana acotada por TTL: pasado ese tiempo el mismo id volvería a
// procesarse, que es el comportamiento seguro si Kapso reusa identificadores.
type inboundDedupe struct {
	mu   sync.Mutex
	ttl  time.Duration
	seen map[string]time.Time
	now  func() time.Time // inyectable en tests
}

func newInboundDedupe(ttl time.Duration) *inboundDedupe {
	return &inboundDedupe{ttl: ttl, seen: make(map[string]time.Time), now: time.Now}
}

// first reports whether id is being seen for the first time inside the TTL. A
// nil dedupe or an empty id always processes (no hay nada por lo que dedupar).
func (d *inboundDedupe) first(id string) bool {
	if d == nil || id == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	for k, t := range d.seen { // barrido perezoso: el mapa no crece sin límite
		if now.Sub(t) > d.ttl {
			delete(d.seen, k)
		}
	}
	if t, ok := d.seen[id]; ok && now.Sub(t) <= d.ttl {
		return false
	}
	d.seen[id] = now
	return true
}

// processKapsoWebhook is the transport-agnostic core of POST /api/whatsapp/webhook
// (extracted for testability). It verifies the signature (when a secret is
// configured), filters non-message events, extracts each (from, text) pair and
// hands them to process(). Repeated deliveries of a message already seen by
// `dedupe` are acked and dropped (dedupe nil ⇒ sin deduplicación). It returns
// the HTTP status to ack with and a short debug string. Kapso requires a 200
// within 10s — process() must be cheap or async (in main it runs in a goroutine).
func processKapsoWebhook(body []byte, eventHeader, signature, secret string, dedupe *inboundDedupe, process func(from, text string)) (status int, debug string) {
	if secret != "" && !verifyKapsoSignature(body, signature, secret) {
		return 401, "invalid webhook signature"
	}
	if eventHeader != "" && eventHeader != "whatsapp.message.received" {
		return 200, "ignored event " + eventHeader
	}
	payloads := parseKapsoWebhook(body)
	if len(payloads) == 0 {
		return 200, "no payloads"
	}
	n, dup := 0, 0
	for _, p := range payloads {
		from, text, msgID := parseKapsoInbound(p)
		if from == "" || text == "" {
			continue // no-texto o malformado: ack e ignorar
		}
		if !dedupe.first(msgID) {
			dup++
			continue // reentrega del mismo mensaje: ack sin volver a procesarlo
		}
		process(from, text)
		n++
	}
	if dup > 0 {
		return 200, fmt.Sprintf("processed %d/%d message(s), %d duplicate(s) ignored", n, len(payloads), dup)
	}
	return 200, fmt.Sprintf("processed %d/%d message(s)", n, len(payloads))
}
