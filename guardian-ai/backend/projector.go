package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Projector turns a finished call's event log into the post-call analytics
// record shown by "Pipeline de Llamadas". Derivation is a pure function so it
// can be tested without a database; persistence is a separate step.
//
// It only reads the event log — it never alters the live event path.

// ---------- record types ----------

type PhaseRec struct {
	Idx         int
	Name        string
	StartSec    int
	EndSec      int
	PctOfTotal  int
	Objective   string
	AgentPct    int
	CustomerPct int
	Emotion     string
	EmotionConf float64
	Status      string
	Checklist   []string
	Keywords    []string
	Findings    string
}

type LineRec struct {
	Idx     int
	Speaker string
	Role    string
	Text    string
	AtSec   int
	DurSec  int
}

type ScoreRec struct {
	Dimension string
	Label     string
	Value     int
}

type InsightRec struct {
	Idx      int
	Text     string
	Priority string
	Category string
}

type CallRecord struct {
	CallID       string
	CallCode     string
	Phone        string
	CustomerName string
	Channel      string
	Outcome      string
	AdvisorName  string
	AdvisorVoice string
	StartedAt    time.Time
	DurationSec  int
	ScoreOverall int
	ScoreLabel   string
	Profile      map[string]string
	Phases       []PhaseRec
	Transcript   []LineRec
	Scores       []ScoreRec
	Insights     []InsightRec
}

// ---------- derivation (pure) ----------

var phaseNames = []string{
	"Saludo y Bienvenida",
	"Descubrimiento",
	"Análisis de Riesgos",
	"Presentación de Opciones",
	"Cierre y Siguientes Pasos",
}

var phaseObjectives = []string{
	"Generar confianza y establecer el contexto",
	"Identificar necesidades y construir el perfil",
	"Evaluar exposición y capacidad de pago",
	"Presentar producto ajustado al perfil",
	"Confirmar interés y acordar siguientes pasos",
}

// profileKeys are feature keys that describe the customer (not derived signals).
var profileKeys = map[string]bool{
	"family_status": true, "employment": true, "budget": true, "pets": true,
	"age_band": true, "health": true, "travel_freq": true, "name": true,
}

