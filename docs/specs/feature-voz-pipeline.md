# 05 — Feature: Llamadas, Voz y Pipeline de Llamadas

> **Guardian AI** · Documentación detallada de feature
> Estado: **implementado y verificado** · Commit base: `b23c38b`
> Cubre: los cuatro caminos de llamada, la capa de voz, la proyección analítica y la vista "Pipeline de Llamadas".

---

## 0. Convención de documentación

A partir de este documento, **toda feature nueva se documenta en un `.md` numerado en la raíz** del repositorio, con esta misma estructura:

1. Propósito y alcance
2. Arquitectura y flujo
3. Contrato de datos (eventos / tablas / API)
4. Reglas de derivación o lógica de negocio
5. Casos borde cubiertos
6. Pruebas
7. Configuración
8. Limitaciones conocidas
9. Cómo verificar

Numeración actual: `PRD.md`, `02_ADR.md`, `03_SRS.md`, `04_EVENT_CATALOG.md`, **`05_FEATURE_LLAMADAS_VOZ_PIPELINE.md`**.

---

## 1. Propósito y alcance

Esta feature entrega el ciclo completo de una llamada comercial de seguros:

- **Entrada**: cuatro formas de iniciar una conversación (demo scripted, texto con GPT-4o real, voz por navegador, teléfono).
- **Proceso**: motor conversacional event-driven que analiza en vivo y decide.
- **Salida de voz**: síntesis real con ElevenLabs, con sustituto gratuito en navegador.
- **Persistencia analítica**: al terminar, la llamada se **proyecta automáticamente** a las tablas de analítica.
- **Visualización**: la página `/pipeline` muestra la bitácora post-llamada con fases, transcripción, scoring, perfil e insights.

**Principio rector:** todo lo que se muestra en el Pipeline se **deriva del log de eventos**, no se inventa ni se guarda por separado. El log de eventos sigue siendo la fuente de verdad (RN-003).

---

## 2. Arquitectura y flujo

### 2.1 Vista general

```
[ Origen de llamada ]          [ Núcleo event-driven ]         [ Salidas ]

Simular (mock)      ─┐
Conversación GPT-4o ─┤
Llamada web (Vapi)  ─┼──►  Event Bus  ──►  EventStore (memoria)
Teléfono (Vapi)     ─┘        │                │
                              │                ├──► WebSocket Hub ──► Mission Control (vivo)
                              │                ├──► Supabase `events` (persistencia)
                              │                └──► Projector ──► tablas analíticas ──► /pipeline
                              │
                              └──► ElevenLabs TTS ──► audio al navegador
```

Regla dura: los módulos **solo** se comunican por el Event Bus (RA-002). El proyector es un consumidor más; **no** altera la ruta de eventos en vivo.

### 2.2 Los cuatro caminos de llamada

| # | Camino | Entrada | Cerebro | Voz | Se cierra | Estado |
|---|--------|---------|---------|-----|-----------|--------|
| 1 | **Demo mock** | `POST /api/calls/simulate` | guion fijo (`orchestrator.go`) | ElevenLabs / navegador | automático | ✅ operativo |
| 2 | **Conversación real** | `POST /api/calls/start` + `/turn` | **GPT-4o real** | ElevenLabs / navegador | botón ⏹ Finalizar | ✅ operativo |
| 3 | **Llamada web** | Vapi Web SDK (micrófono) | GPT-4o vía Vapi | Vapi | al colgar | ✅ operativo |
| 4 | **Teléfono** | `POST /api/phone/call` | GPT-4o vía Vapi | Vapi | al colgar | ⚠️ bloqueado (ver §8) |

Los cuatro terminan emitiendo `CALL_ENDED`, que es lo que dispara la proyección al Pipeline.

### 2.3 Secuencia de un turno real (camino 2)

```
USER_SPOKE
  → STATE_CHANGED (DISCOVERY → LISTENING)
  → TRANSCRIPT_UPDATED (role=user)
  → LLM_REQUESTED  → STATE_CHANGED (LISTENING → THINKING)
  → [ TOOL_CALLED → TOOL_EXECUTED → STATE_CHANGED ]        (si el modelo decide usar herramienta)
  → FEATURE_UPDATED × N   (rasgos detectados en el habla)
  → FEATURE_UPDATED (risk_level)  · FEATURE_UPDATED (sentiment)
  → INTENT_DETECTED
  → LLM_RESPONSE (tokens, costo, latencia reales)
  → [ RECOMMENDATION_GENERATED ]                            (si hay perfil suficiente)
  → VOICE_SENT → STATE_CHANGED (RESPONDING → LISTENING)
  → TRANSCRIPT_UPDATED (role=agent)
```

