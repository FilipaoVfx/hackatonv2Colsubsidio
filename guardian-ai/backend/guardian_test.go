package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// scriptedLLM implements GuardianLLM returning one canned decision per call.
type scriptedLLM struct {
	decisions []GuardianDecision
	i         int
	prompts   []string
}

func (s *scriptedLLM) DecideGuardian(_ context.Context, system string, _ []oaMessage) (*GuardianDecision, error) {
	s.prompts = append(s.prompts, system)
	if s.i >= len(s.decisions) {
		return &GuardianDecision{Intent: "provide_info", NextAction: ActionAsk, AssistantMessage: "ok", Confidence: 0.9}, nil
	}
	d := s.decisions[s.i]
	s.i++
	return &d, nil
}

// guardianAPI is a minimal in-memory Protege API for the engine tests.
func guardianAPI(t *testing.T) *httptest.Server {
	t.Helper()
	vars := map[string]map[string]interface{}{} // user -> key -> value
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/users/search", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProtegeUser{}) // siempre nuevo
	})
	mux.HandleFunc("POST /api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(ProtegeUser{ID: "user-1", Phone: "+573000000001"})
	})
	mux.HandleFunc("POST /api/v1/conversations", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "conv-1", "user_id": "user-1", "channel": "whatsapp", "status": "collecting_data"})
	})
	mux.HandleFunc("GET /api/v1/questions", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProtegeQuestion{
			{ID: "q1", VariableKey: "full_name", FieldType: "text", Required: true, OrderIndex: 1, Text: "¿Nombre?"},
			{ID: "q2", VariableKey: "has_pet", FieldType: "boolean", Required: true, OrderIndex: 2, Text: "¿Mascota?"},
			{ID: "q3", VariableKey: "monthly_income", FieldType: "currency", Required: true, OrderIndex: 9, Text: "¿Ingresos?"},
		})
	})
	mux.HandleFunc("GET /api/v1/products", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProtegeProduct{{ID: "p1", Name: "Seguro Mascota Protegida", Category: "mascotas", BasePrice: 22000, Active: true}})
	})
	mux.HandleFunc("GET /api/v1/rules", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProtegeRule{{ID: "r1", ProductID: "p1", VariableKey: "has_pet", Operator: "equals", ExpectedValue: true, Weight: 0.7, Reason: "Tienes mascota.", Active: true}})
	})
	mux.HandleFunc("PUT /api/v1/users/{uid}/variables", func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("uid")
		var in []UserVariable
		_ = json.NewDecoder(r.Body).Decode(&in)
		if vars[uid] == nil {
			vars[uid] = map[string]interface{}{}
		}
		out := []UserVariable{}
		for _, v := range in {
			vars[uid][v.Key] = v.Value
		}
		for k, v := range vars[uid] {
			out = append(out, UserVariable{Key: k, Value: v, Source: "whatsapp"})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /api/v1/users/{uid}/variables", func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("uid")
		out := []UserVariable{}
		for k, v := range vars[uid] {
			out = append(out, UserVariable{Key: k, Value: v, Source: "whatsapp"})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("POST /api/v1/recommendations/users/{uid}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"recommendations": []map[string]interface{}{{"name": "Seguro Mascota Protegida", "reason": "Tienes mascota.", "score": 0.9}},
		})
	})
	mux.HandleFunc("POST /api/v1/conversations/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"conversation_id": "conv-1", "status": "completed", "recommendations": []interface{}{}})
	})
	return httptest.NewServer(mux)
}

func newGuardianForTest(t *testing.T, srvURL string, llm GuardianLLM) (*GuardianEngine, *capture) {
	t.Helper()
	bus := NewEventBus()
	cap := &capture{}
	bus.Subscribe("*", cap.on)
	api := &ColsubsidioClient{base: srvURL, http: &http.Client{Timeout: 5 * time.Second}}
	tools := NewTools(api, bus)
	sessions := NewWhatsAppSessions()
	return NewGuardianEngine(bus, api, llm, tools, sessions, &RAG{}, nil), cap
}

