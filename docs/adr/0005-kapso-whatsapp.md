# ADR-0005 — Kapso en vez de Meta Cloud API directo

## Estado

Aceptada.

## Contexto

WhatsApp es el canal principal: es donde un afiliado de Colsubsidio pregunta de
verdad. Integrarlo era obligatorio, y el hackathon dura días, no semanas.

La Meta Cloud API exige verificación de negocio, una app de Meta, revisión de
permisos y un número registrado. El proceso tarda más que el hackathon entero.

## Opciones consideradas

**A. Meta Cloud API directo.**
Sin intermediarios ni margen. Bloqueado por la verificación de negocio.

**B. Twilio.**
Maduro y documentado. Onboarding igualmente pesado y más caro por mensaje.

**C. Kapso.**
Proxy sobre Meta Cloud API con sandbox inmediato: cuenta gratis, número de
prueba, mensajes reales en minutos.

## Decisión

**Opción C.** [`kapso.go`](../../guardian-ai/backend/kapso.go) (328 líneas) más
[`whatsapp_sessions.go`](../../guardian-ai/backend/whatsapp_sessions.go).

El webhook entra por `POST /api/whatsapp/webhook` y verifica HMAC contra
`KAPSO_WEBHOOK_SECRET` cuando está configurado.

Sin `KAPSO_API_KEY` el canal corre en modo simulación por
`POST /api/whatsapp/simulate-inbound`, que recorre el mismo pipeline completo.
La demo nunca depende de que WhatsApp esté disponible.

## Consecuencias

**A favor**

- **Mensajes reales en minutos** en vez de semanas de verificación.
- Sandbox con número de prueba: cualquiera del equipo escribe desde su celular.
- El modo simulación hace la demo reproducible **sin red ni cuenta**.
- Migrar a Meta directo después es cambiar un cliente: el resto del sistema solo
  ve eventos.

**En contra**

- **Dependencia de un tercero pequeño** entre el sistema y Meta. Su
  disponibilidad no se controla, y su estado no tiene SLA público.
- Margen sobre el precio de Meta.
- El sandbox exige registrar cada número de prueba a mano — no sirve para un
  piloto abierto.
- **La verificación HMAC se salta si falta la variable.** Cómodo en desarrollo,
  inaceptable en producción. Anotado en [seguridad](../security.md).
- Las sesiones se persisten **por número de teléfono**, así que reusar un número
  continúa la conversación anterior. Costó un bug real en la demo hasta que la
  CLI empezó a generar un número fresco por disparo.

**Cuándo revisar**

Para un piloto con afiliados reales hace falta número propio y verificación de
negocio. Ahí Kapso deja de ser el camino corto y pasa a ser un intermediario
innecesario.

## Ver también

- [API](../api/README.md) · [Seguridad](../security.md)
