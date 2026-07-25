package main

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptSections(t *testing.T) {
	in := PromptInput{
		State: StateProfile,
		Memory: CustomerMemory{
			User:      &ProtegeUser{FirstName: "Ana", LastName: "Rojas", Phone: "+573001112233"},
			Variables: []UserVariable{{Key: "has_pet", Value: true, Source: "whatsapp"}},
		},
		Products: []ProtegeProduct{{Name: "Seguro de Vida Familiar", Category: "vida", Description: "desc", BasePrice: 25000}},
		Rules:    []ProtegeRule{{Active: true, Reason: "Tienes dependientes económicos."}},
		MissingVars: []ProtegeQuestion{
			{VariableKey: "housing_type", Text: "¿Vives en casa propia?"},
		},
		Retrieved: []Chunk{{Doc: "faq.md", Heading: "Cobertura", Text: "La cobertura aplica desde el día uno."}},
	}
	p := BuildSystemPrompt(in)

	for _, want := range []string{
		"## Persona",
		"## Reglas de negocio",
		"Seguro de Vida Familiar",
		"Tienes dependientes económicos.",
		"## Reglas de conversación",
		"## Etapa actual: PROFILE_DISCOVERY",
		"housing_type",
		"## Memoria del cliente",
		"has_pet = true",
		"Ana Rojas",
		"## Contexto documental",
		"faq.md",
		"## Formato de salida",
		ActionAsk,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt sin %q", want)
		}
	}
	// El enum de acciones respeta el estado: en PROFILE no se ofrece cerrar por
	// recomendación entregada, pero sí pedir asesor.
	if !strings.Contains(p, ActionHandoff) {
		t.Error("handoff debe ofrecerse en PROFILE_DISCOVERY")
	}
	// El intent también es enum cerrado: el prompt lo enumera.
	for _, want := range []string{"request_advisor", "objection"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt sin el intent %q del enum", want)
		}
	}
}

func TestBuildSystemPromptDegrades(t *testing.T) {
	p := BuildSystemPrompt(PromptInput{State: StateAffiliation})
	if strings.Contains(p, "Catálogo REAL") {
		t.Error("sin productos no debe haber sección de catálogo")
	}
	if strings.Contains(p, "## Memoria del cliente") {
		t.Error("sin memoria no debe haber sección de memoria")
	}
	if strings.Contains(p, "## Contexto documental") {
		t.Error("sin RAG no debe haber contexto documental")
	}
	if !strings.Contains(p, "## Persona") || !strings.Contains(p, "## Formato de salida") {
		t.Error("secciones fijas ausentes")
	}
}
