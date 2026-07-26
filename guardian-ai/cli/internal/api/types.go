package api

import "time"

// Event mirrors the backend envelope in backend/events.go. Kept in sync via
// TestEventEnvelopeMatchesBackend (api_test.go) against the live API — do not
// hand-edit without re-running that test.
type Event struct {
	EventID   string         `json:"event_id"`
	Type      string         `json:"type"`
	CallID    string         `json:"call_id"`
	Sequence  int            `json:"sequence"`
	Timestamp string         `json:"timestamp"`
	Producer  string         `json:"producer"`
	Payload   map[string]any `json:"payload"`
}

// Event type constants — must match backend/events.go.
const (
	CallStarted             = "CALL_STARTED"
	CallEnded               = "CALL_ENDED"
	StateChanged            = "STATE_CHANGED"
	UserSpoke               = "USER_SPOKE"
	TranscriptUpdated       = "TRANSCRIPT_UPDATED"
	IntentDetected          = "INTENT_DETECTED"
	FeatureUpdated          = "FEATURE_UPDATED"
	LLMRequested            = "LLM_REQUESTED"
	LLMResponse             = "LLM_RESPONSE"
	ToolCalled              = "TOOL_CALLED"
	ToolExecuted            = "TOOL_EXECUTED"
	RecommendationGenerated = "RECOMMENDATION_GENERATED"
	VoiceSent               = "VOICE_SENT"
	MessageReceived         = "MESSAGE_RECEIVED"
	MessageSent             = "MESSAGE_SENT"
	LeadReady               = "LEAD_READY"
	QuoteCreated            = "QUOTE_CREATED"
	EnrollmentCreated       = "ENROLLMENT_CREATED"
	KnowledgeRetrieved      = "KNOWLEDGE_RETRIEVED"
	TurnCompleted           = "TURN_COMPLETED"
	SummaryGenerated        = "SUMMARY_GENERATED"
	ErrorOccurred           = "ERROR_OCCURRED"
)

// Conversation states — must match backend/statemachine.go.
const (
	StateNew                    = "NEW"
	StateProfileDiscovery       = "PROFILE_DISCOVERY"
	StateAffiliationCheck       = "AFFILIATION_CHECK"
	StateFinancialQualification = "FINANCIAL_QUALIFICATION"
	StateProjectMatching        = "PROJECT_MATCHING"
	StateClosing                = "CLOSING"
	StateReadyForAdvisor        = "READY_FOR_ADVISOR"
	StateNurturing              = "NURTURING"
	StateCompleted              = "COMPLETED"
)

// PipelineStates is the ordered happy-path stepper shown in the Pipeline module.
// NURTURING and COMPLETED are terminal branches, rendered but not on the main line.
var PipelineStates = []string{
	StateNew,
	StateProfileDiscovery,
	StateAffiliationCheck,
	StateFinancialQualification,
	StateProjectMatching,
	StateClosing,
	StateReadyForAdvisor,
}

type Health struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Time    string `json:"time"`
}

type Capabilities struct {
	LLM         bool `json:"llm"`
	ElevenLabs  bool `json:"elevenlabs"`
	Vapi        bool `json:"vapi"`
	WhatsApp    bool `json:"whatsapp"`
	Colsubsidio bool `json:"colsubsidio"`
	Guardian    bool `json:"guardian"`
	VapiWeb     bool `json:"vapi_web"`
	// VapiPublicKey / VapiAssistantID intentionally NOT decoded here — doctor
	// must never surface these even though the endpoint returns them.
}

type KPIs struct {
	AvgLLMLatencyMS   float64 `json:"avg_llm_latency_ms"`
	CostUSD           float64 `json:"cost_usd"`
	LeadsNurturing    int     `json:"leads_nurturing"`
	LeadsReady        int     `json:"leads_ready"`
	LeadsWhatsApp     int     `json:"leads_whatsapp"`
	TokensIn          int     `json:"tokens_in"`
	TokensOut         int     `json:"tokens_out"`
	ToolCalls         int     `json:"tool_calls"`
	VariablesCaptured int     `json:"variables_captured"`
}

type CallSummary struct {
	CallID string `json:"call_id"`
}

// CallDetail is intentionally loose (map passthrough) — the backend's
// analytics.Detail response shape isn't a stable contract yet.
type CallDetail map[string]any

type SimulateInboundRequest struct {
	From string `json:"from"`
	Text string `json:"text"`
}

type SourceMode int

const (
	ModeLive SourceMode = iota
	ModeOffline
	ModeReplay
)

func (m SourceMode) String() string {
	switch m {
	case ModeLive:
		return "live"
	case ModeOffline:
		return "offline"
	case ModeReplay:
		return "replay"
	default:
		return "unknown"
	}
}

// StreamState tracks the EventStream connection lifecycle.
type StreamState int32

const (
	StreamConnecting StreamState = iota
	StreamLive
	StreamReconnecting
	StreamOffline
)

func (s StreamState) String() string {
	switch s {
	case StreamConnecting:
		return "connecting"
	case StreamLive:
		return "live"
	case StreamReconnecting:
		return "reconnecting"
	case StreamOffline:
		return "offline"
	default:
		return "unknown"
	}
}

// Elapsed is a small helper used across modules to render relative time.
func Elapsed(since time.Time) time.Duration {
	return time.Since(since)
}
