# System Specification

Este documento define el MVP de Guardian AI.

## Objetivo
Construir un AI Voice Sales Advisor con:
- Vapi
- ElevenLabs
- Go (Fiber)
- React + Bun
- Supabase PostgreSQL

## Alcance
- Llamadas por voz
- Perfilamiento dinámico
- Recomendación explicable
- Dashboard Mission Control
- Pipeline en tiempo real

## Arquitectura
Frontend (React+Bun) → Go API → Conversation Engine → Decision Engine → LLM → Tool Layer → Supabase

## Patrones
Observer, Singleton, State, Strategy, Adapter, Repository.

## Roadmap
Día1 infraestructura.
Día2 telefonía e IA.
Día3 motores conversacionales.
Día4 dashboard.
Día5 pruebas y demo.

## Fuera del scope
Kubernetes, microservicios, MCP, Pipecat, LiveKit, Kafka, RabbitMQ, Pinecone.
