package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Regresiones de robustez del flujo Guardian: ráfaga de mensajes del mismo
// cliente, reentrega del webhook y catálogo de preguntas caído. Los tres eran
// fallos reales reproducidos antes del arreglo.

// slowLLM es un GuardianLLM seguro para uso concurrente y lento a propósito:
// dos turnos en paralelo se solapan de verdad.
type slowLLM struct {
	mu       sync.Mutex
	calls    int
	decision GuardianDecision
}

func (s *slowLLM) DecideGuardian(_ context.Context, _ string, _ []oaMessage) (*GuardianDecision, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	d := s.decision
	return &d, nil
}

func (s *slowLLM) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestGuardianConcurrentInboundSerialized: WhatsApp entrega en ráfaga y el
// webhook lanza una goroutine por mensaje. Los turnos del MISMO teléfono deben
// serializarse; con -race este test detectaba la escritura simultánea sobre
// st.history (sendAgent) mientras el otro turno lo leía hacia el LLM.
func TestGuardianConcurrentInboundSerialized(t *testing.T) {
	srv := guardianAPI(t)
	defer srv.Close()

	llm := &slowLLM{decision: GuardianDecision{Intent: "provide_info", Confidence: 0.9,
		NextAction: ActionAsk, AssistantMessage: "ok",
		Entities: []GuardianEntity{{Key: "full_name", Value: "Ana", Confidence: 0.95}}}}
	g, _ := newGuardianForTest(t, srv.URL, llm)

	const msgs = 4
	var wg sync.WaitGroup
	for i := 0; i < msgs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.HandleInbound(context.Background(), "+573000000001", "hola"); err != nil {
				t.Errorf("inbound: %v", err)
			}
		}()
	}
	wg.Wait()

	if n := llm.count(); n != msgs {
		t.Errorf("turnos ejecutados = %d, want %d", n, msgs)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.convs) != 1 {
		t.Fatalf("conversaciones vivas = %d, want 1 (una por cliente)", len(g.convs))
	}
	for _, st := range g.convs {
		// un par usuario+agente por turno, sin entradas perdidas ni duplicadas
		if want := 2 * msgs; len(st.history) != want && len(st.history) != maxHistory {
			t.Errorf("historial = %d mensajes, want %d", len(st.history), want)
		}
	}
}

// TestWebhookDedupeIgnoresRedelivery: Kapso reentrega el mensaje cuando no
// recibe el 200 a tiempo. El mismo message id no puede correr dos turnos.
func TestWebhookDedupeIgnoresRedelivery(t *testing.T) {
	dedupe := newInboundDedupe(15 * time.Minute)
	calls := 0
	process := func(from, text string) { calls++ }
	body := []byte(kapsoReceivedBody)

	if status, _ := processKapsoWebhook(body, "whatsapp.message.received", "", "", dedupe, process); status != 200 || calls != 1 {
		t.Fatalf("primera entrega: status=%d calls=%d", status, calls)
	}
	status, debug := processKapsoWebhook(body, "whatsapp.message.received", "", "", dedupe, process)
	if status != 200 {
		t.Errorf("reentrega: status=%d, want 200 (ack)", status)
	}
	if calls != 1 {
		t.Errorf("reentrega procesada: calls=%d, want 1 (%s)", calls, debug)
	}

	// un mensaje distinto sí se procesa
	other := `{"message":{"id":"wamid.OTRO","type":"text","from":"573001234567","text":{"body":"hola"}},"conversation":{"id":"c1","phone_number":"573001234567"}}`
	if _, _ = processKapsoWebhook([]byte(other), "whatsapp.message.received", "", "", dedupe, process); calls != 2 {
		t.Errorf("mensaje nuevo: calls=%d, want 2", calls)
	}

	// pasada la ventana, el id vuelve a ser procesable
	dedupe.now = func() time.Time { return time.Now().Add(30 * time.Minute) }
	if _, _ = processKapsoWebhook(body, "whatsapp.message.received", "", "", dedupe, process); calls != 3 {
		t.Errorf("fuera de la ventana TTL: calls=%d, want 3", calls)
	}
}

