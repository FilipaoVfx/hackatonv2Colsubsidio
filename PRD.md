# Guardian AI - MVP Requirements
## AI Voice Sales Advisor for Insurance
### Hackathon MVP Specification v1.0

---

# 1. Objetivo

Construir un **asesor comercial por voz impulsado por IA** capaz de acompañar a un potencial cliente desde el descubrimiento de sus necesidades hasta la recomendación personalizada de un seguro, sin intervención humana.

El objetivo **NO** es vender un seguro.

El objetivo es demostrar una plataforma inteligente capaz de:

- comprender al usuario
- construir un perfil dinámico
- detectar necesidades
- justificar recomendaciones
- mostrar el razonamiento del agente en tiempo real

---

# 2. Objetivos del MVP

Debe ser posible demostrar:

✅ Llamada por voz

✅ Conversación natural

✅ Perfilamiento automático

✅ Recomendación personalizada

✅ Dashboard en tiempo real

✅ Pipeline de decisión

✅ Métricas de IA

Todo en menos de 5 minutos.

---

# 3. Alcance (Scope)

## Incluye

- Llamadas mediante Vapi
- Voz ElevenLabs
- GPT-4o
- Dashboard React
- Backend Go
- Persistencia Supabase
- Observabilidad básica
- Historial de llamadas
- Timeline de conversación
- Feature Store
- Recomendación explicable

---

## Fuera del MVP

- Multiagente
- MCP
- Pipecat
- LiveKit
- Kubernetes
- RabbitMQ
- Kafka
- Pinecone
- Microservicios
- Integración real con aseguradoras
- Firma electrónica
- Pagos
- WhatsApp
- CRM empresarial

---

# 4. Arquitectura

```text
Cliente

↓

Vapi

↓

ElevenLabs

↓

Go Backend

↓

Conversation Engine

↓

Decision Engine

↓

LLM

↓

Tool Layer

↓

Supabase

↓

Dashboard React
```

---

# 5. Stack

## Frontend

React

Vite

Tailwind

shadcn/ui

Zustand

TanStack Query

---

## Backend

Go

Fiber

WebSocket

REST

---

## IA

GPT-4o

---

## Voz

ElevenLabs

---

## Telefonía

Vapi

---

## Base de datos

Supabase

PostgreSQL

---

## Vector Search

pgvector

---

## Observabilidad

Langfuse

---

# 6. Arquitectura lógica

```text
Presentation

↓

API

↓

Conversation Engine

↓

Decision Engine

↓

Tool Engine

↓

LLM

↓

Persistence
```

---

# 7. Componentes

## Conversation Engine

Responsable de:

- mantener contexto
- construir memoria
- controlar estados
- emitir eventos

---

## Decision Engine

Responsable de:

- decidir siguiente acción
- seleccionar herramientas
- determinar narrativa

---

## Tool Engine

Responsable de ejecutar herramientas.

Ejemplo

- buscar productos
- consultar cliente
- guardar llamada

---

## Prompt Builder

Construcción dinámica del prompt.

---

## Feature Store

Perfil dinámico del cliente.

Ejemplo

```yaml
family:true

children:2

pet_owner:true

traveler:false

risk:high

budget:medium

hesitation:price
```

---

## Dashboard

Visualización en tiempo real.

---

# 8. Patrones de diseño

## Singleton

Usar únicamente para:

- Config
- Logger
- Database
- Redis
- EventBus
- OpenAI Client
- Vapi Client
- ElevenLabs Client

---

## Observer

Todo el sistema será orientado a eventos.

Ejemplo

```
CALL_STARTED

↓

Publish()

↓

Dashboard

↓

Analytics

↓

Logger

↓

Timeline
```

---

## State

Estados de llamada.

```
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

---

## Strategy

Motor de recomendación.

Cada narrativa implementa una estrategia distinta.

---

## Adapter

Para desacoplar:

- Vapi
- ElevenLabs
- GPT
- Supabase

---

## Repository

Acceso único a datos.

---

# 9. Eventos

Eventos mínimos

```
CALL_STARTED

CALL_CONNECTED

USER_SPOKE

TRANSCRIPT_UPDATED

FEATURE_UPDATED

INTENT_UPDATED

PROMPT_GENERATED

LLM_RESPONSE

TOOL_CALLED

TOOL_FINISHED

VOICE_SENT

CALL_ENDED

