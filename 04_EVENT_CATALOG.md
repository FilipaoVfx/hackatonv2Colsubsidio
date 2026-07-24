# 04 — Event Catalog

> **Guardian AI** · Contrato entre módulos
> Este documento es la fuente de verdad de la comunicación interna. Todo módulo publica y consume eventos **solo** a través del Event Bus (RA-002). Ningún módulo llama a otro de forma directa.

---

## 1. Propósito

El Event Catalog define el contrato de eventos del sistema. Antes de escribir código, cada evento debe existir aquí con: nombre, disparador, publicador, consumidores, payload y efectos. Si un evento no está en este catálogo, no existe en el sistema.

Reglas que este documento hace cumplir:

- **RA-002** — toda comunicación inter-módulo fluye por el Event Bus.
- **RN-002** — todo cambio de perfil emite `FEATURE_UPDATED`.
- **RN-003** — la secuencia de eventos persistida debe reconstruir la llamada completa.
- **RN-004** — el frontend nunca emite eventos que alteren decisiones del backend; solo consume.

---

## 2. Convenciones

### 2.1 Nomenclatura

- Nombre de evento: `SUJETO_VERBO_PASADO` en mayúsculas. Ej: `CALL_STARTED`, `FEATURE_UPDATED`.
- Un evento describe algo **que ya ocurrió**, nunca una orden. `SEND_VOICE` (orden) es incorrecto; `VOICE_SENT` (hecho) es correcto.

### 2.2 Envelope común

Todo evento comparte una envoltura. El campo `payload` varía por tipo.

```json
{
  "event_id": "uuid",
  "type": "CALL_STARTED",
  "call_id": "uuid",
  "sequence": 0,
  "timestamp": "2026-07-23T16:00:00.000Z",
  "producer": "conversation_engine",
  "payload": { }
}
```

| Campo | Tipo | Descripción |
|-------|------|-------------|
| `event_id` | uuid | Identificador único del evento. |
| `type` | string | Nombre del evento (ver catálogo). |
| `call_id` | uuid | Llamada a la que pertenece. Correlaciona toda la secuencia. |
| `sequence` | int | Orden monotónico creciente dentro de la llamada. Empieza en 0. |
| `timestamp` | ISO-8601 UTC | Momento de emisión. |
| `producer` | string | Módulo que publicó el evento. |
| `payload` | object | Datos específicos del evento. |

### 2.3 Persistencia (RN-003 / ADR-015)

Cada evento se persiste en orden. La llamada se reconstruye reproduciendo la secuencia ordenada por `sequence`. Ningún estado se guarda fuera del flujo de eventos salvo proyecciones derivadas.

### 2.4 Módulos productores/consumidores

| Módulo | Rol |
|--------|-----|
| `telephony_adapter` | Puente Vapi ↔ Event Bus (entrada de audio/estado de línea). |
| `conversation_engine` | Contexto, memoria, máquina de estados. |
| `decision_engine` | Elige acción, herramienta y estrategia narrativa. |
| `tool_engine` | Ejecuta herramientas (búsqueda de producto, consulta de cliente). |
| `feature_store` | Perfil dinámico del cliente. |
| `llm_gateway` | Abstracción del modelo (GPT-4o). |
| `voice_adapter` | Síntesis de voz (ElevenLabs). |
| `persistence` | Escritura del log de eventos. |
| `ws_hub` | Difusión al dashboard vía WebSocket. |

---

## 3. Máquina de estados de la conversación

Los eventos empujan la conversación por estos estados (ADR-009):

```
CREATED → CONNECTED → DISCOVERY → LISTENING → THINKING
        → TOOL_EXECUTION → RESPONDING → (LISTENING | ENDED)
```

Cada transición se refleja con `STATE_CHANGED`.

---

## 4. Catálogo de eventos

### 4.1 Ciclo de vida de la llamada

#### `CALL_STARTED`
- **Disparador:** Vapi acepta llamada entrante.
- **Producer:** `telephony_adapter`
- **Consumers:** `conversation_engine`, `persistence`, `ws_hub`
- **Efecto:** crea sesión, estado → `CONNECTED`.
- **Payload:**
```json
{ "from": "+57...", "vapi_call_id": "string", "channel": "voice" }
```

#### `CALL_ENDED`
- **Disparador:** cuelgue, timeout o cierre por sistema.
- **Producer:** `telephony_adapter` | `conversation_engine`
- **Consumers:** `conversation_engine`, `persistence`, `ws_hub`
- **Efecto:** estado → `ENDED`, dispara `SUMMARY_GENERATED`.
- **Payload:**
```json
{ "reason": "hangup|timeout|system", "duration_ms": 0 }
```

#### `STATE_CHANGED`
- **Disparador:** transición en la máquina de estados.
- **Producer:** `conversation_engine`
- **Consumers:** `persistence`, `ws_hub`
- **Payload:**
```json
{ "from": "DISCOVERY", "to": "LISTENING" }
```

