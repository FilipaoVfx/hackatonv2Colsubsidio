package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Fase 0 del Agent Studio: la configuración entra al motor SIN cambiar el
// comportamiento y SIN reabrir la carrera que costó el fallo 1 del informe de
// robustez. Estos tests son la red que lo sostiene.

// promptFixture es la entrada representativa con la que se congela el prompt.
func promptFixture() PromptInput {
	return PromptInput{
		State: StateProfile,
		Memory: CustomerMemory{
			User: &ProtegeUser{ID: "user-1", Phone: "+573000000001", FirstName: "Ana"},
			Variables: []UserVariable{
				{Key: "has_pet", Value: true, Source: "whatsapp"},
			},
		},
		Products: []ProtegeProduct{
			{ID: "p1", Name: "Seguro Mascota Protegida", Category: "mascotas",
				Description: "Cubre urgencias veterinarias", BasePrice: 22000, Active: true},
		},
		Rules: []ProtegeRule{
			{ID: "r1", ProductID: "p1", VariableKey: "has_pet", Operator: "equals",
				ExpectedValue: true, Weight: 0.7, Reason: "Tienes mascota.", Active: true},
		},
		MissingVars: []ProtegeQuestion{
			{ID: "q1", VariableKey: "full_name", FieldType: "text", Required: true, Text: "¿Nombre?"},
		},
	}
}

// TestPromptDefaultGolden congela el prompt que produce la configuración por
// defecto. En la fase 0 servía para probar que la consola no cambiaba nada; en
// la fase 2, con la persona ya compuesta desde la configuración, sirve para lo
// que de verdad importa a partir de ahora: que el comportamiento del agente de
// fábrica no se mueva por accidente. Cambiar este archivo es una decisión que
// se revisa en el diff, no un efecto colateral.
//
// El texto por defecto conserva la sustancia del prompt original (español
// colombiano, trato cordial, 2-4 frases, un emoji como mucho) y añade los
// objetivos y los límites que ahora son explícitos.
func TestPromptDefaultGolden(t *testing.T) {
	got := BuildSystemPrompt(promptFixture())
	path := filepath.Join("testdata", "prompt_default.golden")

	want, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("fixture creada en %s a partir del comportamiento actual; vuelve a ejecutar los tests", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("el prompt por defecto cambió.\n--- esperado ---\n%s\n--- obtenido ---\n%s", want, got)
	}
}

func TestEngineConfigDefaultsWhenUnset(t *testing.T) {
	var nilEngine *GuardianEngine
	if got := nilEngine.Config(); got.Persona.AgentName != "Guardian" {
		t.Errorf("un motor nil debe devolver los defaults, hubo %+v", got.Persona)
	}
	e := &GuardianEngine{}
	if got := e.Config(); got.Version != 0 {
		t.Errorf("sin configuración publicada se esperan los defaults, hubo v%d", got.Version)
	}
	cfg := DefaultConfig()
	cfg.Version = 4
	cfg.Persona.Empathy = 2
	e.SetConfig(cfg)
	if got := e.Config(); got.Version != 4 || got.Persona.Empathy != 2 {
		t.Errorf("SetConfig no se reflejó: %+v", got)
	}
	// El snapshot es una copia: mutar el original no toca al motor.
	cfg.Persona.Empathy = 9
	cfg.Sales.Goals[0] = "cerrar_venta"
	if got := e.Config(); got.Persona.Empathy != 2 || got.Sales.Goals[0] == "cerrar_venta" {
		t.Error("el motor comparte memoria con quien publicó la configuración")
	}
}