Cierre:
```
SUMMARY_GENERATED → CALL_ENDED → STATE_CHANGED (→ ENDED)
                        │
                        └──► Projector.OnEvent → Derive() → Save()
```

---

## 3. Capa de voz

### 3.1 Salida (TTS)

| Proveedor | Endpoint | Condición | Notas |
|-----------|----------|-----------|-------|
| **ElevenLabs** | `POST /api/tts` → MP3 | `ELEVENLABS_API_KEY` + `ELEVENLABS_VOICE_ID` | modelo `eleven_multilingual_v2`, `stability 0.5`, `similarity_boost 0.75` |
| **Web Speech (navegador)** | — | fallback automático | `SpeechSynthesisUtterance`, `lang=es-ES`, `rate=1.05` |

El dashboard consulta `GET /api/capabilities`; si `elevenlabs=true` pide el MP3 al backend y lo reproduce, si falla cae al sintetizador del navegador. El checkbox **voz** apaga ambos.

**Restricción de plan real:** la cuenta free de ElevenLabs **no permite voces de librería vía API** (HTTP 402 `paid_plan_required`). Sí permite **voces premade**. Por eso `ELEVENLABS_VOICE_ID` apunta a una premade (`EXAVITQu4vr4xnSDxMaL`). Verificado: 200 OK, MP3 de ~44 KB.

### 3.2 Grabaciones de las llamadas sembradas

Las 5 llamadas de demostración tienen **audio real generado con ElevenLabs** a partir de su transcripción, con **dos voces premade** para distinguir interlocutores:

| Rol | Voz | ID |
|-----|-----|-----|
| Sofía (IA) | Sarah | `EXAVITQu4vr4xnSDxMaL` |
| Cliente | Adam | `pNInz6obpgDQGcFmaJgB` |

Proceso: una petición TTS por línea (44 en total, `eleven_multilingual_v2`), concatenadas por llamada con ffmpeg insertando **0,45 s de silencio** entre turnos, a MP3 128 kbps.

Salida en `frontend/audio/`, servida por nginx en `/audio/<archivo>.mp3` y referenciada desde `call_analytics.recording_url`:

| Archivo | Duración audio | `duration_sec` (nominal) |
|---------|---------------|--------------------------|
| `call-1423.mp3` | 1:02 | 12:48 |
| `call-1547.mp3` | 0:39 | 17:22 |
| `call-0912.mp3` | 0:35 | 8:54 |
| `call-1105.mp3` | 0:30 | 11:29 |
| `call-1638.mp3` | 0:26 | 6:42 |

> **El audio es una dramatización condensada**, no una grabación de una llamada de esa duración: recorre solo los turnos guardados en la transcripción. Por eso dura menos que la duración nominal. La barra del reproductor se rige por la **duración real del audio**; el encabezado y la línea de tiempo de fases conservan la **nominal**. Ambas cifras son correctas en su propio marco, pero no son la misma magnitud.

El reproductor (`pipeline.js`) soporta play/pausa, ±10 s, velocidad (1×/1,25×/1,5×/0,75×) y búsqueda por clic en la barra. Si la llamada no tiene grabación, se deshabilita solo.

### 3.3 Entrada (STT)

- **Navegador**: `SpeechRecognition` / `webkitSpeechRecognition`, `lang=es-ES`. Botón 🎤 dicta y envía el turno.
- **Vapi**: transcriptor Deepgram con `language: es` (caminos 3 y 4).

---

## 4. Contrato de datos

### 4.1 Eventos consumidos por el proyector

Definidos en `04_EVENT_CATALOG.md`. El proyector lee: `CALL_STARTED`, `TRANSCRIPT_UPDATED`, `FEATURE_UPDATED`, `INTENT_DETECTED`, `TOOL_CALLED`, `RECOMMENDATION_GENERATED`, `CALL_ENDED`.

### 4.2 Tablas analíticas (Supabase)

Aditivas: **no** tocan `events` ni `calls`, que sostienen el flujo en vivo.

**`customers`** — perfil comercial
`customer_id` · `full_name` · `phone` · `age` · `marital_status` · `children` · `occupation` · `income_range` · `city` · `is_new` · `created_at`

