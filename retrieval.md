# Guardian Conversation Engine
## Requerimiento Técnico - Módulo Conversacional WhatsApp (MVP)

> Versión: 1.0
> Objetivo: Hackathon Colsubsidio
> Estado: MVP Funcional

---

# 1. Objetivo

Desarrollar un módulo conversacional autónomo capaz de perfilar, calificar y nutrir leads provenientes de WhatsApp utilizando IA, integrándose con la API de Colsubsidio Protege para ejecutar las reglas de negocio de forma determinista.

El objetivo principal **NO** es construir un chatbot.

El objetivo es convertir un lead en un **Lead Ready for Advisor**, completamente perfilado y con alta probabilidad de cierre.

---

# 2. Principios de Arquitectura

## 2.1 IA Conversacional ≠ Motor de Decisiones

El LLM únicamente será responsable de:

- Comprender lenguaje natural.
- Mantener una conversación humana.
- Extraer información.
- Explicar recomendaciones.
- Generar respuestas naturales.
- Resumir conversaciones.

El LLM **NO** podrá:

- decidir elegibilidad
- calcular subsidios
- recomendar proyectos por cuenta propia
- validar afiliación
- aplicar reglas de negocio
- modificar estados arbitrariamente

Todas las decisiones de negocio deberán provenir de la API de Colsubsidio.

---

## 2.2 Arquitectura General

```text
                    WhatsApp

                        │

                 WhatsApp Gateway
               (Baileys / WPPConnect)

                        │

             Guardian Conversation Engine

                        │

          Conversation State Machine

                        │

               Decision Layer

                        │

        ┌───────────────┴────────────────┐
        │                                │
 Colsubsidio API                     LLM
```

---

# 3. Arquitectura por Componentes

## 3.1 WhatsApp Gateway

### Responsabilidad

Abstraer completamente WhatsApp.

Debe encargarse únicamente de:

- recibir mensajes
- enviar mensajes
- recibir multimedia
- administrar sesiones
- identificar usuario

No deberá contener ninguna lógica de negocio.

---

## 3.2 Conversation Engine

Es el núcleo del sistema.

Responsabilidades:

- interpretar intención
- construir contexto
- decidir siguiente acción
- ejecutar herramientas
- actualizar memoria
- generar respuesta

Debe ser completamente independiente del canal.

El mismo módulo deberá funcionar posteriormente con:

- Voz (ElevenLabs)
- Web Chat
- Contact Center

---

## 3.3 State Machine

Toda conversación deberá seguir estados controlados.

Estados mínimos:

```text
NEW

↓

AFFILIATION_CHECK

↓

PROFILE_DISCOVERY

↓

FINANCIAL_QUALIFICATION

↓

PROJECT_MATCHING

↓

READY_FOR_ADVISOR

↓

NURTURING

↓

COMPLETED
```

No se permitirán transiciones arbitrarias.

Cada transición deberá registrarse.

---

# 4. Flujo Conversacional

## Fase 1 — Identificación

Objetivo:

Identificar al usuario.

Datos esperados:

- documento
- teléfono

Consultar inmediatamente:

```
GET /users/search
```

Resultado esperado:

- afiliado
- no afiliado
- usuario existente

---

## Fase 2 — Perfilamiento Inteligente

Objetivo:

Construir el perfil financiero sin generar sensación de interrogatorio.

Extraer naturalmente:

- ingresos
- estado civil
- número de hijos
- dependientes
- municipio
- vivienda actual
- capacidad de ahorro
- subsidios
- crédito
- mascotas (opcional)
- intereses

Las preguntas deberán adaptarse al contexto.

Nunca utilizar formularios conversacionales.

---

## Fase 3 — Persistencia

Cada dato confirmado deberá persistirse inmediatamente.

Nunca esperar al final de la conversación.

Ejemplo:

```
PUT /users/{id}/variables
```

---

## Fase 4 — Calificación

Consultar:

- Rules
- Recommendations

La IA nunca calculará elegibilidad.

Toda decisión será responsabilidad de la API.

---

## Fase 5 — Explicación

La IA explicará el resultado obtenido por la API.

No inventará reglas.

No resumirá incorrectamente.

No modificará recomendaciones.

---

## Fase 6 — Cierre

Si:

```
READY_FOR_ADVISOR
```

crear handoff.

Si no:

```
NURTURING
```

crear flujo de nutrición.

---

# 5. Prompt Builder

El System Prompt será modular.

Nunca existirá un único prompt gigante.

Construcción:

```text
Persona

+

Business Rules

+

Conversation Rules

+

Conversation State

+

Customer Memory

+

Retrieved Context

+

Available Tools

↓

System Prompt
```

---

# 6. Gestión de Memoria

La memoria nunca dependerá del contexto del LLM.

Siempre deberá reconstruirse desde la API.

Fuente oficial:

```text
User

↓

Variables

↓

Conversation

↓

Prompt Builder
```

Tipos:

## Conversacional

Últimos mensajes.

---

## Estratégica

Variables persistentes.

---

## Comercial

- objeciones
- preferencias
- intereses
- proyectos vistos
- intención de compra

---

# 7. Tool Calling

El agente únicamente podrá utilizar herramientas registradas.

Lista inicial:

```
search_user

create_user

get_questions

save_variable

get_rules

get_recommendations

create_conversation

update_conversation

complete_conversation
```

No se permitirán llamadas arbitrarias.

---

# 8. Structured Outputs

Toda respuesta del LLM deberá cumplir un esquema.

Ejemplo:

```json
{
  "intent": "",
  "entities": {},
  "confidence": 0,
  "next_action": "",
  "assistant_message": ""
}
```

