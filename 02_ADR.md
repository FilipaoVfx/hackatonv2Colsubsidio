# Architecture Decision Records (ADR)
# Guardian AI

Versión: 2.0

---

# Filosofía Arquitectónica

Guardian AI no se concibe como un chatbot.

Se diseña como una plataforma de inteligencia conversacional orientada a eventos, donde la voz es únicamente un canal de entrada y salida.

Toda la lógica de negocio reside en el backend y permanece independiente de cualquier proveedor externo.

Principios:

- Event First
- Realtime by Design
- AI Explainability
- Low Coupling
- Provider Agnostic
- Modular Architecture
- Hackathon MVP, Production Mindset

---

# ADR-001
## Arquitectura General

Estado

✅ Aprobado

### Decisión

Se implementará un **Modular Monolith** organizado por dominios.

```text
backend/

conversation/

decision/

analytics/

repository/

events/

dashboard/

adapters/

config/
```

### Razón

Permite:

- simplicidad
- rapidez de desarrollo
- facilidad de pruebas
- futura migración a microservicios

---

# ADR-002
## Backend

Estado

✅ Aprobado

### Decisión

Go será el único backend del sistema.

Framework

Fiber

### Razón

El sistema requiere:

- concurrencia
- WebSockets
- streaming
- procesamiento en tiempo real
- baja latencia

Go ofrece mejor rendimiento para este escenario que alternativas como FastAPI o Node.js.

Toda la lógica de negocio residirá exclusivamente en Go.

---

# ADR-003
## Frontend

Estado

✅ Aprobado

### Decisión

React

Vite

Tailwind

shadcn/ui

Bun

### Razón

React ofrece el ecosistema más sólido para aplicaciones SPA.

Bun será utilizado como:

- runtime
- package manager
- task runner

No será utilizado como servidor backend.

---

# ADR-004
## Plataforma de Datos

Estado

✅ Aprobado

### Decisión

Supabase será utilizado exclusivamente como plataforma de datos.

Guardian AI NO dependerá del SDK de Supabase para implementar la lógica de negocio.

Toda comunicación con la base de datos se realizará mediante PostgreSQL utilizando pgx.

```text
Go

↓

Repository Layer

↓

pgx

↓

Supabase PostgreSQL
```

### Razón

Esto permite:

- independencia del proveedor
- mayor rendimiento
- control total sobre consultas
- evitar acoplamiento con APIs específicas de Supabase

Supabase aporta:

- PostgreSQL administrado
- Backups
- Dashboard
- Storage
- pgvector (futuro)

---

# ADR-005
## Repository Pattern

Estado

✅ Aprobado

Toda interacción con la base de datos deberá implementarse mediante repositories.

Ejemplo

CustomerRepository

CallRepository

FeatureRepository

RecommendationRepository

No se permitirá acceso SQL desde la lógica de negocio.

---

# ADR-006
## Arquitectura Orientada a Eventos

Estado

✅ Aprobado

Toda interacción relevante generará un evento.

Ejemplo

CALL_STARTED

↓

FEATURE_UPDATED

↓

TOOL_CALLED

↓

LLM_RESPONSE

↓

CALL_ENDED

Los módulos nunca se comunicarán directamente.

---

# ADR-007
## Observer Pattern

Estado

✅ Aprobado

El Event Bus implementará el patrón Observer.

Cada módulo podrá suscribirse a eventos sin modificar el productor.

Esto garantiza bajo acoplamiento.

---

# ADR-008
## Singleton Pattern

Estado

✅ Aprobado

Singleton únicamente para:

- Config
- Logger
- EventBus
- Database Pool
- OpenAI Client
- ElevenLabs Client
- Vapi Client

Nunca para:

- Conversation Context
- Feature Store
- Call State

---

# ADR-009
## State Pattern

Estado

✅ Aprobado

Cada conversación tendrá un estado claramente definido.

```text
CREATED

↓

CONNECTED

↓

DISCOVERY

↓

LISTENING

↓

THINKING

↓

RESPONDING

↓

ENDED
```

---

# ADR-010
## Strategy Pattern

Estado

✅ Aprobado

Cada narrativa comercial implementará una estrategia independiente.

Ejemplo

- Protector Familiar

- Dueño de Mascotas

- Profesional

- Emprendedor

- Viajero

Esto elimina lógica basada en múltiples condicionales.

---

# ADR-011
## Adapter Pattern

Estado

✅ Aprobado

Los proveedores externos nunca serán utilizados directamente.

Se implementarán adapters para:

- Vapi

- ElevenLabs

- OpenAI

Esto permite reemplazar cualquier proveedor sin modificar el dominio.

---

# ADR-012
## LLM Gateway

Estado

✅ Aprobado

Todo acceso a modelos de lenguaje pasará por una capa Gateway.

```text
Conversation Engine

↓

LLM Gateway

↓

GPT

↓

Claude

↓

Gemini
```

El dominio nunca conocerá el proveedor.

---

# ADR-013
## Mission Control

Estado

✅ Aprobado

La interfaz principal del sistema será un Mission Control.

No un CRM.

No un dashboard tradicional.

Permitirá visualizar en tiempo real:

- Timeline
- Pipeline
- Eventos
- Tool Calls
- Features
- Riesgo
- Costos
- Latencia
- Estado del agente

---

# ADR-014
## Observabilidad

Estado

✅ Aprobado

Langfuse será utilizado para registrar:

- prompts
- respuestas
- tokens
- costos
- latencias
- herramientas

---

# ADR-015
## Persistencia basada en Eventos

Estado

✅ Aprobado

Además del estado final de una llamada, el sistema almacenará los eventos que construyeron dicho estado.

Ejemplo

CALL_STARTED

↓

USER_SPOKE

↓

FEATURE_UPDATED

↓

PROMPT_GENERATED

↓

LLM_RESPONSE

↓

CALL_ENDED

Esto permitirá reconstruir completamente una conversación para análisis posteriores.

---

# ADR-016
## Comunicación en Tiempo Real

Estado

✅ Aprobado

El Dashboard se comunicará exclusivamente mediante WebSocket con el backend Go.

Supabase Realtime no será utilizado durante el MVP.

La única fuente de verdad para eventos será el Event Bus interno.

---

# ADR-017
## Dependencias Externas

Estado

✅ Aprobado

El dominio de Guardian AI no dependerá directamente de:

- Supabase
- OpenAI
- Vapi
- ElevenLabs

Todos los proveedores serán consumidos mediante interfaces y adapters.

Esto garantiza portabilidad y evolución futura.

---

# Arquitectura Objetivo

```text
                 React
             (Bun + Vite)

                  │

        REST + WebSocket

                  │

             Go (Fiber)

                  │

────────────────────────────────────

Conversation Engine

Decision Engine

Tool Engine

Prompt Builder

Event Bus

Mission Control

Repository Layer

────────────────────────────────────

                  │

          pgx Connection Pool

                  │

        Supabase PostgreSQL

────────────────────────────────────

Vapi Adapter

ElevenLabs Adapter

LLM Gateway

────────────────────────────────────
```

# Principio Final

> **Guardian AI considera a Supabase como infraestructura de datos, no como backend. El backend pertenece exclusivamente a Go. Toda decisión arquitectónica debe preservar esa separación de responsabilidades.**
