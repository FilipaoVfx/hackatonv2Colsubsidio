# 09 — Robustez del flujo conversacional (Guardian WhatsApp)

> Auditoría del flujo del bot (2026-07-25) sobre `backend/guardian.go`,
> `statemachine.go`, `tools.go`, `kapso.go`. Se reprodujeron los fallos con
> tests ejecutables **antes** de tocar el código; cada arreglo tiene su test de
> regresión. Ningún cambio altera el contrato de la Protege API ni el catálogo
> de eventos existente.

## 1. Qué se arregló y por qué importaba

El motor era correcto en el camino feliz de un solo mensaje a la vez con la API
sana. Fuera de ese carril tenía cinco fallos que se manifestaban justo en la
demo: ráfagas de mensajes (normal en WhatsApp), reintentos del webhook e
intermitencia de la API.

| # | Fallo | Cómo se manifestaba ante el cliente | Arreglo |
|---|---|---|---|
| 1 | **Race entre turnos del mismo cliente** — el webhook lanza una goroutine por mensaje y no había serialización | Dos mensajes seguidos ⇒ historial pisado, variables guardadas dos veces, respuestas fuera de orden, dos conversaciones para la misma persona | `keyedMutex` por teléfono: `HandleInbound`/`StartContact` hacen atómico "resolver-o-abrir sesión + ejecutar turno" |
| 2 | **Webhook sin idempotencia** — `message.id` se parseaba y se descartaba | Reentrega de Kapso (no llegó el 200 a tiempo) ⇒ turno duplicado: doble costo de LLM y respuesta repetida | `inboundDedupe` con TTL de 15 min por `message.id`; el duplicado se acusa 200 y se descarta |
| 3 | **Un GET caído saltaba el descubrimiento** — sin catálogo de preguntas, `MissingQuestions` devolvía vacío y eso se leía como "etapa completa" | `GET /questions` en 500 ⇒ `PROFILE_DISCOVERY → FINANCIAL_QUALIFICATION` con **cero** variables descubiertas, y recomendación sobre la nada | `StageComplete` exige catálogo cargado; el fallo se registra (`questions_unavailable`) y cada turno reintenta recuperarlo |
| 4 | **`PROJECT_MATCHING` sin salida** — si `get_recommendations` fallaba al entrar, nunca se reintentaba | Conversación viva pero muda para siempre: sin recomendación, sin `LEAD_READY`, sin cierre | Reintento por turno hasta `maxRecAttempts = 3`; agotado, el lead se deriva con mensaje honesto (`NURTURING` + `CALL_ENDED`) |
| 5 | **Whitelist de acciones saltable** — el escalamiento leía `d.NextAction` crudo y un `intent` de texto libre | El LLM podía disparar el handoff con una acción prohibida en la etapa, y `fastForward` corría `PROFILE→FINANCIAL→MATCHING` en un turno sin datos financieros | Todo el flujo usa la acción **validada**; `intent` pasa a enum cerrado del esquema; `handoff` se declara legal donde de verdad se honra; `escalate` camina flechas legales sin fabricar recomendación |

## 2. Antes vs Después — impacto por dimensión

| Dimensión | ANTES | DESPUÉS |
|---|---|---|
| Ráfaga de 2+ mensajes del mismo cliente | `DATA RACE` confirmado (escritura de `st.history` en `sendAgent` contra su lectura hacia el LLM); estado de la conversación corrupto | Turnos serializados por teléfono; clientes distintos siguen en paralelo. `go test -race` limpio |
| Reentrega del webhook | El mismo mensaje corría un turno completo otra vez | Se descarta por `message.id`; el debug lo reporta (`… N duplicate(s) ignored`) |
| `GET /questions` intermitente | Salto de etapa silencioso y recomendación sin perfil | Sin catálogo no hay avance; error visible en el dashboard y recuperación automática al volver la API |
| Motor de recomendaciones caído | Lead atrapado en `PROJECT_MATCHING`, sin evento de cierre | Reintento por turno y, tras 3 fallos, derivación explícita a asesor |
| Acción ilegal propuesta por el LLM | Se degradaba a `ask`, que en `PROJECT_MATCHING` **tampoco era legal** | `FallbackAction(state)` garantiza una acción legal del propio estado + `ERROR_OCCURRED{code:"illegal_action"}` |
| Cliente pide asesor en `PROFILE_DISCOVERY` | Bypass de la whitelist y salto hasta recomendación con perfil vacío | Se honra caminando flechas legales hasta `READY_FOR_ADVISOR`, **sin** generar recomendación: decide el asesor |
| Cliente pide recomendación con perfil incompleto | Se recomendaba igual | Se sigue descubriendo; la recomendación exige el perfil completo |
| Vocabulario de `intent` | Texto libre del LLM que controlaba escalamientos | Enum cerrado (`guardianIntents`), misma lista en el esquema JSON y en el prompt |

