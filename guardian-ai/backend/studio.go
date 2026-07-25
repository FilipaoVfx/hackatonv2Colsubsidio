package main

import (
	"sort"

	"github.com/gofiber/fiber/v2"
)

// Agent Studio — capa HTTP (plan 10_PLAN_AGENT_STUDIO.md §8). Vive aparte de
// main.go para que la consola crezca sin engordar el arranque.
//
// Fase 1: lectura (configuración, catálogos, estado real del runtime, prompt
// generado). Fase 2: guardar el borrador. Publicar llega en la fase 4: hasta
// entonces el motor sigue con lo que haya publicado, y el borrador solo se ve
// en el Inspector y en el Playground.

// StudioDeps es lo que la consola necesita del resto del sistema. Se pasa
// explícito en vez de leer variables globales: así los tests montan el router
// con dobles y sin arrancar el motor entero.
type StudioDeps struct {
	Store    *ConfigStore
	Engine   *GuardianEngine
	RAG      *RAG
	Products func() []ProtegeProduct // catálogo real para el Prompt Inspector
	Rules    func() []ProtegeRule
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
		States: []string{
			string(StateNew), string(StateAffiliation), string(StateProfile),
			string(StateFinancial), string(StateMatching), string(StateReady),
			string(StateNurturing), string(StateCompleted),
		},
		Intents: guardianIntents,
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
		return c.JSON(fiber.Map{
			"published":   deps.Store.Published(),
			"draft":       deps.Store.Draft(),
			"defaults":    DefaultConfig(),
			"live":        deps.Engine.Config(), // lo que el motor está usando AHORA
			"runtime":     currentRuntime(deps.RAG),
			"store_error": deps.Store.LoadError(),
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
}

func knownState(s LeadState) bool {
	switch s {
	case StateNew, StateAffiliation, StateProfile, StateFinancial,
		StateMatching, StateReady, StateNurturing, StateCompleted:
		return true
	}
	return false
}