// Derive builds the analytics record from a call's ordered event log.
// It tolerates partial logs: missing transcript, no recommendation, zero
// duration, out-of-order events and absent CALL_STARTED.
func Derive(events []Event) (*CallRecord, error) {
	if len(events) == 0 {
		return nil, fmt.Errorf("derive: empty event log")
	}

	ordered := make([]Event, len(events))
	copy(ordered, events)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Sequence < ordered[j].Sequence })

	rec := &CallRecord{
		CallID:       ordered[0].CallID,
		AdvisorName:  "Sofía",
		AdvisorVoice: "Voz Natural",
		Channel:      "web",
		Profile:      map[string]string{},
	}
	if rec.CallID == "" {
		return nil, fmt.Errorf("derive: missing call_id")
	}

	// Start time: CALL_STARTED if present, else earliest parsable timestamp.
	var start time.Time
	for _, ev := range ordered {
		ts := parseTS(ev.Timestamp)
		if ts.IsZero() {
			continue
		}
		if start.IsZero() || ts.Before(start) {
			start = ts
		}
		if ev.Type == CALL_STARTED {
			start = ts
			if s, ok := ev.Payload["from"].(string); ok {
				rec.Phone = s
			}
			if s, ok := ev.Payload["channel"].(string); ok && s != "" {
				rec.Channel = s
			}
			break
		}
	}
	if start.IsZero() {
		start = time.Now().UTC()
	}
	rec.StartedAt = start

	// Walk the log.
	var (
		end            time.Time
		lastIntent     string
		hasRecommend   bool
		toolCount      int
		riskAt, recAt  = -1, -1
		acceptAt       = -1
		firstCustAt    = -1
		sentiment      = "Neutral"
		featureCount   int
		durationFromEv = -1
		recProduct     string
		recReason      string
	)

	at := func(ev Event) int {
		ts := parseTS(ev.Timestamp)
		if ts.IsZero() {
			return 0
		}
		d := int(ts.Sub(start).Seconds())
		if d < 0 {
			return 0
		}
		return d
	}

	lineIdx := 0
	for _, ev := range ordered {
		sec := at(ev)
		if ts := parseTS(ev.Timestamp); !ts.IsZero() && ts.After(end) {
			end = ts
		}

		switch ev.Type {
		case TRANSCRIPT_UPDATED:
			role, _ := ev.Payload["role"].(string)
			text, _ := ev.Payload["text"].(string)
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			if role != "agent" && role != "customer" && role != "user" {
				role = "customer"
			}
			if role == "user" {
				role = "customer"
			}
			speaker := rec.AdvisorName + " (IA)"
			if role == "customer" {
				speaker = "Cliente"
				if firstCustAt < 0 {
					firstCustAt = sec
				}
			}
			lineIdx++
			rec.Transcript = append(rec.Transcript, LineRec{
				Idx: lineIdx, Speaker: speaker, Role: role, Text: text, AtSec: sec,
			})

		case FEATURE_UPDATED:
			k, _ := ev.Payload["key"].(string)
			v := fmt.Sprintf("%v", ev.Payload["value"])
			switch k {
			case "risk_level":
				if riskAt < 0 {
					riskAt = sec
				}
			case "sentiment":
				sentiment = normalizeSentiment(v)
			default:
				if profileKeys[k] && v != "" && v != "<nil>" {
					rec.Profile[k] = v
					featureCount++
				}
			}

		case INTENT_DETECTED:
			if s, ok := ev.Payload["intent"].(string); ok && s != "" {
				lastIntent = s
				if s == "acceptance" && acceptAt < 0 {
					acceptAt = sec
				}
			}

		case TOOL_CALLED:
			toolCount++

		case RECOMMENDATION_GENERATED:
			hasRecommend = true
			if recAt < 0 {
				recAt = sec
			}
			if s, ok := ev.Payload["product_name"].(string); ok {
				recProduct = s
			}
			if s, ok := ev.Payload["reasoning"].(string); ok {
				recReason = s
			}

		case CALL_ENDED:
			if ms, ok := toFloat(ev.Payload["duration_ms"]); ok && ms > 0 {
				durationFromEv = int(ms / 1000)
			}
		}
	}

	// Duration: prefer the explicit value, else wall time, never negative.
	rec.DurationSec = durationFromEv
	if rec.DurationSec < 0 {
		if !end.IsZero() {
			rec.DurationSec = int(end.Sub(start).Seconds())
		}
	}
	if rec.DurationSec < 0 {
		rec.DurationSec = 0
	}
	// A transcript line beyond the computed end extends the call.
	for _, l := range rec.Transcript {
		if l.AtSec > rec.DurationSec {
			rec.DurationSec = l.AtSec
		}
	}

	// Per-line duration: until the next line, else remainder.
	for i := range rec.Transcript {
		if i+1 < len(rec.Transcript) {
			d := rec.Transcript[i+1].AtSec - rec.Transcript[i].AtSec
			if d < 0 {
				d = 0
			}
			rec.Transcript[i].DurSec = d
		} else {
			d := rec.DurationSec - rec.Transcript[i].AtSec
			if d < 0 {
				d = 0
			}
			rec.Transcript[i].DurSec = d
		}
	}

	rec.CallCode = "CALL-" + start.Format("2006-01-02-1504")
	rec.CustomerName = deriveName(rec.Profile, rec.Phone)
	rec.Outcome = deriveOutcome(lastIntent, hasRecommend)
	rec.Phases = derivePhases(rec, firstCustAt, riskAt, recAt, acceptAt, sentiment)
	rec.Scores, rec.ScoreOverall, rec.ScoreLabel = deriveScores(rec, featureCount, toolCount, hasRecommend, lastIntent, sentiment)
	rec.Insights = deriveInsights(rec, hasRecommend, recProduct, recReason, lastIntent, sentiment)
	return rec, nil
}

