# Architecture Decision Records (ADR)
## Guardian AI
### Versión 1.0

---

# Propósito

Este documento registra las decisiones arquitectónicas tomadas para el MVP de Guardian AI.

Cada ADR incluye:

- Contexto
- Decisión
- Alternativas consideradas
- Consecuencias
- Estado

---

# ADR-001
## Arquitectura Modular Monolítica

### Estado

✅ Aprobado

### Contexto

El proyecto será desarrollado durante un hackathon con un tiempo limitado.

Se requiere:

- alta velocidad de desarrollo
- facilidad para depurar
- mínimo overhead
- posibilidad de evolucionar posteriormente

### Decisión

Construir un **Modular Monolith**.

Cada dominio estará desacoplado mediante interfaces.

```text
Backend

├── conversation
├── decision
├── tools
├── dashboard
├── analytics
├── repository
└── adapters
```

### Alternativas

- Microservicios
- Serverless
- Hexagonal completa

### Razón

Un monolito modular reduce complejidad sin sacrificar escalabilidad futura.

---

# ADR-002
## Backend en Go

### Estado

✅ Aprobado

### Contexto

El sistema manejará:

- WebSockets
- Streaming
- Eventos
- Tiempo real
- Llamadas concurrentes

### Decisión

Go será el lenguaje principal del backend.

Framework seleccionado:

Fiber

### Alternativas

- FastAPI
- NestJS
- Express

### Razón

Go ofrece:

- excelente concurrencia
- bajo consumo de memoria
- simplicidad
- despliegue sencillo

---

# ADR-003
## Frontend React + Bun

### Estado

✅ Aprobado

### Contexto

El Dashboard requiere:

- SPA
- actualización en tiempo real
- componentes reutilizables

### Decisión

Frontend:

- React
- Vite
- Bun (runtime y package manager)
- Tailwind
- heroui/https://heroui.com/en/docs/react/getting-started

### Alternativas

- Next.js
- Angular
- Vue

### Razón

React ofrece el ecosistema más maduro para dashboards en tiempo real.

Bun acelera el desarrollo, instalación de dependencias y ejecución local.

No se utilizará Bun como backend.

---

# ADR-004
## Base de Datos

### Estado

✅ Aprobado

### Decisión

Supabase PostgreSQL

### Razón

Permite disponer rápidamente de:

- PostgreSQL
- Auth (si se requiere)
- Storage
- pgvector
- APIs

Reduce considerablemente el tiempo de desarrollo.

---

# ADR-005
## Arquitectura Event Driven

### Estado

✅ Aprobado

### Contexto

El sistema necesita múltiples consumidores de los mismos eventos.

Ejemplo:

CALL_STARTED

↓

Dashboard

↓

Analytics

↓

Logger

↓

Timeline

### Decisión

Toda interacción importante será publicada como evento.

### Razón

Reduce el acoplamiento.

Permite agregar funcionalidades sin modificar el flujo principal.

---

# ADR-006
## Observer Pattern

### Estado

✅ Aprobado

### Decisión

El Event Bus implementará Observer.

Todos los módulos escucharán eventos.

Nunca se comunicarán directamente.

### Beneficios

- bajo acoplamiento
- extensibilidad
- mantenibilidad

---

# ADR-007
## Singleton Pattern

### Estado

✅ Aprobado

### Contexto

Existen recursos compartidos.

### Se utilizará Singleton únicamente para:

- Config
- Logger
- Database
- Supabase Client
- EventBus
- OpenAI Client
- ElevenLabs Client
- Vapi Client

### No utilizar

Conversation Context

Feature Store

Estados de llamada

---

# ADR-008
## State Pattern

### Estado

✅ Aprobado

### Decisión

Cada llamada tendrá un estado.

```text
CREATED

↓

CONNECTED

↓

LISTENING

↓

THINKING

↓

RESPONDING

↓

ENDED
```

Evita lógica basada en múltiples condicionales.

---

# ADR-009
## Strategy Pattern

### Estado

✅ Aprobado

