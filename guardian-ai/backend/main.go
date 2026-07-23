package main

import (
	"log"
	"os"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/google/uuid"
)

func main() {
	bus := NewEventBus()
	store := NewEventStore()
	hub := NewHub()

	// persistence + dashboard consume every event (invariant §7.1)
	bus.Subscribe("*", store.Append)
	bus.Subscribe("*", hub.Broadcast)
	bus.Subscribe("*", func(ev Event) {
		log.Printf("[%s seq=%d] %s <- %s", ev.CallID[:8], ev.Sequence, ev.Type, ev.Producer)
	})

	// Real Supabase persistence via pgx (ADR-004/005). Optional: if the DB URL
	// is unset or unreachable the demo still runs on the in-memory store.
	if p, err := NewSupabasePersistence(); err != nil {
		log.Printf("supabase persistence disabled: %v", err)
	} else if p != nil {
		bus.Subscribe("*", p.Append)
		log.Printf("supabase persistence enabled")
	}

	step := 500 * time.Millisecond
	if v := os.Getenv("STEP_MS"); v != "" {
		if d, err := time.ParseDuration(v + "ms"); err == nil {
			step = d
		}
	}
	orch := NewOrchestrator(bus, step)

	// Real conversation engine (GPT-4o). Present only when a key is configured.
	var engine *ConversationEngine
	if os.Getenv("OPENAI_API_KEY") != "" {
		engine = NewConversationEngine(bus, NewLLMClient())
		log.Printf("real LLM engine enabled (gpt-4o)")
	}

	voice := NewVoiceAdapter()
	if voice.Enabled() {
		log.Printf("elevenlabs voice enabled")
	}
	vapi := NewVapiAdapter()
	if vapi.Enabled() {
		log.Printf("vapi telephony enabled")
	}

	app := fiber.New(fiber.Config{AppName: "Guardian AI"})
	app.Use(cors.New())

	app.Get("/api/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok", "service": "guardian-ai", "time": time.Now().UTC(),
			"llm": engine != nil,
		})
	})

	// Trigger a scripted end-to-end call (offline demo, no API cost).
	app.Post("/api/calls/simulate", func(c *fiber.Ctx) error {
		from := c.Query("from", "+57 300 000 0000")
		id := orch.Run(from)
		return c.JSON(fiber.Map{"call_id": id, "status": "started"})
	})

	// Start a REAL conversation (GPT-4o driven).
	app.Post("/api/calls/start", func(c *fiber.Ctx) error {
		if engine == nil {
			return fiber.NewError(503, "LLM engine not configured")
		}
		from := c.Query("from", "web-client")
		return c.JSON(fiber.Map{"call_id": engine.Start(from), "status": "started"})
	})

	// Send one user utterance into a real conversation.
	app.Post("/api/calls/:id/turn", func(c *fiber.Ctx) error {
		if engine == nil {
			return fiber.NewError(503, "LLM engine not configured")
		}
		var in struct {
			Text string `json:"text"`
		}
		if err := c.BodyParser(&in); err != nil || in.Text == "" {
			return fiber.NewError(400, "text required")
		}
		if err := engine.Turn(c.Context(), c.Params("id"), in.Text); err != nil {
			return fiber.NewError(502, err.Error())
		}
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Real ElevenLabs TTS: text -> MP3 (dashboard plays it).
	app.Post("/api/tts", func(c *fiber.Ctx) error {
		if !voice.Enabled() {
			return fiber.NewError(503, "voice not configured")
		}
		var in struct {
			Text string `json:"text"`
		}
		if err := c.BodyParser(&in); err != nil || in.Text == "" {
			return fiber.NewError(400, "text required")
		}
		audio, err := voice.Synthesize(c.Context(), in.Text)
		if err != nil {
			return fiber.NewError(502, err.Error())
		}
		c.Set("Content-Type", "audio/mpeg")
		return c.Send(audio)
	})

	// Voice capability flags for the dashboard. The Vapi PUBLIC key + assistant
	// id are safe to expose to the browser (that's their purpose for web calls).
	app.Get("/api/capabilities", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"llm": engine != nil, "elevenlabs": voice.Enabled(), "vapi": vapi.Enabled(),
			"vapi_web":          os.Getenv("VAPI_PUBLIC_KEY") != "" && os.Getenv("VAPI_ASSISTANT_ID") != "",
			"vapi_public_key":   os.Getenv("VAPI_PUBLIC_KEY"),
			"vapi_assistant_id": os.Getenv("VAPI_ASSISTANT_ID"),
		})
	})

	// Bridge browser-side Vapi web-call events into our event bus so a real voice
	// call is mirrored live on Mission Control and persisted to Supabase.
	// No public HTTPS webhook needed — the browser forwards the SDK events here.
	app.Post("/api/vapi/ingest", func(c *fiber.Ctx) error {
		var in struct {
			CallID string `json:"call_id"`
			Event  string `json:"event"` // start | user | agent | end
			Text   string `json:"text"`
		}
		if err := c.BodyParser(&in); err != nil {
			return fiber.NewError(400, "bad body")
		}
		if in.CallID == "" {
			in.CallID = uuid.NewString()
		}
		switch in.Event {
		case "start":
			bus.Publish(in.CallID, CALL_STARTED, "vapi_web", map[string]interface{}{
				"from": "web-mic", "channel": "webrtc",
			})
			bus.Publish(in.CallID, STATE_CHANGED, "conversation_engine", map[string]interface{}{"from": "CREATED", "to": "DISCOVERY"})
		case "user":
			bus.Publish(in.CallID, USER_SPOKE, "vapi_web", map[string]interface{}{"is_final": true})
			bus.Publish(in.CallID, TRANSCRIPT_UPDATED, "vapi_web", map[string]interface{}{"role": "user", "text": in.Text, "is_final": true})
		case "agent":
			bus.Publish(in.CallID, TRANSCRIPT_UPDATED, "voice_adapter", map[string]interface{}{"role": "agent", "text": in.Text, "is_final": true})
			bus.Publish(in.CallID, VOICE_SENT, "voice_adapter", map[string]interface{}{"text": in.Text, "voice_id": "vapi-web"})
		case "end":
			bus.Publish(in.CallID, CALL_ENDED, "vapi_web", map[string]interface{}{"reason": "hangup"})
			bus.Publish(in.CallID, STATE_CHANGED, "conversation_engine", map[string]interface{}{"from": "RESPONDING", "to": "ENDED"})
		}
		return c.JSON(fiber.Map{"call_id": in.CallID})
	})

	// Place a real outbound phone call (customer talks to Guardian AI live).
	app.Post("/api/phone/call", func(c *fiber.Ctx) error {
		if !vapi.Enabled() {
			return fiber.NewError(503, "vapi not configured")
		}
		var in struct {
			To string `json:"to"`
		}
		if err := c.BodyParser(&in); err != nil || in.To == "" {
			return fiber.NewError(400, "to (E.164 phone) required")
		}
		id, err := vapi.PlaceCall(c.Context(), in.To)
		if err != nil {
			return fiber.NewError(502, err.Error())
		}
		return c.JSON(fiber.Map{"vapi_call_id": id, "status": "dialing"})
	})

	// Replay the persisted event log for a call (RN-003).
	app.Get("/api/calls/:id/events", func(c *fiber.Ctx) error {
		return c.JSON(store.Get(c.Params("id")))
	})

	app.Get("/api/calls", func(c *fiber.Ctx) error {
		return c.JSON(store.Calls())
	})

	// Dashboard WebSocket (read-only stream).
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		hub.Add(c)
		defer hub.Remove(c)
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))

	addr := ":3000"
	log.Printf("Guardian AI backend on %s (step=%s)", addr, step)
	log.Fatal(app.Listen(addr))
}
