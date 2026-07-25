package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"
)

// Playground del Agent Studio (plan 10_PLAN_AGENT_STUDIO.md §7).
//
// Requisito duro: probar un borrador JAMÁS puede enviar un WhatsApp real,
// contaminar el pipeline ni interferir con una conversación viva. El
// aislamiento es por CONSTRUCCIÓN, no por una bandera que alguien pueda
// olvidar:
//
//	Recurso      Producción                          Playground
//	bus          persistencia + hub + entrega Kapso  bus propio, sin esos suscriptores
//	sesiones     sessions                            sesiones propias
//	motor        guardian                            instancia aparte (estado propio)
//	API Protege  COLSUBSIDIO_API_URL                 STUDIO_API_URL (por defecto, el mock)
//	config       la publicada                        el BORRADOR
//
// El consumidor que entrega por Kapso está suscrito al bus principal, así que
// un MESSAGE_SENT del Playground no tiene camino físico hacia WhatsApp. La
// persistencia y el proyector de analíticas también viven en el bus principal,
// así que /api/calls y el Pipeline no ven nada de aquí. Está probado en
// TestPlaygroundIsolation, no confiado.
//
// La única superficie compartida que queda es la API Protege: el Playground
// escribe usuarios y variables en la API a la que apunte. Por eso su valor por
// defecto es el mock y la consola muestra siempre contra cuál está corriendo.

const (
	// maxPlaygroundTurns: tope de turnos por sesión de prueba. Una pestaña
	// olvidada no puede quemar tokens indefinidamente.
	maxPlaygroundTurns = 20

	// playgroundIdle: una sesión de consola no es una conversación de WhatsApp;
	// se libera mucho antes que la ventana de 24h.
	playgroundIdle = 1 * time.Hour

	// maxPlaygroundEvents: cota de memoria por conversación de prueba.
	maxPlaygroundEvents = 600

	// playgroundPhonePrefix: prefijo sintético. +999 no es un indicativo de país
	// asignado, así que ninguna sesión de prueba puede colisionar con un cliente
	// real ni con una sesión de producción.
	playgroundPhonePrefix = "+999"
)

// studioAPIBase resuelve contra qué API Protege corre el Playground. Por
// defecto, el mock del stack: probar una configuración no debe crear usuarios
// ni escribir variables en la API que atiende a clientes.
func studioAPIBase() string {
	if u := os.Getenv("STUDIO_API_URL"); u != "" {
		return u
	}
	return "http://mock-protege:9000"
}

// Playground es el entorno de pruebas aislado. Todo lo que contiene es suyo.
type Playground struct {
	bus      *EventBus
	hub      *Hub
	sessions *WhatsAppSessions
	engine   *GuardianEngine
	store    *ConfigStore
	api      *ColsubsidioClient

	mu       sync.Mutex
	sessByID map[string]*playSession // session id (teléfono sintético) -> sesión
	byConv   map[string][]Event      // conversación -> eventos del bus del Playground
}

type playSession struct {
	id       string // teléfono sintético; handle estable para el frontend
	convID   string // se conoce tras el primer turno (lo asigna la API)
	turns    int
	lastSeen time.Time
}

// NewPlayground monta el entorno aislado. rag se comparte con producción a
// propósito: es un índice de documentación de solo lectura, sin estado por
// conversación, y el Playground debe recuperar exactamente los mismos
// fragmentos que verá el cliente.
func NewPlayground(store *ConfigStore, rag *RAG, llm GuardianLLM, apiBase, apiToken string) *Playground {
	bus := NewEventBus()
	hub := NewHub()
	api := newColsubsidioClientAt(apiBase, apiToken)
	sessions := NewWhatsAppSessions()

	p := &Playground{
		bus: bus, hub: hub, sessions: sessions, store: store, api: api,
		engine:   NewGuardianEngine(bus, api, llm, NewTools(api, bus), sessions, rag, nil),
		sessByID: make(map[string]*playSession),
		byConv:   make(map[string][]Event),
	}
	// Los ÚNICOS suscriptores del bus del Playground: el búfer que alimenta la
	// respuesta HTTP y el hub de /ws/studio. Ni persistencia, ni proyector, ni
	// entrega por Kapso.
	bus.Subscribe("*", p.record)
	bus.Subscribe("*", hub.Broadcast)
	return p
}

func (p *Playground) Enabled() bool { return p != nil && p.store != nil && p.engine.Enabled() }

// APIBase declara contra qué API está escribiendo el Playground.
func (p *Playground) APIBase() string {
	if p == nil {
		return ""
	}
	return p.api.Base()
}

// Hub expone el hub propio para montar /ws/studio.
func (p *Playground) Hub() *Hub { return p.hub }

// record guarda cada evento del bus aislado. Como el bus es exclusivo del
// Playground, todo lo que pasa por aquí pertenece a una conversación de prueba.
func (p *Playground) record(ev Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	evs := append(p.byConv[ev.CallID], ev)
	if len(evs) > maxPlaygroundEvents {
		evs = evs[len(evs)-maxPlaygroundEvents:]
	}
	p.byConv[ev.CallID] = evs
}

// PlaygroundSession es el estado de una sesión de prueba tal como lo ve la UI.
type PlaygroundSession struct {
	SessionID string  `json:"session_id"`
	ConvID    string  `json:"conversation_id"`
	Turns     int     `json:"turns"`
	TurnsLeft int     `json:"turns_left"`
	State     string  `json:"state"`
	CostUSD   float64 `json:"cost_usd"`
	API       string  `json:"api"`
}