### Contexto

La recomendación cambia dependiendo del perfil.

### Decisión

Cada narrativa implementará una estrategia.

Ejemplos

- Protector Familiar
- Profesional
- Emprendedor
- Viajero
- Dueño de Mascotas

---

# ADR-010
## Adapter Pattern

### Estado

✅ Aprobado

### Contexto

Los proveedores externos pueden cambiar.

### Decisión

Todos los proveedores serán encapsulados mediante adapters.

```text
VapiAdapter

ElevenLabsAdapter

LLMAdapter

SupabaseAdapter
```

Esto evita dependencia directa.

---

# ADR-011
## Repository Pattern

### Estado

✅ Aprobado

### Decisión

Todo acceso a datos será mediante repositories.

Ejemplo

CustomerRepository

CallRepository

FeatureRepository

No se ejecutarán consultas SQL desde la lógica de negocio.

---

# ADR-012
## LLM Gateway

### Estado

✅ Aprobado

### Contexto

Es posible cambiar de proveedor.

### Decisión

Todo acceso al modelo se hará mediante una capa Gateway.

```text
Conversation Engine

↓

LLM Gateway

↓

GPT

Claude

Gemini
```

---

# ADR-013
## Telefonía

### Estado

✅ Aprobado

### Decisión

Vapi será el proveedor del MVP.

### Razón

Reduce semanas de desarrollo.

La lógica permanecerá desacoplada.

---

# ADR-014
## Voz

### Estado

✅ Aprobado

Proveedor

ElevenLabs

### Razón

Excelente calidad para español latino.

Streaming estable.

---

# ADR-015
## Dashboard Realtime

### Estado

✅ Aprobado

### Decisión

Toda la comunicación Dashboard ↔ Backend será mediante WebSocket.

REST únicamente para consultas históricas.

---

# ADR-016
## Mission Control

### Estado

✅ Aprobado

### Decisión

La interfaz principal será un Mission Control.

No un CRM.

No un Dashboard tradicional.

El usuario observará:

- Timeline
- Pipeline
- Eventos
- Riesgo
- Costos
- Herramientas
- Features
- Estado del agente

en tiempo real.

---

# ADR-017
## Feature Store

### Estado

✅ Aprobado

### Contexto

El perfil del cliente cambia durante la conversación.

### Decisión

Mantener un Feature Store dinámico.

Ejemplo

```yaml
children:2

pet_owner:true

traveler:false

risk:high

budget:medium

hesitation:price
```

---

# ADR-018
## Event Sourcing (Light)

### Estado

✅ Aprobado para MVP

### Decisión

No almacenar únicamente el estado final.

Registrar también los eventos.

Ejemplo

CALL_STARTED

↓

FEATURE_UPDATED

↓

TOOL_CALLED

↓

PROMPT_GENERATED

↓

LLM_RESPONSE

↓

CALL_ENDED

### Razón

Permite reconstruir completamente la conversación.

---

# ADR-019
## Observabilidad

### Estado

✅ Aprobado

Proveedor

Langfuse

### Razón

Visualizar:

- prompts
- respuestas
- costos
- latencias
- tool calls

Sin desarrollar herramientas propias.

---

# ADR-020
## Filosofía Arquitectónica

Guardian AI NO será construido como un chatbot.

Será construido como una plataforma de inteligencia conversacional basada en eventos.

Los canales (voz, WhatsApp, web, etc.) serán únicamente adaptadores.

La lógica de negocio permanecerá completamente independiente de cualquier proveedor externo.

---

# Principios Arquitectónicos

1. Event First

Todo cambio importante genera un evento.

---

2. Explainability

Toda recomendación debe poder justificarse.

---

3. Realtime

Toda la información relevante será visible en tiempo real.

---

4. Low Coupling

Los módulos no deben conocerse entre sí.

---

5. Provider Agnostic

Ningún proveedor externo debe afectar la lógica de negocio.

---

6. Evolutivo

Toda decisión del MVP debe facilitar una evolución posterior sin reescrituras importantes.
