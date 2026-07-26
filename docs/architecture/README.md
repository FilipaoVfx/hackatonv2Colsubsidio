# Arquitectura

> Un monolito Go event-sourced. La decisión central del proyecto es que **nada
> muta estado directamente**: todo pasa por un evento inmutable, y las vistas
> que consume la interfaz son proyecciones derivadas de ese log.

## Por qué event sourcing

Un agente de IA que habla con clientes reales tiene que poder responder tres
preguntas después del hecho: qué le dijo al cliente, por qué lo dijo, y cuánto
costó. Con un modelo CRUD clásico —una tabla `conversations` que se sobrescribe—
ninguna de las tres tiene respuesta.

El log de eventos las responde todas sin instrumentación extra. La misma
estructura que persiste el estado alimenta el dashboard en vivo, el replay de
una llamada y la analítica de costos. Ver [ADR-0001](../adr/0001-event-sourcing.md).

## Vista general

```mermaid
graph TB
  subgraph entrada["Entrada"]
    WA["Webhook WhatsApp<br/>/api/whatsapp/webhook"]
    VP["Ingesta Vapi<br/>/api/vapi/ingest"]
    SIM["Simulación<br/>/api/whatsapp/simulate-inbound"]
    PG["Playground<br/>/api/studio/playground"]
  end

  subgraph nucleo["Núcleo · guardian-ai/backend"]
    ORQ["orchestrator.go<br/>coordina el turno"]
    GUA["guardian.go<br/>motor conversacional"]
    SM["statemachine.go<br/>transiciones válidas"]
    PB["promptbuilder.go<br/>arma el prompt"]
    RAG["rag.go<br/>recuperación"]
    TOOLS["tools.go<br/>ejecución de herramientas"]
    CFG["agentconfig.go<br/>persona · draft/publish"]
  end

  subgraph salida["Salida"]
    LLM["openrouter.go / openai.go"]
    KAP["kapso.go<br/>WhatsApp"]
    VOZ["voice.go · vapi.go"]
    COL["colsubsidio.go · protege.go"]
  end

  subgraph persistencia["Persistencia"]
    STORE["store.go<br/>append-only"]
    PERSIST["persist.go<br/>pgx → Supabase"]
    PROJ["projector.go<br/>proyecciones"]
    HUB["hub.go<br/>fan-out WebSocket"]
  end

  WA --> ORQ
  VP --> ORQ
  SIM --> ORQ
  PG --> ORQ

  ORQ --> GUA
  GUA --> SM
  GUA --> PB
  PB --> RAG
  PB --> CFG
  GUA --> LLM
  LLM --> TOOLS
  TOOLS --> COL
  GUA --> KAP
  GUA --> VOZ

  ORQ ==> STORE
  STORE --> PERSIST
  STORE --> PROJ
  STORE --> HUB
  HUB -. "/ws" .-> CLIENTES["CLI · Dashboard"]

  classDef in fill:#16233d,stroke:#4f8bff,color:#fff
  classDef core fill:#2a2612,stroke:#ffe600,color:#fff
  classDef out fill:#1a2233,stroke:#a3aec4,color:#fff
  classDef db fill:#12281c,stroke:#2bd576,color:#fff
  class WA,VP,SIM,PG in
  class ORQ,GUA,SM,PB,RAG,TOOLS,CFG core
  class LLM,KAP,VOZ,COL out
  class STORE,PERSIST,PROJ,HUB db
```

Las flechas gruesas (`==>`) marcan el camino obligatorio: **todo turno termina
en el store**. No hay ruta que escriba estado saltándose el log.

## El envelope de evento

Definido en [`events.go`](../../guardian-ai/backend/events.go). Siete campos,
estables desde el primer commit:

```go
type Event struct {
    EventID   string                 `json:"event_id"`
    Type      string                 `json:"type"`
    CallID    string                 `json:"call_id"`
    Sequence  int                    `json:"sequence"`   // monótono por call_id
    Timestamp string                 `json:"timestamp"`
    Producer  string                 `json:"producer"`
    Payload   map[string]interface{} `json:"payload"`
}
```

`Sequence` monótono por `call_id` es lo que hace detectable un hueco: si un
consumidor recibe 7 después de 5, sabe que perdió el 6 y puede rehidratar por
REST sin adivinar. La CLI usa exactamente eso.

Los 22 tipos de evento y su payload están en
[event-catalog.md](event-catalog.md).

## De eventos a vistas