Nunca parsear texto libre.

---

# 9. RAG

Uso exclusivo para documentación.

Permitido:

- FAQ
- subsidios
- proyectos
- beneficios
- glosario
- políticas

Prohibido:

- reglas de negocio
- decisiones
- scoring

---

# 10. API Colsubsidio

La API será la única fuente oficial de verdad.

### Usuarios

```
GET /users/search
```

Buscar afiliación.

---

### Variables

```
PUT /users/{id}/variables
```

Persistir información.

---

### Conversaciones

Crear.

Actualizar.

Completar.

---

### Rules

Validar reglas.

---

### Recommendations

Generar proyectos elegibles.

---

# 11. Observabilidad

Registrar absolutamente todo.

Por mensaje:

```
timestamp

conversation_id

user_id

latencia

tokens

tool_calls

estado

confidence

variables nuevas

respuesta enviada

errores
```

---

# 12. KPIs

El sistema deberá medir:

- Leads recibidos.
- Afiliados detectados.
- Tiempo hasta perfilamiento.
- Tiempo hasta READY_FOR_ADVISOR.
- Leads nutridos.
- Conversaciones abandonadas.
- Preguntas realizadas.
- Conversión por canal.
- Tool Calls ejecutados.
- Tiempo promedio de respuesta.

---

# 13. Manejo de Errores

Si una herramienta falla:

- nunca inventar información
- informar al usuario de forma natural
- registrar el error
- reintentar únicamente cuando sea seguro

---

# 14. Requisitos del LLM

El asesor deberá:

✔ Ser empático.

✔ Mantener conversaciones naturales.

✔ Evitar preguntas redundantes.

✔ Adaptarse al contexto.

✔ Explicar decisiones.

✔ Guiar al usuario.

✔ Detectar objeciones.

✔ Identificar intención.

✔ Mantener memoria.

✔ Priorizar cierre comercial.

Nunca deberá:

✘ Inventar proyectos.

✘ Inventar subsidios.

✘ Inventar reglas.

✘ Saltarse estados.

✘ Tomar decisiones financieras.

---

# 15. Requisitos Técnicos

## Backend

- Conversation Engine
- Prompt Builder
- State Machine
- Tool Calling
- Memory Manager
- API Client
- Structured Outputs

---

## WhatsApp

Proveedor sugerido:

- Baileys

Alternativas:

- Evolution API
- WPPConnect

---

## IA

Proveedor:

OpenAI / Claude / Gemini

Debe soportar:

- Tool Calling
- Structured Outputs
- JSON Mode

---

## Base de Datos

Persistir:

- conversaciones
- eventos
- estados
- métricas
- logs

---

# 16. Entregables MVP

- Conversation Engine funcional.
- WhatsApp Gateway.
- Prompt Builder modular.
- State Machine.
- Integración con API Colsubsidio.
- Tool Calling.
- Structured Outputs.
- Persistencia inmediata.
- Observabilidad básica.
- Dashboard mínimo para seguimiento.

---

# 17. Fuera del Scope (No implementar en el MVP)

Las siguientes funcionalidades no aportan valor suficiente para la primera demostración y deberán dejarse únicamente documentadas como evolución futura.

## IA

- Fine-tuning.
- Multiagentes.
- Modelos propios.
- Predicción ML.
- Entrenamiento de scoring.

---

## Conversación

- Personalidad configurable.
- Soporte multilenguaje.
- Traducción automática.
- Emojis dinámicos.

---

## Integraciones

- CRM externos.
- Google Calendar.
- Gmail.
- SMS.
- Pasarelas de pago.
- Firma electrónica.

---

## Analytics

- Dashboards ejecutivos avanzados.
- BI.
- Modelos predictivos.
- Heatmaps.

---

## Automatización

- Campañas automáticas.
- Nutrición completa.
- Remarketing.
- Automatización omnicanal.

---

## Voz

La arquitectura deberá ser compatible con ElevenLabs, pero la implementación de llamadas quedará fuera del MVP.

---

## Vectorización

No indexar CRM completo.

Únicamente documentación necesaria para el RAG.

---

# 18. Evolución Futura (Sugerencias)

Una vez validado el MVP, se recomienda evolucionar el sistema con los siguientes módulos:

## Guardian Voice

Integración con ElevenLabs para llamadas telefónicas utilizando exactamente el mismo Conversation Engine.

---

## Guardian Dashboard

Panel de observabilidad con:

- timeline de conversaciones
- pipeline comercial
- reasoning del agente
- tool calls
- score del lead
- métricas operativas

---

## Guardian Digital Twin

Construcción de un perfil dinámico del usuario basado en historial, variables persistentes y eventos de vida.

---

## Guardian Life Events

Motor para detectar eventos relevantes como:

- matrimonio
- nacimiento de hijos
- compra de vivienda
- cambios laborales

con el fin de anticipar oportunidades comerciales.

---

## Guardian Analytics

Métricas avanzadas:

- abandono por etapa
- objeciones frecuentes
- proyectos más recomendados
- desempeño del agente
- conversión por canal

---

# 19. Criterios de Éxito

El MVP será considerado exitoso si:

- El usuario completa todo el flujo desde WhatsApp sin intervención humana.
- La afiliación se valida correctamente.
- Las variables se persisten en tiempo real.
- Las recomendaciones provienen exclusivamente de la API.
- El asesor recibe un lead completamente perfilado.
- La conversación es natural y no se percibe como un formulario.
- La arquitectura permite reutilizar el mismo motor para WhatsApp, Voz y Web sin cambios significativos.