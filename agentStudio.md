# PRD — Módulo "Agent Studio"
## Panel UX-First para Configuración del Asesor IA

**Versión:** MVP Hackathon v1.0

---

# Objetivo

Construir un **Agent Studio**, una interfaz visual que permita a un administrador configurar el comportamiento completo del asesor IA sin necesidad de editar prompts manualmente.

El objetivo NO es permitir escribir prompts.

El objetivo es permitir **diseñar el comportamiento del agente** mediante componentes visuales intuitivos.

El sistema debe generar automáticamente el System Prompt a partir de configuraciones estructuradas.

---

# Filosofía del Producto

## UX First

El administrador nunca debería preguntarse:

> "¿Qué debo escribir?"

Debe pensar únicamente:

> "¿Cómo quiero que se comporte mi asesor?"

La interfaz debe sentirse como:

- Figma
- Notion
- Stripe Dashboard
- Vercel
- Linear

No como un CMS antiguo.

---

# Principios

✓ Sin código

✓ Sin Prompt Engineering

✓ Sin JSON

✓ Sin YAML

✓ Sin Markdown

Todo visual.

Todo reversible.

Todo versionado.

Todo explicable.

---

# Arquitectura

```text
                 Agent Studio
                       │
             Config Builder
                       │
        ┌──────────────┼──────────────┐
        │              │              │
   Personality      Rules        Knowledge
        │              │              │
        └──────────────┼──────────────┘
                       │
              Prompt Composer
                       │
             Generated Prompt
                       │
                Runtime LLM
```

---

# Objetivos UX

El usuario debe poder:

✓ Cambiar personalidad

✓ Cambiar objetivos

✓ Activar herramientas

✓ Configurar voz

✓ Configurar memoria

✓ Configurar RAG

✓ Configurar seguridad

✓ Publicar cambios

Todo en menos de 3 minutos.

---

# Navegación

Sidebar izquierda

```
General

Personality

Sales

Knowledge

Voice

Memory

Reasoning

Safety

Channels

Experiments

Versions

Playground
```

Siempre visible.

---

# Página General

Resumen.

Debe mostrar:

Nombre del agente

Estado

Publicado

Borrador

Versión

Modelo

Última actualización

Botón

Preview

Botón

Publicar

---

# Personality

La pantalla más importante.

No usar formularios.

Todo sliders.

---

## Empatía

```
Muy baja

──────────────

Muy alta
```

Tooltip

"Aumenta la capacidad del asesor para reconocer emociones."

---

## Formalidad

```
Casual

──────────────

Corporativa
```

---

## Cercanía

```
Profesional

──────────────

Amigable
```

---

## Persuasión Comercial

```
Consultor

──────────────

Vendedor
```

Nunca usar

"agresivo"

---

## Longitud

○ Breve

○ Media

○ Detallada

---

## Uso de emojis

Switch

---

## Humor

Switch

---

## Proactividad

Slider

Qué tanto toma iniciativa.

---

# Preview dinámico

Lado derecho.

Siempre visible.

Cada cambio modifica una conversación simulada.

Ejemplo

```
Empatía = 2

↓

Hola.

¿Cómo puedo ayudar?

------------------

Empatía = 9

↓

Hola 👋

Entiendo que elegir un seguro puede generar muchas dudas.

Estoy aquí para ayudarte.
```

Sin refrescar.

---

# Sales

Objetivos del agente.

Checklist.

```
Resolver dudas

Calificar cliente

Recomendar producto

Cerrar venta

Agendar llamada

Derivar humano
```

Orden drag & drop.

El primero tiene prioridad.

---

# Knowledge

Mostrar fuentes.

```
Productos

Activo

----------------

FAQ

Activo

----------------

Objeciones

Activo

----------------

Narrativas

Activo
```

Cada fuente

ON/OFF

---

Cantidad máxima de documentos

Slider

```
2

3

4

5

6
```

---

Threshold

Slider

```
0.65

0.72

0.81

0.90
```

Mostrar explicación.

---

# Voice

Proveedor

Dropdown

ElevenLabs

OpenAI

Azure

---

Velocidad

Slider

---

Pausas

Slider

---

Naturalidad

Slider

---

Idioma

Dropdown

---

Tono

Dropdown

Calmado

Formal

Energético

Seguro

---

# Memory

Últimos mensajes

```
5

10

20

50
```

---

Recordar preferencias

