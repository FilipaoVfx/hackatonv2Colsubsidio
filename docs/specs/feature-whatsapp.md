# 06 — Feature: Chat por WhatsApp (canal sobre Colsubsidio Protege API)

> **Guardian AI** · Documentación detallada de feature
> Estado: **implementado (demo offline + código real cableado)** · Sigue la convención de `05_FEATURE_LLAMADAS_VOZ_PIPELINE.md` §0.
> Cubre: el canal WhatsApp como adaptador frente a la **Colsubsidio Protege API** (motor de conversación guiada + recomendación), su transporte (Kapso, proxy del Meta Cloud API), y el reuso de eventos/analítica del Pipeline.

---

## 1. Propósito y alcance

WhatsApp es una **variante de canal** del sistema autónomo de contacto de venta de seguros. A diferencia de la voz —donde GPT-4o conduce libremente— el chat WhatsApp **gira en torno a la Colsubsidio Protege API** (`http://147.93.11.136:9000`, OpenAPI 0.1.0):

- La **API es el cerebro**: posee los usuarios, las **preguntas dinámicas**, las **variables** de perfil, las **reglas** y las **recomendaciones**. Su enum de canal incluye `whatsapp` de forma nativa.
- **WhatsApp es el canal**: transporte (Kapso) + UX de texto.
- **GPT-4o baja a NLU** (opcional): mapear texto libre del cliente → `value` tipado según el `field_type` de la pregunta. Hoy se hace con coerción local (`coerceAnswer`); GPT es un hook opcional.

Dos flujos: **saliente autónomo** (el sistema abre la conversación) y **entrante** (el cliente responde). Sin la API configurada, WhatsApp cae al motor GPT-4o local (fallback).

---

## 2. Arquitectura y flujo

### 2.1 Vista general

```
Cliente WhatsApp ─┐                         ┌─ Kapso (envío real)
                  ▼                         │
 webhook / simulate-inbound → ProtegeEngine ┤→ MESSAGE_SENT → delivery consumer ─┘
                                  │         └─ ws_hub (UI en vivo /chat)
                                  ▼
                     Colsubsidio Protege API
        users.search/create · conversations · answers · complete
                                  │
                     (emite eventos internos: CALL_STARTED, TRANSCRIPT_UPDATED,
                      MESSAGE_*, FEATURE_UPDATED, RECOMMENDATION_GENERATED, CALL_ENDED)
                                  ▼
                         Projector → Pipeline (channel=whatsapp)
```

### 2.2 Secuencia (una conversación WhatsApp)

1. **Inbound / start** → `ProtegeEngine`.
2. `GET /api/v1/users/search?phone=` → si vacío `POST /api/v1/users` (**buscar-o-crear**).
3. `POST /api/v1/conversations {user_id, channel:"whatsapp", external_session_id:phone}` → devuelve `next_question`.
4. Se envía `next_question.text` al cliente (opener = saludo + primera pregunta).
5. Respuesta del cliente → `coerceAnswer(field_type, texto)` → `POST /conversations/{id}/answers {question_id, value, source:"whatsapp"}` → siguiente pregunta.
6. Loop hasta `can_generate_recommendation` / `status=ready_for_recommendation`.
7. `POST /conversations/{id}/complete?limit=3` → recomendaciones → se envían al cliente.

El **`call_id` interno = el `conversation.id` de la API**, así los eventos, las sesiones y la API comparten un único identificador.

### 2.3 Reuso vs cambio

| Componente | Estado |
|------------|--------|
| Transporte Kapso (`kapso.go`), sesiones (`whatsapp_sessions.go`), `chat.html/js`, eventos `MESSAGE_RECEIVED/SENT`, badge Pipeline, projector | **reusado** |
| Selección de canal en `conversation.go` (fallback GPT) | **reusado como fallback** |
| Cerebro de la conversación | **la API** (`colsubsidio.go` + `protege.go`), no GPT free-form |
| GPT-4o | **NLU opcional** (hoy coerción local) |

---

## 3. Contrato de datos (eventos / API / tablas)

### 3.1 Eventos internos (ver `04_EVENT_CATALOG.md` §4.7b)

`MESSAGE_RECEIVED` / `MESSAGE_SENT` (canal texto) + reuso de `CALL_STARTED`, `TRANSCRIPT_UPDATED`, `FEATURE_UPDATED` (una por variable respondida), `RECOMMENDATION_GENERATED` (una por recomendación), `SUMMARY_GENERATED`, `CALL_ENDED`. El Projector no cambia.

### 3.2 Colsubsidio Protege API (endpoints usados)