---

### 4.2 Conversación y transcripción

#### `USER_SPOKE`
- **Disparador:** turno de habla del usuario detectado por Vapi.
- **Producer:** `telephony_adapter`
- **Consumers:** `conversation_engine`
- **Efecto:** estado → `LISTENING`.
- **Payload:**
```json
{ "audio_ref": "string", "is_final": true }
```

#### `TRANSCRIPT_UPDATED`
- **Disparador:** transcripción parcial o final disponible.
- **Producer:** `telephony_adapter` | `llm_gateway`
- **Consumers:** `conversation_engine`, `feature_store`, `decision_engine`, `persistence`, `ws_hub`
- **Payload:**
```json
{ "role": "user|agent", "text": "string", "is_final": true }
```

#### `INTENT_DETECTED`
- **Disparador:** cambio de intención del usuario detectado.
- **Producer:** `decision_engine`
- **Consumers:** `conversation_engine`, `persistence`, `ws_hub`
- **Payload:**
```json
{ "intent": "price_objection|interest|end_call|question", "confidence": 0.0 }
```

---

### 4.3 Perfil y features (RN-002)

#### `FEATURE_UPDATED`
- **Disparador:** el feature store detecta o modifica un rasgo del perfil.
- **Producer:** `feature_store`
- **Consumers:** `decision_engine`, `persistence`, `ws_hub`
- **Regla:** **todo** cambio de perfil emite este evento (RN-002). Sin excepción.
- **Payload:**
```json
{
  "key": "family_status|pets|employment|risk_level|budget|hesitation",
  "value": "any",
  "previous": "any",
  "source": "transcript|tool|inference"
}
```

---

### 4.4 Razonamiento LLM

#### `LLM_REQUESTED`
- **Disparador:** el decision engine solicita generación al modelo.
- **Producer:** `decision_engine`
- **Consumers:** `llm_gateway`, `persistence`, `ws_hub`
- **Efecto:** estado → `THINKING`.
- **Payload:**
```json
{ "prompt_ref": "string", "strategy": "family_protector|pet_owner|professional|entrepreneur|traveler" }
```

#### `LLM_RESPONSE`
- **Disparador:** el modelo devuelve respuesta.
- **Producer:** `llm_gateway`
- **Consumers:** `conversation_engine`, `decision_engine`, `persistence`, `ws_hub`
- **Efecto:** puede disparar `TOOL_CALLED` o `VOICE_SENT`.
- **Payload:**
```json
{
  "text": "string",
  "tool_calls": [],
  "tokens_in": 0,
  "tokens_out": 0,
  "cost_usd": 0.0,
  "latency_ms": 0,
  "model": "gpt-4o"
}
```

---

### 4.5 Herramientas

#### `TOOL_CALLED`
- **Disparador:** el modelo/decision engine invoca una herramienta.
- **Producer:** `decision_engine`
- **Consumers:** `tool_engine`, `persistence`, `ws_hub`
- **Efecto:** estado → `TOOL_EXECUTION`.
- **Payload:**
```json
{ "tool": "product_search|client_lookup|record_call", "args": {} }
```

#### `TOOL_EXECUTED`
- **Disparador:** la herramienta termina.
- **Producer:** `tool_engine`
- **Consumers:** `decision_engine`, `feature_store`, `persistence`, `ws_hub`
- **Payload:**
```json
{ "tool": "product_search", "result": {}, "latency_ms": 0, "error": null }
```

---

### 4.6 Recomendación (RN-001 / RN-005)

#### `RECOMMENDATION_GENERATED`
- **Disparador:** el sistema produce una recomendación de producto.
- **Producer:** `decision_engine`
- **Consumers:** `conversation_engine`, `persistence`, `ws_hub`
- **Regla:** toda recomendación debe incluir `reasoning` con las variables de perfil que la justifican (RN-001). Prohibida la aleatoriedad (RN-005).
- **Payload:**
```json
{
  "product_id": "string",
  "product_name": "string",
  "reasoning": "string",
  "profile_factors": ["family_status", "risk_level"],
  "confidence": 0.0
}
```

---

### 4.7 Voz

#### `VOICE_SENT`
- **Disparador:** ElevenLabs sintetiza y se envía audio al usuario.
- **Producer:** `voice_adapter`
- **Consumers:** `conversation_engine`, `persistence`, `ws_hub`
- **Efecto:** estado → `RESPONDING` → `LISTENING`.
- **Payload:**
```json
{ "text": "string", "audio_ref": "string", "latency_ms": 0, "voice_id": "string" }
```

---

### 4.7b Chat / WhatsApp (canal de texto)

El canal WhatsApp reutiliza el mismo pipeline LLM que la voz. Solo cambian los
eventos de entrada/salida: `MESSAGE_RECEIVED` es el análogo de `USER_SPOKE` y
`MESSAGE_SENT` el de `VOICE_SENT`. `TRANSCRIPT_UPDATED` se emite igual en ambos
canales, por lo que el proyector y la analítica no cambian. Ver `06_FEATURE_CHAT_WHATSAPP.md`.

