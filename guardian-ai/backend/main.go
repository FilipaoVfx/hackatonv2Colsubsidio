package main

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/google/uuid"
)

// hookSnapshot is the lock-free, JSON-serializable view of the last webhook.
type hookSnapshot struct {
	At     string `json:"at"`
	Event  string `json:"event"`
	Status int    `json:"status"`
	Debug  string `json:"debug"`
}

// hookDiag tracks the last Kapso webhook delivery for /api/whatsapp/debug.
type hookDiag struct {
	mu   sync.Mutex
	snap hookSnapshot
}

func (h *hookDiag) set(event string, status int, debug string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snap = hookSnapshot{At: time.Now().UTC().Format(time.RFC3339), Event: event, Status: status, Debug: debug}
}

// get returns a copy of the snapshot (no mutex copied — safe to return by value).
func (h *hookDiag) get() hookSnapshot { h.mu.Lock(); defer h.mu.Unlock(); return h.snap }

var lastHook = &hookDiag{}

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
	var analytics *Analytics
	if p, err := NewSupabasePersistence(); err != nil {
		log.Printf("supabase persistence disabled: %v", err)
	} else if p != nil {
		bus.Subscribe("*", p.Append)
		analytics = NewAnalytics(p.Pool())
		// Every finished call is projected into the analytics tables so it
		// shows up in "Pipeline de Llamadas" (demo, real GPT call or web call).
		bus.Subscribe("*", NewProjector(store, p.Pool()).OnEvent)
		log.Printf("supabase persistence + analytics projector enabled")
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

	// WhatsApp text channel (Kapso — Meta Cloud API proxy). Same graceful
	// degradation as voice/vapi: without credentials the demo runs offline via
	// /api/whatsapp/simulate-inbound and replies stream to the UI over /ws.
	wa := NewKapsoAdapter()
	sessions := NewWhatsAppSessions()
	if wa.Enabled() {
		log.Printf("whatsapp (kapso) enabled")
		if os.Getenv("KAPSO_WEBHOOK_SECRET") == "" {
			log.Printf("SECURITY WARNING: KAPSO_WEBHOOK_SECRET unset — the /api/whatsapp/webhook endpoint accepts UNVERIFIED requests. Set it in production so inbound webhooks are HMAC-checked.")
		}
	}

	// Colsubsidio Protege API — server-side conversation + recommendation engine
	// that the WhatsApp channel drives. When configured it is the brain; without
	// it the WhatsApp routes fall back to the local GPT-4o engine.
	protegeAPI := NewColsubsidioClient()
	protege := NewProtegeEngine(bus, protegeAPI, sessions)
	if protege.Enabled() {
		log.Printf("colsubsidio protege api enabled (whatsapp brain)")
	}

	// Guardian Conversation Engine (spec retrieval.md): the preferred WhatsApp
	// brain. Needs BOTH the Protege API (decisions) and the LLM (language).
	// GUARDIAN_DISABLED=1 forces the fallback chain for live comparison.
	var guardian *GuardianEngine
	if protegeAPI.Enabled() && os.Getenv("OPENAI_API_KEY") != "" && os.Getenv("GUARDIAN_DISABLED") == "" {
		rag := NewRAG(knowledgeDir(), NewLLMClient())
		affiliates := NewAffiliates()
		guardian = NewGuardianEngine(bus, protegeAPI, NewLLMClient(), NewTools(protegeAPI, bus), sessions, rag, affiliates)
		log.Printf("guardian conversation engine enabled (whatsapp brain, rag=%s, afiliado360=%v)", rag.Mode(), affiliates.Enabled())

		// Seed de reglas 360 (derivadas de la base de afiliados) vía el CRUD de
		// la propia API (POST /api/v1/rules) — mismo camino para mock y real.
		// Idempotente: el mock upserta por Name.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			products, err := protegeAPI.GetProducts(ctx)
			if err != nil {
				log.Printf("afiliado360: seed rules skipped (products: %v)", err)
				return
			}
			n := 0
			for _, r := range derivedRules(products) {
				if _, err := protegeAPI.CreateRule(ctx, r); err != nil {
					log.Printf("afiliado360: seed rule %q failed: %v", r.Name, err)
					continue
				}
				n++
			}
			log.Printf("afiliado360: %d regla(s) 360 sembradas en la API", n)
		}()
	}
	// Barrido de conversaciones abandonadas: pasada la ventana de 24h de
	// WhatsApp la sesión ya no sirve, pero nadie la borraba y su estado en el
	// motor (historial, catálogo, recomendaciones) vivía hasta el reinicio.
	go func() {
		for range time.Tick(30 * time.Minute) {
			if guardian.Enabled() {
				if n := guardian.Sweep(); n > 0 {
					log.Printf("guardian: %d conversación(es) liberadas por ventana de 24h vencida", n)
				}
				continue
			}
			if n := len(sessions.Sweep()); n > 0 {
				log.Printf("whatsapp: %d sesión(es) liberadas por ventana de 24h vencida", n)
			}
		}
	}()

	// Delivery consumer (bus -> Kapso), per RA-002. Every MESSAGE_SENT is sent to
	// the customer's phone; in demo mode (no creds) it is a no-op because the reply
	// already reached the UI via the ws_hub subscriber.
	bus.Subscribe(MESSAGE_SENT, func(ev Event) {
		if !wa.Enabled() {
			return
		}
		to := sessions.PhoneFor(ev.CallID)
		if to == "" {
			return
		}
		text, _ := ev.Payload["text"].(string)
		if text == "" {
			return
		}
		if _, err := wa.Send(context.Background(), to, text); err != nil {
			log.Printf("whatsapp send failed (call=%s): %v", ev.CallID[:8], err)
		}
	})

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
		return c.JSON(fiber.Map{"call_id": engine.Start(from, "web"), "status": "started"})
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

	// End a real conversation: emits the closing events so the call gets
	// projected into the Pipeline view.
	app.Post("/api/calls/:id/end", func(c *fiber.Ctx) error {
		id := c.Params("id")
		if id == "" {
			return fiber.NewError(400, "call id required")
		}
		bus.Publish(id, SUMMARY_GENERATED, "decision_engine", map[string]interface{}{
			"summary": "Conversación finalizada por el operador.",
		})
		bus.Publish(id, CALL_ENDED, "conversation_engine", map[string]interface{}{"reason": "hangup"})
		bus.Publish(id, STATE_CHANGED, "conversation_engine", map[string]interface{}{"from": "RESPONDING", "to": "ENDED"})
		sessions.Close(id) // no-op for non-WhatsApp conversations
		return c.JSON(fiber.Map{"status": "ended", "call_id": id})
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
			"whatsapp":          wa.Enabled(),
			"colsubsidio":       protegeAPI.Enabled(),
			"guardian":          guardian.Enabled(),
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

	// ---- Chat / WhatsApp (text channel — variante del sistema autónomo) ----

	// waInbound drives one inbound customer message through the shared LLM
	// pipeline: resolve (or open) the session, then Turn. Used by both the offline
	// simulate route and the real Kapso webhook.
	waInbound := func(ctx context.Context, from, text string) (string, error) {
		from = canonPhone(from) // una sola forma canónica por cliente
		// Preferred: Guardian Conversation Engine (LLM entiende, API decide).
		if guardian.Enabled() {
			convID, err := guardian.HandleInbound(ctx, from, text)
			if err != nil {
				return convID, fiber.NewError(502, err.Error())
			}
			return convID, nil
		}
		// Fallback 1: rigid Protege question flow (API only, no LLM).
		if protege.Enabled() {
			convID, err := protege.HandleInbound(ctx, from, text)
			if err != nil {
				return convID, fiber.NewError(502, err.Error())
			}
			return convID, nil
		}
		// Fallback: local GPT-4o free-form engine.
		if engine == nil {
			return "", fiber.NewError(503, "LLM engine not configured")
		}
		convID, isNew := sessions.Resolve(from)
		if isNew {
			convID = engine.Start(from, "whatsapp")
			sessions.Register(from, convID)
		}
		if err := engine.Turn(ctx, convID, text); err != nil {
			return convID, fiber.NewError(502, err.Error())
		}
		return convID, nil
	}

	// Autonomous OUTBOUND contact: the system opens a WhatsApp conversation and
	// sends the opener (fixed template — respects the 24h-window rule). Real
	// delivery happens via the MESSAGE_SENT consumer when Kapso is configured.
	app.Post("/api/chat/start", func(c *fiber.Ctx) error {
		var in struct {
			To      string `json:"to"`
			Opening string `json:"opening"`
		}
		if err := c.BodyParser(&in); err != nil || in.To == "" {
			return fiber.NewError(400, "to (E.164 phone) required")
		}
		in.To = canonPhone(in.To) // misma forma canónica que el webhook
		// Preferred: Guardian engine opens the conversation with its opener.
		if guardian.Enabled() {
			convID, err := guardian.StartContact(c.Context(), in.To)
			if err != nil {
				return fiber.NewError(502, err.Error())
			}
			return c.JSON(fiber.Map{"conversation_id": convID, "status": "started", "engine": "guardian"})
		}
		// Fallback 1: rigid Protege question flow.
		if protege.Enabled() {
			convID, err := protege.StartContact(c.Context(), in.To)
			if err != nil {
				return fiber.NewError(502, err.Error())
			}
			return c.JSON(fiber.Map{"conversation_id": convID, "status": "started"})
		}
		// Fallback: local GPT-4o engine with a fixed opener.
		if engine == nil {
			return fiber.NewError(503, "LLM engine not configured")
		}
		opening := in.Opening
		if opening == "" {
			opening = "Hola, soy Guardian AI, tu asesor de seguros de Colsubsidio. " +
				"¿Te gustaría conocer una protección pensada para ti? 🛡️"
		}
		convID := engine.Start(in.To, "whatsapp")
		sessions.Register(in.To, convID)
		bus.Publish(convID, TRANSCRIPT_UPDATED, "whatsapp_adapter", map[string]interface{}{
			"role": "agent", "text": opening, "is_final": true,
		})
		bus.Publish(convID, MESSAGE_SENT, "whatsapp_adapter", map[string]interface{}{
			"text": opening, "channel": "whatsapp", "status": "queued", "to": in.To, "wa_message_id": "",
		})
		return c.JSON(fiber.Map{"conversation_id": convID, "status": "started"})
	})

	// DEMO offline: simulate an inbound customer message without a public webhook
	// (analogous to /api/vapi/ingest). The reply streams to the UI over /ws.
	app.Post("/api/whatsapp/simulate-inbound", func(c *fiber.Ctx) error {
		var in struct {
			From string `json:"from"`
			Text string `json:"text"`
		}
		if err := c.BodyParser(&in); err != nil || in.From == "" || in.Text == "" {
			return fiber.NewError(400, "from and text required")
		}
		convID, err := waInbound(c.Context(), in.From, in.Text)
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"conversation_id": convID, "status": "ok"})
	})

	// REAL inbound from Kapso. Requires a public HTTPS URL (Kapso only delivers
	// to HTTPS; we expose one via cloudflared). Kapso requires a 200 within 10s,
	// so we ack immediately and process the conversation turn asynchronously
	// (events stream to the UI over /ws; replies go out via the MESSAGE_SENT
	// delivery consumer).
	// Reentregas de Kapso (no llegó el 200 a tiempo) no deben correr el turno
	// dos veces: se descartan por message id dentro de esta ventana.
	webhookDedupe := newInboundDedupe(15 * time.Minute)
	app.Post("/api/whatsapp/webhook", func(c *fiber.Ctx) error {
		body := c.Body()
		status, debug := processKapsoWebhook(body, c.Get("X-Webhook-Event"),
			c.Get("X-Webhook-Signature"), os.Getenv("KAPSO_WEBHOOK_SECRET"), webhookDedupe,
			func(from, text string) {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
					defer cancel()
					if _, err := waInbound(ctx, from, text); err != nil {
						log.Printf("whatsapp webhook turn failed: %v", err)
						// El turno falló antes de existir la conversación (API
						// caída al abrir): no hay bus por el que responder, así
						// que el cliente recibiría SILENCIO. Se le contesta por
						// el canal directamente.
						if wa.Enabled() {
							if _, sErr := wa.Send(ctx, from, guardianFallbackMsg); sErr != nil {
								log.Printf("whatsapp fallback reply failed: %v", sErr)
							}
						}
					}
				}()
			})
		lastHook.set(c.Get("X-Webhook-Event"), status, debug)
		if status != 200 {
			return fiber.NewError(status, debug)
		}
		return c.SendStatus(200)
	})

	// Debug/diagnostics for the WhatsApp wiring (demo + hackathon judges).
	app.Get("/api/whatsapp/debug", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"enabled":           wa.Enabled(),
			"phone_number_id":   os.Getenv("KAPSO_PHONE_NUMBER_ID") != "",
			"signature_check":   os.Getenv("KAPSO_WEBHOOK_SECRET") != "",
			"guardian":          guardian.Enabled(),
			"colsubsidio_brain": protege.Enabled(),
			"live_sessions":     sessions.List(),
			"last_webhook":      lastHook.get(),
		})
	})

	// Live WhatsApp sessions — the UI re-attaches to an ongoing conversation
	// after a reload (robust realtime).
	app.Get("/api/whatsapp/sessions", func(c *fiber.Ctx) error {
		return c.JSON(sessions.List())
	})

	// ---- Pipeline de Llamadas (post-call analytics, read-only) ----
	app.Get("/api/analytics/calls", func(c *fiber.Ctx) error {
		if analytics == nil {
			return fiber.NewError(503, "analytics requires supabase")
		}
		list, err := analytics.List(c.Context())
		if err != nil {
			return fiber.NewError(500, err.Error())
		}
		return c.JSON(list)
	})

	// Guardian KPIs (spec §12 subset): funnel + costos agregados.
	app.Get("/api/analytics/kpis", func(c *fiber.Ctx) error {
		if analytics == nil {
			return fiber.NewError(503, "analytics requires supabase")
		}
		kpis, err := analytics.KPIs(c.Context())
		if err != nil {
			return fiber.NewError(502, err.Error())
		}
		return c.JSON(kpis)
	})

	app.Get("/api/analytics/calls/:id", func(c *fiber.Ctx) error {
		if analytics == nil {
			return fiber.NewError(503, "analytics requires supabase")
		}
		d, err := analytics.Detail(c.Context(), c.Params("id"))
		if err != nil {
			return fiber.NewError(404, err.Error())
		}
		return c.JSON(d)
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