Switch

---

Recordar contexto

Switch

---

Recordar objeciones

Switch

---

# Reasoning

No mostrar "Chain of Thought".

Mostrar comportamiento.

Ejemplo

```
Pensar antes de responder

ON

----------------

Verificar datos

ON

----------------

Consultar API

ON

----------------

Consultar Pinecone

ON

----------------

Solicitar confirmación

OFF
```

---

# Safety

Nunca responder

Checklist

```
Coberturas inventadas

Promesas falsas

Consejos legales

Consejos médicos

Información inexistente
```

---

Nivel de protección

Slider

Bajo

Medio

Alto

---

# Channels

Tabs

WhatsApp

Web

Llamadas

Cada canal puede tener

Personalidad distinta.

---

# Playground

La joya del sistema.

Pantalla dividida.

Izquierda

Configuración.

Derecha

Chat en vivo.

Cada cambio

↓

nuevo comportamiento

↓

sin recargar.

---

Botones rápidos

```
Cliente molesto

Cliente curioso

Cliente indeciso

Cliente directo

Cliente emocional
```

Permite probar.

---

# Prompt Inspector

Solo lectura.

No editable.

Mostrar

```
Prompt generado

↓

Copiar

↓

Comparar versión
```

Nunca permitir editar.

---

# Versionado

Cada publicación

↓

Nueva versión.

```
v1

Producción

--------------

v2

Campaña Mayo

--------------

v3

Más empático

--------------

v4

Rollback
```

Timeline.

---

# Comparador

Comparar

v2

vs

v5

Mostrar diferencias.

No texto.

Cards.

```
Empatía

7 → 9

----------

Temperatura

0.4 → 0.7

----------

Objetivo principal

Calificar

↓

Cerrar venta
```

---

# Publicación

Botón

Publicar

Antes

Modal.

```
Resumen

5 cambios

¿Deseas publicar?

Cancelar

Publicar
```

---

# Validaciones

Antes de publicar.

Ejecutar

```
Saludo

FAQ

Objeción

Venta

Error

Fallback
```

Todo verde.

Si falla

No permitir publicar.

---

# Analytics (MVP)

No implementar.

Solo mock.

Cards.

```
Conversaciones

Lead Score

Conversión

Tiempo promedio

Satisfacción
```

---

# Microinteracciones

Hover

Glow

Spring animations

Progress

Animated sliders

Animated switches

Live preview

Loading skeletons

Nada debe aparecer de golpe.

---

# Diseño

Mucho espacio.

Nada saturado.

Cards grandes.

Bordes suaves.

Radius 16px

Sombras sutiles.

Glass solo donde aporte.

---

# Stack

React

TypeScript

Vite

TailwindCSS

shadcn/ui

Framer Motion

TanStack Query

Zustand

React Hook Form

Zod

Monaco Editor (solo Prompt Inspector)

React Flow (opcional)

---

# Accesibilidad

Contraste AA.

Navegable con teclado.

Tooltips descriptivos.

Sliders accesibles.

Focus visible.

---

# Fuera del Scope (Hackathon)

No implementar:

- Multiusuario.
- Roles y permisos.
- Auditoría completa.
- Historial de conversaciones.
- A/B Testing real.
- Métricas de producción.
- Integración con LangSmith.
- Gestión de secretos.
- Marketplace de prompts.
- Edición manual del System Prompt.
- Sincronización entre múltiples agentes.
- Control por organización.
- Publicación programada.
- Workflows de aprobación.
- Integración con Git.
- CI/CD de prompts.

Documentarlos únicamente como roadmap.

---

# Definición de Éxito

El jurado debe poder abrir el módulo y comprender en menos de un minuto que:

- Configurar un agente no requiere conocimientos de IA.
- Toda la lógica es visual y explicable.
- Cada cambio tiene una vista previa inmediata.
- El prompt es un artefacto generado, no escrito manualmente.
- El sistema es seguro, versionado y escalable.

## Diferenciador del Hackathon

La mayoría de equipos mostrará un chatbot con un prompt fijo.

Este proyecto mostrará un ****Agent Studio**, una plataforma donde cualquier persona de negocio puede diseñar, probar, versionar y publicar el comportamiento de un asesor inteligente sin escribir una sola línea de código. Eso cambia la percepción del jurado: deja de ser una demo de IA y se convierte en un producto SaaS listo para evolucionar.
