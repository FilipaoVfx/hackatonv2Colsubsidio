package main

import (
	"context"
	"strings"
	"testing"
)

// Fase 3 del Agent Studio: el Playground prueba el borrador sin tocar nada de
// producción. Estos tests son el criterio de aceptación de la fase — el
// aislamiento se comprueba, no se confía.

// total: cuántos eventos vio el bus en total (el discriminante más duro para
// "aquí no llegó nada", porque no depende de qué tipo de evento sea).
func (c *capture) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

// prodStack levanta producción cableada como main.go: persistencia y consumidor
// de entrega de WhatsApp suscritos al bus principal.
type prodStack struct {
	bus       *EventBus
	store     *EventStore
	cap       *capture
	sessions  *WhatsAppSessions
	engine    *GuardianEngine
	delivered *int
}

func newProdStack(t *testing.T, apiURL string) *prodStack {
	t.Helper()
	bus := NewEventBus()
	store := NewEventStore()
	cap := &capture{}
	sessions := NewWhatsAppSessions()
	delivered := 0

	bus.Subscribe("*", store.Append)
	bus.Subscribe("*", cap.on)
	// Análogo del consumidor real: en producción esto llama a Kapso.
	bus.Subscribe(MESSAGE_SENT, func(ev Event) {
		if sessions.PhoneFor(ev.CallID) != "" {
			delivered++
		}
	})

	api := newColsubsidioClientAt(apiURL, "")
	engine := NewGuardianEngine(bus, api, &scriptedLLM{}, NewTools(api, bus), sessions, &RAG{}, nil)
	return &prodStack{bus: bus, store: store, cap: cap, sessions: sessions, engine: engine, delivered: &delivered}
}

// TestPlaygroundIsolation: un turno del Playground no se entrega por WhatsApp,
// no se persiste, no aparece en /api/calls y no altera ni las sesiones vivas ni
// la configuración con la que corre el agente real.
func TestPlaygroundIsolation(t *testing.T) {
	prodAPI := guardianAPI(t)
	defer prodAPI.Close()
	studioAPI := guardianAPI(t)
	defer studioAPI.Close()

	ctx := context.Background()
	prod := newProdStack(t, prodAPI.URL)

	// La configuración viva del agente real, que nadie debe mover.
	live := DefaultConfig()
	live.Version = 3
	prod.engine.SetConfig(live)

	prodConv, err := prod.engine.HandleInbound(ctx, "+573001112233", "hola")
	if err != nil {
		t.Fatal(err)
	}
	var (
		callsBefore     = len(prod.store.Calls())
		eventsBefore    = prod.cap.total()
		persistedBefore = len(prod.store.Get(prodConv))
		deliveredBefore = *prod.delivered
		sessionsBefore  = len(prod.sessions.List())
	)
	if deliveredBefore == 0 {
		t.Fatal("el montaje de producción no está entregando: el test no probaría nada")
	}

	// --- Playground, con un borrador distinguible ---
	cfgStore := NewConfigStore(t.TempDir(), nil)
	draft := DefaultConfig()
	draft.Persona.AgentName = "Ensayo"
	draft.Persona.Length = "breve"
	if _, errs, err := cfgStore.SaveDraft(draft); err != nil || len(errs) > 0 {
		t.Fatalf("no se pudo guardar el borrador: %v %v", errs, err)
	}

	llm := &scriptedLLM{}
	pg := NewPlayground(cfgStore, &RAG{}, llm, studioAPI.URL, "")
	if !pg.Enabled() {
		t.Fatal("el playground debería estar activo con API y LLM")
	}

	sess := pg.Start()
	if !strings.HasPrefix(sess.SessionID, playgroundPhonePrefix) {
		t.Errorf("la sesión de prueba usa un teléfono no sintético: %s", sess.SessionID)
	}
	turn, err := pg.Message(ctx, sess.SessionID, "hola, ¿cuánto cuesta?")
	if err != nil {
		t.Fatal(err)
	}

	// El Playground SÍ hizo su trabajo.
	if turn.Reply == "" {
		t.Error("el playground no produjo respuesta")
	}
	if len(turn.Events) == 0 {
		t.Error("el playground no produjo eventos propios")
	}
	if turn.Session.Turns != 1 || turn.Session.TurnsLeft != maxPlaygroundTurns-1 {
		t.Errorf("contador de turnos = %d/%d", turn.Session.Turns, turn.Session.TurnsLeft)
	}

	// Y producción no se enteró de nada.
	if got := *prod.delivered; got != deliveredBefore {
		t.Errorf("un turno del Playground pasó por el consumidor de entrega de WhatsApp (%d -> %d)", deliveredBefore, got)
	}
	if got := prod.cap.total(); got != eventsBefore {
		t.Errorf("el bus de producción recibió %d eventos del Playground", got-eventsBefore)
	}
	if got := len(prod.store.Get(prodConv)); got != persistedBefore {
		t.Errorf("la persistencia creció con el Playground (%d -> %d)", persistedBefore, got)
	}
	if got := len(prod.store.Calls()); got != callsBefore {
		t.Errorf("/api/calls creció con el Playground (%d -> %d)", callsBefore, got)
	}
	if got := len(prod.sessions.List()); got != sessionsBefore {
		t.Errorf("las sesiones vivas cambiaron (%d -> %d)", sessionsBefore, got)
	}
	if got := prod.engine.Config(); got.Version != 3 || got.Persona.AgentName != DefaultConfig().Persona.AgentName {
		t.Errorf("el Playground cambió la configuración del agente real: %+v", got.Persona)
	}

	// Y probó el BORRADOR, que es su razón de existir.
	if len(llm.prompts) == 0 {
		t.Fatal("el playground no llamó al modelo")
	}
	if !strings.Contains(llm.prompts[0], `Eres "Ensayo"`) {
		t.Error("el playground no corrió con la configuración en borrador")
	}
	if turn.Version != draft.Version {
		t.Errorf("config_version del turno = %d, want %d", turn.Version, draft.Version)
	}
}

