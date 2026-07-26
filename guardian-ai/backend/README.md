# Backend — Guardian AI

Go 1.22 + Fiber v2.52. Un binario, `package main`, 12.635 líneas en 48 archivos.
Cuatro dependencias directas.

```bash
go run .          # :3000
go test ./...     # 22 suites, sin red ni claves
```

## Responsabilidad

Orquestar la conversación de punta a punta: recibir el mensaje (WhatsApp, voz o
web), resolver el estado del lead, recuperar contexto, llamar al modelo,
ejecutar herramientas contra la API de Colsubsidio, responder, y **emitir cada
paso como evento inmutable**.

**Entradas** — webhook de Kapso, ingesta de Vapi, simulación, Playground.
**Salidas** — mensaje al afiliado, eventos al store, fan-out por WebSocket.
**Dependencias** — OpenRouter/OpenAI, Kapso, Vapi, ElevenLabs, Colsubsidio
Protege, Supabase. Todas con modo degradado; ver
[arquitectura](../../docs/architecture/README.md#tolerancia-a-fallos).

## Mapa de archivos

### Núcleo conversacional

| Archivo | Líneas | Responsabilidad |
|---|---|---|
| `guardian.go` | 1.048 | Motor conversacional. El turno completo |
| `orchestrator.go` | 204 | Coordina el turno y el orden de los eventos |
| `statemachine.go` | 303 | Los 9 estados y sus transiciones válidas |
| `conversation.go` | 193 | Estado de una conversación en curso |
| `closing.go` | 344 | Flujo de cierre: cotización y matrícula |
| `memory.go` | 51 | Variables capturadas del afiliado |

### Prompt y recuperación

| Archivo | Líneas | Responsabilidad |
|---|---|---|
| `promptbuilder.go` | 239 | Arma el prompt por capas |
| `rag.go` | 216 | Chunking, embeddings en memoria, coseno |
| `tools.go` | 124 | Definición y ejecución de herramientas |
| `agentconfig.go` | 435 | Persona y perillas de comportamiento |
| `configstore.go` | 290 | Versionado: draft, publish, rollback |

### Integraciones

| Archivo | Líneas | Responsabilidad |
|---|---|---|
| `openrouter.go` | — | Cliente LLM principal (`claude-sonnet-4`) |
| `openai.go` | 360 | Fallback (`gpt-4o`) y embeddings |
| `kapso.go` | 328 | WhatsApp Business API |
| `whatsapp_sessions.go` | 139 | Sesiones persistidas por teléfono |
| `vapi.go` / `voice.go` | 76 / 64 | Telefonía y TTS |
| `colsubsidio.go` | 357 | Cliente de la API de Colsubsidio |
| `protege.go` | 339 | Motor de reglas de Protege |
| `affiliates.go` | 234 | Lógica de afiliación |

### Eventos y persistencia

| Archivo | Líneas | Responsabilidad |
|---|---|---|
| `events.go` | 94 | El envelope de 7 campos y los 22 tipos |
| `store.go` | 41 | Append-only en memoria |
| `persist.go` | 76 | Escritura a Supabase vía pgx |
| `projector.go` | 884 | Eventos → proyecciones. La pieza más grande |
| `hub.go` | 48 | Fan-out a los suscriptores de `/ws` |
| `analytics.go` | 231 | KPIs agregados |

### Superficie HTTP

| Archivo | Líneas | Responsabilidad |
|---|---|---|
| `main.go` | 627 | Rutas, arranque, `/ws` |
| `studio.go` | 321 | Agent Studio y `/ws/studio` |
| `playground.go` | 321 | Sesión aislada de pruebas |

## Variables de entorno

Ver [`.env.example`](../.env.example). Mínimo para conversar: `OPENAI_API_KEY`.
Todo lo demás activa un canal o una integración, y su ausencia se refleja en
`GET /api/capabilities` — nada falla en silencio.

## Errores

| Situación | Comportamiento |
|---|---|
| Supabase caído | Degrada a memoria; **sigue conversando** |
| Embeddings no disponibles | RAG cae a modo keyword y lo registra |
| OpenRouter caído | Fallback a OpenAI directo |
| Protege caída | Motor de reglas deshabilitado, GPT libre |
| Timeout del modelo | `ERROR_OCCURRED` con reintento |

## Ejemplo

```bash
curl -X POST localhost:8099/api/whatsapp/simulate-inbound \
  -H 'Content-Type: application/json' \
  -d '{"from":"573001234567","text":"quiero asegurar mi carro"}'
```

Produce, en orden: `CALL_STARTED` → `MESSAGE_RECEIVED` → `STATE_CHANGED` →
`KNOWLEDGE_RETRIEVED` → `LLM_REQUESTED` → `LLM_RESPONSE` → `TOOL_CALLED` →
`TOOL_EXECUTED` → `MESSAGE_SENT` → `TURN_COMPLETED`.

Míralo en vivo: `secura tail` o `websocat ws://localhost:8099/ws`.

## Ver también

- [Arquitectura](../../docs/architecture/README.md) · [API](../../docs/api/README.md) · [ADRs](../../docs/adr/README.md)