| Método | Ruta | Uso |
|--------|------|-----|
| GET | `/api/v1/users/search?phone=` | buscar usuario por teléfono |
| POST | `/api/v1/users` | crear usuario (si no existe) |
| POST | `/api/v1/conversations` | abrir conversación `channel=whatsapp` |
| POST | `/api/v1/conversations/{id}/answers` | responder la pregunta actual |
| POST | `/api/v1/conversations/{id}/complete?limit=` | cerrar y generar recomendaciones |

Estados de conversación: `new → collecting_data → ready_for_recommendation → completed`. `field_type` soportados por `coerceAnswer`: `boolean`, `number`, `currency` (convención CO: punto=miles, coma=decimal); el resto pasa como texto y la API valida (un 422 re-pregunta).

### 3.3 Tablas

Ninguna nueva. `channel="whatsapp"` viaja en `CALL_STARTED`; el Projector lo persiste en la columna string `channel`. La analítica se deriva igual que en voz.

### 3.4 API interna (backend Go)

| Ruta | Uso | Requiere infra pública |
|------|-----|------------------------|
| `POST /api/chat/start {to}` | contacto saliente autónomo (abre conversación Protege) | No (envío real: sí) |
| `POST /api/whatsapp/simulate-inbound {from,text}` | demo offline de mensaje entrante | No |
| `POST /api/whatsapp/webhook` | entrante real de Kapso (ack 200 inmediato + turno async) | Sí (HTTPS público) |
| `POST /api/calls/:id/end` | cierre (reutilizado) → proyecta al Pipeline | No |
| `GET /api/capabilities` | expone `whatsapp` y `colsubsidio` (bool) | No |

---

## 4. Lógica de negocio

- **buscar-o-crear** por teléfono (`ResolveUser`): WhatsApp trae el teléfono en cada inbound, alineado con `users/search?phone=`.
- **Flujo dinámico**: las preguntas, condiciones y variables las decide la API; el adaptador no hardcodea el cuestionario. Añadir un "nodo" (ej. mascotas) = crear la Question/Variable en la API, sin tocar el backend Go.
- **Ventana 24h / plantillas**: el primer saliente usa un opener fijo (equivalente a plantilla HSM); dentro de la ventana, texto libre. En el Sandbox de Kapso basta registrar el celular de prueba y enviar el código de activación de 6 caracteres.
- **NLU**: `coerceAnswer` local hoy; GPT-4o como hook opcional para casos ambiguos (source `ai` con `confidence`).

---

## 5. Casos borde cubiertos

- Usuario inexistente → se crea (`ResolveUser`).
- Inbound sin sesión viva → abre conversación (el mensaje solo dispara el contacto).
- Respuesta inválida (422 de la API) → emite `ERROR_OCCURRED` y re-pregunta, sin avanzar.
- `complete` falla → mensaje de disculpa + cierre; no crashea.
- Recomendaciones con forma desconocida (array untyped) → `recFields` extrae name/reason best-effort; si ninguna, mensaje genérico.
- Conversación ya sin pregunta pendiente → intenta cerrar (`finish`).
- Sin `COLSUBSIDIO_API_URL` → fallback al motor GPT-4o local.
- Coerción numérica CO (`$1.500.000` → 1500000; `2,5` → 2.5) y booleana (sí/no).

---

## 6. Pruebas

`backend/protege_test.go`:
- `coerceAnswer` (boolean/number/currency/text), `recFields` (map/nested/string/desconocido).
- **`TestProtegeFullFlow`**: mock httptest de la API completa → `StartContact` + dos `HandleInbound` → verifica `call_id == conversation.id`, `CALL_STARTED channel=whatsapp`, value coercionado (30), `source=whatsapp`, 1× `RECOMMENDATION_GENERATED` con el producto correcto, `CALL_ENDED`, ≥3 `MESSAGE_SENT`, 2× `FEATURE_UPDATED`.

`backend/kapso_test.go`: transporte Kapso (`parseKapsoWebhook`/`parseKapsoInbound`, `verifyKapsoSignature`, `Enabled`), `backend/whatsapp_test.go`: sesiones y `replyEvent`. `backend/projector_test.go`: `TestDeriveWhatsAppChannel`.

```bash
docker run --rm -v "$PWD":/app -w /app -e GOFLAGS=-mod=mod golang:1.22-alpine \
  sh -c 'go build ./... && go test ./...'
# ok  guardianai
```

---

## 7. Configuración

`.env` (gitignoreado). Todas opcionales; sin ellas, demo offline con fallback GPT.

| Variable | Uso |
|----------|-----|
| `COLSUBSIDIO_API_URL` | base de la Protege API (ej. `http://147.93.11.136:9000`) |
| `COLSUBSIDIO_API_TOKEN` | bearer opcional (el spec no declara auth) |
| `KAPSO_API_KEY` / `KAPSO_PHONE_NUMBER_ID` | transporte WhatsApp real (Kapso) |
| `KAPSO_WEBHOOK_SECRET` (opcional) | verificación HMAC-SHA256 del webhook |
| `PUBLIC_BASE_URL` | base HTTPS para el webhook entrante |