**`call_analytics`** — cabecera
`call_id` (PK) · `call_code` (único) · `customer_id` → customers · `advisor_name` · `advisor_voice` · `channel` · `outcome` · `started_at` · `duration_sec` · `score_overall` · `score_label` · `recording_url` · `created_at`

**`call_phases`** — 5 por llamada
`phase_id` · `call_id` · `idx` · `name` · `start_sec` · `end_sec` · `pct_of_total` · `objective` · `agent_pct` · `customer_pct` · `emotion` · `emotion_conf` · `status` · `checklist` (jsonb) · `keywords` (jsonb) · `findings`
Único: `(call_id, idx)`

**`call_transcript`** — turno a turno
`call_id` · `idx` · `speaker` · `role` (`agent`|`customer`) · `text` · `at_sec` · `dur_sec`
Único: `(call_id, idx)`

**`call_scores`** — 5 dimensiones
`call_id` · `dimension` · `label` · `value` (0-100)
Único: `(call_id, dimension)`

**`call_insights`**
`insight_id` · `call_id` · `idx` · `text` · `priority` (`Alta`|`Media`|`Baja`) · `category`
Único: `(call_id, idx)`

### 4.3 API

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/health` | estado del servicio |
| GET | `/api/capabilities` | flags: `llm`, `elevenlabs`, `vapi`, `vapi_web` |
| POST | `/api/calls/simulate` | demo scripted (sin costo de API) |
| POST | `/api/calls/start` | inicia conversación real GPT-4o |
| POST | `/api/calls/:id/turn` | envía un turno del cliente `{text}` |
| **POST** | **`/api/calls/:id/end`** | cierra la conversación y **dispara la proyección** |
| POST | `/api/tts` | `{text}` → MP3 ElevenLabs |
| POST | `/api/vapi/ingest` | puente de eventos del Vapi Web SDK (`start`/`user`/`agent`/`end`) |
| POST | `/api/phone/call` | llamada telefónica saliente `{to}` E.164 |
| GET | `/api/calls/:id/events` | replay del log (RN-003) |
| **GET** | **`/api/analytics/calls`** | listado del Pipeline |
| **GET** | **`/api/analytics/calls/:id`** | detalle completo de la llamada |
| WS | `/ws` | stream de eventos al dashboard (solo lectura, RN-004) |

---

## 5. Reglas de derivación (`projector.go`)

`Derive(events) → CallRecord` es una **función pura**: mismo log, mismo resultado. Sin DB, sin reloj, sin red. Por eso es testeable de forma aislada. `Save()` persiste en una transacción aparte.

### 5.1 Cabecera

- **`started_at`**: timestamp de `CALL_STARTED`; si falta, el más antiguo parseable; si no hay ninguno, `now()`.
- **`call_code`**: `CALL-YYYY-MM-DD-HHMM` derivado del inicio.
- **`channel`**: payload de `CALL_STARTED` (`voice`, `web`, `webrtc`), por defecto `web`.
- **`duration_sec`**: `duration_ms` de `CALL_ENDED` si es numérico; si no, tiempo de pared entre primer y último evento; **nunca negativo**; se **extiende** si alguna línea de transcripción cae después.
- **`full_name`**: rasgo `name` si existe → `Cliente <últimos 4 dígitos>` → `Cliente Demo`.

### 5.2 Resultado comercial (`outcome`)

Del último `INTENT_DETECTED`:

| Intención | ¿Hubo recomendación? | Resultado |
|-----------|---------------------|-----------|
| `acceptance` | cualquiera | **Cerrado** |
| `price_objection` | cualquiera | **Seguimiento** |
| `end_call` | cualquiera | **No interesado** |
| `interest` / `question` | sí | **Interesado** |
| `interest` / `question` | no | **Seguimiento** |
| ninguna / desconocida | sí | **Interesado** |
| ninguna / desconocida | no | **Seguimiento** |

### 5.3 Fases

Cinco fases fijas: Saludo y Bienvenida · Descubrimiento · Análisis de Riesgos · Presentación de Opciones · Cierre y Siguientes Pasos.

Las fronteras usan **marcadores reales** cuando existen, con respaldo proporcional:

| Frontera | Marcador real | Respaldo |
|----------|---------------|----------|
| fin fase 1 | primer turno del cliente | 7 % de la duración |
| fin fase 2 | primer `FEATURE_UPDATED` de `risk_level` | 36 % |
| fin fase 3 | primer `RECOMMENDATION_GENERATED` | 68 % |
| fin fase 4 | primer `INTENT_DETECTED` = `acceptance` | 88 % |
| fin fase 5 | siempre la duración total | — |

**Invariantes garantizadas** (verificadas por test): fronteras monótonas crecientes, `start ≤ end`, ninguna excede la duración, sin huecos entre fases, la última cierra exactamente en `duration_sec`, y los porcentajes suman 100 ± 3 (redondeo).

- **Participación** (`agent_pct` / `customer_pct`): proporción de **caracteres hablados** por rol dentro de la ventana. Sin transcripción → `0/0`. Con datos → suman 100.
- **Palabras clave**: tokens de ≥4 caracteres, sin stop-words, ordenados por frecuencia y luego alfabéticamente; máximo 4 por fase.
- **Emoción**: último `sentiment` normalizado a español (`Positiva`/`Negativa`/`Neutral`).

### 5.4 Scoring

Cinco dimensiones, todas recortadas a `[0, 100]`:

| Dimensión | Fórmula |
|-----------|---------|
| Calidad de Descubrimiento | `55 + 8 × rasgos_de_perfil` (+5 si usó herramienta) |
| Empatía | `60 + {Positiva:+25, Neutral:+10, Negativa:−5}` |
| Claridad | `65 + 2 × líneas_de_transcripción` |
| Manejo de Objeciones | `60` (`70` si hubo objeción de precio) `+12` si hubo recomendación |
| Cierre | `acceptance:92` · `interest:78` · `end_call:45` · otro `55` |

`score_overall` = media redondeada. Etiqueta: **≥85 Excelente** · **≥70 Bueno** · **≥55 Regular** · **<55 Bajo**.

> Estas fórmulas son **heurísticas explicables**, no un modelo entrenado. Se eligieron para que el puntaje sea trazable a hechos del log (rasgos detectados, intención, sentimiento, uso de herramientas). Sustituirlas por un evaluador LLM es trabajo de fase siguiente.

### 5.5 Insights

Se generan de hechos del log: producto recomendado con su razón, perfil construido en vivo, intención de cierre/objeción/abandono y sentimiento negativo. **Nunca quedan vacíos**: si no hay señal comercial, se emite un insight de respaldo.

### 5.6 Idempotencia

`Save()` corre en transacción:
1. Resuelve el cliente **reusando** — enlace existente de la llamada → coincidencia por teléfono → insertar como último recurso. *(Sin esto, reproyectar dejaba clientes huérfanos.)*
2. `UPSERT` en `call_analytics` por `call_id`.
3. **Borra** hijos de esa llamada y los reinserta.

Resultado: reproyectar N veces deja el mismo estado. Verificado con 3 reproyecciones seguidas: clientes sin crecer, 0 huérfanos, 5 fases (no 15).

---

## 6. Casos borde cubiertos

| Caso | Comportamiento |
|------|---------------|
| Log vacío / `nil` | error explícito, no proyecta |
| `call_id` ausente | error explícito |
| Sin `CALL_STARTED` (backend reiniciado a mitad) | usa el evento más antiguo, sigue derivando |
| Sin transcripción | 5 fases igual, participación `0/0`, insight de respaldo |
| Líneas vacías o solo espacios | descartadas; índices siguen contiguos desde 1 |
| Texto con espacios sobrantes | recortado |
| Rol desconocido / `user` | normalizado a `customer` |
| Eventos desordenados | reordenados por `sequence` antes de derivar |
| `CALL_ENDED` duplicado | registro idéntico, sin duplicar filas |
| Duración cero | sin división por cero; porcentajes en 0 |
| Transcripción posterior a la duración reportada | la duración se extiende para cubrirla |
| Timestamp malformado o vacío | ignorado, sin pánico |
| `duration_ms` no numérico | cae a tiempo de pared |
| Unicode, comillas, backslash, saltos de línea | sobreviven el encoding a `jsonb` |
| Scores fuera de rango | recortados a `[0,100]` |
| DB caída | la llamada sigue en vivo; solo se omite la proyección (log de error) |

---

## 7. Pruebas

`backend/projector_test.go` — **24 tests, todos en verde**.

```bash
cd guardian-ai/backend
docker run --rm -v "$PWD":/src -w /src golang:1.22-alpine go test ./...
# ok  guardianai
```

Cobertura por área:

- **Derivación completa**: cabecera, perfil, transcripción, fases, scores (`TestDeriveFullCall`)
- **Invariantes de fases** sobre 5 logs distintos, incluido desordenado y duración cero (`TestPhasesAreContiguousAndInRange`)
- **Entradas inválidas**: log vacío, sin `call_id`, sin `CALL_STARTED`, timestamps rotos
- **Transcripción**: líneas en blanco, recorte, normalización de roles, índices, duración por línea, desborde de duración
- **Mapeo de resultado**: 9 combinaciones intención × recomendación
- **Scoring**: rangos con log mínimo, completo y "charlatán" (40 turnos); fronteras de etiqueta
- **Serialización**: escapes JSON round-trip, unicode y comillas
- **Auxiliares**: `participation`, `phaseKeywords`, `deriveName`, `pctOf`, `normalizeSentiment`

La lógica pura se prueba **sin base de datos**; la persistencia se verifica end-to-end contra Supabase real (§9).

---

## 8. Configuración y limitaciones

### 8.1 Variables (`.env`, gitignoreado — ver `.env.example`)

| Variable | Uso | Obligatoria |
|----------|-----|-------------|
| `OPENAI_API_KEY` | GPT-4o | sí para conversación real |
| `ELEVENLABS_API_KEY` + `ELEVENLABS_VOICE_ID` | TTS | no (cae a navegador) |
| `VAPI_API_KEY` (**privada**) + `VAPI_PHONE_ID` | telefonía | no |
| `VAPI_PUBLIC_KEY` + `VAPI_ASSISTANT_ID` | llamada web | no |
| `SUPABASE_DB_URL` | persistencia + Pipeline | no (cae a memoria) |
| `STEP_MS` | ritmo del demo mock (por defecto **450**) | no |

**Supabase debe apuntar al pooler IPv4**, no al host directo: el host `db.<ref>.supabase.co` resuelve solo IPv6 y es inalcanzable desde este entorno.
Formato: `postgresql://postgres.<ref>:<pass>@aws-0-<region>.pooler.supabase.com:5432/postgres`