// Start abre una sesión de prueba. NO abre todavía la conversación en la API:
// el primer mensaje la abre por el mismo camino que un cliente que escribe
// primero, así la primera respuesta ya sale de la configuración en borrador
// (el saludo de salida es una plantilla fija y no probaría nada).
func (p *Playground) Start() PlaygroundSession {
	id := p.newSessionID()
	p.mu.Lock()
	p.sessByID[id] = &playSession{id: id, lastSeen: time.Now()}
	p.mu.Unlock()
	return PlaygroundSession{
		SessionID: id, Turns: 0, TurnsLeft: maxPlaygroundTurns, API: p.APIBase(),
	}
}

func (p *Playground) newSessionID() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000_000))
	if err != nil {
		n = big.NewInt(time.Now().UnixNano() % 1_000_000_000)
	}
	return fmt.Sprintf("%s%09d", playgroundPhonePrefix, n.Int64())
}

// PlaygroundTurn es el resultado de un turno de prueba: lo que respondería el
// agente y lo que costó decirlo.
type PlaygroundTurn struct {
	Session   PlaygroundSession `json:"session"`
	Reply     string            `json:"reply"`
	Buttons   []string          `json:"buttons,omitempty"`
	Events    []Event           `json:"events"`
	CostUSD   float64           `json:"turn_cost_usd"`
	LatencyMS int               `json:"latency_ms"`
	Version   int               `json:"config_version"`
}

// errPlaygroundLimit se traduce a 429 en la capa HTTP.
var errPlaygroundLimit = fmt.Errorf("la sesión de prueba llegó a su tope de %d turnos; reiníciala para seguir", maxPlaygroundTurns)

// Message corre un turno con el BORRADOR. La configuración se aplica al motor
// aislado justo antes del turno; el motor de producción no se toca nunca.
func (p *Playground) Message(ctx context.Context, sessionID, text string) (*PlaygroundTurn, error) {
	p.mu.Lock()
	sess := p.sessByID[sessionID]
	if sess == nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("sesión de prueba desconocida")
	}
	if sess.turns >= maxPlaygroundTurns {
		p.mu.Unlock()
		return nil, errPlaygroundLimit
	}
	// Eventos ya registrados de esta conversación: lo que venga después es de
	// este turno. En el primer turno la conversación aún no existe y el corte
	// es 0, que es justo lo que se quiere (la apertura forma parte del turno 1).
	before := len(p.byConv[sess.convID])
	if sess.convID == "" {
		before = 0
	}
	p.mu.Unlock()

	// El Playground prueba el BORRADOR: esa es toda su razón de existir.
	draft := p.store.Draft()
	p.engine.SetConfig(draft)

	convID, err := p.engine.HandleInbound(ctx, sessionID, text)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	sess.convID = convID
	sess.turns++
	sess.lastSeen = time.Now()
	all := p.byConv[convID]
	if before > len(all) {
		before = len(all) // el búfer se recortó por la cota: se devuelve lo que hay
	}
	turnEvents := append([]Event{}, all[before:]...)
	snapshot := p.sessionLocked(sess, all)
	p.mu.Unlock()

	out := &PlaygroundTurn{Session: snapshot, Events: turnEvents, Version: draft.Version}
	for _, ev := range turnEvents {
		switch ev.Type {
		case MESSAGE_SENT:
			if s, ok := ev.Payload["text"].(string); ok {
				out.Reply = s
			}
			out.Buttons = nil
			if b, ok := ev.Payload["buttons"].([]string); ok {
				out.Buttons = b
			}
		case LLM_RESPONSE:
			if c, ok := toFloat(ev.Payload["cost_usd"]); ok {
				out.CostUSD += c
			}
			if l, ok := toFloat(ev.Payload["latency_ms"]); ok {
				out.LatencyMS = int(l)
			}
		}
	}
	return out, nil
}

// Session devuelve el estado actual de una sesión de prueba.
func (p *Playground) Session(sessionID string) (PlaygroundSession, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sess := p.sessByID[sessionID]
	if sess == nil {
		return PlaygroundSession{}, false
	}
	return p.sessionLocked(sess, p.byConv[sess.convID]), true
}

// sessionLocked arma la vista de una sesión. El coste acumulado se recalcula
// desde los eventos: es el mismo dato que vería el pipeline, no un contador
// paralelo que pueda desviarse.
func (p *Playground) sessionLocked(sess *playSession, events []Event) PlaygroundSession {
	cost := 0.0
	for _, ev := range events {
		if ev.Type == LLM_RESPONSE {
			if c, ok := toFloat(ev.Payload["cost_usd"]); ok {
				cost += c
			}
		}
	}
	return PlaygroundSession{
		SessionID: sess.id,
		ConvID:    sess.convID,
		Turns:     sess.turns,
		TurnsLeft: maxPlaygroundTurns - sess.turns,
		State:     string(p.engine.State(sess.convID)),
		CostUSD:   cost,
		API:       p.api.Base(),
	}
}

// Reset cierra la sesión de prueba y libera su estado (motor, sesión y búfer de
// eventos). La siguiente prueba empieza de cero.
func (p *Playground) Reset(sessionID string) {
	p.mu.Lock()
	sess := p.sessByID[sessionID]
	delete(p.sessByID, sessionID)
	var convID string
	if sess != nil {
		convID = sess.convID
		delete(p.byConv, convID)
	}
	p.mu.Unlock()

	if convID != "" {
		p.engine.close(convID)
		p.sessions.Close(convID)
	}
}

// Sweep libera las sesiones de prueba inactivas. Se llama desde el mismo
// ticker que barre las conversaciones de WhatsApp.
func (p *Playground) Sweep() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	var stale []string
	for id, sess := range p.sessByID {
		if time.Since(sess.lastSeen) >= playgroundIdle {
			stale = append(stale, id)
		}
	}
	p.mu.Unlock()

	for _, id := range stale {
		p.Reset(id)
	}
	return len(stale)
}
