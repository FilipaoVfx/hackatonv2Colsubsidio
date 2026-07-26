# ADR-0001 — Event sourcing como modelo de datos

## Estado

Aceptada.

## Contexto

Secura es un agente de IA que conversa con afiliados reales sobre productos
financieros. Tres preguntas tienen que poder responderse **después** de que la
conversación terminó:

1. ¿Qué le dijo exactamente el sistema al cliente?
2. ¿Por qué lo dijo — qué contexto recuperó, qué herramienta consultó, en qué
   estado del embudo estaba?
3. ¿Cuánto costó ese turno en tokens y en tiempo?

Con un modelo CRUD clásico —una tabla `conversations` que se sobrescribe en cada
turno— ninguna tiene respuesta. El último UPDATE borra la evidencia del
anterior.

Además, el hackathon necesitaba una interfaz que mostrara el razonamiento del
agente en vivo. Eso exige un stream de lo que va pasando, no un poll de estado
final.

## Opciones consideradas

**A. CRUD + tabla de auditoría aparte.**
Familiar y rápido de escribir. Pero la auditoría se desincroniza del estado en
cuanto alguien añade un camino de escritura y olvida registrar. La causalidad
—qué provocó qué— no queda representada.

**B. CRUD + logs estructurados a un APM.**
Resuelve la observabilidad, no la auditoría: los logs caducan, no son fuente de
verdad, y reconstruir una conversación desde ellos es arqueología.

**C. Event sourcing.**
El estado es la reducción de un log append-only. Las vistas son proyecciones
derivadas.

## Decisión

**Opción C.** Todo cambio de estado se emite como un evento inmutable con un
envelope de siete campos ([`events.go`](../../guardian-ai/backend/events.go)):
`event_id`, `type`, `call_id`, `sequence`, `timestamp`, `producer`, `payload`.

22 tipos de evento. `sequence` es monótono por `call_id`.

Las vistas que consume la interfaz —`calls`, `call_transcript`, `call_phases`,
`call_scores`, `call_insights`, `call_analytics`— las escribe
[`projector.go`](../../guardian-ai/backend/projector.go), de forma idempotente
por `event_id`.

## Consecuencias

**A favor**

- Auditoría completa **por construcción**, no por instrumentación añadida. Las
  tres preguntas del contexto se responden leyendo el log.
- El mismo stream sirve cuatro propósitos a la vez: persistencia, dashboard en
  vivo por WebSocket, replay de una conversación y analítica de costos. No hay
  cuatro sistemas que puedan discrepar.
- `sequence` monótono hace **detectable** la pérdida de mensajes: un consumidor
  que recibe 7 después de 5 sabe que le falta el 6 y rehidrata por REST. La CLI
  hace exactamente eso.
- Reproyectar el log completo reconstruye cualquier vista. Un bug en una
  proyección se arregla y se recalcula, sin pérdida de datos.

**En contra**

- **`projector.go` es la pieza más grande del backend** (884 líneas). Todo tipo
  de evento nuevo obliga a tocarlo. Es el archivo que más va a doler mantener.
- **Latencia entre evento y proyección.** Una conversación recién creada tiene
  eventos pero todavía no tiene fila en `call_analytics`, así que
  `/api/analytics/calls/:id` devuelve 404. Todo cliente tiene que tratar ese 404
  como estado normal — la CLI necesitó separar el error de detalle del error de
  eventos justamente por esto.
- **El log crece sin límite.** No hay retención, compactación ni snapshots.
- **El envelope es un contrato.** La CLI lo duplica en su propio módulo Go;
  cambiarlo en el backend sin cambiarlo allí rompe el cliente en tiempo de
  ejecución, no de compilación.
- Curva de aprendizaje: "para cambiar un dato hay que emitir un evento" no es
  obvio para quien llega de CRUD.

**Cuándo revisar**

Si el volumen hace inviable reproyectar, o si el proyector se vuelve
inmanejable, el siguiente paso natural son snapshots periódicos por `call_id` —
no abandonar el modelo.

## Ver también

- [Arquitectura](../architecture/README.md)
- [Catálogo de eventos](../architecture/event-catalog.md)
- [ADR-0002](0002-supabase-event-store.md)
