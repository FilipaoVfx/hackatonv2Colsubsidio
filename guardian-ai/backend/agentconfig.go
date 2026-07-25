package main

import (
	"fmt"
	"strings"
	"time"
)

// Agent Studio — modelo de configuración del asesor (PRD agentStudio.md, plan
// 10_PLAN_AGENT_STUDIO.md, fase 0). El Studio NO permite escribir prompts: solo
// mover perillas de un vocabulario CERRADO. Este archivo es el contrato de esas
// perillas — structs, catálogos, defaults y validación. Todo funciones puras:
// sin red, sin estado global, testeables.
//
// Invariante de seguridad (§2 del plan): ninguna configuración válida puede
// tocar las reglas del motor — flechas de la máquina de estados, whitelist de
// acciones por etapa, vocabulario de variables del perfil, structured outputs,
// ni el principio de que la API de Colsubsidio decide el negocio. Lo que no
// está en estos structs, no se configura. Por eso el único texto libre que
// puede llegar al modelo es el nombre del agente, y va saneado: un campo de
// texto abierto sería una vía de inyección de instrucciones.

// AgentConfig es una configuración completa del asesor. Se trata como
// INMUTABLE una vez publicada: publicar crea una copia nueva y cambia el
// puntero atómico del motor (ver GuardianEngine.SetConfig), de modo que un
// turno en curso nunca ve dos configuraciones distintas.
type AgentConfig struct {
	Version   int       `json:"version"` // monotónico; 0 = defaults de fábrica
	Status    string    `json:"status"`  // "draft" | "published"
	Note      string    `json:"note"`    // nota de versión (nunca entra al prompt)
	UpdatedAt time.Time `json:"updated_at"`

	Persona PersonaConfig `json:"persona"`
	Sales   SalesConfig   `json:"sales"`
	Safety  SafetyConfig  `json:"safety"`
}

// PersonaConfig son las perillas de comportamiento conversacional. Los enteros
// son 1..10 y se traducen a FRASES por rango (fase 2): nunca se interpola el
// número crudo en el prompt, que para el modelo no significaría nada.
type PersonaConfig struct {
	AgentName   string `json:"agent_name"`
	Empathy     int    `json:"empathy"`
	Formality   int    `json:"formality"`
	Closeness   int    `json:"closeness"`
	Persuasion  int    `json:"persuasion"`
	Proactivity int    `json:"proactivity"`
	Length      string `json:"length"` // breve | media | detallada
	Emojis      bool   `json:"emojis"`
	Humor       bool   `json:"humor"`
}

// SalesConfig es la lista ordenada de objetivos comerciales: el orden ES la
// prioridad. No sustituye a StateGoal — el objetivo de etapa es lo que sostiene
// la máquina de estados y no está en manos de la configuración.
type SalesConfig struct {
	Goals []string `json:"goals"`
}

// SafetyConfig son las prohibiciones explícitas y cuánto se protege el agente
// ante la duda.
type SafetyConfig struct {
	Forbid []string `json:"forbid"`
	Level  string   `json:"level"` // bajo | medio | alto
}

// ---- catálogos cerrados ----
//
// Se sirven a la UI desde la API para que el frontend no duplique literales: si
// mañana entra un objetivo nuevo, entra aquí y aparece solo en el Studio.

// SalesGoalCatalog: objetivos disponibles, en el orden en que los presenta el PRD.
var SalesGoalCatalog = []string{
	"resolver_dudas",
	"calificar_cliente",
	"recomendar_producto",
	"cerrar_venta",
	"agendar_llamada",
	"derivar_humano",
}

// SafetyForbidCatalog: qué se le puede prohibir explícitamente al agente.
var SafetyForbidCatalog = []string{
	"coberturas_inventadas",
	"promesas_falsas",
	"consejos_legales",
	"consejos_medicos",
	"informacion_inexistente",
}

var lengthOptions = []string{"breve", "media", "detallada"}
var safetyLevels = []string{"bajo", "medio", "alto"}

const (
	maxAgentNameLen = 40
	maxNoteLen      = 120
	// MaxConfigHistory: cuántas versiones publicadas se conservan.
	MaxConfigHistory = 20
)

// DefaultConfig es el comportamiento ACTUAL del asesor expresado como
// configuración. Es la línea base de la "prueba de oro": con estos valores el
// prompt generado debe ser idéntico al que el motor producía antes de existir
// el Studio (ver TestPromptDefaultGolden).
func DefaultConfig() AgentConfig {
	return AgentConfig{
		Version:   0,
		Status:    "published",
		UpdatedAt: time.Time{},
		Persona: PersonaConfig{
			AgentName:   "Guardian",
			Empathy:     8,
			Formality:   5,
			Closeness:   7,
			Persuasion:  4,
			Proactivity: 6,
			Length:      "media",
			Emojis:      true,
			Humor:       false,
		},
		Sales: SalesConfig{Goals: []string{
			"resolver_dudas", "calificar_cliente", "recomendar_producto", "derivar_humano",
		}},
		Safety: SafetyConfig{
			Forbid: []string{"coberturas_inventadas", "promesas_falsas", "informacion_inexistente"},
			Level:  "alto",
		},
	}
}