// TestGuardianFullFlow drives a lead NEW→…→READY_FOR_ADVISOR and asserts the
// spec invariants: legal transitions only, immediate persistence, LEAD_READY.
func TestGuardianFullFlow(t *testing.T) {
	srv := guardianAPI(t)
	defer srv.Close()

	llm := &scriptedLLM{decisions: []GuardianDecision{
		// turno 1: cliente se presenta; extrae nombre + mascota
		{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAsk, AssistantMessage: "¡Mucho gusto Ana!",
			Entities: []GuardianEntity{
				{Key: "full_name", Value: "Ana Rojas", Confidence: 0.95},
				{Key: "has_pet", Value: true, Confidence: 0.9},
			}},
		// turno 2: ingresos → completa financial → matching
		{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAsk, AssistantMessage: "Gracias por contarme.",
			Entities: []GuardianEntity{{Key: "monthly_income", Value: 3000000.0, Confidence: 0.9}}},
		// turno 3: acepta → ready
		{Intent: "accept", Confidence: 0.95, NextAction: ActionHandoff, AssistantMessage: "¡Genial!"},
	}}

	g, cap := newGuardianForTest(t, srv.URL, llm)
	ctx := context.Background()

	convID, err := g.HandleInbound(ctx, "+573000000001", "Hola, soy Ana Rojas y tengo un perro")
	if err != nil {
		t.Fatalf("turno 1: %v", err)
	}
	if convID != "conv-1" {
		t.Fatalf("convID = %s", convID)
	}
	if _, err := g.HandleInbound(ctx, "+573000000001", "gano 3 millones"); err != nil {
		t.Fatalf("turno 2: %v", err)
	}
	if _, err := g.HandleInbound(ctx, "+573000000001", "sí, quiero el asesor"); err != nil {
		t.Fatalf("turno 3: %v", err)
	}

	// Estados: secuencia legal completa registrada.
	var seq []string
	cap.mu.Lock()
	for _, ev := range cap.events {
		if ev.Type == STATE_CHANGED {
			seq = append(seq, ev.Payload["from"].(string)+">"+ev.Payload["to"].(string))
		}
	}
	cap.mu.Unlock()
	joined := strings.Join(seq, " ")
	for _, want := range []string{"NEW>AFFILIATION_CHECK", "AFFILIATION_CHECK>PROFILE_DISCOVERY",
		"PROFILE_DISCOVERY>FINANCIAL_QUALIFICATION", "FINANCIAL_QUALIFICATION>PROJECT_MATCHING",
		"PROJECT_MATCHING>READY_FOR_ADVISOR", "READY_FOR_ADVISOR>COMPLETED"} {
		if !strings.Contains(joined, want) {
			t.Errorf("falta transición %s en %s", want, joined)
		}
	}

	// Persistencia inmediata: FEATURE_UPDATED de full_name en el turno 1.
	if n := cap.count(FEATURE_UPDATED); n < 3 { // afiliacion + full_name + has_pet + monthly_income
		t.Errorf("FEATURE_UPDATED = %d, want >= 3", n)
	}
	// LEAD_READY con variables y recomendaciones.
	lr, ok := cap.first(LEAD_READY)
	if !ok {
		t.Fatal("sin LEAD_READY")
	}
	if lr.Payload["user_id"] != "user-1" {
		t.Errorf("LEAD_READY user_id = %v", lr.Payload["user_id"])
	}
	if cap.count(RECOMMENDATION_GENERATED) != 1 {
		t.Errorf("RECOMMENDATION_GENERATED = %d, want 1", cap.count(RECOMMENDATION_GENERATED))
	}
	if cap.count(CALL_ENDED) != 1 {
		t.Errorf("CALL_ENDED = %d, want 1", cap.count(CALL_ENDED))
	}
	if cap.count(TURN_COMPLETED) != 3 {
		t.Errorf("TURN_COMPLETED = %d, want 3 (uno por mensaje)", cap.count(TURN_COMPLETED))
	}
	// El prompt del turno 2 lleva memoria (no re-preguntar): contiene has_pet.
	if len(llm.prompts) >= 2 && !strings.Contains(llm.prompts[1], "has_pet") {
		t.Error("el prompt del turno 2 no lleva la memoria de has_pet")
	}
}

// TestGuardianIllegalActionIgnored: acción fuera de la whitelist se degrada a ask.
func TestGuardianIllegalActionIgnored(t *testing.T) {
	srv := guardianAPI(t)
	defer srv.Close()
	llm := &scriptedLLM{decisions: []GuardianDecision{
		{Intent: "provide_info", Confidence: 0.9, NextAction: ActionHandoff, AssistantMessage: "hola"}, // handoff ilegal en PROFILE
	}}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	if _, err := g.HandleInbound(context.Background(), "+573000000002", "hola"); err != nil {
		t.Fatal(err)
	}
	if cap.count(LEAD_READY) != 0 {
		t.Error("handoff ilegal no debe producir LEAD_READY")
	}
	if cap.count(CALL_ENDED) != 0 {
		t.Error("handoff ilegal no debe cerrar la conversación")
	}
}

// TestGuardianLowConfidenceNotPersisted: entidades dudosas no se guardan.
func TestGuardianLowConfidenceNotPersisted(t *testing.T) {
	srv := guardianAPI(t)
	defer srv.Close()
	llm := &scriptedLLM{decisions: []GuardianDecision{
		{Intent: "provide_info", Confidence: 0.5, NextAction: ActionAsk, AssistantMessage: "¿me confirmas?",
			Entities: []GuardianEntity{{Key: "monthly_income", Value: 100.0, Confidence: 0.3}}},
	}}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	if _, err := g.HandleInbound(context.Background(), "+573000000003", "mmm no sé"); err != nil {
		t.Fatal(err)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	for _, ev := range cap.events {
		if ev.Type == FEATURE_UPDATED && ev.Payload["key"] == "monthly_income" {
			t.Error("variable con confidence 0.3 no debe persistirse")
		}
	}
}