func deriveName(profile map[string]string, phone string) string {
	if n := strings.TrimSpace(profile["name"]); n != "" {
		return n
	}
	p := strings.TrimSpace(phone)
	if p == "" || p == "web-client" || p == "web-mic" {
		return "Cliente Demo"
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, p)
	if len(digits) >= 4 {
		return "Cliente " + digits[len(digits)-4:]
	}
	return "Cliente Demo"
}

func deriveOutcome(intent string, hasRecommend bool) string {
	switch intent {
	case "acceptance":
		return "Cerrado"
	case "price_objection":
		return "Seguimiento"
	case "end_call":
		return "No interesado"
	case "interest", "question":
		if hasRecommend {
			return "Interesado"
		}
		return "Seguimiento"
	}
	if hasRecommend {
		return "Interesado"
	}
	return "Seguimiento"
}

// derivePhases segments the call into the five pipeline phases using real
// markers when present, falling back to proportional boundaries.
func derivePhases(rec *CallRecord, firstCustAt, riskAt, recAt, acceptAt int, sentiment string) []PhaseRec {
	d := rec.DurationSec
	// Proportional fallbacks (same shape as the reference call).
	bounds := []int{
		pctOf(d, 7), pctOf(d, 36), pctOf(d, 68), pctOf(d, 88), d,
	}
	if firstCustAt > 0 {
		bounds[0] = firstCustAt
	}
	if riskAt > 0 {
		bounds[1] = riskAt
	}
	if recAt > 0 {
		bounds[2] = recAt
	}
	if acceptAt > 0 {
		bounds[3] = acceptAt
	}
	// Enforce monotonic, in-range boundaries.
	prev := 0
	for i := range bounds {
		if bounds[i] < prev {
			bounds[i] = prev
		}
		if bounds[i] > d {
			bounds[i] = d
		}
		prev = bounds[i]
	}
	bounds[len(bounds)-1] = d

	phases := make([]PhaseRec, 0, 5)
	startSec := 0
	for i := 0; i < 5; i++ {
		endSec := bounds[i]
		agent, customer := participation(rec.Transcript, startSec, endSec)
		pct := 0
		if d > 0 {
			pct = int(math.Round(float64(endSec-startSec) / float64(d) * 100))
		}
		phases = append(phases, PhaseRec{
			Idx: i + 1, Name: phaseNames[i], StartSec: startSec, EndSec: endSec,
			PctOfTotal: pct, Objective: phaseObjectives[i],
			AgentPct: agent, CustomerPct: customer,
			Emotion: sentiment, EmotionConf: 0.8, Status: "Completada",
			Checklist: phaseChecklist(i, rec),
			Keywords:  phaseKeywords(rec.Transcript, startSec, endSec),
			Findings:  phaseFindings(i, rec),
		})
		startSec = endSec
	}
	return phases
}

// participation splits speaking share by characters spoken in the window.
func participation(lines []LineRec, from, to int) (agent, customer int) {
	var a, c int
	for _, l := range lines {
		if l.AtSec < from || l.AtSec > to {
			continue
		}
		if l.Role == "agent" {
			a += len(l.Text)
		} else {
			c += len(l.Text)
		}
	}
	total := a + c
	if total == 0 {
		return 0, 0
	}
	agent = int(math.Round(float64(a) / float64(total) * 100))
	return agent, 100 - agent
}

var stopWords = map[string]bool{
	"para": true, "pero": true, "como": true, "esta": true, "este": true, "esto": true,
	"que": true, "los": true, "las": true, "una": true, "uno": true, "con": true,
	"por": true, "del": true, "the": true, "and": true, "mas": true, "muy": true,
	"tengo": true, "quiero": true, "puedo": true, "sobre": true, "hola": true,
}

