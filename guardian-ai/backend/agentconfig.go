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

// ---- del control visual a la instrucción ----
//
// Cada perilla se traduce a una FRASE por rango. Nunca se interpola el número
// crudo ("empatía = 8" no significa nada para el modelo) ni el texto que
// escriba el administrador: el vocabulario de instrucciones es cerrado, y por
// eso mover sliders no puede inyectar comportamiento arbitrario.

// scalePhrase elige la frase del tramo: 1-3 bajo, 4-7 medio, 8-10 alto.
func scalePhrase(value int, low, mid, high string) string {
	switch {
	case value <= 3:
		return low
	case value <= 7:
		return mid
	default:
		return high
	}
}

func empathyPhrase(v int) string {
	return scalePhrase(v,
		"No te detengas en emociones: responde a lo concreto que te plantean.",
		"Reconoce en una frase cómo se siente la persona antes de continuar.",
		"Nombra lo que la persona siente y valida su preocupación antes de dar cualquier dato.")
}

func formalityPhrase(v int) string {
	return scalePhrase(v,
		"Tutea con lenguaje coloquial colombiano, como alguien de confianza que sabe del tema.",
		"Tutea con registro profesional y cercano, en español colombiano.",
		"Trata de usted, con registro corporativo y sin coloquialismos.")
}

func closenessPhrase(v int) string {
	return scalePhrase(v,
		"Mantén distancia profesional: sin comentarios personales.",
		"Sé cordial y retoma lo que la persona acaba de contarte.",
		"Sé cálido y personal: recuerda detalles que compartió y nómbralos.")
}

func persuasionPhrase(v int) string {
	return scalePhrase(v,
		"Actúa como consultor: informa y deja que la persona decida, sin empujar.",
		"Sugiere el siguiente paso una vez, sin insistir si no hay interés.",
		"Propón el cierre de forma activa cuando el perfil lo justifique, nunca presionando.")
}

func proactivityPhrase(v int) string {
	return scalePhrase(v,
		"Responde lo que te preguntan; no abras temas nuevos.",
		"Cuando el tema actual quede resuelto, propón el siguiente paso.",
		"Lleva tú la conversación: en cada turno propón el siguiente paso.")
}

func lengthPhrase(length string) string {
	switch length {
	case "breve":
		return "Responde en 1-2 frases."
	case "detallada":
		return "Puedes extenderte hasta 6 frases cuando el tema lo pida."
	default:
		return "Responde en 2-4 frases, tono de chat."
	}
}

func emojiPhrase(on bool) string {
	if on {
		return "Como máximo un emoji por mensaje, y solo cuando aporte calidez."
	}
	return "No uses emojis."
}

func humorPhrase(on bool) string {
	if on {
		return "Un toque de humor amable está permitido si la persona lo propicia."
	}
	return "Sin bromas: mantén un tono serio."
}

// salesGoalLabels / salesGoalPhrases: etiqueta para la consola y redacción para
// el prompt. Ambas viven aquí para que no se desincronicen.
var salesGoalLabels = map[string]string{
	"resolver_dudas":      "Resolver dudas",
	"calificar_cliente":   "Calificar al cliente",
	"recomendar_producto": "Recomendar producto",
	"cerrar_venta":        "Cerrar venta",
	"agendar_llamada":     "Agendar llamada",
	"derivar_humano":      "Derivar a un asesor",
}

var salesGoalPhrases = map[string]string{
	"resolver_dudas":      "resolver las dudas de la persona con la información de este prompt",
	"calificar_cliente":   "descubrir conversando el perfil que el sistema necesita para calificar",
	"recomendar_producto": "llegar a la recomendación que entrega el sistema y explicarla con sus razones",
	"cerrar_venta":        "conseguir que la persona acepte formalizar con un asesor",
	"agendar_llamada":     "acordar un momento para que un asesor la llame",
	"derivar_humano":      "dejar el lead listo para que lo tome un asesor humano",
}

var safetyForbidLabels = map[string]string{
	"coberturas_inventadas":   "Coberturas inventadas",
	"promesas_falsas":         "Promesas falsas",
	"consejos_legales":        "Consejos legales",
	"consejos_medicos":        "Consejos médicos",
	"informacion_inexistente": "Información inexistente",
}

var safetyForbidPhrases = map[string]string{
	"coberturas_inventadas":   "mencionar coberturas o beneficios que no estén en el catálogo de este prompt",
	"promesas_falsas":         "prometer resultados, tiempos o precios que no vengan del sistema",
	"consejos_legales":        "dar asesoría legal",
	"consejos_medicos":        "dar asesoría médica",
	"informacion_inexistente": "afirmar datos que no aparezcan en este prompt",
}

func safetyLevelPhrase(level string) string {
	switch level {
	case "alto":
		return "Ante cualquier duda, dilo con claridad y ofrece que un asesor humano lo confirme."
	case "medio":
		return "Si no estás seguro de un dato, adviértelo antes de continuar."
	default:
		return ""
	}
}

// personaPhraseTable expone las frases de cada perilla por tramo (bajo, medio,
// alto) para que la consola muestre EXACTAMENTE la instrucción que recibirá el
// modelo al mover el slider. Una copia de estas frases en JavaScript se
// desincronizaría el día que alguien las mejore aquí.
func personaPhraseTable() map[string][]string {
	scale := func(f func(int) string) []string { return []string{f(1), f(5), f(10)} }
	return map[string][]string{
		"empathy":     scale(empathyPhrase),
		"formality":   scale(formalityPhrase),
		"closeness":   scale(closenessPhrase),
		"persuasion":  scale(persuasionPhrase),
		"proactivity": scale(proactivityPhrase),
		"length": {
			lengthPhrase("breve"), lengthPhrase("media"), lengthPhrase("detallada"),
		},
		"emojis": {emojiPhrase(false), emojiPhrase(true)},
		"humor":  {humorPhrase(false), humorPhrase(true)},
		"safety_level": {
			"Sin advertencias adicionales.", safetyLevelPhrase("medio"), safetyLevelPhrase("alto"),
		},
	}
}

// personaScaleLabels son los extremos que la consola pinta a los lados de cada
// slider (PRD: "nunca usar agresivo").
func personaScaleLabels() map[string][2]string {
	return map[string][2]string{
		"empathy":     {"Muy baja", "Muy alta"},
		"formality":   {"Casual", "Corporativa"},
		"closeness":   {"Profesional", "Amigable"},
		"persuasion":  {"Consultor", "Vendedor"},
		"proactivity": {"Reactiva", "Proactiva"},
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