ERROR
```

---

# 10. Dashboard (Mission Control)

Debe visualizar en tiempo real.

## Panel izquierdo

Timeline

## Panel central

Conversación

## Panel derecho

IA

---

Información

- Estado
- Latencia
- Tokens
- Coste
- Riesgo
- Narrativa
- Features
- Herramientas
- Sentimiento

---

# 11. Pipeline View

Cada llamada se representa como pipeline.

```
Call Started

↓

Discovery

↓

Profile

↓

Risk

↓

Recommendation

↓

Objection

↓

Acceptance

↓

Summary
```

Cada etapa debe mostrar:

- duración
- eventos
- herramientas
- prompts
- respuesta IA

---

# 12. Funcionales

## RF-001

Iniciar llamada.

---

## RF-002

Recibir audio.

---

## RF-003

Transcribir conversación.

---

## RF-004

Actualizar Feature Store.

---

## RF-005

Construir perfil.

---

## RF-006

Seleccionar narrativa.

---

## RF-007

Generar recomendación.

---

## RF-008

Responder mediante voz.

---

## RF-009

Registrar llamada.

---

## RF-010

Mostrar Mission Control.

---

## RF-011

Mostrar Timeline.

---

## RF-012

Mostrar Tool Calls.

---

## RF-013

Mostrar costos IA.

---

## RF-014

Guardar transcripción.

---

## RF-015

Generar resumen final.

---

# 13. No funcionales

Tiempo de respuesta

< 2 segundos.

---

Dashboard

Tiempo real.

---

Disponibilidad

99% durante demo.

---

Código

Modular.

---

Arquitectura

Orientada a eventos.

---

API

REST + WebSocket.

---

Persistencia

Automática.

---

# 14. Base de datos

customers

calls

transcripts

events

features

recommendations

prompts

tool_calls

---

# 15. Flujo

```
Cliente

↓

Vapi

↓

Speech

↓

Transcript

↓

Conversation Engine

↓

Feature Store

↓

Decision Engine

↓

Prompt Builder

↓

GPT

↓

Voice

↓

Cliente
```

---

# 16. Observabilidad

Registrar

- Prompt
- Tiempo
- Tokens
- Coste
- Tool Calls
- Latencia
- Errores

---

# 17. Métricas

Por llamada

- duración
- coste
- sentimiento
- narrativa
- score
- herramientas utilizadas
- número de interrupciones
- tiempo de respuesta

---

# 18. Estructura

```
frontend/

backend/

docs/

database/

scripts/

docker/

deploy/
```

---

# 19. Roadmap MVP

## Día 1

Backend

React

Supabase

---

## Día 2

Vapi

ElevenLabs

GPT

---

## Día 3

Conversation Engine

Feature Store

Dashboard

---

## Día 4

Mission Control

Timeline

Analytics

---

## Día 5

Testing

Pulido

Pitch

---

# 20. Criterios de éxito

El jurado debe poder:

- realizar una llamada
- mantener una conversación natural
- recibir una recomendación personalizada
- observar cómo la IA razona en tiempo real
- entender por qué tomó cada decisión
- visualizar métricas de la llamada
- finalizar con un resumen generado automáticamente

---

# 21. Recomendaciones (Fuera del Scope)

Estas funcionalidades quedan documentadas para una segunda fase, pero **NO deben implementarse durante el hackathon**.

## Arquitectura

- Migrar de arquitectura modular a microservicios.
- Incorporar NATS como Event Bus distribuido.
- Adoptar Event Sourcing completo.
- Añadir CQRS para consultas analíticas.

## IA

- Multiagente (Discovery Agent, Sales Agent, Closing Agent).
- MCP para integración universal con herramientas.
- Evaluador automático de calidad de llamadas.
- Motor A/B de prompts.
- Memoria persistente entre conversaciones.

## Integraciones

- WhatsApp.
- Email.
- CRM (HubSpot, Salesforce).
- Google Calendar.
- Pasarela de pagos.
- Firma electrónica.

## Infraestructura

- Kubernetes.
- Autoscaling.
- Pipecat + LiveKit para reemplazar Vapi.
- Redis Cluster.
- Observabilidad completa con OpenTelemetry + Grafana.

## Analítica

- Dashboard ejecutivo.
- Predicción de conversión.
- Heatmaps conversacionales.
- Comparación entre agentes y versiones de prompts.
- Costeo detallado por proveedor de IA.

---

# Principio rector del MVP

> **"Construimos una plataforma de inteligencia comercial por voz, no un chatbot."**

Toda decisión técnica deberá favorecer:

- simplicidad,
- demostración en tiempo real,
- explicabilidad,
- desacoplamiento,
- capacidad de evolución posterior.
