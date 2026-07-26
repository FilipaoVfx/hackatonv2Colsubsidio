# Documentación

## Empezar

| Documento | Para qué |
|---|---|
| [README del proyecto](../README.md) | Qué es Secura y cómo levantarlo |
| [Contribuir](../CONTRIBUTING.md) | Entorno, commits, PRs |
| [FAQ](../FAQ.md) | Las preguntas que hace todo el mundo |

## Cómo funciona

| Documento | Para qué |
|---|---|
| [Arquitectura](architecture/README.md) | Event sourcing, proyecciones, tolerancia a fallos |
| [Catálogo de eventos](architecture/event-catalog.md) | Los 22 tipos y sus payloads |
| [API](api/README.md) | Los 33 endpoints |
| [Ejemplos de API](api/examples.md) | curl que funciona, copiado y pegado |

## Inteligencia

| Documento | Para qué |
|---|---|
| [LLM](llm.md) | Modelos, prompt por capas, tool calling, costos |
| [RAG](rag.md) | Chunking, embeddings en memoria, degradación |
| [Prompt engineering](prompt-engineering.md) | Persona, variables, guardrails |

## Operación

| Documento | Para qué |
|---|---|
| [Despliegue](deployment.md) | Docker, túneles, Pages, releases |
| [Observabilidad](observability.md) | Eventos como telemetría, procedencia |
| [Seguridad](security.md) | Secretos, prompt injection, **y lo que falta** |
| [Testing](testing.md) | 22 suites, qué cubren y qué no |
| [Defectos conocidos](reference/known-issues.md) | Lo roto, por escrito |

## Decisiones

[ADRs](adr/README.md) — ocho decisiones con sus alternativas descartadas y sus
consecuencias, incluido cuándo habría que revisar cada una.

## Referencia

| Documento | Para qué |
|---|---|
| [API de Colsubsidio Protege](reference/colsubsidio-protege-api.md) | Análisis del contrato externo |
| [Runbook de exposición](reference/runbook-api-protege.md) | Operación de la API de Protege |
| [Portafolio](reference/colsubsidio-portfolio.json) | Catálogo real capturado |

## Especificaciones

Documentos de producto del arranque del proyecto. Se conservan por trazabilidad;
donde discrepen con el código, **manda el código** — y la discrepancia debería
resolverse en un ADR.

[PRD](specs/prd.md) · [SRS](specs/srs.md) · [Agent Studio](specs/agent-studio.md) ·
[Motor conversacional](specs/conversation-engine-retrieval.md) ·
[Voz](specs/feature-voz-pipeline.md) · [WhatsApp](specs/feature-whatsapp.md) ·
[Afiliado 360](specs/feature-afiliado-360.md) ·
[Robustez](specs/feature-robustez-bot.md)

## Diseño

[Manifiesto de marca](design/brand-manifesto.md)
