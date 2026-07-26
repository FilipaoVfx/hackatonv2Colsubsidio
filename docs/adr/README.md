# Architecture Decision Records

Cada ADR documenta una decisión **que se tomó de verdad**, con las opciones que
se descartaron y lo que costó. Ninguno describe tecnología que el proyecto no
usa.

| # | Decisión | Estado |
|---|---|---|
| [0001](0001-event-sourcing.md) | Event sourcing como modelo de datos | Aceptada |
| [0002](0002-supabase-event-store.md) | Supabase + pgx como event store, sin ORM | Aceptada |
| [0003](0003-rag-en-memoria.md) | RAG en memoria — **sin base vectorial** | Aceptada |
| [0004](0004-openrouter-gateway.md) | OpenRouter como gateway de LLM | Aceptada |
| [0005](0005-kapso-whatsapp.md) | Kapso en vez de Meta Cloud API directo | Aceptada |
| [0006](0006-cli-bubbletea.md) | CLI en Bubble Tea como interfaz de operación | Aceptada |
| [0007](0007-guarda-read-only.md) | Guarda de solo lectura en la capa de datos | Aceptada |
| [0008](0008-monolito-go.md) | Monolito Go en vez de microservicios | Aceptada |

## Formato

```markdown
# ADR-NNNN — Título

## Estado
Propuesta | Aceptada | Reemplazada por ADR-XXXX

## Contexto
Qué problema había. Con datos, no adjetivos.

## Opciones consideradas
Las alternativas reales, con lo bueno y lo malo de cada una.

## Decisión
Qué se eligió y por qué.

## Consecuencias
Lo bueno, lo malo, y **cuándo habría que revisar esto**.
```

La sección de consecuencias es la que importa. Un ADR sin desventajas anotadas
no es una decisión documentada, es una justificación a posteriori.

## Histórico

[`adr-original.md`](../architecture/adr-original.md) conserva los ADR previos
del arranque del proyecto. Se mantiene por trazabilidad; algunos quedaron
desactualizados frente a lo que terminó implementándose, y estos ADR numerados
son la referencia vigente.
