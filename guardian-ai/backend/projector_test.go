package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ---------- helpers ----------

const testCall = "11111111-2222-4333-8444-555555555555"

var base = time.Date(2026, 7, 23, 15, 0, 0, 0, time.UTC)

func ev(seq int, offsetSec int, typ string, payload map[string]interface{}) Event {
	return Event{
		EventID:   "e" + string(rune('a'+seq%26)),
		Type:      typ,
		CallID:    testCall,
		Sequence:  seq,
		Timestamp: base.Add(time.Duration(offsetSec) * time.Second).Format(time.RFC3339Nano),
		Producer:  "test",
		Payload:   payload,
	}
}

func p(kv ...interface{}) map[string]interface{} {
	m := map[string]interface{}{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

// fullCall is a realistic happy-path log used by several tests.
func fullCall() []Event {
	return []Event{
		ev(0, 0, CALL_STARTED, p("from", "+57 310 123 4567", "channel", "voice")),
		ev(1, 1, STATE_CHANGED, p("from", "CREATED", "to", "DISCOVERY")),
		ev(2, 2, TRANSCRIPT_UPDATED, p("role", "agent", "text", "Hola, soy Sofía, tu asesora de protección.", "is_final", true)),
		ev(3, 10, TRANSCRIPT_UPDATED, p("role", "user", "text", "Hola, tengo esposa y dos hijos pequeños.", "is_final", true)),
		ev(4, 11, FEATURE_UPDATED, p("key", "family_status", "value", "casado_2_hijos", "source", "transcript")),
		ev(5, 12, FEATURE_UPDATED, p("key", "employment", "value", "independiente", "source", "transcript")),
		ev(6, 13, INTENT_DETECTED, p("intent", "interest", "confidence", 0.9)),
		ev(7, 30, FEATURE_UPDATED, p("key", "risk_level", "value", "medium", "source", "inference")),
		ev(8, 31, FEATURE_UPDATED, p("key", "sentiment", "value", "positiva", "source", "inference")),
		ev(9, 40, TOOL_CALLED, p("tool", "product_search")),
		ev(10, 41, TOOL_EXECUTED, p("tool", "product_search")),
		ev(11, 50, RECOMMENDATION_GENERATED, p("product_id", "COL-VIDA-PLUS-2", "product_name", "Vida Protección Familiar Plus", "reasoning", "familia con hijos e ingreso independiente")),
		ev(12, 52, TRANSCRIPT_UPDATED, p("role", "agent", "text", "Te recomiendo el plan Vida Protección Familiar Plus.", "is_final", true)),
		ev(13, 60, INTENT_DETECTED, p("intent", "acceptance", "confidence", 0.95)),
		ev(14, 62, TRANSCRIPT_UPDATED, p("role", "user", "text", "Me interesa, gracias.", "is_final", true)),
		ev(15, 70, SUMMARY_GENERATED, p("summary", "Cliente interesado")),
		ev(16, 72, CALL_ENDED, p("reason", "hangup", "duration_ms", float64(72000))),
	}
}

// ---------- channel-agnostic analytics ----------

// The projector must derive WhatsApp (text-channel) conversations with no schema
// or code change — the channel is read straight from CALL_STARTED.
func TestDeriveWhatsAppChannel(t *testing.T) {
	log := []Event{
		ev(0, 0, CALL_STARTED, p("from", "+57 310 123 4567", "channel", "whatsapp")),
		ev(1, 1, STATE_CHANGED, p("from", "CREATED", "to", "DISCOVERY")),
		ev(2, 2, TRANSCRIPT_UPDATED, p("role", "agent", "text", "Hola, soy Guardian AI.", "is_final", true)),
		ev(3, 10, TRANSCRIPT_UPDATED, p("role", "user", "text", "Quiero un seguro para mi familia.", "is_final", true)),
		ev(4, 11, FEATURE_UPDATED, p("key", "family_status", "value", "casado_2_hijos", "source", "transcript")),
		ev(5, 20, INTENT_DETECTED, p("intent", "acceptance", "confidence", 0.95)),
		ev(6, 22, TRANSCRIPT_UPDATED, p("role", "agent", "text", "Te recomiendo Vida Protección Familiar Plus.", "is_final", true)),
		ev(7, 30, CALL_ENDED, p("reason", "hangup", "duration_ms", float64(30000))),
	}
	rec, err := Derive(log)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Channel != "whatsapp" {
		t.Errorf("channel = %q, want whatsapp", rec.Channel)
	}
	if rec.DurationSec != 30 {
		t.Errorf("duration = %d, want 30", rec.DurationSec)
	}
	if len(rec.Transcript) != 3 {
		t.Errorf("transcript lines = %d, want 3", len(rec.Transcript))
	}
	if len(rec.Phases) != 5 {
		t.Errorf("phases = %d, want 5", len(rec.Phases))
	}
}

// ---------- core behaviour ----------

func TestDeriveFullCall(t *testing.T) {
	rec, err := Derive(fullCall())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.CallID != testCall {
		t.Errorf("call_id = %q", rec.CallID)
	}
	if rec.CallCode != "CALL-2026-07-23-1500" {
		t.Errorf("call_code = %q", rec.CallCode)
	}
	if rec.Channel != "voice" {
		t.Errorf("channel = %q, want voice", rec.Channel)
	}
	if rec.DurationSec != 72 {
		t.Errorf("duration = %d, want 72", rec.DurationSec)
	}
	if rec.Outcome != "Cerrado" {
		t.Errorf("outcome = %q, want Cerrado (acceptance)", rec.Outcome)
	}
	if rec.CustomerName != "Cliente 4567" {
		t.Errorf("customer = %q", rec.CustomerName)
	}
	if len(rec.Transcript) != 4 {
		t.Fatalf("transcript lines = %d, want 4", len(rec.Transcript))
	}
	if len(rec.Phases) != 5 {
		t.Fatalf("phases = %d, want 5", len(rec.Phases))
	}
	if len(rec.Scores) != 5 {
		t.Errorf("scores = %d, want 5", len(rec.Scores))
	}
	if rec.ScoreOverall < 1 || rec.ScoreOverall > 100 {
		t.Errorf("score_overall out of range: %d", rec.ScoreOverall)
	}
	if rec.ScoreLabel == "" {
		t.Error("empty score label")
	}
	// profile keeps customer traits, excludes derived signals
	if rec.Profile["family_status"] != "casado_2_hijos" {
		t.Errorf("profile family_status = %q", rec.Profile["family_status"])
	}
	if _, ok := rec.Profile["risk_level"]; ok {
		t.Error("risk_level must not be stored as a profile trait")
	}
	if _, ok := rec.Profile["sentiment"]; ok {
		t.Error("sentiment must not be stored as a profile trait")
	}
}

func TestPhasesAreContiguousAndInRange(t *testing.T) {
	for name, events := range map[string][]Event{
		"full":      fullCall(),
		"noTrans":   {ev(0, 0, CALL_STARTED, p("from", "+571", "channel", "web")), ev(1, 30, CALL_ENDED, p("duration_ms", float64(30000)))},
		"zeroDur":   {ev(0, 0, CALL_STARTED, p("from", "+571")), ev(1, 0, CALL_ENDED, p("duration_ms", float64(0)))},
		"noEndEvt":  fullCall()[:12],
		"outOfOrder": func() []Event {
			e := fullCall()
			e[0], e[5] = e[5], e[0] // shuffle; Derive must re-sort by Sequence
			return e
		}(),
	} {
		rec, err := Derive(events)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(rec.Phases) != 5 {
			t.Fatalf("%s: phases = %d", name, len(rec.Phases))
		}
		prevEnd := 0
		sumPct := 0
		for _, ph := range rec.Phases {
			if ph.StartSec != prevEnd {
				t.Errorf("%s: phase %d starts at %d, previous ended %d (gap)", name, ph.Idx, ph.StartSec, prevEnd)
			}
			if ph.EndSec < ph.StartSec {
				t.Errorf("%s: phase %d end %d < start %d", name, ph.Idx, ph.EndSec, ph.StartSec)
			}
			if ph.EndSec > rec.DurationSec {
				t.Errorf("%s: phase %d end %d exceeds duration %d", name, ph.Idx, ph.EndSec, rec.DurationSec)
			}
			if ph.AgentPct < 0 || ph.AgentPct > 100 || ph.CustomerPct < 0 || ph.CustomerPct > 100 {
				t.Errorf("%s: phase %d participation out of range: %d/%d", name, ph.Idx, ph.AgentPct, ph.CustomerPct)
			}
			if ph.AgentPct+ph.CustomerPct != 0 && ph.AgentPct+ph.CustomerPct != 100 {
				t.Errorf("%s: phase %d participation sums %d", name, ph.Idx, ph.AgentPct+ph.CustomerPct)
			}
			prevEnd = ph.EndSec
			sumPct += ph.PctOfTotal
		}
		if last := rec.Phases[4].EndSec; last != rec.DurationSec {
			t.Errorf("%s: last phase ends %d, duration %d", name, last, rec.DurationSec)
		}
		if rec.DurationSec > 0 && (sumPct < 97 || sumPct > 103) {
			t.Errorf("%s: phase percentages sum to %d", name, sumPct)
		}
	}
}

// ---------- edge cases ----------

func TestDeriveEmptyLog(t *testing.T) {
	if _, err := Derive(nil); err == nil {
		t.Error("expected error for empty log")
	}
	if _, err := Derive([]Event{}); err == nil {
		t.Error("expected error for empty slice")
	}
}

func TestDeriveMissingCallID(t *testing.T) {
	e := ev(0, 0, CALL_STARTED, p("from", "+571"))
	e.CallID = ""
	if _, err := Derive([]Event{e}); err == nil {
		t.Error("expected error when call_id is missing")
	}
}

func TestDeriveWithoutCallStarted(t *testing.T) {
	// Partial log (e.g. backend restarted mid-call): must not panic.
	events := []Event{
		ev(3, 5, TRANSCRIPT_UPDATED, p("role", "user", "text", "Hola", "is_final", true)),
		ev(4, 20, CALL_ENDED, p("reason", "hangup")),
	}
	rec, err := Derive(events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.DurationSec < 0 {
		t.Errorf("negative duration: %d", rec.DurationSec)
	}
	if rec.CustomerName == "" {
		t.Error("customer name must have a fallback")
	}
	if len(rec.Phases) != 5 {
		t.Errorf("phases = %d", len(rec.Phases))
	}
}

func TestDeriveNoTranscript(t *testing.T) {
	events := []Event{
		ev(0, 0, CALL_STARTED, p("from", "web-mic", "channel", "webrtc")),
		ev(1, 15, CALL_ENDED, p("duration_ms", float64(15000))),
	}
	rec, err := Derive(events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.Transcript) != 0 {
		t.Errorf("expected no transcript, got %d", len(rec.Transcript))
	}
	if rec.CustomerName != "Cliente Demo" {
		t.Errorf("customer = %q, want Cliente Demo for web-mic", rec.CustomerName)
	}
	if rec.Channel != "webrtc" {
		t.Errorf("channel = %q", rec.Channel)
	}
	for _, ph := range rec.Phases {
		if ph.AgentPct != 0 || ph.CustomerPct != 0 {
			t.Errorf("phase %d: participation should be 0/0 without transcript", ph.Idx)
		}
	}
	if len(rec.Insights) == 0 {
		t.Error("insights must never be empty (fallback expected)")
	}
}

func TestDeriveEmptyAndWhitespaceTranscript(t *testing.T) {
	events := []Event{
		ev(0, 0, CALL_STARTED, p("from", "+573001112233")),
		ev(1, 2, TRANSCRIPT_UPDATED, p("role", "agent", "text", "", "is_final", true)),
		ev(2, 3, TRANSCRIPT_UPDATED, p("role", "user", "text", "   ", "is_final", true)),
		ev(3, 4, TRANSCRIPT_UPDATED, p("role", "agent", "text", "  Hola de nuevo  ", "is_final", true)),
		ev(4, 10, CALL_ENDED, p("duration_ms", float64(10000))),
	}
	rec, _ := Derive(events)
	if len(rec.Transcript) != 1 {
		t.Fatalf("blank lines must be dropped, got %d", len(rec.Transcript))
	}
	if rec.Transcript[0].Text != "Hola de nuevo" {
		t.Errorf("text not trimmed: %q", rec.Transcript[0].Text)
	}
	if rec.Transcript[0].Idx != 1 {
		t.Errorf("indices must be contiguous from 1, got %d", rec.Transcript[0].Idx)
	}
}

func TestTranscriptRoleNormalisationAndIndices(t *testing.T) {
	events := []Event{
		ev(0, 0, CALL_STARTED, p("from", "+571")),
		ev(1, 1, TRANSCRIPT_UPDATED, p("role", "user", "text", "soy user", "is_final", true)),
		ev(2, 2, TRANSCRIPT_UPDATED, p("role", "agent", "text", "soy agente", "is_final", true)),
		ev(3, 3, TRANSCRIPT_UPDATED, p("role", "bogus", "text", "rol raro", "is_final", true)),
		ev(4, 20, CALL_ENDED, p("duration_ms", float64(20000))),
	}
	rec, _ := Derive(events)
	if len(rec.Transcript) != 3 {
		t.Fatalf("lines = %d", len(rec.Transcript))
	}
	want := []string{"customer", "agent", "customer"}
	for i, l := range rec.Transcript {
		if l.Role != want[i] {
			t.Errorf("line %d role = %q, want %q", i, l.Role, want[i])
		}
		if l.Idx != i+1 {
			t.Errorf("line %d idx = %d", i, l.Idx)
		}
		if l.DurSec < 0 {
			t.Errorf("line %d negative dur %d", i, l.DurSec)
		}
	}
	// last line runs to the end of the call
	last := rec.Transcript[len(rec.Transcript)-1]
	if last.AtSec+last.DurSec != rec.DurationSec {
		t.Errorf("last line ends at %d, duration %d", last.AtSec+last.DurSec, rec.DurationSec)
	}
}

func TestTranscriptBeyondReportedDurationExtendsCall(t *testing.T) {
	events := []Event{
		ev(0, 0, CALL_STARTED, p("from", "+571")),
		ev(1, 90, TRANSCRIPT_UPDATED, p("role", "agent", "text", "tarde", "is_final", true)),
		ev(2, 95, CALL_ENDED, p("duration_ms", float64(10000))), // claims 10s
	}
	rec, _ := Derive(events)
	if rec.DurationSec < 90 {
		t.Errorf("duration %d must cover the last transcript line at 90s", rec.DurationSec)
	}
	if rec.Phases[4].EndSec != rec.DurationSec {
		t.Errorf("phases must span the extended duration")
	}
}

func TestOutcomeMapping(t *testing.T) {
	cases := []struct {
		intent      string
		hasRecommend bool
		want        string
	}{
		{"acceptance", true, "Cerrado"},
		{"acceptance", false, "Cerrado"},
		{"price_objection", true, "Seguimiento"},
		{"end_call", false, "No interesado"},
		{"interest", true, "Interesado"},
		{"interest", false, "Seguimiento"},
		{"", true, "Interesado"},
		{"", false, "Seguimiento"},
		{"unknown_intent", false, "Seguimiento"},
	}
	for _, c := range cases {
		if got := deriveOutcome(c.intent, c.hasRecommend); got != c.want {
			t.Errorf("deriveOutcome(%q, %v) = %q, want %q", c.intent, c.hasRecommend, got, c.want)
		}
	}
}

func TestDuplicateCallEndedIsStable(t *testing.T) {
	events := append(fullCall(), ev(17, 73, CALL_ENDED, p("reason", "hangup", "duration_ms", float64(72000))))
	a, _ := Derive(fullCall())
	b, err := Derive(events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.DurationSec != b.DurationSec || a.Outcome != b.Outcome || len(a.Transcript) != len(b.Transcript) {
		t.Error("a duplicate CALL_ENDED changed the derived record")
	}
}

func TestUnicodeAndQuotesSurviveJSONEncoding(t *testing.T) {
	events := []Event{
		ev(0, 0, CALL_STARTED, p("from", "+571")),
		ev(1, 2, TRANSCRIPT_UPDATED, p("role", "user", "text", `Dijo "hola" — año, mañana, 100% seguro`, "is_final", true)),
		ev(2, 30, CALL_ENDED, p("duration_ms", float64(30000))),
	}
	rec, _ := Derive(events)
	if len(rec.Transcript) != 1 {
		t.Fatal("expected one line")
	}
	if !strings.Contains(rec.Transcript[0].Text, `"hola"`) {
		t.Errorf("quotes lost: %q", rec.Transcript[0].Text)
	}
	// checklist/keywords are serialised as JSON for jsonb columns
	for _, ph := range rec.Phases {
		var out []string
		if err := json.Unmarshal([]byte(jsonArray(ph.Checklist)), &out); err != nil {
			t.Errorf("checklist is not valid JSON: %v", err)
		}
		if err := json.Unmarshal([]byte(jsonArray(ph.Keywords)), &out); err != nil {
			t.Errorf("keywords are not valid JSON: %v", err)
		}
	}
}

func TestJSONArrayEscaping(t *testing.T) {
	got := jsonArray([]string{`a "b"`, `back\slash`, "line\nbreak"})
	var out []string
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("invalid JSON %q: %v", got, err)
	}
	if len(out) != 3 || out[0] != `a "b"` || out[1] != `back\slash` || out[2] != "line\nbreak" {
		t.Errorf("round-trip mismatch: %#v", out)
	}
	if jsonArray(nil) != "[]" {
		t.Errorf("nil should encode as []")
	}
}

func TestMalformedTimestampsDoNotPanic(t *testing.T) {
	e := ev(0, 0, CALL_STARTED, p("from", "+571"))
	e.Timestamp = "not-a-timestamp"
	e2 := ev(1, 0, TRANSCRIPT_UPDATED, p("role", "user", "text", "hola", "is_final", true))
	e2.Timestamp = ""
	e3 := ev(2, 0, CALL_ENDED, p("reason", "hangup"))
	rec, err := Derive([]Event{e, e2, e3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.DurationSec < 0 {
		t.Errorf("negative duration %d", rec.DurationSec)
	}
}

func TestNonNumericDurationFallsBackToWallClock(t *testing.T) {
	events := []Event{
		ev(0, 0, CALL_STARTED, p("from", "+571")),
		ev(1, 45, CALL_ENDED, p("duration_ms", "not-a-number")),
	}
	rec, _ := Derive(events)
	if rec.DurationSec != 45 {
		t.Errorf("duration = %d, want 45 from wall clock", rec.DurationSec)
	}
}

func TestScoresAlwaysInRange(t *testing.T) {
	logs := [][]Event{
		fullCall(),
		{ev(0, 0, CALL_STARTED, p("from", "+571")), ev(1, 1, CALL_ENDED, p())},
	}
	// a very chatty call with many features should still clamp to 100
	chatty := []Event{ev(0, 0, CALL_STARTED, p("from", "+571"))}
	for i := 1; i <= 40; i++ {
		chatty = append(chatty, ev(i, i, TRANSCRIPT_UPDATED, p("role", "agent", "text", "mensaje largo de prueba", "is_final", true)))
		chatty = append(chatty, ev(i+100, i, FEATURE_UPDATED, p("key", "family_status", "value", "x", "source", "transcript")))
	}
	chatty = append(chatty, ev(500, 60, CALL_ENDED, p("duration_ms", float64(60000))))
	logs = append(logs, chatty)

	for i, events := range logs {
		rec, err := Derive(events)
		if err != nil {
			t.Fatalf("log %d: %v", i, err)
		}
		for _, s := range rec.Scores {
			if s.Value < 0 || s.Value > 100 {
				t.Errorf("log %d: %s = %d out of range", i, s.Dimension, s.Value)
			}
		}
		if rec.ScoreOverall < 0 || rec.ScoreOverall > 100 {
			t.Errorf("log %d: overall %d out of range", i, rec.ScoreOverall)
		}
		if rec.ScoreLabel == "" {
			t.Errorf("log %d: missing score label", i)
		}
	}
}

func TestScoreLabelBoundaries(t *testing.T) {
	cases := map[int]string{0: "Bajo", 54: "Bajo", 55: "Regular", 69: "Regular", 70: "Bueno", 84: "Bueno", 85: "Excelente", 100: "Excelente"}
	for v, want := range cases {
		if got := scoreLabel(v); got != want {
			t.Errorf("scoreLabel(%d) = %q, want %q", v, got, want)
		}
	}
}

func TestInsightsNeverEmptyAndIndexed(t *testing.T) {
	for name, events := range map[string][]Event{
		"full":  fullCall(),
		"empty": {ev(0, 0, CALL_STARTED, p("from", "+571")), ev(1, 5, CALL_ENDED, p())},
	} {
		rec, _ := Derive(events)
		if len(rec.Insights) == 0 {
			t.Fatalf("%s: no insights", name)
		}
		for i, in := range rec.Insights {
			if in.Idx != i+1 {
				t.Errorf("%s: insight %d has idx %d", name, i, in.Idx)
			}
			if in.Text == "" || in.Priority == "" {
				t.Errorf("%s: insight %d incomplete", name, i)
			}
		}
	}
}

func TestKeywordsExcludeStopWordsAndShortTokens(t *testing.T) {
	lines := []LineRec{
		{Role: "customer", Text: "hola que tal, tengo una familia grande y familia unida", AtSec: 1},
	}
	kw := phaseKeywords(lines, 0, 10)
	for _, w := range kw {
		if stopWords[w] {
			t.Errorf("stop word leaked: %q", w)
		}
		if len([]rune(w)) < 4 {
			t.Errorf("short token leaked: %q", w)
		}
	}
	if len(kw) > 4 {
		t.Errorf("expected at most 4 keywords, got %d", len(kw))
	}
	if len(kw) > 0 && kw[0] != "familia" {
		t.Errorf("most frequent keyword should be 'familia', got %q", kw[0])
	}
}

func TestParticipationWindow(t *testing.T) {
	lines := []LineRec{
		{Role: "agent", Text: "aaaa", AtSec: 1},     // 4 chars
		{Role: "customer", Text: "bbbbbb", AtSec: 2}, // 6 chars
		{Role: "agent", Text: "ignorado", AtSec: 99}, // outside window
	}
	a, c := participation(lines, 0, 10)
	if a != 40 || c != 60 {
		t.Errorf("participation = %d/%d, want 40/60", a, c)
	}
	if a, c := participation(lines, 200, 300); a != 0 || c != 0 {
		t.Errorf("empty window should be 0/0, got %d/%d", a, c)
	}
}

func TestDeriveNameFallbacks(t *testing.T) {
	cases := []struct{ profile, phone, want string }{
		{"", "+57 310 123 4567", "Cliente 4567"},
		{"", "web-client", "Cliente Demo"},
		{"", "web-mic", "Cliente Demo"},
		{"", "", "Cliente Demo"},
		{"", "+12", "Cliente Demo"},
		{"Ana Pérez", "+573001112233", "Ana Pérez"},
	}
	for _, c := range cases {
		prof := map[string]string{}
		if c.profile != "" {
			prof["name"] = c.profile
		}
		if got := deriveName(prof, c.phone); got != c.want {
			t.Errorf("deriveName(%q,%q) = %q, want %q", c.profile, c.phone, got, c.want)
		}
	}
}

func TestNormalizeSentiment(t *testing.T) {
	cases := map[string]string{
		"positive": "Positiva", "positiva": "Positiva", "POSITIVO": "Positiva",
		"negative": "Negativa", "negativa": "Negativa",
		"neutral": "Neutral", "neutro": "Neutral", "": "Neutral",
		" positive ": "Positiva",
	}
	for in, want := range cases {
		if got := normalizeSentiment(in); got != want {
			t.Errorf("normalizeSentiment(%q) = %q, want %q", in, got, want)
		}
	}
}

// Sentiment reaching the phases must always be a Spanish label.
func TestPhaseEmotionIsSpanish(t *testing.T) {
	events := []Event{
		ev(0, 0, CALL_STARTED, p("from", "+571")),
		ev(1, 5, FEATURE_UPDATED, p("key", "sentiment", "value", "positive", "source", "inference")),
		ev(2, 20, CALL_ENDED, p("duration_ms", float64(20000))),
	}
	rec, _ := Derive(events)
	for _, ph := range rec.Phases {
		if ph.Emotion != "Positiva" {
			t.Errorf("phase %d emotion = %q, want Positiva", ph.Idx, ph.Emotion)
		}
	}
}

func TestPctOfGuardsZero(t *testing.T) {
	if pctOf(0, 50) != 0 {
		t.Error("pctOf must guard against zero total")
	}
	if pctOf(-10, 50) != 0 {
		t.Error("pctOf must guard against negative total")
	}
	if got := pctOf(200, 25); got != 50 {
		t.Errorf("pctOf(200,25) = %d, want 50", got)
	}
}