// TestPlaygroundTurnLimit: una pestaña olvidada no puede quemar tokens sin fin.
func TestPlaygroundTurnLimit(t *testing.T) {
	srv := guardianAPI(t)
	defer srv.Close()
	ctx := context.Background()

	pg := NewPlayground(NewConfigStore(t.TempDir(), nil), &RAG{}, &scriptedLLM{}, srv.URL, "")
	sess := pg.Start()
	for i := 0; i < maxPlaygroundTurns; i++ {
		if _, err := pg.Message(ctx, sess.SessionID, "otra pregunta"); err != nil {
			t.Fatalf("turno %d falló: %v", i+1, err)
		}
	}
	if _, err := pg.Message(ctx, sess.SessionID, "una más"); err != errPlaygroundLimit {
		t.Errorf("pasado el tope se esperaba errPlaygroundLimit, hubo %v", err)
	}

	// Reiniciar libera la sesión: ni turnos, ni conversación, ni eventos.
	pg.Reset(sess.SessionID)
	if _, ok := pg.Session(sess.SessionID); ok {
		t.Error("la sesión sigue viva tras el reset")
	}
	pg.mu.Lock()
	leftovers := len(pg.byConv)
	pg.mu.Unlock()
	if leftovers != 0 {
		t.Errorf("quedaron %d conversaciones de prueba en memoria tras el reset", leftovers)
	}
	if _, err := pg.Message(ctx, sess.SessionID, "hola"); err == nil {
		t.Error("una sesión reiniciada no debería aceptar mensajes")
	}
}

// TestPlaygroundDisabledWithoutAPI: sin API de pruebas la consola sigue entera,
// solo que el Playground se declara apagado (nunca cae al de producción).
func TestPlaygroundDisabledWithoutAPI(t *testing.T) {
	pg := NewPlayground(NewConfigStore(t.TempDir(), nil), &RAG{}, &scriptedLLM{}, "", "")
	if pg.Enabled() {
		t.Error("sin STUDIO_API_URL el playground debe estar apagado")
	}
	var none *Playground
	if none.Enabled() || none.APIBase() != "" || none.Sweep() != 0 {
		t.Error("un Playground nil debe ser inocuo")
	}
}