### 8.2 Limitaciones conocidas

1. **Telefonía PSTN bloqueada.** El número Vapi (`provider: vapi`) no tiene transporte: toda llamada saliente muere con `call.start.error-get-transport`. La API responde y autentica correctamente; falta aprovisionar un número real (comprar en Vapi o importar uno de Twilio). El código no requiere cambios.
2. **ElevenLabs plan free**: solo voces premade vía API.
3. **Grabaciones**: las 5 llamadas sembradas **sí tienen MP3 real** generado con ElevenLabs (ver §3.3) y el reproductor funciona. Las llamadas **proyectadas** (demo, GPT, web) dejan `recording_url` en `null` y el reproductor se deshabilita — no se guarda audio de las conversaciones en vivo.
4. **Escala temporal del demo mock**: con `STEP_MS=450` una llamada simulada dura ~12 s reales, así que sus fases son mucho más cortas que una llamada real y algunos porcentajes redondean a 0. Los datos son correctos; la escala es artificial.
5. **Scoring heurístico**, no un modelo entrenado (ver §5.4).
6. **Sincronía en vivo de llamada telefónica**: para ver una llamada PSTN en el dashboard mientras ocurre haría falta un webhook HTTPS público (`serverUrl` de Vapi). La llamada web sí se sincroniza, porque el navegador reenvía los eventos a `/api/vapi/ingest`.

---

## 9. Cómo verificar

```bash
docker compose up -d --build
# Mission Control: http://localhost:8099
# Pipeline:        http://localhost:8099/pipeline
```

**Demo mock → Pipeline**
```bash
curl -X POST http://localhost:8099/api/calls/simulate
sleep 15
curl -s http://localhost:8099/api/analytics/calls | head
```

**Conversación real → Pipeline**
```bash
CID=$(curl -s -X POST http://localhost:8099/api/calls/start | jq -r .call_id)
curl -X POST http://localhost:8099/api/calls/$CID/turn \
  -H 'Content-Type: application/json' \
  -d '{"text":"Tengo dos hijos y trabajo independiente."}'
curl -X POST http://localhost:8099/api/calls/$CID/end
curl -s http://localhost:8099/api/analytics/calls/$CID
```

En la interfaz: **Nueva conversación** → escribir o dictar → **⏹ Finalizar** → la llamada aparece en **Pipeline →**.

**Resultado esperado:** en los logs del backend
```
[projector] projected call CALL-YYYY-MM-DD-HHMM (<resultado>, <n> lines, score <s>)
```
