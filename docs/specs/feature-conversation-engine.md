# 07 — Guardian Conversation Engine (WhatsApp)

> Implementa el requerimiento técnico `retrieval.md` (Guardian Conversation Engine, MVP).
> Estado: implementado y verificado contra el mock de la Protege API.

## 1. Objetivo

Convertir un lead de WhatsApp en un **Lead Ready for Advisor**: perfilado con
naturalidad (nunca formulario), calificado por reglas de negocio y entregado al
asesor con todo el contexto. **No es un chatbot**: es un motor comercial.

Principio rector (spec §2.1): el LLM solo **entiende, extrae y explica**.
Elegibilidad, reglas, scoring y recomendaciones vienen SIEMPRE de la
Colsubsidio Protege API.

## 2. Arquitectura

```
WhatsApp ── Kapso (gateway) ── /api/whatsapp/webhook
                                     │
                          GuardianEngine (guardian.go)
                                     │
              ┌─────────────┬────────┴────────┬──────────────┐
        State Machine   Prompt Builder   Memory Manager    RAG
       (statemachine.go)(promptbuilder.go)  (memory.go)   (rag.go)
              │                                   │
        Tools registry (tools.go) ───── Colsubsidio Protege API (colsubsidio.go)
              │
         LLM Gateway (openai.go · DecideGuardian, json_schema strict)
```

- **Gateway**: Kapso (proxy Meta Cloud API), ya existente (`kapso.go`); abstrae
  el canal por completo. *Alternativas documentadas*: Baileys / Evolution API /
  WPPConnect (requieren servicio Node aparte; fuera del MVP).
- **Canal-agnóstico**: el engine solo toca el EventBus y las sesiones; el mismo
  motor puede servir voz (ElevenLabs) o web chat cambiando el adaptador.

## 3. Cadena de preferencia (graceful degradation)

`waInbound` y `/api/chat/start` eligen el cerebro disponible:

1. **GuardianEngine** — requiere `COLSUBSIDIO_API_URL` + `OPENAI_API_KEY`.
2. **ProtegeEngine** (flujo rígido de preguntas de la API) — solo API.
3. **Motor GPT-4o libre** — solo key de OpenAI.
4. 503.

`GUARDIAN_DISABLED=1` fuerza el fallback (comparación en vivo para demo).

## 4. Máquina de estados del lead (spec §3.3)

```
NEW → AFFILIATION_CHECK → PROFILE_DISCOVERY → FINANCIAL_QUALIFICATION
    → PROJECT_MATCHING → READY_FOR_ADVISOR → COMPLETED
                       ↘ NURTURING → COMPLETED
```

- Transiciones SOLO por las flechas anteriores (`CanTransition`); una ilegal
  emite `ERROR_OCCURRED{code:illegal_transition}` y no se aplica.
- **El engine decide las transiciones, nunca el LLM**: avanza cuando el set de
  `variable_key` requeridas de la etapa está completo (derivado de
  `GET /questions`, particionado perfil/financiero, respetando `conditions`).
- Cada transición se emite como `STATE_CHANGED{from,to,reason}` → persistida.

## 5. Mini-ADR: tools determinísticas (no tool-calling nativo)

**Decisión**: el LLM devuelve `next_action` de un enum cerrado
(`ask | answer_question | request_recommendation | handoff | close`) dentro del
structured output; el engine valida contra la whitelist del estado
(`AllowedActions`) y ejecuta él mismo las tools registradas.

**Por qué**: (1) cumple literalmente "no se permitirán llamadas arbitrarias";
(2) evita loops multi-turn no determinísticos en demo; (3) hay tools que deben
dispararse sin intervención del LLM (`search_user` al primer contacto,
`save_variable` apenas se confirma un dato). Se emiten `TOOL_CALLED` /
`TOOL_EXECUTED` reales con latencia y error.

Registry cerrado (`tools.go`): `search_user, create_user, get_questions,
save_variable, get_rules, get_recommendations, create_conversation,
update_conversation, complete_conversation` (+ lecturas `get_products`,
`get_variables`). Retry: 1, solo GETs idempotentes.

## 6. Flujo por turno

1. `MESSAGE_RECEIVED` + `TRANSCRIPT_UPDATED(user)`.
2. **Pre-LLM determinístico**: primer contacto → `search_user`/`create_user` +
   `create_conversation` (la afiliación la responde la API, no el LLM);
   memoria estratégica SIEMPRE reconstruida con `get_variables` (spec §6).
