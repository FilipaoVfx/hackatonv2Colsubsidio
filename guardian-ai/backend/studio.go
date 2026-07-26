package main

import (
	"sort"
	"strconv"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

// Agent Studio — capa HTTP (plan 10_PLAN_AGENT_STUDIO.md §8). Vive aparte de
// main.go para que la consola crezca sin engordar el arranque.
//
// Fase 1: lectura (configuración, catálogos, estado real del runtime, prompt
// generado). Fase 2: guardar el borrador. Fase 3: probar el borrador en un
// Playground aislado, que nunca toca producción.

// StudioDeps es lo que la consola necesita del resto del sistema. Se pasa
// explícito en vez de leer variables globales: así los tests montan el router
// con dobles y sin arrancar el motor entero.
type StudioDeps struct {
	Store      *ConfigStore
	Engine     *GuardianEngine
	RAG        *RAG
	Products   func() []ProtegeProduct // catálogo real para el Prompt Inspector
	Rules      func() []ProtegeRule
	Playground *Playground // entorno de pruebas aislado (nil = sin Playground)
}

// RuntimeSnapshot son los valores REALES con los que corre el motor. La consola
// los muestra en solo lectura: es más honesto que pintar interruptores que no
// mueven nada (Knowledge/Memory/Reasoning del PRD todavía no son editables).
type RuntimeSnapshot struct {
	Model               string   `json:"model"`
	Temperature         float64  `json:"temperature"`
	HistoryWindow       int      `json:"history_window"`
	RAGMode             string   `json:"rag_mode"`
	RAGChunks           int      `json:"rag_chunks"`
	RAGTopK             int      `json:"rag_top_k"`
	ConfidenceThreshold float64  `json:"confidence_threshold"`
	MaxRecAttempts      int      `json:"max_rec_attempts"`
	Tools               []string `json:"tools"`
	States              []string `json:"states"`
	Intents             []string `json:"intents"`
}

// leadStateNames rinde el recorrido canónico (LeadStateOrder) como strings para
// el JSON. La lista NO se reescribe aquí: duplicarla fue lo que hizo que la
// consola siguiera mostrando el recorrido viejo al entrar CLOSING.
func leadStateNames() []string {
	out := make([]string, 0, len(LeadStateOrder))
	for _, s := range LeadStateOrder {
		out = append(out, string(s))
	}
	return out
}

// currentRuntime lee las constantes vivas del motor. Si mañana una de ellas se
// vuelve configurable, este es el sitio que deja de ser literal.
func currentRuntime(rag *RAG) RuntimeSnapshot {
	tools := make([]string, 0, len(registered))
	for name := range registered {
		tools = append(tools, name)
	}
	sort.Strings(tools)

	mode, chunks := "sin corpus", 0
	if rag.Enabled() {
		mode, chunks = rag.Mode(), rag.Chunks()
	}
	return RuntimeSnapshot{
		Model:               model,
		Temperature:         guardianTemperature,
		HistoryWindow:       maxHistory,
		RAGMode:             mode,
		RAGChunks:           chunks,
		RAGTopK:             ragTopK,
		ConfidenceThreshold: entityConfidence,
		MaxRecAttempts:      maxRecAttempts,
		Tools:               tools,
		States:              leadStateNames(),
		Intents:             guardianIntents,
	}
}

// RegisterStudioRoutes monta la consola. Solo se llama cuando hay motor y
// store: sin Guardian no hay nada que configurar.
func RegisterStudioRoutes(app *fiber.App, deps StudioDeps) {
	if deps.Store == nil {
		return
	}

	// Estado completo de la consola en una sola llamada: lo publicado, el
	// borrador, los defaults de fábrica (para el botón "restablecer"), los
	// catálogos cerrados (para que el frontend no duplique literales) y el
	// runtime real.
	app.Get("/api/studio/config", func(c *fiber.Ctx) error {
		draft := deps.Store.Draft()
		return c.JSON(fiber.Map{
			"published": deps.Store.Published(),
			"draft":     draft,
			"defaults":  DefaultConfig(),
			"live":      deps.Engine.Config(), // lo que el motor está usando AHORA
			"runtime":   currentRuntime(deps.RAG),
			// Cuánto pesa el borrador dentro del prompt, y el presupuesto que
			// nos pusimos: mover controles tiene un coste y se ve.
			"config_bytes":     configPromptBytes(draft),
			"config_bytes_max": maxConfigPromptBytes,
			"store_error":      deps.Store.LoadError(),
			"catalogs": fiber.Map{
				"sales_goals":   SalesGoalCatalog,
				"safety_forbid": SafetyForbidCatalog,
				"lengths":       lengthOptions,
				"safety_levels": safetyLevels,
				"goal_labels":   salesGoalLabels,
				"forbid_labels": safetyForbidLabels,
				// Etiquetas y frases de cada perilla: la consola las pinta tal
				// cual, así que la redacción vive en un solo sitio (Go) y no se
				// puede desincronizar con lo que recibe el modelo.
				"persona_scales":  personaScaleLabels(),
				"persona_phrases": personaPhraseTable(),
			},
		})
	})

	// Historial de versiones publicadas, de la más reciente a la más antigua.
	app.Get("/api/studio/versions", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"versions": deps.Store.History()})
	})

	// Prompt Inspector: el prompt REAL que se le manda al modelo, compuesto con
	// el catálogo vivo de la API. Solo lectura — nunca editable, por diseño del
	// PRD y porque un prompt escrito a mano se saltaría toda la validación.
	app.Get("/api/studio/prompt", func(c *fiber.Ctx) error {
		cfg := deps.Store.Published()
		if c.Query("draft") == "1" {
			cfg = deps.Store.Draft()
		}
		state := LeadState(c.Query("state", string(StateProfile)))
		if !knownState(state) {
			return fiber.NewError(400, "estado desconocido")
		}
		in := PromptInput{
			State:  state,
			Config: cfg,
			Memory: CustomerMemory{
				User: &ProtegeUser{FirstName: "Ana", Phone: "+57300…"},
				Variables: []UserVariable{
					{Key: "has_pet", Value: true, Source: "whatsapp"},
					{Key: "city", Value: "BOGOTA D.C.", Source: "colsubsidio_360"},
				},
			},
			MissingVars: []ProtegeQuestion{
				{VariableKey: "monthly_income", FieldType: "currency", Text: "¿Ingresos mensuales?"},
			},
		}
		if deps.Products != nil {
			in.Products = deps.Products()
		}
		if deps.Rules != nil {
			in.Rules = deps.Rules()
		}
		prompt := BuildSystemPrompt(in)
		return c.JSON(fiber.Map{
			"prompt":  prompt,
			"state":   string(state),
			"version": cfg.Version,
			"bytes":   len(prompt),
			"source":  map[bool]string{true: "draft", false: "published"}[c.Query("draft") == "1"],
		})
	})

	// Guardar borrador (fase 2). Validación por campo: la consola marca el
	// control que falla, no un cartel genérico. El borrador NO afecta al motor.
	app.Put("/api/studio/config/draft", func(c *fiber.Ctx) error {
		var in AgentConfig
		if err := c.BodyParser(&in); err != nil {
			return fiber.NewError(400, "cuerpo inválido")
		}
		saved, fieldErrs, err := deps.Store.SaveDraft(in)
		if err != nil {
			return fiber.NewError(500, "no se pudo guardar el borrador: "+err.Error())
		}
		if len(fieldErrs) > 0 {
			return c.Status(422).JSON(fiber.Map{"errors": fieldErrs})
		}
		return c.JSON(fiber.Map{"draft": saved})
	})

	// Publicar (fase 4): el borrador pasa a ser la configuración viva. El orden
	// importa — primero el disco, después el motor. Si la escritura falla no se
	// aplica nada, así que lo que corre siempre es lo que quedó guardado.
	app.Post("/api/studio/config/publish", func(c *fiber.Ctx) error {
		var in struct {
			Note string `json:"note"`
		}
		_ = c.BodyParser(&in) // la nota es opcional

		published, fieldErrs, err := deps.Store.Publish(in.Note)
		if err != nil {
			return fiber.NewError(500, "no se pudo publicar: "+err.Error())
		}
		if len(fieldErrs) > 0 {
			return c.Status(422).JSON(fiber.Map{"errors": fieldErrs})
		}
		deps.Engine.SetConfig(published)
		return c.JSON(fiber.Map{
			"published": published, "version": published.Version,
			"config_bytes": configPromptBytes(published),
		})
	})

	// Volver a una versión anterior. No borra historia: la versión recuperada
	// entra como una versión NUEVA, así se puede deshacer el deshacer.
	app.Post("/api/studio/config/rollback/:version", func(c *fiber.Ctx) error {
		version, err := strconv.Atoi(c.Params("version"))
		if err != nil {
			return fiber.NewError(400, "versión inválida")
		}
		var in struct {
			Note string `json:"note"`
		}
		_ = c.BodyParser(&in)

		published, fieldErrs, err := deps.Store.Restore(version, in.Note)
		if err != nil {
			return fiber.NewError(404, err.Error())
		}
		if len(fieldErrs) > 0 {
			return c.Status(422).JSON(fiber.Map{"errors": fieldErrs})
		}
		deps.Engine.SetConfig(published)
		return c.JSON(fiber.Map{
			"published": published, "version": published.Version,
			"restored_from": version,
		})
	})

	registerPlaygroundRoutes(app, deps.Playground)
}