// TestGuardianRejectsInventedVariableKeys: el LLM solo puede escribir en el
// perfil las variable_key del catálogo de la API (más la serie de afiliado).
// Una clave inventada no entra a la memoria estratégica y queda reportada.
func TestGuardianRejectsInventedVariableKeys(t *testing.T) {
	srv := guardianAPI(t)
	defer srv.Close()

	llm := &scriptedLLM{decisions: []GuardianDecision{
		{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAsk, AssistantMessage: "ok",
			Entities: []GuardianEntity{
				{Key: "full_name", Value: "Ana", Confidence: 0.95},         // del catálogo
				{Key: "affiliate_serie", Value: "2", Confidence: 0.95},     // serie
				{Key: "signo_zodiacal", Value: "leo", Confidence: 0.99},    // inventada
				{Key: "prefiere_descuento", Value: true, Confidence: 0.99}, // inventada
			}},
	}}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	if _, err := g.HandleInbound(context.Background(), "+573000000007", "hola"); err != nil {
		t.Fatal(err)
	}

	saved := map[string]bool{}
	cap.mu.Lock()
	for _, ev := range cap.events {
		if ev.Type == FEATURE_UPDATED {
			if k, ok := ev.Payload["key"].(string); ok {
				saved[k] = true
			}
		}
	}
	cap.mu.Unlock()
	if !saved["full_name"] {
		t.Error("una clave del catálogo debe guardarse")
	}
	for _, k := range []string{"signo_zodiacal", "prefiere_descuento"} {
		if saved[k] {
			t.Errorf("la clave inventada %q no debe entrar al perfil", k)
		}
	}

	tc, ok := cap.first(TURN_COMPLETED)
	if !ok {
		t.Fatal("sin TURN_COMPLETED")
	}
	rejected, _ := tc.Payload["rejected_variables"].([]string)
	if len(rejected) != 2 {
		t.Errorf("rejected_variables = %v, want las 2 claves inventadas", tc.Payload["rejected_variables"])
	}
}

// TestGuardianCatalogRecoversAfterFailure: el catálogo de productos/reglas no
// puede cachear su propio fallo (con sync.Once el prompt se quedaba sin
// productos hasta reiniciar el proceso).
func TestGuardianCatalogRecoversAfterFailure(t *testing.T) {
	base := guardianAPI(t)
	defer base.Close()
	down := true
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/products", func(w http.ResponseWriter, r *http.Request) {
		if down {
			http.Error(w, "upstream down", 500)
			return
		}
		http.Redirect(w, r, base.URL+r.URL.RequestURI(), 307)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base.URL+r.URL.RequestURI(), 307)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	llm := &scriptedLLM{}
	g, _ := newGuardianForTest(t, srv.URL, llm)
	ctx := context.Background()
	if _, err := g.HandleInbound(ctx, "+573000000008", "hola"); err != nil {
		t.Fatal(err)
	}
	if len(llm.prompts) > 0 && strings.Contains(llm.prompts[0], "Catálogo REAL") {
		t.Error("con /products caído el prompt no debería traer catálogo")
	}

	down = false
	if _, err := g.HandleInbound(ctx, "+573000000008", "y qué me ofreces?"); err != nil {
		t.Fatal(err)
	}
	if len(llm.prompts) < 2 || !strings.Contains(llm.prompts[1], "Catálogo REAL") {
		t.Error("el catálogo no se recuperó tras volver la API")
	}
}

// TestGuardianSweepReleasesExpiredConversations: pasada la ventana de 24h la
// conversación abandonada se libera de memoria (antes vivía hasta el reinicio).
func TestGuardianSweepReleasesExpiredConversations(t *testing.T) {
	srv := guardianAPI(t)
	defer srv.Close()
	g, _ := newGuardianForTest(t, srv.URL, &scriptedLLM{})

	fake := time.Now()
	g.sessions.now = func() time.Time { return fake }
	if _, err := g.HandleInbound(context.Background(), "+573000000009", "hola"); err != nil {
		t.Fatal(err)
	}
	if n := g.Sweep(); n != 0 {
		t.Errorf("una sesión viva no debe barrerse (barridas %d)", n)
	}

	fake = fake.Add(waWindow + time.Minute)
	if n := g.Sweep(); n != 1 {
		t.Errorf("conversaciones liberadas = %d, want 1", n)
	}
	g.mu.Lock()
	live := len(g.convs)
	g.mu.Unlock()
	if live != 0 {
		t.Errorf("quedan %d conversaciones en memoria", live)
	}
	if len(g.sessions.List()) != 0 {
		t.Error("la sesión caducada sigue listada")
	}
}

// recProxy delega en base salvo el POST de recomendaciones, que falla mientras
// `fails` sea > 0 (se decrementa en cada intento).
func recProxy(base *httptest.Server, fails *int) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/recommendations/", func(w http.ResponseWriter, r *http.Request) {
		if *fails > 0 {
			*fails--
			http.Error(w, "upstream down", 500)
			return
		}
		http.Redirect(w, r, base.URL+r.URL.RequestURI(), 307)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base.URL+r.URL.RequestURI(), 307)
	})
	return httptest.NewServer(mux)
}

// matchingLLM lleva al lead hasta PROJECT_MATCHING en dos turnos y después se
// queda charlando (ask), que es cuando el motor debe reintentar la recomendación.
func matchingLLM() *scriptedLLM {
	return &scriptedLLM{decisions: []GuardianDecision{
		{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAsk, AssistantMessage: "hola",
			Entities: []GuardianEntity{
				{Key: "full_name", Value: "Ana", Confidence: 0.95},
				{Key: "has_pet", Value: true, Confidence: 0.9},
			}},
		{Intent: "provide_info", Confidence: 0.9, NextAction: ActionAsk, AssistantMessage: "gracias",
			Entities: []GuardianEntity{{Key: "monthly_income", Value: 3000000.0, Confidence: 0.9}}},
	}}
}

