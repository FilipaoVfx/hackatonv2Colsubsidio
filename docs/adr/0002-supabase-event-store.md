# ADR-0002 — Supabase + pgx como event store, sin ORM

## Estado

Aceptada.

## Contexto

El log de eventos de [ADR-0001](0001-event-sourcing.md) necesita persistencia
duradera, consultable por `call_id` y ordenada por `sequence`. El equipo ya
tenía un proyecto Supabase disponible.

La pregunta real era cómo hablarle: SDK de Supabase, un ORM de Go, o SQL.

## Opciones consideradas

**A. SDK de Supabase (PostgREST).**
Rápido de empezar. Pero mete una capa HTTP entre el proceso y una base que está
a un socket de distancia, y sus errores llegan como respuestas REST genéricas en
vez de códigos de Postgres.

**B. Un ORM (GORM, ent).**
Migraciones y modelos tipados. A cambio: reflexión en caliente, SQL generado
difícil de auditar, y un modelo de datos que no encaja — un event store tiene
una tabla append-only y proyecciones, no un grafo de entidades con relaciones.

**C. `pgx/v5` directo.**
El driver nativo de Postgres para Go. SQL escrito a mano.

## Decisión

**Opción C.** [`persist.go`](../../guardian-ai/backend/persist.go) habla pgx
contra Supabase con SQL explícito. Sin ORM, sin SDK.

Conexión por el **pooler** (IPv4), no por el host directo:

```
postgresql://postgres.<ref>:<password>@aws-0-<region>.pooler.supabase.com:5432/postgres
```

Si `SUPABASE_DB_URL` falta o la base no responde, el sistema **degrada a
memoria**: sigue conversando, y los eventos se pierden al reiniciar. Una base
caída no puede tumbar una conversación con un cliente.

## Consecuencias

**A favor**

- **Una dependencia directa** en vez de un stack. `go.mod` del backend tiene
  cuatro requires en total.
- El SQL está a la vista: se lee, se explica y se optimiza sin adivinar qué
  generó una capa intermedia.
- Errores de Postgres con su código real, útiles para distinguir un conflicto de
  una caída.
- **Degradación explícita**: la persistencia es importante, la conversación lo
  es más.

**En contra**

- **Sin migraciones.** El esquema se creó a mano y no hay herramienta que lo
  versione. Levantar el proyecto en un Supabase nuevo exige recrearlo a mano.
  Es la deuda más concreta de esta decisión.
- **Sin modelos tipados**: los payloads viajan como `map[string]interface{}` y
  los errores de tipo aparecen en ejecución.
- SQL repetido entre proyecciones.
- Atarse al pooler y a la forma de la URL de Supabase acopla al proveedor más
  de lo que parece.

**Cuándo revisar**

Si el esquema empieza a cambiar seguido, hace falta una herramienta de
migraciones (`goose`, `atlas`) antes que un ORM. El problema es el versionado
del esquema, no el acceso a datos.

## Ver también

- [Arquitectura](../architecture/README.md) · [ADR-0001](0001-event-sourcing.md)