---

## 8. Limitaciones conocidas

1. **La Protege API no es alcanzable desde el entorno de build/CI** (`147.93.11.136:9000` filtra por IP origen; nuestra IP de egress `137.184.139.130` es dropeada). Por eso la demo corre contra el **mock local `mock-protege/`** (mismo contrato OpenAPI 0.1.0; catálogo real capturado el 2026-07-24): para efectos de la demo se usa un mock de la API de integración debido a restricciones de acceso de red. El sistema completo (backend, WhatsApp, Pipeline) lo consume sin saber que es un mock — para la API real basta `COLSUBSIDIO_API_URL=http://147.93.11.136:9000` en `.env` cuando haya allowlist, sin tocar código.
2. **Envío/recepción real por WhatsApp** requiere Kapso + HTTPS público (Kapso solo entrega webhooks por HTTPS; se expone con cloudflared — ver §9).
3. **Sesiones y estado en memoria**: se pierden al reiniciar (igual que el `EventStore`).
4. **NLU**: coerción local simple; casos ambiguos podrían necesitar GPT (hook pendiente).

### Frontera demo vs infra

| Capacidad | Demo local | Requiere infra |
|---|---|---|
| Flujo Protege (users/conversations/answers/complete) | ✅ contra mock local (`mock-protege`) | API real: allowlist IP o red alcanzable |
| Saliente autónomo + entrante | ✅ `chat/start`, `simulate-inbound` | — |
| Visualización en vivo (`/chat` + WS) | ✅ | — |
| Proyección a Pipeline (`channel=whatsapp`) | ✅ | — |
| Envío/recepción real por WhatsApp | ❌ | Kapso Sandbox (código de 6 caracteres) + túnel HTTPS (cloudflared) |

---

## 9. Cómo verificar

```bash
docker compose up --build
# Chat WhatsApp: http://localhost:8099/chat   ·   Pipeline: http://localhost:8099/pipeline

# 1) Contacto saliente (abre conversación Protege; devuelve conversation_id)
curl -s -X POST http://localhost:8099/api/chat/start \
  -H 'Content-Type: application/json' -d '{"to":"+57 300 123 4567"}'

# 2) Respuesta entrante del cliente (responde la pregunta actual)
curl -s -X POST http://localhost:8099/api/whatsapp/simulate-inbound \
  -H 'Content-Type: application/json' \
  -d '{"from":"+57 300 123 4567","text":"tengo 34 años"}'
#   ...repetir con más respuestas hasta que la API genere la recomendación.

# 3) Capacidades
curl -s http://localhost:8099/api/capabilities   # "colsubsidio": true|false, "whatsapp": true|false
```

En la UI: `/chat` → "Iniciar contacto" → responder cada pregunta que manda la API → al completar, la conversación aparece en `/pipeline` con la etiqueta **WhatsApp**.

> **Nota de runtime:** el backend necesita alcanzar `COLSUBSIDIO_API_URL`. En entornos donde `147.93.11.136:9000` esté bloqueado, dejar la variable sin setear activa el fallback GPT-4o local (útil para demo), pero no ejercita la API real.

### Camino real con Kapso (WhatsApp de verdad)

1. **Kapso:** cuenta free → `Integrations → API keys` → crear key. En `WhatsApp → Sandbox`: *Add Test Number* (tu celular) y enviar el **código de 6 caracteres** al número sandbox. Anotar el `phone_number_id`.
2. **`.env`:** `KAPSO_API_KEY`, `KAPSO_PHONE_NUMBER_ID` (y `KAPSO_WEBHOOK_SECRET` si se define). `docker compose up -d --build backend`.
3. **HTTPS:** Kapso solo entrega webhooks por HTTPS. Exponer el backend con un túnel: `cloudflared tunnel --url http://localhost:8099` → `PUBLIC_BASE_URL=https://<random>.trycloudflare.com`.
4. **Webhook:** registrar `$PUBLIC_BASE_URL/api/whatsapp/webhook` suscrito a `whatsapp.message.received` (UI: Sandbox config → Manage Webhooks; o API `POST /platform/v1/whatsapp/phone_numbers/{id}/webhooks`).
5. **Probar:** escribe un WhatsApp al número sandbox → el webhook recibe, el flujo Protege pregunta, y las respuestas llegan a tu celular. Todo se ve en vivo en `/chat` y termina en `/pipeline`.

El webhook hace **ack 200 inmediato** y procesa el turno en async (Kapso exige 200 en <10s); los eventos llegan a la UI por `/ws` y las respuestas salen por el consumidor de `MESSAGE_SENT`.
