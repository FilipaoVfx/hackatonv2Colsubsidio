# ADR-0008 — Monolito Go en vez de microservicios

## Estado

Aceptada.

## Contexto

El sistema toca cinco integraciones externas (OpenRouter, OpenAI, Kapso, Vapi,
ElevenLabs, Colsubsidio Protege), persiste eventos, proyecta vistas y sirve un
WebSocket. Es tentador partirlo en servicios: uno por canal, uno de IA, uno de
persistencia.

Contexto real: equipo de hackathon, días de plazo, una demo en vivo que no puede
fallar.

## Opciones consideradas

**A. Microservicios por dominio.**
Escalado independiente y límites claros. A cambio: orquestación, service
discovery, trazado distribuido, fallos parciales, y una demo con cinco cosas que
pueden caerse por separado.

**B. Serverless por endpoint.**
Escala a cero. Pero el arranque en frío arruina una conversación, y el
`EventStream` por WebSocket no encaja con funciones efímeras.

**C. Monolito.**

## Decisión

**Opción C.** Un binario Go, `package main`, 12.635 líneas repartidas en 48
archivos con responsabilidad única cada uno: `guardian.go` el motor,
`projector.go` las proyecciones, `rag.go` la recuperación, `kapso.go` WhatsApp.

Fiber v2 sirve HTTP y WebSocket en el mismo proceso. Docker Compose levanta el
backend, los mocks de Protege y nginx.

## Consecuencias

**A favor**

- **Una cosa que desplegar y una que puede fallar.** En una demo en vivo eso
  vale más que cualquier propiedad de escalado.
- El fan-out de eventos es una llamada en memoria: sin cola, sin broker, sin
  entrega al menos una vez que haya que deduplicar.
- Refactorizar entre módulos es mover una función, no versionar una API.
- Arranque en milisegundos; el binario cabe en un contenedor mínimo.
- **Cuatro dependencias directas.** Esa cifra es la decisión hecha número.

**En contra**

- **No escala horizontalmente sin trabajo.** El hub de WebSocket tiene estado en
  proceso: dos réplicas no comparten suscriptores.
- `package main` plano significa que **los tipos no son importables desde
  fuera** — por eso la CLI duplica el envelope `Event` en vez de compartirlo.
  Es el costo más concreto de esta decisión.
- Sin aislamiento de fallos: un panic en el proyector tumba el canal de
  WhatsApp.
- Un archivo de 1.048 líneas (`guardian.go`) empieza a pedir separación.
- Todo se despliega junto, aunque solo cambie una línea del RAG.

**Cuándo revisar**

El primer paso no es partir en servicios, sino **extraer `pkg/events`** para que
la CLI importe el envelope en vez de duplicarlo. Eso resuelve el costo real de
hoy sin traer sistemas distribuidos a un problema que no los necesita.

## Ver también

- [Arquitectura](../architecture/README.md) · [ADR-0001](0001-event-sourcing.md)