## 3. Cambios por archivo

| Archivo | Cambio |
|---|---|
| `backend/guardian.go` | `keyedMutex` (lock por teléfono, con refcount para no acumular entradas); lock en `HandleInbound`/`StartContact`; reintento del catálogo de preguntas por turno; acción validada en todo el switch; `escalate()`; `enterMatching()` reintentable con `st.recTries`; `fastForward()` exige perfil completo |
| `backend/statemachine.go` | `StageComplete` false sin catálogo; `allowedActions` honesta (`handoff` legal en etapas vivas, `ask` legal en `PROJECT_MATCHING`); `FallbackAction()`; `guardianIntents` |
| `backend/openai.go` | `intent` con `enum` en el JSON Schema estricto |
| `backend/promptbuilder.go` | El prompt enumera el mismo `guardianIntents` — una sola fuente de verdad |
| `backend/kapso.go` | `inboundDedupe` (TTL, barrido perezoso, reloj inyectable); `processKapsoWebhook` recibe el deduplicador y reporta duplicados |
| `backend/main.go` | Deduplicador de 15 min conectado a `POST /api/whatsapp/webhook` |

Eventos nuevos: ninguno. Dos **códigos** nuevos de `ERROR_OCCURRED`
(`questions_unavailable`, `illegal_action`) — registrados en
`04_EVENT_CATALOG.md`.

## 4. Honestidad metodológica (declarable a jurados)

- Los tres primeros fallos se **reprodujeron ejecutando**, no por lectura: el
  race con `go test -race`, y los otros dos con un proxy que devuelve 500 en el
  endpoint concreto. Los arreglos se validaron contra esos mismos repros.
- El lock es **por proceso**. Con varias réplicas del backend detrás de un
  balanceador, dos mensajes del mismo cliente podrían caer en réplicas distintas
  y volver a solaparse. Para el hackathon (una instancia) está resuelto; en
  producción hace falta un lock distribuido o afinidad por teléfono.
- La deduplicación también es **en memoria y por proceso**: un reinicio olvida
  los ids vistos. La ventana de 15 min cubre los reintentos de Kapso, no un
  historial permanente.
- Ceder al asesor sin recomendación (fallos 4 y 5) es una decisión de producto:
  se prefiere un lead honesto con perfil parcial antes que un match fabricado
  sobre datos que nadie confirmó.
- Quedan pendientes de esta misma auditoría: validación de las `entities` que
  inventa el LLM, el perfil 360 estimado por hash que se guarda como dato
  confirmado, el catálogo de productos que cachea su propio fallo y la falta de
  expiración de conversaciones en memoria.

## 5. Cómo verificar

```bash
cd guardian-ai/backend

# Suite completa con detector de carreras (incluye las 5 regresiones)
go test -race ./...

# Solo las regresiones de robustez del flujo
go test -race -v -run "Concurrent|Dedupe|QuestionsDown|Matching|Handoff|Illegal" ./...
```

Sin Go instalado, con Docker:

```bash
docker run --rm -v "$PWD/backend":/src -w /src golang:1.22-alpine \
  sh -c 'apk add --no-cache gcc musl-dev >/dev/null; go test -race ./...'
```

| Test | Qué prueba |
|---|---|
| `TestGuardianConcurrentInboundSerialized` | 4 mensajes simultáneos del mismo teléfono ⇒ 4 turnos, una sola conversación, historial íntegro |
| `TestWebhookDedupeIgnoresRedelivery` | Reentrega del mismo `message.id` no reprocesa; otro id sí; fuera del TTL vuelve a procesarse |
| `TestGuardianQuestionsDownDoesNotSkipDiscovery` | Con `/questions` caído no hay salto a `FINANCIAL_QUALIFICATION`; al volver la API el catálogo se recupera |
| `TestGuardianMatchingRetriesRecommendation` | Un fallo del motor ⇒ reintento en el turno siguiente ⇒ recomendación entregada |
| `TestGuardianMatchingDerivesAfterPersistentFailure` | Motor siempre caído ⇒ `NURTURING` + `CALL_ENDED` en vez de conversación muda |
| `TestGuardianHandoffWalksLegalArrows` | Handoff en `PROFILE_DISCOVERY` ⇒ transiciones legales hasta `READY_FOR_ADVISOR`, 0 recomendaciones |
| `TestGuardianIllegalActionIgnored` | Acción fuera de la whitelist ⇒ degradación a acción legal + `ERROR_OCCURRED{illegal_action}` |