#### `MESSAGE_RECEIVED`
- **Disparador:** mensaje entrante del cliente por WhatsApp (webhook Twilio o simulación).
- **Producer:** `whatsapp_adapter`
- **Consumers:** `conversation_engine`
- **Efecto:** estado → `LISTENING`.
- **Payload:**
```json
{ "is_final": true }
```

#### `MESSAGE_SENT`
- **Disparador:** el sistema envía un mensaje de texto al cliente (respuesta LLM u opener).
- **Producer:** `whatsapp_adapter`
- **Consumers:** `whatsapp_adapter` (entrega vía Twilio), `persistence`, `ws_hub`
- **Efecto:** estado → `RESPONDING` → `LISTENING`.
- **Payload:**
```json
{ "text": "string", "to": "string", "wa_message_id": "string", "status": "queued", "channel": "whatsapp" }
```

---

### 4.8 Cierre

#### `SUMMARY_GENERATED`
- **Disparador:** fin de llamada; el sistema genera el resumen automático.
- **Producer:** `decision_engine`
- **Consumers:** `persistence`, `ws_hub`
- **Payload:**
```json
{
  "summary": "string",
  "profile_snapshot": {},
  "recommendations": [],
  "total_cost_usd": 0.0,
  "total_tokens": 0,
  "duration_ms": 0
}
```

---

### 4.9 Errores

#### `ERROR_OCCURRED`
- **Disparador:** fallo en cualquier módulo (adapter, LLM, tool, voz).
- **Producer:** cualquiera
- **Consumers:** `conversation_engine`, `persistence`, `ws_hub`
- **Payload:**
```json
{ "source": "llm_gateway", "code": "string", "message": "string", "recoverable": true }
```

---

## 5. Flujo de referencia (turno completo)

```
USER_SPOKE
  → TRANSCRIPT_UPDATED (user)
  → FEATURE_UPDATED (si aplica)
  → INTENT_DETECTED (si aplica)
  → LLM_REQUESTED
  → LLM_RESPONSE
      ├─ TOOL_CALLED → TOOL_EXECUTED → LLM_REQUESTED (2da pasada)
      └─ (sin tool)
  → RECOMMENDATION_GENERATED (si aplica)
  → VOICE_SENT
  → TRANSCRIPT_UPDATED (agent)
  → STATE_CHANGED (RESPONDING → LISTENING)
```

Cierre:

```
CALL_ENDED → SUMMARY_GENERATED → STATE_CHANGED (→ ENDED)
```

---

## 6. Matriz productor/consumidor

| Evento | Producer | Consumers |
|--------|----------|-----------|
| `CALL_STARTED` | telephony_adapter | conversation_engine, persistence, ws_hub |
| `CALL_ENDED` | telephony_adapter / conversation_engine | conversation_engine, persistence, ws_hub |
| `STATE_CHANGED` | conversation_engine | persistence, ws_hub |
| `USER_SPOKE` | telephony_adapter | conversation_engine |
| `TRANSCRIPT_UPDATED` | telephony_adapter / llm_gateway | conversation_engine, feature_store, decision_engine, persistence, ws_hub |
| `INTENT_DETECTED` | decision_engine | conversation_engine, persistence, ws_hub |
| `FEATURE_UPDATED` | feature_store | decision_engine, persistence, ws_hub |
| `LLM_REQUESTED` | decision_engine | llm_gateway, persistence, ws_hub |
| `LLM_RESPONSE` | llm_gateway | conversation_engine, decision_engine, persistence, ws_hub |
| `TOOL_CALLED` | decision_engine | tool_engine, persistence, ws_hub |
| `TOOL_EXECUTED` | tool_engine | decision_engine, feature_store, persistence, ws_hub |
| `RECOMMENDATION_GENERATED` | decision_engine | conversation_engine, persistence, ws_hub |
| `VOICE_SENT` | voice_adapter | conversation_engine, persistence, ws_hub |
| `MESSAGE_RECEIVED` | whatsapp_adapter | conversation_engine |
| `MESSAGE_SENT` | whatsapp_adapter | whatsapp_adapter, persistence, ws_hub |
| `SUMMARY_GENERATED` | decision_engine | persistence, ws_hub |
| `ERROR_OCCURRED` | cualquiera | conversation_engine, persistence, ws_hub |

---

## 7. Reglas de invariancia

1. `persistence` y `ws_hub` consumen **todos** los eventos. No hay evento que no se persista ni que no llegue al dashboard.
2. `sequence` es único y creciente por `call_id`. Sin huecos.
3. Ningún consumidor muta el payload de un evento; lo trata como inmutable.
4. El frontend (`ws_hub` → dashboard) es **solo lectura** (RN-004). No hay bus de vuelta desde el dashboard.
5. Un nuevo evento requiere PR que actualice este catálogo antes que el código.
