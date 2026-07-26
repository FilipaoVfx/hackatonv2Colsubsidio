# API

Base local: `http://localhost:8099` (nginx → backend :3000).
Sin autenticación: el backend está pensado para correr detrás de un proxy.
Ver [seguridad](../security.md#exposición-de-la-api).

Todas las respuestas son JSON. Los ejemplos son **capturas reales** del sistema
corriendo, no invenciones.

---

## Salud e inventario

### `GET /api/health`

Latido del servicio. Es lo primero que debe responder tras `make dev`.

```bash
curl -s localhost:8099/api/health
```

```json
{"llm":true,"service":"guardian-ai","status":"ok","time":"2026-07-26T18:44:27Z"}
```

`llm: false` significa que falta `OPENAI_API_KEY` — el servicio arranca igual,
pero no conversa.

### `GET /api/capabilities`

Qué integraciones están realmente configuradas. Cada campo es un booleano
derivado de si la variable de entorno correspondiente existe y responde.

```bash
curl -s localhost:8099/api/capabilities
```

```json
{
  "colsubsidio": true,
  "elevenlabs": true,
  "guardian": true,
  "llm": true,
  "vapi": true,
  "vapi_web": true,
  "whatsapp": true
}
```

> ⚠️ **La respuesta real también incluye `vapi_public_key` y
> `vapi_assistant_id`.** Son necesarios para el widget de voz web, pero ningún
> cliente debe imprimirlos ni registrarlos. La CLI los omite deliberadamente
> (ver [`types.go`](../../guardian-ai/cli/internal/api/types.go)). Detalle en
> [seguridad](../security.md).

---

## Conversaciones

### `GET /api/calls`

IDs de todas las conversaciones, más recientes primero.

```json
["ddf95b6a-b615-4d4a-ba03-b2cd1c92dedf", "569c603e-5b08-4058-b772-04c6d6a5c8c0"]
```

> **Defecto conocido:** el listado ocasionalmente contiene entradas malformadas
> como `"ics/calls/53701544-8c04-498e-86d3-5b"` — un `call_id` truncado por un
> path mal parseado en la ingesta. Los clientes deben validar el formato UUID
> antes de usarlo. Registrado en [known-issues](../reference/known-issues.md).

### `GET /api/calls/:id/events`

El log completo de una conversación, ordenado por `sequence`. Esta es la fuente
de verdad: todo lo demás son proyecciones de esto.

```bash
curl -s localhost:8099/api/calls/<call_id>/events | jq '.[0]'
```

```json
{
  "event_id": "2f1c...",
  "type": "CALL_STARTED",
  "call_id": "b7e3849c-f18b-4e7b-83ad-3525e89f286c",
  "sequence": 1,
  "timestamp": "2026-07-26T18:10:22Z",
  "producer": "guardian",
  "payload": {"channel": "whatsapp", "from": "+5730...", "engine": "guardian"}
}
```

Úsalo para **rehidratar tras perder el WebSocket**: compara el último
`sequence` conocido con el del log.

### `POST /api/calls/start` · `POST /api/calls/:id/turn` · `POST /api/calls/:id/end`

Ciclo de vida de una llamada de voz. `turn` acepta el texto transcrito del
usuario y devuelve la respuesta del agente.

### `POST /api/calls/simulate`

Genera una conversación sintética completa. Útil para poblar el dashboard sin
gastar tokens de un canal real.

---

## WhatsApp

### `POST /api/whatsapp/webhook`

Entrada real desde Kapso. Verifica HMAC contra `KAPSO_WEBHOOK_SECRET` cuando
está configurado. No lo llames a mano.

### `POST /api/whatsapp/simulate-inbound`

**El endpoint de la demo.** Inyecta un mensaje como si viniera de WhatsApp y
recorre el sistema entero: estado, RAG, LLM, herramientas, persistencia.

```bash
curl -X POST localhost:8099/api/whatsapp/simulate-inbound \
  -H 'Content-Type: application/json' \
  -d '{"from":"573001234567","text":"quiero asegurar mi carro"}'
```

> El backend persiste la conversación **por número de teléfono**. Reusar el
> mismo `from` continúa la conversación anterior en vez de empezar una nueva —
> por eso la CLI genera un número fresco en cada disparo.

### `GET /api/whatsapp/sessions`

Sesiones activas y su última actividad.

```json
[{"phone":"+573004419188","conversation_id":"c2e58221-...","last_activity":"2026-07-26T18:01:46Z"}]
```

### `GET /api/whatsapp/debug`

Volcado del estado interno del canal. Solo diagnóstico.

---

## Voz

| Endpoint | Qué hace |
|---|---|
| `POST /api/phone/call` | Lanza una llamada saliente vía Vapi |
| `POST /api/vapi/ingest` | Recibe transcripción de Vapi y la mete al mismo pipeline de eventos |
| `POST /api/tts` | Sintetiza voz con ElevenLabs |
| `POST /api/chat/start` | Abre una sesión de chat web |

La ingesta de voz produce exactamente los mismos eventos que WhatsApp. Es lo que
permite que el pipeline de la CLI no distinga canal.

---

## Analítica

### `GET /api/analytics/kpis`

Métricas agregadas, **todas medidas**, ninguna estimada.

```json
{
  "avg_llm_latency_ms": 1438.03,
  "cost_usd": 1.28961,
  "leads_nurturing": 0,
  "leads_ready": 21,
  "leads_whatsapp": 39,
  "tokens_in": 452298,
  "tokens_out": 14772,
  "tool_calls": 300,
  "variables_captured": 254
}
```

### `GET /api/analytics/calls` · `GET /api/analytics/calls/:id`

Proyecciones por conversación: transcripción, fases, scores, insights.

> Devuelve **404 si la conversación es demasiado reciente**: las proyecciones
> se escriben tras el primer turno completo. No es un error, es el estado
> normal de una llamada recién nacida. Trátalo como vacío, no como fallo.

Requiere Supabase. Sin él responde `503` con mensaje explícito.

---

## Agent Studio

Configuración del agente en caliente, con versionado y rollback.

| Endpoint | Método | Qué hace |
|---|---|---|
| `/api/studio/prompt` | GET | Prompt del sistema publicado |
| `/api/studio/config` | GET | Config completa: `draft` + `published` |
| `/api/studio/config/draft` | PUT | Guarda borrador — **no** afecta producción |
| `/api/studio/config/publish` | POST | Promueve el borrador a producción |
| `/api/studio/config/rollback/:version` | POST | Vuelve a una versión anterior |
| `/api/studio/versions` | GET | Historial con autor, nota y fecha |

```bash
curl -s localhost:8099/api/studio/versions | jq '.versions[0]'
```

> ⚠️ `publish` y `rollback` **escriben en el agente que atiende clientes
> reales**. Cualquier cliente que los exponga debe confirmar en dos pasos. La
> CLI lo hace, y ofrece `--read-only` para bloquearlos por completo en la
> sesión pública.

### Playground

Sesión aislada para probar un borrador sin tocar producción.

| Endpoint | Método |
|---|---|
| `/api/studio/playground` | GET — estado y disponibilidad |
| `/api/studio/playground/start` | POST |
| `/api/studio/playground/message` | POST |
| `/api/studio/playground/reset` | POST |

Apunta a `STUDIO_API_URL`, un contenedor Protege distinto del de producción,
justamente para que los usuarios de ensayo no se mezclen con los reales.

---

## WebSocket

### `GET /ws`

Todos los eventos del sistema, en vivo. Es el endpoint que hace posible la CLI.

```bash
# Verificar que hace upgrade (usa --http1.1: HTTP/2 no soporta este handshake)
curl -sI --http1.1 -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  localhost:8099/ws
# → HTTP/1.1 101 Switching Protocols
```

Cada frame es un `Event` completo. Sin historial al conectar: para eso está
`GET /api/calls/:id/events`.

### `GET /ws/studio`

Stream del Playground: tokens del modelo conforme se generan.

---

## Códigos de error

| Código | Significado | Qué hacer |
|---|---|---|
| `404` en `/api/analytics/calls/:id` | Proyección aún no escrita | Tratar como vacío |
| `503` en `/api/analytics/*` | Sin Supabase | Mostrar estado vacío honesto |
| `426` al probar `/ws` con curl | curl negoció HTTP/2 | Añadir `--http1.1` |
| `502` | Dependencia externa caída | Reintentar; revisar `/api/capabilities` |

## Ver también

- [Catálogo de eventos](../architecture/event-catalog.md)
- [Arquitectura](../architecture/README.md)
- [Colección de ejemplos curl](examples.md)