// registerPlaygroundRoutes monta el entorno de pruebas (fase 3). Vive en su
// propio bloque porque su contrato es distinto al del resto de la consola: es
// lo único del Studio que EJECUTA turnos, y lo hace en un mundo aparte.
func registerPlaygroundRoutes(app *fiber.App, pg *Playground) {
	// Sin Playground (sin motor o sin API de pruebas) la consola sigue
	// funcionando entera: se declara desactivado y la UI lo dice.
	app.Get("/api/studio/playground", func(c *fiber.Ctx) error {
		if !pg.Enabled() {
			return c.JSON(fiber.Map{"enabled": false})
		}
		return c.JSON(fiber.Map{
			"enabled": true, "api": pg.APIBase(), "max_turns": maxPlaygroundTurns,
		})
	})

	if !pg.Enabled() {
		return
	}

	app.Post("/api/studio/playground/start", func(c *fiber.Ctx) error {
		return c.JSON(pg.Start())
	})

	app.Post("/api/studio/playground/message", func(c *fiber.Ctx) error {
		var in struct {
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		}
		if err := c.BodyParser(&in); err != nil || in.SessionID == "" || in.Text == "" {
			return fiber.NewError(400, "session_id y text son obligatorios")
		}
		turn, err := pg.Message(c.Context(), in.SessionID, in.Text)
		switch {
		case err == errPlaygroundLimit:
			return fiber.NewError(429, err.Error())
		case err != nil:
			return fiber.NewError(502, err.Error())
		}
		return c.JSON(turn)
	})

	app.Post("/api/studio/playground/reset", func(c *fiber.Ctx) error {
		var in struct {
			SessionID string `json:"session_id"`
		}
		if err := c.BodyParser(&in); err != nil || in.SessionID == "" {
			return fiber.NewError(400, "session_id obligatorio")
		}
		pg.Reset(in.SessionID)
		return c.JSON(fiber.Map{"status": "reset"})
	})

	// WebSocket propio: los eventos del Playground NO viajan por /ws, que es el
	// que alimenta el Puesto de operación con conversaciones reales.
	app.Use("/ws/studio", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/ws/studio", websocket.New(func(c *websocket.Conn) {
		hub := pg.Hub()
		hub.Add(c)
		defer hub.Remove(c)
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
}

func knownState(s LeadState) bool {
	switch s {
	case StateNew, StateAffiliation, StateProfile, StateFinancial,
		StateMatching, StateReady, StateNurturing, StateCompleted:
		return true
	}
	return false
}