// FieldError localiza el problema en el control que lo produjo, para que el
// Studio lo pinte junto al slider y no en un cartel genérico.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

// Clone devuelve una copia profunda. Las configuraciones se comparten entre
// goroutines (el motor las lee por turno), así que nunca se entrega el mismo
// slice que guarda el store.
func (c AgentConfig) Clone() AgentConfig {
	out := c
	out.Sales.Goals = append([]string(nil), c.Sales.Goals...)
	out.Safety.Forbid = append([]string(nil), c.Safety.Forbid...)
	return out
}

// Normalize limpia lo que es ruido de entrada (espacios, mayúsculas de los
// enums) SIN corregir lo que es un error de verdad: un valor fuera de rango se
// rechaza en Validate, no se recorta en silencio. Recortar callado haría que el
// Studio mostrara una cosa y el agente se comportara según otra.
func (c *AgentConfig) Normalize() {
	c.Persona.AgentName = strings.TrimSpace(c.Persona.AgentName)
	c.Persona.Length = strings.ToLower(strings.TrimSpace(c.Persona.Length))
	c.Safety.Level = strings.ToLower(strings.TrimSpace(c.Safety.Level))
	c.Note = strings.TrimSpace(c.Note)
	for i, g := range c.Sales.Goals {
		c.Sales.Goals[i] = strings.ToLower(strings.TrimSpace(g))
	}
	for i, f := range c.Safety.Forbid {
		c.Safety.Forbid[i] = strings.ToLower(strings.TrimSpace(f))
	}
}

// Validate devuelve TODOS los errores encontrados (no solo el primero): el
// Studio necesita marcar cada control problemático de una vez.
func (c AgentConfig) Validate() []FieldError {
	var errs []FieldError
	add := func(field, msg string) { errs = append(errs, FieldError{Field: field, Message: msg}) }

	// El nombre es el ÚNICO texto libre que llega al modelo: se limita en
	// longitud y se prohíben los caracteres con los que se podría abrir una
	// sección nueva del prompt o inyectar instrucciones en otra línea.
	name := c.Persona.AgentName
	switch {
	case name == "":
		add("persona.agent_name", "el nombre del agente es obligatorio")
	case len([]rune(name)) > maxAgentNameLen:
		add("persona.agent_name", fmt.Sprintf("máximo %d caracteres", maxAgentNameLen))
	case strings.ContainsAny(name, "\n\r#*`"):
		add("persona.agent_name", "no puede contener saltos de línea ni marcas de formato")
	}

	for _, s := range []struct {
		field string
		value int
	}{
		{"persona.empathy", c.Persona.Empathy},
		{"persona.formality", c.Persona.Formality},
		{"persona.closeness", c.Persona.Closeness},
		{"persona.persuasion", c.Persona.Persuasion},
		{"persona.proactivity", c.Persona.Proactivity},
	} {
		if s.value < 1 || s.value > 10 {
			add(s.field, "debe estar entre 1 y 10")
		}
	}

	if !inCatalog(c.Persona.Length, lengthOptions) {
		add("persona.length", "debe ser breve, media o detallada")
	}
	if !inCatalog(c.Safety.Level, safetyLevels) {
		add("safety.level", "debe ser bajo, medio o alto")
	}

	if len(c.Sales.Goals) == 0 {
		add("sales.goals", "elige al menos un objetivo")
	}
	if dup := firstDuplicate(c.Sales.Goals); dup != "" {
		add("sales.goals", "objetivo repetido: "+dup)
	}
	for _, g := range c.Sales.Goals {
		if !inCatalog(g, SalesGoalCatalog) {
			add("sales.goals", "objetivo desconocido: "+g)
		}
	}

	if dup := firstDuplicate(c.Safety.Forbid); dup != "" {
		add("safety.forbid", "prohibición repetida: "+dup)
	}
	for _, f := range c.Safety.Forbid {
		if !inCatalog(f, SafetyForbidCatalog) {
			add("safety.forbid", "prohibición desconocida: "+f)
		}
	}

	if len([]rune(c.Note)) > maxNoteLen {
		add("note", fmt.Sprintf("máximo %d caracteres", maxNoteLen))
	}
	if c.Status != "" && c.Status != "draft" && c.Status != "published" {
		add("status", "debe ser draft o published")
	}
	return errs
}

func inCatalog(v string, catalog []string) bool {
	for _, c := range catalog {
		if c == v {
			return true
		}
	}
	return false
}

func firstDuplicate(values []string) string {
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if seen[v] {
			return v
		}
		seen[v] = true
	}
	return ""
}
