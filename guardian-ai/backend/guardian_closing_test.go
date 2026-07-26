package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// El criterio del reto que antes NO se cumplía: "cierra la vinculación:
// aceptación, confirmación y resumen. La persona termina asegurada". Estos
// tests recorren ese tramo contra una API que se comporta como el mock real.

// closingAPI extiende la API de prueba con recomendaciones que traen
// coberturas y con los endpoints de cotización y vinculación.
func closingAPI(t *testing.T) (*httptest.Server, *closingCalls) {
	t.Helper()
	calls := &closingCalls{}
	vars := map[string]map[string]interface{}{}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/users/search", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProtegeUser{})
	})
	mux.HandleFunc("POST /api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(ProtegeUser{ID: "user-1", Phone: "+573000000001"})
	})
	mux.HandleFunc("POST /api/v1/conversations", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "conv-1", "user_id": "user-1", "status": "collecting_data"})
	})
	mux.HandleFunc("GET /api/v1/questions", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProtegeQuestion{
			{ID: "q1", VariableKey: "full_name", FieldType: "text", Required: true, OrderIndex: 1, Text: "¿Nombre?"},
			{ID: "q2", VariableKey: "has_pet", FieldType: "boolean", Required: true, OrderIndex: 2, Text: "¿Mascota?"},
			{ID: "q3", VariableKey: "monthly_income", FieldType: "currency", Required: true, OrderIndex: 9, Text: "¿Ingresos?"},
		})
	})
	mux.HandleFunc("GET /api/v1/products", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProtegeProduct{})
	})
	mux.HandleFunc("GET /api/v1/rules", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProtegeRule{})
	})
	mux.HandleFunc("PUT /api/v1/users/{uid}/variables", func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("uid")
		var in []UserVariable
		_ = json.NewDecoder(r.Body).Decode(&in)
		if vars[uid] == nil {
			vars[uid] = map[string]interface{}{}
		}
		for _, v := range in {
			vars[uid][v.Key] = v.Value
		}
		_ = json.NewEncoder(w).Encode([]UserVariable{})
	})
	mux.HandleFunc("GET /api/v1/users/{uid}/variables", func(w http.ResponseWriter, r *http.Request) {
		out := []UserVariable{}
		for k, v := range vars[r.PathValue("uid")] {
			out = append(out, UserVariable{Key: k, Value: v, Source: "whatsapp"})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("POST /api/v1/recommendations/users/{uid}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"recommendations": []map[string]interface{}{
			{"product_id": "p-vida", "name": "Seguro de vida", "reason": "Tienes dependientes.", "base_price": 25000.0,
				"coverages": []map[string]interface{}{
					{"key": "fallecimiento", "label": "Fallecimiento", "included": true, "price_delta": 0.0, "source": "portafolio_publicado"},
					{"key": "auxilio_exequial", "label": "Auxilio exequial complementario", "included": false, "price_delta": 6000.0, "source": "estimacion_demo"},
				}},
			{"product_id": "p-mascotas", "name": "Seguro de mascotas", "reason": "Tienes mascota.", "base_price": 22000.0,
				"coverages": []map[string]interface{}{
					{"key": "mascota", "label": "Atención veterinaria", "included": true, "price_delta": 0.0, "source": "portafolio_publicado"},
				}},
		}})
	})
	mux.HandleFunc("POST /api/v1/quotes", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ProductID string   `json:"product_id"`
			Coverages []string `json:"coverages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		calls.quotes = append(calls.quotes, in.ProductID)
		calls.quotedCoverages = in.Coverages
		price := 25000.0
		name := "Seguro de vida"
		if in.ProductID == "p-mascotas" {
			price, name = 22000.0, "Seguro de mascotas"
		}
		covs := []map[string]interface{}{
			{"key": "fallecimiento", "label": "Fallecimiento", "included": true, "price_delta": 0.0},
		}
		for _, k := range in.Coverages {
			if k == "auxilio_exequial" {
				price += 6000
				covs = append(covs, map[string]interface{}{"key": k, "label": "Auxilio exequial complementario", "included": true, "price_delta": 6000.0})
			}
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "quote-1", "product_id": in.ProductID, "product_name": name,
			"base_price": price, "monthly_price": price, "currency": "COP", "coverages": covs,
		})
	})
	mux.HandleFunc("POST /api/v1/enrollments", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			QuoteID  string `json:"quote_id"`
			Accepted bool   `json:"accepted"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		calls.enrollAccepted = in.Accepted
		calls.enrollments++
		if !in.Accepted {
			w.WriteHeader(422)
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "enr-1", "application_number": "COL-2026-123456", "product_id": "p-vida",
			"product_name": "Seguro de vida", "monthly_price": 31000.0, "currency": "COP",
			"status": "CONFIRMED", "summary": "Solicitud COL-2026-123456 emitida.",
			"next_step_url": "https://www.colsubsidio.com/seguros/familiares/vida",
			"coverages":     []map[string]interface{}{{"key": "fallecimiento", "label": "Fallecimiento", "included": true}},
		})
	})
	mux.HandleFunc("POST /api/v1/conversations/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"conversation_id": "conv-1", "status": "completed"})
	})
	return httptest.NewServer(mux), calls
}