3. RAG si el texto parece pregunta → `KNOWLEDGE_RETRIEVED` (solo documentación).
4. Prompt modular (`BuildSystemPrompt`: Persona + Business Rules con el catálogo
   REAL + Conversation Rules + Estado + Memoria + Contexto + Formato) →
   `DecideGuardian` con `response_format: json_schema strict`
   (`{intent, entities[], confidence, next_action, assistant_message}`) →
   `LLM_RESPONSE` (tokens/costo/latencia reales) + `INTENT_DETECTED`.
5. **Post-LLM determinístico**:
   - Entidades con `confidence ≥ 0.6` → `save_variable` INMEDIATO
     (`PUT /users/{id}/variables`, spec fase 3) + `FEATURE_UPDATED` por variable.
     Confianza baja → no se persiste; el mensaje pide confirmación.
   - Etapa completa → transición; al entrar a `PROJECT_MATCHING` →
     `get_recommendations` (la API decide) → `RECOMMENDATION_GENERATED` +
     explicación con las `reason` exactas de las reglas.
   - Aceptación/handoff en matching → `LEAD_READY` + `SUMMARY_GENERATED` +
     `CALL_ENDED` (aparece en /pipeline). Objeción/cierre → `NURTURING` (stub).
6. `MESSAGE_SENT` + `TRANSCRIPT_UPDATED(agent)` + `TURN_COMPLETED`
   (observabilidad por mensaje, spec §11).

## 7. RAG (spec §9)

Corpus local `backend/knowledge/` (`faq.md`, `subsidios.md`, `productos.md`,
`glosario.md`). Chunk por heading; embeddings `text-embedding-3-small` al
arranque (1 request batch) + cosine top-k; sin key degrada a keyword matching
(`mode` visible en el evento). Prohibido para reglas/decisiones/scoring — el
prompt lo marca: "úsalo SOLO para explicar".

## 8. Observabilidad y KPIs

- `TURN_COMPLETED` por mensaje: estado, intent, confidence, latencia total,
  tools ejecutadas, variables nuevas, error.
- `call_analytics` ganó columnas aditivas: `lead_state, tokens_in, tokens_out,
  cost_usd, avg_llm_latency_ms, tool_calls, variables_captured` (migración
  `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`, aplicada).
- `GET /api/analytics/kpis`: leads WhatsApp, leads listos, en nutrición, tool
  calls, tokens, costo, latencia LLM promedio.
- UI `/chat`: stepper de etapas del lead en vivo, panel "Perfil capturado"
  (FEATURE_UPDATED), feed de tools/eventos, tarjeta "🏆 Lead listo para asesor".
- `/pipeline`: estado del lead + costo/tokens por conversación.

## 9. Cómo verificar

```bash
# build + tests (backend y mock)
docker run --rm -v "$PWD":/repo -w /repo golang:1.22-alpine sh -c \
  'cd backend && go vet ./... && go test ./... && cd ../mock-protege && go build ./...'

# stack completo (mock protege incluido)
docker compose up -d --build

# lead entrante natural (el cliente escribe primero)
curl -X POST localhost:8099/api/whatsapp/simulate-inbound \
  -H 'Content-Type: application/json' \
  -d '{"from":"+573001112233","text":"Hola, soy Ana, tengo 2 hijos y un perro"}'

# variables persistidas EN VIVO en la API (momento demo)
curl localhost:9000/api/v1/users/<user_id>/variables

# KPIs
curl localhost:8099/api/analytics/kpis
```

Guion demo (4 mensajes): saludo+familia+mascota → vivienda/movilidad →
ingresos/ahorro → "sí, quiero el asesor" → tarjeta LEAD_READY + /pipeline.

## 10. Fuera de scope del MVP (honesto)

- NURTURING real (solo estado + mensaje; sin campañas/remarketing).
- KPIs de tiempo-hasta-perfilamiento y conversión por canal.
- Handoff a CRM (solo evento `LEAD_READY` + tarjeta UI).
- Multimedia entrante (el gateway filtra a texto).
- Voz con el engine nuevo (compatible por diseño; no cableada).
- Fine-tuning, multiagentes, pgvector, colas de reintento.
- **La API real (147.93.11.136:9000) es inalcanzable desde el entorno de
  desarrollo (firewall)**: todo se valida contra `mock-protege`, que replica el
  contrato OpenAPI; en producción basta apuntar `COLSUBSIDIO_API_URL`.
