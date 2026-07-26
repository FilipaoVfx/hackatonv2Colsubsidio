// Package prov enforces the honesty contract from PRODUCT.md: no fabricated
// business figure may reach the screen unlabeled. Every displayed metric is a
// prov.Value[T], never a bare number.
package prov

// Provenance classifies where a value came from.
type Provenance int

const (
	Measured  Provenance = iota // read directly off a live endpoint / measured wall clock
	Derived                     // computed from measured data (e.g. conversion rate)
	Simulated                   // fixture / replay / no backing endpoint exists
)

func (p Provenance) Badge() string {
	switch p {
	case Measured:
		return "◆"
	case Derived:
		return "◈"
	case Simulated:
		return "◇"
	default:
		return "?"
	}
}

func (p Provenance) Label() string {
	switch p {
	case Measured:
		return "medido"
	case Derived:
		return "derivado"
	case Simulated:
		return "simulado"
	default:
		return "desconocido"
	}
}

// Value wraps any displayed metric with its provenance and an optional note
// (e.g. the endpoint it came from, or why it's simulated).
type Value[T any] struct {
	V    T
	P    Provenance
	Note string
}

func Measure[T any](v T, note string) Value[T]  { return Value[T]{V: v, P: Measured, Note: note} }
func Derive[T any](v T, note string) Value[T]   { return Value[T]{V: v, P: Derived, Note: note} }
func Simulate[T any](v T, note string) Value[T] { return Value[T]{V: v, P: Simulated, Note: note} }

// Entry is one row in the provenance registry, backing both `secura
// provenance` and PROVENANCE.md so the two can never drift.
type Entry struct {
	Metric string
	Source string
	P      Provenance
}

type Registry struct {
	entries []Entry
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Add(metric, source string, p Provenance) {
	r.entries = append(r.entries, Entry{Metric: metric, Source: source, P: p})
}

func (r *Registry) Entries() []Entry { return r.entries }

// DefaultRegistry documents every metric the TUI can show. Kept as data, not
// scattered comments, so `secura provenance` and PROVENANCE.md render from
// the same source.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Add("Estado API / latencia", "GET /api/health", Measured)
	r.Add("Capabilities (7 flags)", "GET /api/capabilities", Measured)
	r.Add("Tokens in/out, costo USD, tool calls, latencia LLM", "GET /api/analytics/kpis", Measured)
	r.Add("Leads ready / whatsapp / nurturing", "GET /api/analytics/kpis", Measured)
	r.Add("Lista de llamadas", "GET /api/calls", Measured)
	r.Add("Detalle de llamada (fases, scores, insights)", "GET /api/analytics/calls/:id", Measured)
	r.Add("Eventos de una llamada (replay)", "GET /api/calls/:id/events", Measured)
	r.Add("Stream en vivo (Pipeline, Calls, tail)", "WS /ws", Measured)
	r.Add("Tasa de conversión (leads_ready/total)", "derivado de KPIs", Derived)
	r.Add("Conocimiento (RAG): corpus y consultas", "KNOWLEDGE_RETRIEVED events", Measured)
	r.Add("Prompt / versiones / publish / rollback", "/api/studio/*", Measured)
	r.Add("Playground", "/api/studio/playground/* + /ws/studio", Measured)
	r.Add("Modo offline / replay", "fixture capturado de una llamada real", Simulated)
	return r
}
