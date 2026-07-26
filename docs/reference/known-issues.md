# Defectos conocidos

Lo que está roto y todavía no se arregló. Registrarlo cuesta menos que
explicarlo en vivo.

## `call_id` malformados en `GET /api/calls`

**Síntoma.** El listado contiene entradas como
`"ics/calls/53701544-8c04-498e-86d3-5b"`: un fragmento de path colado como si
fuera un ID, y truncado.

**Causa.** Parseo de path en la ingesta que toma la porción equivocada.

**Impacto.** Un cliente que seleccione esa entrada pide eventos de un ID que no
existe. La CLI lo notó al ordenar la lista tras el poll de 2 s.

**Rodeo.** Validar formato UUID antes de usar un ID del listado.

## Analítica 404 en conversaciones recientes

**Síntoma.** `GET /api/analytics/calls/:id` devuelve 404 en una conversación
recién creada, aunque `GET /api/calls/:id/events` sí responde.

**Causa.** No es un defecto: las proyecciones se escriben tras el primer turno
completo. Es la latencia normal entre evento y proyección
([ADR-0001](../adr/0001-event-sourcing.md)).

**Rodeo.** Tratar el 404 como estado vacío. Los consumidores deben separar el
error de detalle del error de eventos — si los mezclan, un 404 inofensivo
bloquea vistas que sí tenían datos.

## Las sesiones de WhatsApp se persisten por teléfono

**Síntoma.** Reusar el mismo `from` en `simulate-inbound` continúa la
conversación anterior en vez de empezar una nueva, y puede aparecer ya en un
estado avanzado o terminal.

**Causa.** Comportamiento correcto para usuarios reales; sorprendente en demos.

**Rodeo.** Generar un número fresco por disparo. La CLI lo hace.

## El workflow de despliegue de la landing falla siempre

**Síntoma.** `deploy.yml` en `landingHackaton30x` sale en rojo en cada push.

**Causa.** Falta el secret `CLOUDFLARE_API_TOKEN`. Los despliegues reales los
hace la integración nativa de Cloudflare Pages con el repo; la Action es
redundante.

**Rodeo.** Configurar el token o borrar el workflow.

## El log de eventos crece sin límite

Sin retención, compactación ni snapshots. A escala de hackathon no molesta; en
producción sí. Ver [roadmap](../../ROADMAP.md).

## Sin tests de la TUI

Los módulos de Bubble Tea no tienen cobertura. Los defectos de render
—desbordes, orden de lista, stepper atascado— los encontró la inspección visual.
`go build` no los ve. Ver [testing](../testing.md#huecos-conocidos).