func phaseKeywords(lines []LineRec, from, to int) []string {
	counts := map[string]int{}
	for _, l := range lines {
		if l.AtSec < from || l.AtSec > to {
			continue
		}
		for _, w := range strings.FieldsFunc(strings.ToLower(l.Text), func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || r >= 128)
		}) {
			if len([]rune(w)) < 4 || stopWords[w] {
				continue
			}
			counts[w]++
		}
	}
	type kv struct {
		w string
		n int
	}
	list := make([]kv, 0, len(counts))
	for w, n := range counts {
		list = append(list, kv{w, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].w < list[j].w
	})
	out := []string{}
	for i := 0; i < len(list) && i < 4; i++ {
		out = append(out, list[i].w)
	}
	return out
}

func phaseChecklist(i int, rec *CallRecord) []string {
	switch i {
	case 0:
		return []string{"El asesor IA se presentó y abrió la conversación.", "Se estableció el contexto de la llamada."}
	case 1:
		items := []string{"Se exploraron las necesidades del cliente."}
		if len(rec.Profile) > 0 {
			items = append(items, fmt.Sprintf("Se detectaron %d rasgos de perfil.", len(rec.Profile)))
		}
		return items
	case 2:
		return []string{"Se evaluó el nivel de riesgo del cliente.", "Se consideró la capacidad de pago."}
	case 3:
		return []string{"Se presentó una opción ajustada al perfil."}
	default:
		return []string{"Se cerró la conversación con los siguientes pasos."}
	}
}

func phaseFindings(i int, rec *CallRecord) string {
	switch i {
	case 1:
		if len(rec.Profile) == 0 {
			return "No se capturaron rasgos de perfil en esta fase."
		}
		parts := make([]string, 0, len(rec.Profile))
		for k, v := range rec.Profile {
			parts = append(parts, k+": "+v)
		}
		sort.Strings(parts)
		return "Perfil detectado — " + strings.Join(parts, "; ") + "."
	case 3:
		if len(rec.Insights) > 0 {
			return rec.Insights[0].Text
		}
		return "Presentación de opciones al cliente."
	}
	return "Fase completada."
}

func deriveScores(rec *CallRecord, featureCount, toolCount int, hasRecommend bool, intent, sentiment string) ([]ScoreRec, int, string) {
	clamp := func(v int) int {
		if v < 0 {
			return 0
		}
		if v > 100 {
			return 100
		}
		return v
	}
	discovery := clamp(55 + featureCount*8)
	empathy := clamp(60 + map[string]int{"Positiva": 25, "Neutral": 10, "Negativa": -5}[sentiment])
	clarity := clamp(65 + len(rec.Transcript)*2)
	objections := 60
	if intent == "price_objection" {
		objections = 70
	}
	if hasRecommend {
		objections += 12
	}
	objections = clamp(objections)
	closing := 55
	switch intent {
	case "acceptance":
		closing = 92
	case "interest":
		closing = 78
	case "end_call":
		closing = 45
	}
	if toolCount > 0 {
		discovery = clamp(discovery + 5)
	}
	closing = clamp(closing)

	scores := []ScoreRec{
		{"discovery_quality", "Calidad de Descubrimiento", discovery},
		{"empathy", "Empatía", empathy},
		{"clarity", "Claridad", clarity},
		{"objection_handling", "Manejo de Objeciones", objections},
		{"closing", "Cierre", closing},
	}
	sum := 0
	for _, s := range scores {
		sum += s.Value
	}
	overall := int(math.Round(float64(sum) / float64(len(scores))))
	return scores, overall, scoreLabel(overall)
}

func scoreLabel(v int) string {
	switch {
	case v >= 85:
		return "Excelente"
	case v >= 70:
		return "Bueno"
	case v >= 55:
		return "Regular"
	default:
		return "Bajo"
	}
}