// TestConfigVersionStampedOnTurn: cada turno declara con qué configuración se
// comportó el agente. Sin esa marca, una conversación del pipeline no se puede
// explicar después ("¿por qué respondió así?").
func TestConfigVersionStampedOnTurn(t *testing.T) {
	srv := guardianAPI(t)
	defer srv.Close()
	g, cap := newGuardianForTest(t, srv.URL, &scriptedLLM{})

	cfg := DefaultConfig()
	cfg.Version = 7
	g.SetConfig(cfg)

	if _, err := g.HandleInbound(context.Background(), "+573000000011", "hola"); err != nil {
		t.Fatal(err)
	}
	tc, ok := cap.first(TURN_COMPLETED)
	if !ok {
		t.Fatal("sin TURN_COMPLETED")
	}
	if tc.Payload["config_version"] != 7 {
		t.Errorf("TURN_COMPLETED.config_version = %v, want 7", tc.Payload["config_version"])
	}
	lr, ok := cap.first(LLM_REQUESTED)
	if !ok {
		t.Fatal("sin LLM_REQUESTED")
	}
	if lr.Payload["config_version"] != 7 {
		t.Errorf("LLM_REQUESTED.config_version = %v, want 7", lr.Payload["config_version"])
	}
}

// TestPublishDuringTurnsIsRaceFree: publicar mientras el bot conversa es el
// caso normal en una demo. El turno en curso termina con su snapshot y el
// siguiente ve la versión nueva — nunca las dos a la vez.
func TestPublishDuringTurnsIsRaceFree(t *testing.T) {
	srv := guardianAPI(t)
	defer srv.Close()
	llm := &slowLLM{decision: GuardianDecision{
		Intent: "provide_info", Confidence: 0.9, NextAction: ActionAsk, AssistantMessage: "ok",
	}}
	g, cap := newGuardianForTest(t, srv.URL, llm)
	g.SetConfig(DefaultConfig())

	// Un solo cliente conversando: el paralelismo entre clientes ya lo cubre
	// TestGuardianConcurrentInboundSerialized. Aquí lo que se pone a prueba es
	// publicar mientras el motor responde. (La API de pruebas devuelve siempre
	// la misma conversation_id, así que varios teléfonos compartirían estado —
	// en producción cada conversación tiene su id.)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for turno := 0; turno < 9; turno++ {
			if _, err := g.HandleInbound(context.Background(), "+573000000021", "hola"); err != nil {
				t.Errorf("inbound: %v", err)
				return
			}
		}
	}()
	// publicaciones simultáneas
	wg.Add(1)
	go func() {
		defer wg.Done()
		for v := 1; v <= 10; v++ {
			cfg := DefaultConfig()
			cfg.Version = v
			g.SetConfig(cfg)
			time.Sleep(2 * time.Millisecond)
		}
	}()
	wg.Wait()

	// Cada turno declaró UNA versión, y todas son versiones que existieron.
	cap.mu.Lock()
	defer cap.mu.Unlock()
	seen := 0
	for _, ev := range cap.events {
		if ev.Type != TURN_COMPLETED {
			continue
		}
		seen++
		v, ok := ev.Payload["config_version"].(int)
		if !ok || v < 0 || v > 10 {
			t.Fatalf("versión de configuración inesperada en un turno: %v", ev.Payload["config_version"])
		}
	}
	if seen != 9 {
		t.Errorf("turnos completados = %d, want 9", seen)
	}
}

// TestStoreToEngineWiring: el camino real de arranque — el store carga del
// disco y el motor queda con esa configuración.
func TestStoreToEngineWiring(t *testing.T) {
	dir := t.TempDir()
	store := NewConfigStore(dir, nil)
	draft := DefaultConfig()
	draft.Persona.AgentName = "Asesora Colsubsidio"
	if _, errs, err := store.SaveDraft(draft); err != nil || len(errs) > 0 {
		t.Fatalf("SaveDraft: %v %v", err, errs)
	}
	if _, errs, err := store.Publish("nombre nuevo"); err != nil || len(errs) > 0 {
		t.Fatalf("Publish: %v %v", err, errs)
	}

	e := &GuardianEngine{}
	e.SetConfig(NewConfigStore(dir, nil).Published())
	got := e.Config()
	if got.Persona.AgentName != "Asesora Colsubsidio" || got.Version != 1 {
		t.Errorf("motor arrancó con %q v%d", got.Persona.AgentName, got.Version)
	}
	if !strings.HasSuffix(store.path, configFileName) {
		t.Errorf("ruta del archivo inesperada: %s", store.path)
	}
}