// TestGuardianMatchingRetriesRecommendation: si el motor de recomendaciones
// falla al entrar a MATCHING, el turno siguiente lo reintenta (antes la
// conversación quedaba viva pero muda en MATCHING para siempre).
func TestGuardianMatchingRetriesRecommendation(t *testing.T) {
	base := guardianAPI(t)
	defer base.Close()
	fails := 1 // solo el primer intento falla
	srv := recProxy(base, &fails)
	defer srv.Close()

	g, cap := newGuardianForTest(t, srv.URL, matchingLLM())
	ctx := context.Background()
	for _, msg := range []string{"soy Ana y tengo un perro", "gano 3 millones", "¿y entonces?"} {
		if _, err := g.HandleInbound(ctx, "+573000000005", msg); err != nil {
			t.Fatal(err)
		}
	}
	if n := cap.count(RECOMMENDATION_GENERATED); n != 1 {
		t.Errorf("RECOMMENDATION_GENERATED = %d, want 1 (reintento exitoso)", n)
	}
	if cap.count(CALL_ENDED) != 0 {
		t.Errorf("la conversación no debía cerrarse tras un fallo recuperable")
	}
}

// TestGuardianMatchingDerivesAfterPersistentFailure: si el motor sigue caído,
// el lead se deriva a un asesor en vez de quedarse atrapado en MATCHING.
func TestGuardianMatchingDerivesAfterPersistentFailure(t *testing.T) {
	base := guardianAPI(t)
	defer base.Close()
	fails := 99 // siempre caído
	srv := recProxy(base, &fails)
	defer srv.Close()

	g, cap := newGuardianForTest(t, srv.URL, matchingLLM())
	ctx := context.Background()
	var convID string
	for i, msg := range []string{"soy Ana y tengo un perro", "gano 3 millones", "¿y entonces?", "sigo esperando"} {
		id, err := g.HandleInbound(ctx, "+573000000006", msg)
		if err != nil {
			t.Fatalf("turno %d: %v", i, err)
		}
		convID = id
	}
	if cap.count(CALL_ENDED) != 1 {
		t.Fatalf("CALL_ENDED = %d, want 1 (lead derivado tras %d intentos)", cap.count(CALL_ENDED), maxRecAttempts)
	}
	var toNurturing bool
	cap.mu.Lock()
	for _, ev := range cap.events {
		if ev.Type == STATE_CHANGED && ev.Payload["to"] == string(StateNurturing) {
			toNurturing = true
		}
	}
	cap.mu.Unlock()
	if !toNurturing {
		t.Error("el lead no pasó a NURTURING con el motor caído")
	}
	g.mu.Lock()
	_, alive := g.convs[convID]
	g.mu.Unlock()
	if alive {
		t.Error("la conversación quedó viva tras derivarse")
	}
}

// TestGuardianQuestionsDownDoesNotSkipDiscovery: si GET /questions falla, el
// motor NO puede declarar "perfil completo" (era un salto de etapa con cero
// variables descubiertas) y debe recuperar el catálogo en el turno siguiente.
func TestGuardianQuestionsDownDoesNotSkipDiscovery(t *testing.T) {
	base := guardianAPI(t)
	defer base.Close()

	var down = true
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/questions", func(w http.ResponseWriter, r *http.Request) {
		if down {
			http.Error(w, "upstream down", 500)
			return
		}
		http.Redirect(w, r, base.URL+r.URL.RequestURI(), 307)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base.URL+r.URL.RequestURI(), 307)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	llm := &slowLLM{decision: GuardianDecision{Intent: "provide_info", Confidence: 0.9,
		NextAction: ActionAsk, AssistantMessage: "ok"}}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	ctx := context.Background()

	convID, err := g.HandleInbound(ctx, "+573000000002", "hola")
	if err != nil {
		t.Fatalf("inbound: %v", err)
	}
	for _, ev := range cap.events {
		if ev.Type == STATE_CHANGED && ev.Payload["to"] == string(StateFinancial) {
			t.Fatalf("saltó a FINANCIAL_QUALIFICATION sin catálogo de preguntas: %v", ev.Payload["reason"])
		}
	}
	if cap.count(ERROR_OCCURRED) == 0 {
		t.Errorf("no se registró el fallo del catálogo de preguntas")
	}

	// la API vuelve: el turno siguiente recupera el catálogo
	down = false
	if _, err := g.HandleInbound(ctx, "+573000000002", "me llamo Ana"); err != nil {
		t.Fatalf("segundo inbound: %v", err)
	}
	g.mu.Lock()
	st := g.convs[convID]
	g.mu.Unlock()
	if st == nil || len(st.questions) == 0 {
		t.Fatalf("el catálogo no se recuperó tras volver la API")
	}
	if st.state != StateProfile {
		t.Errorf("estado = %s, want PROFILE_DISCOVERY (aún falta descubrir)", st.state)
	}
}
