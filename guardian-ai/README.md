# Guardian AI — MVP

Plataforma de inteligencia conversacional por voz para asesoría de seguros.
Arquitectura event-driven (Go + Event Bus + WebSocket) con dashboard Mission Control.
Basado en `PRD.md`, `02_ADR.md`, `03_SRS.md`, `srsImprove.md`, `04_EVENT_CATALOG.md`.

## Arrancar (Docker + nginx)

```bash
docker compose up -d --build
# Dashboard:  http://localhost:8099
# Botón "Simular llamada" dispara el flujo E2E completo
```

Endpoints:

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET  | `/api/health` | Healthcheck |
| POST | `/api/calls/simulate` | Dispara una llamada E2E simulada |
| GET  | `/api/calls` | Lista call_ids |
| GET  | `/api/calls/:id/events` | Replay del log de eventos (RN-003) |
| WS   | `/ws` | Stream de eventos al dashboard (solo lectura, RN-004) |

## Arquitectura (qué es real vs mock en el MVP)

nginx (`:80`) sirve el dashboard estático y hace reverse-proxy de `/api` y `/ws` al backend Go (`:3000`).

**Real (backbone MVP):**
- Event Bus in-memory (ADR-006/007, pub/sub Observer)
- Máquina de estados de conversación (ADR-009)
- EventStore con replay por secuencia (ADR-015, RN-003)
- WebSocket Hub → Mission Control en tiempo real (ADR-013/016, RA-005)
- Envelope de eventos según `04_EVENT_CATALOG.md`

**Mock (swappable por adapter real, ADR-011 — sin tocar el flujo):**
- `telephony_adapter` (Vapi) · `llm_gateway` (GPT-4o) · `voice_adapter` (ElevenLabs) · `tool_engine` · persistencia (Supabase/pgx)

## Flujo funcional E2E (verificado, 63 eventos)

```
CALL_STARTED
  → STATE_CHANGED CONNECTED → DISCOVERY
  ── por cada turno ──
  USER_SPOKE → TRANSCRIPT_UPDATED(user)
    → FEATURE_UPDATED (perfil) → INTENT_DETECTED
    → LLM_REQUESTED (THINKING)
        └─ [TOOL_CALLED → TOOL_EXECUTED → LLM_REQUESTED]
    → LLM_RESPONSE
    → [RECOMMENDATION_GENERATED]
    → VOICE_SENT → TRANSCRIPT_UPDATED(agent) → STATE_CHANGED(LISTENING)
  ── cierre ──
  SUMMARY_GENERATED → CALL_ENDED → STATE_CHANGED(ENDED)
```

## Plan de sprints

### Sprint 0 — Fundaciones (MANDATORY) ✅ hecho aquí
Event Bus, envelope, máquina de estados, EventStore+replay, WS Hub, Mission Control, nginx, docker-compose, flujo E2E mockeado. **Sale de este repo.**

### Sprint 1 — Voz real entrante (MANDATORY)
- Adapter Vapi real: webhook de llamada entrante → `CALL_STARTED` / `USER_SPOKE` / `TRANSCRIPT_UPDATED`
- STT real (transcripción)
- Config/secrets (Singleton Config, ADR-008), `.env`
- **Criterio:** una llamada telefónica real emite eventos al bus.

### Sprint 2 — Cerebro (MANDATORY)
- `llm_gateway` real GPT-4o (LLM Gateway, ADR-012) con Prompt Builder por estado
- Decision Engine: intención, estrategia narrativa (Strategy, ADR-010), selección de tool
- Feature Store real → `FEATURE_UPDATED`
- **Criterio:** perfil dinámico + respuesta contextual reales.

### Sprint 3 — Voz saliente + recomendación (MANDATORY)
- Adapter ElevenLabs real → `VOICE_SENT` con audio devuelto a Vapi
- `tool_engine`: `product_search` real sobre catálogo de seguros
- `RECOMMENDATION_GENERATED` justificable (RN-001/RN-005)
- **Criterio:** el agente habla y recomienda un producto real.

### Sprint 4 — Persistencia + demo (MANDATORY)
- Repositorio Supabase/pgx (Repository, ADR-004/005) reemplaza EventStore in-memory
- `SUMMARY_GENERATED` al cierre, persistido
- Métricas de costo/tokens/latencia en dashboard (ya cableadas)
- **Criterio:** llamada reconstruible desde Supabase; demo de 5 min completa.

### Fases próximas (NO mandatory para MVP)
Langfuse (observabilidad), pgvector/RAG de productos, autenticación, multiagente, MCP, Pipecat/LiveKit, Kafka, Kubernetes, integración real de aseguradora, e-firma, pagos, WhatsApp, CRM. (Todo lo marcado *out of scope* en el PRD.)

## Config
`STEP_MS` (compose): ritmo de la simulación en ms (default 500). Bajar para demo más rápida.