func deriveInsights(rec *CallRecord, hasRecommend bool, product, reason, intent, sentiment string) []InsightRec {
	out := []InsightRec{}
	add := func(text, priority, category string) {
		out = append(out, InsightRec{Idx: len(out) + 1, Text: text, Priority: priority, Category: category})
	}
	if hasRecommend && product != "" {
		r := reason
		if r == "" {
			r = "derivada del perfil detectado"
		}
		add("Producto recomendado: "+product+" — "+r, "Alta", "producto")
	}
	if len(rec.Profile) > 0 {
		parts := make([]string, 0, len(rec.Profile))
		for k, v := range rec.Profile {
			parts = append(parts, k+"="+v)
		}
		sort.Strings(parts)
		add("Perfil construido en vivo: "+strings.Join(parts, ", "), "Media", "perfil")
	}
	switch intent {
	case "acceptance":
		add("Cliente aceptó la propuesta; agendar formalización.", "Alta", "cierre")
	case "price_objection":
		add("Objeción de precio detectada; requiere seguimiento.", "Alta", "objecion")
	case "end_call":
		add("Cliente finalizó sin interés; baja prioridad de recontacto.", "Baja", "seguimiento")
	}
	if sentiment == "Negativa" {
		add("Sentimiento negativo predominante durante la llamada.", "Alta", "riesgo")
	}
	if len(out) == 0 {
		add("Llamada sin señales comerciales relevantes.", "Baja", "general")
	}
	return out
}

// ---------- helpers ----------