type closingCalls struct {
	quotes          []string
	quotedCoverages []string
	enrollments     int
	enrollAccepted  bool
}

// profileTurns son los dos turnos que llevan la conversación hasta MATCHING.
func profileTurns() []GuardianDecision {
	return []GuardianDecision{
		{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAsk, AssistantMessage: "¡Mucho gusto!",
			Entities: []GuardianEntity{
				{Key: "full_name", Value: "Ana Rojas", Confidence: 0.95},
				{Key: "has_pet", Value: true, Confidence: 0.9},
			}},
		{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAsk, AssistantMessage: "Gracias.",
			Entities: []GuardianEntity{{Key: "monthly_income", Value: 3000000.0, Confidence: 0.9}}},
	}
}

func agentMessages(cap *capture) []string {
	var out []string
	cap.mu.Lock()
	defer cap.mu.Unlock()
	for _, ev := range cap.events {
		if ev.Type == MESSAGE_SENT {
			if s, ok := ev.Payload["text"].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// El camino feliz: la persona acepta, confirma y TERMINA ASEGURADA, sin que
// aparezca un asesor humano por ningún lado.
func TestGuardianClosesTheSale(t *testing.T) {
	srv, calls := closingAPI(t)
	defer srv.Close()

	llm := &scriptedLLM{decisions: append(profileTurns(),
		// acepta la recomendación → cotización
		GuardianDecision{Intent: "accept", Confidence: 0.95, NextAction: ActionAccept, AssistantMessage: "¡Genial!"},
		// confirma → vinculación
		GuardianDecision{Intent: "accept", Confidence: 0.95, NextAction: ActionAccept, AssistantMessage: "Confirmo."},
	)}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	ctx := context.Background()

	for _, msg := range []string{"Hola, soy Ana Rojas y tengo un perro", "gano 3 millones",
		"me sirve el seguro de vida", "sí, confirmo"} {
		if _, err := g.HandleInbound(ctx, "+573000000001", msg); err != nil {
			t.Fatalf("turno %q: %v", msg, err)
		}
	}

	var seq []string
	cap.mu.Lock()
	for _, ev := range cap.events {
		if ev.Type == STATE_CHANGED {
			seq = append(seq, ev.Payload["from"].(string)+">"+ev.Payload["to"].(string))
		}
	}
	cap.mu.Unlock()
	joined := strings.Join(seq, " ")
	for _, want := range []string{"FINANCIAL_QUALIFICATION>PROJECT_MATCHING", "PROJECT_MATCHING>CLOSING", "CLOSING>COMPLETED"} {
		if !strings.Contains(joined, want) {
			t.Errorf("falta transición %s en %s", want, joined)
		}
	}
	if strings.Contains(joined, "READY_FOR_ADVISOR") {
		t.Errorf("el camino feliz derivó a un asesor: %s", joined)
	}

	if _, ok := cap.first(QUOTE_CREATED); !ok {
		t.Fatal("sin QUOTE_CREATED: nunca se cotizó")
	}
	ev, ok := cap.first(ENROLLMENT_CREATED)
	if !ok {
		t.Fatal("sin ENROLLMENT_CREATED: la persona no quedó asegurada")
	}
	if ev.Payload["application_number"] != "COL-2026-123456" {
		t.Errorf("radicado = %v", ev.Payload["application_number"])
	}
	if !calls.enrollAccepted {
		t.Error("la vinculación viajó sin aceptación explícita")
	}
	if calls.enrollments != 1 {
		t.Errorf("vinculaciones emitidas = %d, esperada 1", calls.enrollments)
	}

	msgs := strings.Join(agentMessages(cap), "\n")
	for _, want := range []string{"COL-2026-123456", "https://www.colsubsidio.com/seguros/familiares/vida", "Coberturas contratadas"} {
		if !strings.Contains(msgs, want) {
			t.Errorf("el resumen final no contiene %q", want)
		}
	}
	// La primera aceptación NO puede haber vinculado: primero se muestra el
	// precio y se pide confirmación.
	if !strings.Contains(msgs, "¿Confirmas la vinculación") {
		t.Error("no se pidió confirmación explícita antes de vincular")
	}
}

// Ajustar coberturas y comparar opciones cambia lo que se cotiza, sin llamar a
// nadie: es el tercer criterio del reto.
func TestCustomerAdjustsCoverageAndSwitchesProduct(t *testing.T) {
	srv, calls := closingAPI(t)
	defer srv.Close()

	llm := &scriptedLLM{decisions: append(profileTurns(),
		// pide otro producto del ranking
		GuardianDecision{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAdjust, AssistantMessage: "Te muestro ese."},
		// vuelve al de vida y añade una cobertura opcional
		GuardianDecision{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAdjust, AssistantMessage: "Listo."},
	)}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	ctx := context.Background()

	for _, msg := range []string{"Hola, soy Ana Rojas y tengo un perro", "gano 3 millones",
		"mejor el de mascotas", "vuelve al de vida y añade el auxilio exequial complementario"} {
		if _, err := g.HandleInbound(ctx, "+573000000001", msg); err != nil {
			t.Fatalf("turno %q: %v", msg, err)
		}
	}

	if len(calls.quotes) != 2 {
		t.Fatalf("cotizaciones = %v, esperadas 2", calls.quotes)
	}
	if calls.quotes[0] != "p-mascotas" {
		t.Errorf("la primera cotización fue %q; el cliente pidió mascotas", calls.quotes[0])
	}
	if calls.quotes[1] != "p-vida" {
		t.Errorf("la segunda cotización fue %q; el cliente volvió a vida", calls.quotes[1])
	}
	if len(calls.quotedCoverages) != 1 || calls.quotedCoverages[0] != "auxilio_exequial" {
		t.Errorf("coberturas cotizadas = %v, esperada [auxilio_exequial]", calls.quotedCoverages)
	}
	// El ajuste ocurre sin salir de la conversación ni escalar.
	if _, ok := cap.first(LEAD_READY); ok {
		t.Error("ajustar coberturas derivó el lead a un asesor")
	}
	msgs := strings.Join(agentMessages(cap), "\n")
	if !strings.Contains(msgs, "$31.000") {
		t.Errorf("el precio ajustado no se mostró al cliente:\n%s", msgs)
	}
}

// Un "close" mal etiquetado durante el cierre no puede tumbar la venta: se
// exige que la intención lo respalde.
func TestSpuriousCloseDoesNotKillTheSale(t *testing.T) {
	srv, _ := closingAPI(t)
	defer srv.Close()

	llm := &scriptedLLM{decisions: append(profileTurns(),
		// el cliente sigue comprando, pero el modelo propone cerrar
		GuardianDecision{Intent: "provide_info", Confidence: 0.9, NextAction: ActionClose,
			AssistantMessage: "Te cuento más de ese plan."},
		// despedida real: ahora sí cierra
		GuardianDecision{Intent: "goodbye", Confidence: 0.9, NextAction: ActionClose, AssistantMessage: "Hasta luego."},
	)}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	ctx := context.Background()

	for _, msg := range []string{"Hola, soy Ana Rojas y tengo un perro", "gano 3 millones",
		"me sirve ese, cuéntame más"} {
		if _, err := g.HandleInbound(ctx, "+573000000001", msg); err != nil {
			t.Fatalf("turno %q: %v", msg, err)
		}
	}
	if _, ok := cap.first(CALL_ENDED); ok {
		t.Fatal("un close sin intención de cierre terminó la conversación")
	}
	if _, err := g.HandleInbound(ctx, "+573000000001", "gracias, hablamos luego"); err != nil {
		t.Fatalf("turno de despedida: %v", err)
	}
	if _, ok := cap.first(CALL_ENDED); !ok {
		t.Error("una despedida explícita debe cerrar la conversación")
	}
}

// Pedir comparar debe comparar, no cotizar: el cliente aún no eligió.
func TestCompareShowsOptionsInsteadOfQuoting(t *testing.T) {
	srv, calls := closingAPI(t)
	defer srv.Close()

	llm := &scriptedLLM{decisions: append(profileTurns(),
		GuardianDecision{Intent: "ask_info", Confidence: 0.9, NextAction: ActionAdjust, AssistantMessage: "Claro."},
	)}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	ctx := context.Background()
	for _, msg := range []string{"Hola, soy Ana Rojas y tengo un perro", "gano 3 millones",
		"compárame el primero con el segundo"} {
		if _, err := g.HandleInbound(ctx, "+573000000001", msg); err != nil {
			t.Fatalf("turno %q: %v", msg, err)
		}
	}
	if len(calls.quotes) != 0 {
		t.Errorf("comparar no debe cotizar todavía, hubo %v", calls.quotes)
	}
	msgs := strings.Join(agentMessages(cap), "\n")
	if !strings.Contains(msgs, "Te las comparo") {
		t.Fatalf("no se envió la comparación:\n%s", msgs)
	}
	for _, want := range []string{"Seguro de vida", "Seguro de mascotas", "Por qué a ti"} {
		if !strings.Contains(msgs, want) {
			t.Errorf("la comparación no menciona %q", want)
		}
	}
	if got := g.State("conv-1"); got != StateMatching {
		t.Errorf("comparar movió la etapa a %s; debe seguir en PROJECT_MATCHING", got)
	}
}

// La derivación sigue existiendo, pero SOLO cuando la persona la pide.
func TestHandoffOnlyWhenTheCustomerAsksForIt(t *testing.T) {
	srv, _ := closingAPI(t)
	defer srv.Close()

	llm := &scriptedLLM{decisions: append(profileTurns(),
		GuardianDecision{Intent: "request_advisor", Confidence: 0.95, NextAction: ActionHandoff, AssistantMessage: "Claro."},
	)}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	ctx := context.Background()

	for _, msg := range []string{"Hola, soy Ana Rojas y tengo un perro", "gano 3 millones", "prefiero hablar con una persona"} {
		if _, err := g.HandleInbound(ctx, "+573000000001", msg); err != nil {
			t.Fatalf("turno %q: %v", msg, err)
		}
	}
	if _, ok := cap.first(LEAD_READY); !ok {
		t.Error("el cliente pidió un asesor y el lead no se derivó")
	}
	if _, ok := cap.first(ENROLLMENT_CREATED); ok {
		t.Error("se vinculó a alguien que pidió hablar con una persona")
	}
}

// pickOption/pickCoverages son deterministas: se prueban aparte porque son la
// pieza que decide QUÉ se cotiza, y no puede depender del LLM.
func TestDeterministicSelection(t *testing.T) {
	options := []recOption{
		{ProductID: "p1", Name: "Seguro de vida", Coverages: []ProtegeCoverage{
			{Key: "auxilio_exequial", Label: "Auxilio exequial complementario"},
			{Key: "asistencia_medica_telefonica", Label: "Asistencia médica telefónica 24/7"},
		}},
		{ProductID: "p2", Name: "Seguro de mascotas"},
		{ProductID: "p3", Name: "Todo riesgo hogar"},
	}
	for _, tc := range []struct {
		text string
		want int
	}{
		{"cuéntame más", 0},               // sin mención: la de mayor score
		{"me interesa el de mascotas", 1}, // nombra la segunda
		{"prefiero el de hogar", 2},       // nombra la tercera
		{"quiero un seguro", 0},           // "seguro" no identifica nada
	} {
		if got := pickOption(options, tc.text); got != tc.want {
			t.Errorf("pickOption(%q) = %d, esperada %d", tc.text, got, tc.want)
		}
	}

	got := pickCoverages(options[0], "súmame el auxilio exequial complementario")
	if len(got) != 1 || got[0] != "auxilio_exequial" {
		t.Errorf("pickCoverages = %v, esperada [auxilio_exequial]", got)
	}
	if got := pickCoverages(options[0], "no quiero nada más"); len(got) != 0 {
		t.Errorf("pickCoverages sin mención = %v, esperada vacía", got)
	}
	// Una palabra suelta de la etiqueta no basta para cobrar una cobertura.
	if got := pickCoverages(options[0], "tengo una duda médica"); len(got) != 0 {
		t.Errorf("una palabra suelta añadió coberturas: %v", got)
	}
}

// El motor no puede quedarse esperando al LLM: la cotización debe fallar hacia
// un cierre honesto si la API no responde.
func TestQuoteFailureDoesNotLeaveTheCustomerHanging(t *testing.T) {
	// Una API con /quotes caído: el cliente acepta y el motor tiene que
	// reaccionar en vez de quedarse mudo.
	broken := httptest.NewServer(brokenQuoteMux(t))
	defer broken.Close()

	llm := &scriptedLLM{decisions: append(profileTurns(),
		GuardianDecision{Intent: "accept", Confidence: 0.95, NextAction: ActionAccept, AssistantMessage: "¡Genial!"},
	)}
	g, cap := newGuardianForTest(t, broken.URL, llm)
	ctx := context.Background()
	for _, msg := range []string{"Hola, soy Ana Rojas y tengo un perro", "gano 3 millones", "me sirve"} {
		if _, err := g.HandleInbound(ctx, "+573000000001", msg); err != nil {
			t.Fatalf("turno %q: %v", msg, err)
		}
	}
	if _, ok := cap.first(ENROLLMENT_CREATED); ok {
		t.Error("se emitió una vinculación sin cotización válida")
	}
	if _, ok := cap.first(CALL_ENDED); !ok {
		t.Error("la conversación quedó viva y muda tras fallar la cotización")
	}
}

// brokenQuoteMux devuelve la API de cierre con /quotes caído (500).
func brokenQuoteMux(t *testing.T) http.Handler {
	t.Helper()
	srv, _ := closingAPI(t)
	inner := srv.Config.Handler
	srv.Close()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/quotes" {
			w.WriteHeader(500)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// El cliente pide la recomendación con el perfil a medias: el agente no puede
// seguir preguntando. Con un perfil mínimo se avanza y se recomienda.
func TestRecommendationRequestDoesNotKeepAsking(t *testing.T) {
	srv, _ := closingAPI(t)
	defer srv.Close()

	llm := &scriptedLLM{decisions: []GuardianDecision{
		// solo dos variables de perfil: aún falta monthly_income (financiera)
		{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAsk, AssistantMessage: "Mucho gusto.",
			Entities: []GuardianEntity{
				{Key: "full_name", Value: "Marta Gómez", Confidence: 0.95},
				{Key: "has_pet", Value: true, Confidence: 0.9},
			}},
		{Intent: "ask_info", Confidence: 0.9, NextAction: ActionRecommend, AssistantMessage: "Va."},
	}}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	ctx := context.Background()
	for _, msg := range []string{"Hola, soy Marta y tengo un perro", "muéstrame qué me recomiendas"} {
		if _, err := g.HandleInbound(ctx, "+573000000001", msg); err != nil {
			t.Fatalf("turno %q: %v", msg, err)
		}
	}
	if _, ok := cap.first(RECOMMENDATION_GENERATED); !ok {
		t.Fatal("el cliente pidió recomendación y el agente siguió perfilando")
	}
	if got := g.State("conv-1"); got != StateMatching {
		t.Errorf("etapa = %s, esperada PROJECT_MATCHING", got)
	}
}

// Si el modelo deja de extraer variables, la conversación no puede quedarse
// preguntando lo mismo para siempre: tras maxStallTurns el motor avanza.
func TestStalledDiscoveryAdvancesAnyway(t *testing.T) {
	srv, _ := closingAPI(t)
	defer srv.Close()

	turns := []GuardianDecision{
		{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAsk, AssistantMessage: "Mucho gusto.",
			Entities: []GuardianEntity{
				{Key: "full_name", Value: "Marta Gómez", Confidence: 0.95},
				{Key: "has_pet", Value: true, Confidence: 0.9},
			}},
	}
	// turnos que no extraen NADA (el caso real: "dos hijos" que no se convierte
	// en num_dependents)
	for i := 0; i < maxStallTurns; i++ {
		turns = append(turns, GuardianDecision{Intent: "provide_info", Confidence: 0.9,
			NextAction: ActionAsk, AssistantMessage: "Cuéntame más."})
	}
	g, cap := newGuardianForTest(t, srv.URL, &scriptedLLM{decisions: turns})
	ctx := context.Background()
	for i := 0; i <= maxStallTurns; i++ {
		if _, err := g.HandleInbound(ctx, "+573000000001", "tengo dos hijos"); err != nil {
			t.Fatalf("turno %d: %v", i, err)
		}
	}
	if st := g.State("conv-1"); st == StateProfile {
		t.Errorf("la conversación sigue atascada en %s tras %d turnos sin descubrir nada", st, maxStallTurns)
	}
	var stalled bool
	cap.mu.Lock()
	for _, ev := range cap.events {
		if ev.Type == STATE_CHANGED && strings.Contains(fmt.Sprint(ev.Payload["reason"]), "sin descubrimiento nuevo") {
			stalled = true
		}
	}
	cap.mu.Unlock()
	if !stalled {
		t.Error("el avance por atasco debe quedar registrado en STATE_CHANGED con su razón")
	}
}