```mermaid
graph LR
  EV[("public.events<br/>append-only")]

  EV --> C["calls"]
  EV --> T["call_transcript"]
  EV --> P["call_phases"]
  EV --> S["call_scores"]
  EV --> I["call_insights"]
  EV --> A["call_analytics"]
  EV --> CU["customers"]

  C --> API1["/api/calls"]
  T --> API2["/api/analytics/calls/:id"]
  P --> API2
  S --> API2
  I --> API2
  A --> API3["/api/analytics/kpis"]

  classDef src fill:#2a2612,stroke:#ffe600,color:#fff
  classDef view fill:#1a2233,stroke:#2bd576,color:#fff
  classDef ep fill:#16233d,stroke:#4f8bff,color:#fff
  class EV src
  class C,T,P,S,I,A,CU view
  class API1,API2,API3 ep
```

[`projector.go`](../../guardian-ai/backend/projector.go) (884 líneas, la pieza
más grande del backend) traduce cada tipo de evento a escrituras sobre estas
tablas. Es idempotente por `event_id`: reproyectar el log completo produce el
mismo resultado.

**Consecuencia práctica:** una llamada recién creada tiene eventos pero todavía
no tiene fila en `call_analytics`. El consumidor debe tratar ese 404 como
estado normal, no como error — la CLI lo hace separando el error de detalle del
error de eventos.

## Turno conversacional

```mermaid
flowchart TD
  IN["Mensaje entrante"] --> SESS{"¿Sesión<br/>existente?"}
  SESS -->|no| NEW["Crear call_id<br/>CALL_STARTED"]
  SESS -->|sí| LOAD["Cargar estado<br/>por teléfono"]
  NEW --> STATE
  LOAD --> STATE

  STATE["Resolver estado del lead"] --> RAGQ["Recuperar contexto<br/>KNOWLEDGE_RETRIEVED"]
  RAGQ --> PROMPT["Construir prompt<br/>persona + historial + variables"]
  PROMPT --> CALL["LLM_REQUESTED"]
  CALL --> RESP["LLM_RESPONSE<br/>tokens · latencia · costo"]
  RESP --> TOOLQ{"¿Tool calls?"}

  TOOLQ -->|sí| EXEC["TOOL_CALLED<br/>TOOL_EXECUTED"]
  EXEC --> PROMPT
  TOOLQ -->|no| TRANS{"¿Cambia<br/>de estado?"}

  TRANS -->|sí| SC["STATE_CHANGED"]
  TRANS -->|no| SEND
  SC --> SEND["MESSAGE_SENT"]
  SEND --> DONE["TURN_COMPLETED"]

  classDef ev fill:#2a2612,stroke:#ffe600,color:#fff
  class NEW,RAGQ,CALL,RESP,EXEC,SC,SEND,DONE ev
```

El bucle `TOOL_EXECUTED → PROMPT` es el que permite que el modelo consulte la
API de Colsubsidio, reciba el catálogo real y recién entonces responda. Sin ese
ciclo, el agente inventaría productos.

## Streaming en vivo

[`hub.go`](../../guardian-ai/backend/hub.go) es un fan-out sin persistencia: 48
líneas. Cada evento que entra al store se emite a todos los suscriptores de
`/ws`. Los clientes que se conectan tarde no reciben historial por el socket —
lo piden por `GET /api/calls/:id/events`.

Esa separación (socket para lo vivo, REST para lo histórico) es deliberada:
evita que el hub tenga que mantener buffers por cliente y hace trivial la
reconexión.

## Tolerancia a fallos

| Falla | Comportamiento |
|---|---|
| Supabase caído | El sistema **sigue conversando**. `persist.go` degrada a memoria; los eventos se pierden al reiniciar, la conversación no |
| Sin `OPENAI_API_KEY` | RAG cae a modo keyword (ver [rag.md](../rag.md)); el LLM no arranca |
| Sin Kapso | El canal WhatsApp corre en modo simulación por `/api/whatsapp/simulate-inbound` |
| Protege API caída | El motor cae al fallback GPT libre (`GUARDIAN_DISABLED`) |
| WebSocket cortado | El cliente rehidrata por REST usando `Sequence` para detectar el hueco |

El principio: **cada dependencia externa tiene un modo degradado explícito**, y
`/api/capabilities` dice cuál está activo. Nada falla en silencio.

## Qué NO es esta arquitectura

Honestidad sobre los límites, porque un jurado técnico los va a encontrar:

- **No es CQRS distribuido.** Un proceso, una base. Las proyecciones corren en
  el mismo binario, sincrónicamente.
- **No hay workers ni colas.** Un turno se procesa en el request. Con la
  latencia real del LLM (~1.5 s) alcanza; con volumen de producción no.
- **No hay multi-tenancy.** Una instancia, una marca.
- **El RAG es en memoria.** Ver [ADR-0003](../adr/0003-rag-en-memoria.md): con 5
  documentos es la decisión correcta; con 5.000 no lo sería.

## Ver también

- [API](../api/README.md) — los 33 endpoints
- [Catálogo de eventos](event-catalog.md) — los 22 tipos y sus payloads
- [ADRs](../adr/README.md) — por qué cada decisión
