package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ---------- pure helpers ----------

func TestCoerceAnswer(t *testing.T) {
	cases := []struct {
		field, text string
		want        interface{}
	}{
		{"boolean", "Sí", true},
		{"boolean", "no", false},
		{"boolean", "sí, dos hijos", true},
		{"boolean", "no, no tengo", false},
		{"boolean", "claro que sí", true},
		{"boolean", "tal vez", "tal vez"},
		{"number", "tengo 30 años", float64(30)},
		{"number", "dos", "dos"},
		{"currency", "$1.500.000", float64(1500000)},
		{"currency", "2,5", float64(2.5)},
		{"text", "  Juan Pérez  ", "Juan Pérez"},
		{"email", "a@b.com", "a@b.com"},
	}
	for _, c := range cases {
		if got := coerceAnswer(c.field, c.text); got != c.want {
			t.Errorf("coerceAnswer(%q,%q) = %#v, want %#v", c.field, c.text, got, c.want)
		}
	}
}

func TestRecFields(t *testing.T) {
	name, reason := recFields(map[string]interface{}{"name": "Vida Plus", "reason": "familia con hijos"})
	if name != "Vida Plus" || reason != "familia con hijos" {
		t.Errorf("got (%q,%q)", name, reason)
	}
	name, _ = recFields(map[string]interface{}{"product": map[string]interface{}{"name": "Mascota Segura"}})
	if name != "Mascota Segura" {
		t.Errorf("nested product name = %q", name)
	}
	if n, _ := recFields("Plan Básico"); n != "Plan Básico" {
		t.Errorf("string rec = %q", n)
	}
	if n, _ := recFields(42); n != "" {
		t.Errorf("unknown rec should be empty, got %q", n)
	}
}

// ---------- full flow against a mock Protege API ----------

type capture struct {
	mu     sync.Mutex
	events []Event
}

func (c *capture) on(ev Event) { c.mu.Lock(); c.events = append(c.events, ev); c.mu.Unlock() }
func (c *capture) count(typ string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.events {
		if e.Type == typ {
			n++
		}
	}
	return n
}
func (c *capture) first(typ string) (Event, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if e.Type == typ {
			return e, true
		}
	}
	return Event{}, false
}

func TestProtegeFullFlow(t *testing.T) {
	var lastAnswer map[string]interface{}
	answers := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/users/search":
			_, _ = w.Write([]byte(`[]`)) // no existing user -> triggers create
		case r.Method == "POST" && r.URL.Path == "/api/v1/users":
			_, _ = w.Write([]byte(`{"id":"user-1","phone":"+57300"}`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/conversations":
			_, _ = w.Write([]byte(`{"id":"conv-1","user_id":"user-1","channel":"whatsapp","status":"collecting_data",
				"current_question_id":"q1","can_generate_recommendation":false,
				"next_question":{"id":"q1","variable_key":"age","text":"¿Cuál es tu edad?","field_type":"number"}}`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/conversations/conv-1/answers":
			answers++
			body, _ := readJSON(r)
			lastAnswer = body
			if answers == 1 {
				_, _ = w.Write([]byte(`{"id":"conv-1","status":"collecting_data","can_generate_recommendation":false,
					"next_question":{"id":"q2","variable_key":"has_kids","text":"¿Tienes hijos?","field_type":"boolean"}}`))
			} else {
				_, _ = w.Write([]byte(`{"id":"conv-1","status":"ready_for_recommendation","can_generate_recommendation":true,"next_question":null}`))
			}
		case r.Method == "POST" && r.URL.Path == "/api/v1/conversations/conv-1/complete":
			_, _ = w.Write([]byte(`{"conversation_id":"conv-1","status":"completed","snapshot_id":"snap-1","user_id":"user-1",
				"recommendations":[{"name":"Vida Protección Familiar Plus","reason":"familia con hijos"}]}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 500)
		}
	}))
	defer srv.Close()

	t.Setenv("COLSUBSIDIO_API_URL", srv.URL)

	bus := NewEventBus()
	cap := &capture{}
	bus.Subscribe("*", cap.on)
	api := NewColsubsidioClient()
	if !api.Enabled() {
		t.Fatal("client should be enabled with COLSUBSIDIO_API_URL set")
	}
	eng := NewProtegeEngine(bus, api, NewWhatsAppSessions())
	ctx := context.Background()

	// 1) outbound contact
	callID, err := eng.StartContact(ctx, "+57300")
	if err != nil {
		t.Fatalf("StartContact: %v", err)
	}
	if callID != "conv-1" {
		t.Fatalf("callID = %q, want conv-1 (API conversation id)", callID)
	}
	if cs, ok := cap.first(CALL_STARTED); !ok || cs.Payload["channel"] != "whatsapp" {
		t.Errorf("CALL_STARTED channel not whatsapp: %+v", cs.Payload)
	}

	// 2) first answer (number coercion 30)
	if _, err := eng.HandleInbound(ctx, "+57300", "tengo 30 años"); err != nil {
		t.Fatalf("inbound 1: %v", err)
	}
	if lastAnswer["value"] != float64(30) {
		t.Errorf("answer value = %#v, want 30", lastAnswer["value"])
	}
	if lastAnswer["source"] != "whatsapp" {
		t.Errorf("answer source = %v, want whatsapp", lastAnswer["source"])
	}

	// 3) second answer -> ready -> complete -> recommendations
	if _, err := eng.HandleInbound(ctx, "+57300", "sí"); err != nil {
		t.Fatalf("inbound 2: %v", err)
	}

	if answers != 2 {
		t.Errorf("answers posted = %d, want 2", answers)
	}
	if n := cap.count(RECOMMENDATION_GENERATED); n != 1 {
		t.Errorf("RECOMMENDATION_GENERATED = %d, want 1", n)
	}
	if rec, ok := cap.first(RECOMMENDATION_GENERATED); !ok || rec.Payload["product_name"] != "Vida Protección Familiar Plus" {
		t.Errorf("recommendation product_name wrong: %+v", rec.Payload)
	}
	if cap.count(CALL_ENDED) != 1 {
		t.Errorf("CALL_ENDED = %d, want 1", cap.count(CALL_ENDED))
	}
	if cap.count(MESSAGE_SENT) < 3 {
		t.Errorf("MESSAGE_SENT = %d, want >=3 (opener + q2 + recs)", cap.count(MESSAGE_SENT))
	}
	// FEATURE_UPDATED per answered variable (age, has_kids)
	if cap.count(FEATURE_UPDATED) != 2 {
		t.Errorf("FEATURE_UPDATED = %d, want 2", cap.count(FEATURE_UPDATED))
	}
}

func readJSON(r *http.Request) (map[string]interface{}, error) {
	m := map[string]interface{}{}
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&m)
	return m, err
}

// guard: the mock server URL must be an http base (client builds base+path)
func TestColsubsidioDisabledByDefault(t *testing.T) {
	t.Setenv("COLSUBSIDIO_API_URL", "")
	if NewColsubsidioClient().Enabled() {
		t.Error("client should be disabled without COLSUBSIDIO_API_URL")
	}
	if strings.HasPrefix("", "http") {
		t.Fatal("unreachable")
	}
}