func parseTS(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func pctOf(total, pct int) int {
	if total <= 0 {
		return 0
	}
	return int(math.Round(float64(total) * float64(pct) / 100))
}

// normalizeSentiment maps the model's sentiment (Spanish or English) onto the
// Spanish labels used by the Pipeline view.
func normalizeSentiment(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "positive", "positiva", "positivo":
		return "Positiva"
	case "negative", "negativa", "negativo":
		return "Negativa"
	case "neutral", "neutra", "neutro":
		return "Neutral"
	case "":
		return "Neutral"
	}
	return titleCase(v)
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(strings.ToLower(s))
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

// ---------- persistence ----------

type Projector struct {
	store *EventStore
	pool  *pgxpool.Pool
}

func NewProjector(store *EventStore, pool *pgxpool.Pool) *Projector {
	return &Projector{store: store, pool: pool}
}

// OnEvent is subscribed to the bus; it projects a call when it ends.
func (p *Projector) OnEvent(ev Event) {
	if ev.Type != CALL_ENDED || p.pool == nil {
		return
	}
	events := p.store.Get(ev.CallID)
	rec, err := Derive(events)
	if err != nil {
		logProjector("derive failed for %s: %v", ev.CallID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := p.Save(ctx, rec); err != nil {
		logProjector("save failed for %s: %v", ev.CallID, err)
		return
	}
	logProjector("projected call %s (%s, %d lines, score %d)", rec.CallCode, rec.Outcome, len(rec.Transcript), rec.ScoreOverall)
}

// Save upserts the record. Re-projecting the same call replaces its children,
// so the operation is idempotent.
func (p *Projector) Save(ctx context.Context, rec *CallRecord) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Reuse the customer already linked to this call (re-projection), then any
	// customer with the same phone, and only create one as a last resort.
	// Without this a re-projection would orphan the previous customer row.
	customerID, err := reuseCustomer(ctx, tx, rec)
	if err != nil {
		return fmt.Errorf("customer: %w", err)
	}

	_, err = tx.Exec(ctx, `
		insert into public.call_analytics
		  (call_id, call_code, customer_id, advisor_name, advisor_voice, channel, outcome,
		   started_at, duration_sec, score_overall, score_label, recording_url)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,null)
		on conflict (call_id) do update set
		  call_code=excluded.call_code, customer_id=excluded.customer_id,
		  channel=excluded.channel, outcome=excluded.outcome,
		  started_at=excluded.started_at, duration_sec=excluded.duration_sec,
		  score_overall=excluded.score_overall, score_label=excluded.score_label`,
		rec.CallID, rec.CallCode, customerID, rec.AdvisorName, rec.AdvisorVoice,
		rec.Channel, rec.Outcome, rec.StartedAt, rec.DurationSec, rec.ScoreOverall, rec.ScoreLabel)
	if err != nil {
		return fmt.Errorf("call_analytics: %w", err)
	}

	for _, t := range []string{"call_phases", "call_transcript", "call_scores", "call_insights"} {
		if _, err := tx.Exec(ctx, "delete from public."+t+" where call_id=$1", rec.CallID); err != nil {
			return fmt.Errorf("clear %s: %w", t, err)
		}
	}

	for _, ph := range rec.Phases {
		if _, err := tx.Exec(ctx, `
			insert into public.call_phases
			 (call_id, idx, name, start_sec, end_sec, pct_of_total, objective, agent_pct,
			  customer_pct, emotion, emotion_conf, status, checklist, keywords, findings)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			rec.CallID, ph.Idx, ph.Name, ph.StartSec, ph.EndSec, ph.PctOfTotal, ph.Objective,
			ph.AgentPct, ph.CustomerPct, ph.Emotion, ph.EmotionConf, ph.Status,
			jsonArray(ph.Checklist), jsonArray(ph.Keywords), ph.Findings); err != nil {
			return fmt.Errorf("phase %d: %w", ph.Idx, err)
		}
	}
	for _, l := range rec.Transcript {
		if _, err := tx.Exec(ctx, `
			insert into public.call_transcript (call_id, idx, speaker, role, text, at_sec, dur_sec)
			values ($1,$2,$3,$4,$5,$6,$7)`,
			rec.CallID, l.Idx, l.Speaker, l.Role, l.Text, l.AtSec, l.DurSec); err != nil {
			return fmt.Errorf("line %d: %w", l.Idx, err)
		}
	}
	for _, s := range rec.Scores {
		if _, err := tx.Exec(ctx, `
			insert into public.call_scores (call_id, dimension, label, value) values ($1,$2,$3,$4)`,
			rec.CallID, s.Dimension, s.Label, s.Value); err != nil {
			return fmt.Errorf("score %s: %w", s.Dimension, err)
		}
	}
	for _, i := range rec.Insights {
		if _, err := tx.Exec(ctx, `
			insert into public.call_insights (call_id, idx, text, priority, category)
			values ($1,$2,$3,$4,$5)`,
			rec.CallID, i.Idx, i.Text, i.Priority, i.Category); err != nil {
			return fmt.Errorf("insight %d: %w", i.Idx, err)
		}
	}
	return tx.Commit(ctx)
}

// reuseCustomer resolves the customer row for a projection without creating
// duplicates: existing link first, then phone match, then insert.
func reuseCustomer(ctx context.Context, tx pgx.Tx, rec *CallRecord) (string, error) {
	var id string

	err := tx.QueryRow(ctx,
		`select customer_id::text from public.call_analytics where call_id=$1 and customer_id is not null`,
		rec.CallID).Scan(&id)
	if err == nil && id != "" {
		_, err = tx.Exec(ctx, `update public.customers set full_name=$2, phone=coalesce(nullif($3,''),phone) where customer_id=$1`,
			id, rec.CustomerName, rec.Phone)
		return id, err
	}
	if err != nil && err != pgx.ErrNoRows {
		return "", err
	}

	if strings.TrimSpace(rec.Phone) != "" {
		err = tx.QueryRow(ctx,
			`select customer_id::text from public.customers where phone=$1 order by created_at limit 1`,
			rec.Phone).Scan(&id)
		if err == nil && id != "" {
			return id, nil
		}
		if err != nil && err != pgx.ErrNoRows {
			return "", err
		}
	}

	err = tx.QueryRow(ctx, `
		insert into public.customers (full_name, phone, is_new)
		values ($1, nullif($2,''), true)
		returning customer_id::text`, rec.CustomerName, rec.Phone).Scan(&id)
	return id, err
}

func logProjector(format string, args ...interface{}) {
	log.Printf("[projector] "+format, args...)
}

func jsonArray(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, len(items))
	for i, s := range items {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		s = strings.ReplaceAll(s, "\n", `\n`)
		quoted[i] = `"` + s + `"`
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
