package main

import (
	"fmt"
	"strings"
)

// Prompt Builder (spec retrieval.md §5): the system prompt is MODULAR — built
// per turn from independent sections, never a single giant hardcoded prompt.
// Persona + Business Rules + Conversation Rules + State + Customer Memory +
// Retrieved Context + Available Tools. Pure functions: unit-testable, no I/O.

// PromptInput carries everything a turn knows. Empty slices simply omit their
// section, so the builder degrades gracefully (e.g. no RAG, no rules yet).
type PromptInput struct {
	State       LeadState
	Memory      CustomerMemory
	Products    []ProtegeProduct
	Rules       []ProtegeRule
	MissingVars []ProtegeQuestion // what this stage still needs to discover
	Retrieved   []Chunk           // RAG documentation snippets
	Recs        []string          // rendered recommendations (matching stage)
}

// BuildSystemPrompt assembles the modular system prompt.
func BuildSystemPrompt(in PromptInput) string {
	sections := []string{
		sectionPersona(),
		sectionBusinessRules(in),
		sectionConversationRules(),
		sectionState(in),
		sectionMemory(in),
		sectionRetrieved(in),
		sectionOutput(in),
	}
	var nonEmpty []string
	for _, s := range sections {
		if strings.TrimSpace(s) != "" {
			nonEmpty = append(nonEmpty, strings.TrimSpace(s))
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

func sectionPersona() string {
	return `## Persona
Eres "Guardian", asesor comercial de seguros de Colsubsidio por WhatsApp. Hablas español colombiano, cálido, cercano y profesional. Mensajes CORTOS (2-4 frases), tono de chat. Tu meta es entender a la persona y conectarla con la protección que de verdad le sirve.`
}

func sectionBusinessRules(in PromptInput) string {
	var b strings.Builder
	b.WriteString("## Reglas de negocio (fuente: API Colsubsidio — ÚNICA verdad)\n")
	b.WriteString("Las decisiones de elegibilidad, subsidios y recomendación las toma el sistema, NUNCA tú.\n")
	if len(in.Products) > 0 {
		b.WriteString("Catálogo REAL (solo puedes mencionar estos productos):\n")
		for _, p := range in.Products {
			fmt.Fprintf(&b, "- %s (%s): %s. Precio base $%.0f/mes.\n", p.Name, p.Category, p.Description, p.BasePrice)
		}
	}
	if len(in.Rules) > 0 {
		b.WriteString("Criterios del motor (para EXPLICAR, no para decidir):\n")
		for _, r := range in.Rules {
			if r.Active && r.Reason != "" {
				fmt.Fprintf(&b, "- %s\n", r.Reason)
			}
		}
	}
	return b.String()
}

func sectionConversationRules() string {
	return `## Reglas de conversación
- NUNCA inventes productos, precios, subsidios, reglas ni beneficios. Si no está en este prompt, no existe.
- Máximo UNA pregunta por mensaje, y siempre conectada a lo que la persona acaba de contar. Jamás interrogatorio ni formulario.
- No repitas preguntas sobre datos que ya conoces (sección Memoria).
- Si la persona menciona un dato que YA está en Memoria pero con otro valor (por ejemplo dice sus ingresos reales y la memoria trae un estimado), extráelo igual como entity: lo que dice el cliente MANDA sobre el estimado.
- Si la persona pregunta algo informativo, respóndelo con el Contexto documental; si no está ahí, dilo honestamente y ofrece que un asesor lo confirme.
- Detecta objeciones y respóndelas con empatía antes de avanzar.
- Si es natural, invita UNA vez a compartir el número de afiliado Colsubsidio para personalizar mejor ("si tienes a mano tu número de afiliado, lo reviso y te ahorro preguntas"). Si la persona lo comparte, extráelo como entity con key "affiliate_serie". Nunca insistas si no lo tiene.`
}

func sectionState(in PromptInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Etapa actual: %s\nObjetivo: %s\n", in.State, StateGoal(in.State))
	if len(in.MissingVars) > 0 {
		b.WriteString("Aún falta descubrir (guía, NO lista de preguntas literales):\n")
		for _, q := range in.MissingVars {
			if len(q.Options) > 0 {
				fmt.Fprintf(&b, "- %s (%s) — valores permitidos: %v\n", q.VariableKey, q.Text, q.Options)
			} else {
				fmt.Fprintf(&b, "- %s (%s) — tipo %s\n", q.VariableKey, q.Text, q.FieldType)
			}
		}
		b.WriteString("Al extraer entities usa EXACTAMENTE esos variable_key y, si hay valores permitidos, uno de ellos literal.\n")
	}
	if len(in.Recs) > 0 {
		b.WriteString("Recomendaciones ENTREGADAS POR EL SISTEMA (explícalas con sus razones exactas):\n")
		for _, r := range in.Recs {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	return b.String()
}

func sectionMemory(in PromptInput) string {
	m := in.Memory
	if m.User == nil && len(m.Variables) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Memoria del cliente (reconstruida desde la API — datos confirmados)\n")
	if m.User != nil {
		if m.User.FirstName != "" {
			fmt.Fprintf(&b, "- Nombre: %s %s\n", m.User.FirstName, m.User.LastName)
		}
		if m.User.Phone != "" {
			fmt.Fprintf(&b, "- Teléfono: %s\n", m.User.Phone)
		}
	}
	for _, v := range m.Variables {
		fmt.Fprintf(&b, "- %s = %v (fuente %s)\n", v.Key, v.Value, v.Source)
	}
	return b.String()
}

func sectionRetrieved(in PromptInput) string {
	if len(in.Retrieved) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Contexto documental (úsalo SOLO para explicar; nunca para decidir elegibilidad ni calcular)\n")
	for _, c := range in.Retrieved {
		fmt.Fprintf(&b, "[%s — %s]\n%s\n", c.Doc, c.Heading, c.Text)
	}
	return b.String()
}

func sectionOutput(in PromptInput) string {
	actions := AllowedActions(in.State)
	list := strings.Join(actions, " | ")
	if list == "" {
		list = ActionClose
	}
	return fmt.Sprintf(`## Formato de salida (obligatorio)
Responde SOLO el JSON del esquema. Campos:
- intent: intención del cliente en este mensaje, UNA de [%s].
- entities: hechos NUEVOS y confirmados que el cliente reveló, como pares {key, value, confidence}. Usa exactamente las variable_key de la guía cuando apliquen. Si dudas, confidence < 0.6.
- confidence: confianza global de tu lectura del mensaje (0-1).
- next_action: una de [%s]. Es una PROPUESTA; el sistema decide.
- assistant_message: tu respuesta al cliente (texto natural de WhatsApp, sin JSON).`,
		strings.Join(guardianIntents, ", "), list)
}
