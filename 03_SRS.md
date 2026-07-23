# Software Requirements Specification (SRS)
# Guardian AI
## AI Voice Sales Advisor Platform
### MVP v1.0

---

# Control del Documento

| Campo | Valor |
|--------|-------|
| Proyecto | Guardian AI |
| Documento | Software Requirements Specification |
| Versión | 1.0 |
| Estado | MVP |
| Arquitectura | Modular Event-Driven |
| Backend | Go (Fiber) |
| Frontend | React + Bun |
| Base de Datos | Supabase PostgreSQL |

---

# 1. Introducción

## 1.1 Propósito

Este documento especifica el comportamiento funcional y no funcional del MVP Guardian AI.

El objetivo es definir **qué debe hacer el sistema**, independientemente de cómo sea implementado.

---

## 1.2 Objetivo del producto

Guardian AI es una plataforma de inteligencia conversacional por voz capaz de:

- mantener conversaciones naturales
- comprender el contexto del cliente
- construir un perfil dinámico
- recomendar productos de seguros
- explicar el razonamiento utilizado
- visualizar el proceso completo en tiempo real

---

# 2. Alcance

El MVP incluye únicamente:

- llamadas mediante Vapi
- IA conversacional
- Dashboard Mission Control
- Perfilamiento del usuario
- Recomendación de seguros
- Timeline de la llamada
- Pipeline de decisión
- Observabilidad

No incluye:

- compra real
- pagos
- firma electrónica
- integración con aseguradoras

---

# 3. Stakeholders

## Usuario Final

Persona interesada en adquirir un seguro.

---

## Operador

Observa las llamadas.

Consulta analíticas.

---

## Administrador

Gestiona configuración.

Consulta métricas.

---

## Jurado Hackathon

Interactúa con el sistema.

Evalúa innovación.

---

# 4. Casos de Uso

## UC-001

Realizar llamada

Actor

Cliente

Resultado esperado

El agente responde automáticamente.

---

## UC-002

Descubrir necesidades

Actor

IA

Resultado

Construcción del perfil.

---

## UC-003

Actualizar Feature Store

Actor

Sistema

Resultado

El perfil cambia dinámicamente.

---

## UC-004

Recomendar seguro

Actor

Sistema

Resultado

Se genera una recomendación personalizada.

---

## UC-005

Visualizar llamada

Actor

Operador

Resultado

Mission Control actualizado en tiempo real.

---

## UC-006

Consultar historial

Actor

Administrador

Resultado

Visualizar llamadas anteriores.

---

# 5. Requisitos Funcionales

---

## RF-001

El sistema deberá recibir llamadas mediante Vapi.

---

## RF-002

El sistema deberá establecer una sesión conversacional.

---

## RF-003

El sistema deberá mantener el contexto durante toda la llamada.

---

## RF-004

El sistema deberá transcribir el audio recibido.

---

## RF-005

El sistema deberá almacenar la transcripción.

---

## RF-006

El sistema deberá construir un perfil dinámico del usuario.

---

## RF-007

El sistema deberá actualizar el Feature Store durante la conversación.

---

## RF-008

El sistema deberá detectar eventos relevantes.

Ejemplo

- hijos
- mascota
- vivienda
- trabajo

---

## RF-009

El sistema deberá seleccionar una narrativa adecuada.

---

## RF-010

El sistema deberá generar recomendaciones personalizadas.

---

## RF-011

Toda recomendación deberá incluir una explicación.

Ejemplo

"Recomendamos este seguro porque..."

---

## RF-012

El sistema deberá responder mediante voz utilizando ElevenLabs.

---

## RF-013

El sistema deberá registrar todos los eventos de la conversación.

---

## RF-014

El sistema deberá almacenar el historial completo.

---

## RF-015

El sistema deberá calcular el costo aproximado de cada llamada.

---

## RF-016

El sistema deberá registrar los tokens utilizados.

---

## RF-017

El sistema deberá registrar herramientas utilizadas.

---

## RF-018

El sistema deberá emitir eventos en tiempo real.

---

## RF-019

El Dashboard deberá actualizarse automáticamente.

---

## RF-020

El sistema deberá generar un resumen final.

---

# 6. Requisitos No Funcionales

---

## RNF-001

Tiempo máximo de respuesta

< 2 segundos

---

## RNF-002

Actualización Dashboard

Tiempo real (<500 ms desde la emisión del evento cuando sea posible en el entorno de demo).

---

## RNF-003

Arquitectura

Modular.

---

## RNF-004

Desacoplamiento

Los módulos no deberán comunicarse directamente.

---

## RNF-005

Comunicación

Event Driven.

---

## RNF-006

Persistencia

Toda llamada deberá almacenarse automáticamente.

---

## RNF-007

Escalabilidad

El sistema deberá permitir agregar nuevos canales sin modificar la lógica de negocio.

---

## RNF-008

Mantenibilidad

Toda dependencia externa deberá implementarse mediante Adapter.

---

## RNF-009

Configuración

Centralizada.

---

## RNF-010

Observabilidad

Toda interacción deberá ser trazable.

---

# 7. Modelo de Dominio

```
Cliente

↓

Conversación

↓

Narrativa

↓

Feature Store

↓

Risk Assessment

↓

Recommendation

↓

Summary
```

---

# 8. Estados de la Conversación

```
CREATED

↓

CONNECTED

↓

DISCOVERY

↓

LISTENING

↓

THINKING

↓

TOOL_EXECUTION

↓

RESPONDING

↓

ENDED
```

---

# 9. Eventos

Eventos mínimos

```
CALL_STARTED

CALL_CONNECTED

USER_SPOKE

TRANSCRIPT_UPDATED

FEATURE_UPDATED

INTENT_UPDATED

PROMPT_GENERATED

LLM_REQUEST

LLM_RESPONSE

TOOL_CALLED

TOOL_FINISHED

VOICE_SENT

SUMMARY_CREATED

CALL_ENDED

ERROR
```

---

# 10. Dashboard Mission Control

El sistema deberá mostrar:

## Información general

- Estado de llamada
- Cliente
- Duración
- Estado del agente

---

## Conversación

- Transcripción
- Última respuesta
- Historial

---

## IA

- Narrativa
- Riesgo
- Features
- Prompt
- Tokens

---

## Herramientas

- Tool ejecutada
- Tiempo
- Resultado

---

## Pipeline

- Estado actual
- Tiempo por etapa
- Eventos

---

## Costos

- LLM
- Voz
- Telefonía
- Total

---

# 11. Feature Store

Cada llamada deberá construir dinámicamente un conjunto de características.

Ejemplo

```yaml
customer_id:

family:true

children:2

pets:1

traveler:false

business_owner:true

budget:high

hesitation:price

risk_level:high

confidence:0.92
```

---

# 12. Pipeline Conversacional

```
Call Started

↓

Discovery

↓

Profile Building

↓

Risk Analysis

↓

Narrative Selection

↓

Recommendation

↓

Objection Handling

↓

Acceptance

↓

Summary
```

Cada etapa deberá generar eventos.

---

# 13. Interfaces Externas

## Telefonía

Vapi

---

## Voz

ElevenLabs

---

## LLM

OpenAI GPT-4o

---

## Base de Datos

Supabase PostgreSQL

---

# 14. Persistencia

El sistema deberá almacenar:

- clientes
- llamadas
- eventos
- features
- transcripciones
- recomendaciones
- prompts
- métricas
- costos

---

# 15. Seguridad

- Variables mediante .env
- Secretos fuera del repositorio
- HTTPS en producción
- Validación de entrada
- Sanitización de datos
- Principio de mínimo privilegio para claves de servicio

---

# 16. Restricciones

El MVP deberá evitar:

- microservicios
- Kubernetes
- RabbitMQ
- Kafka
- MCP
- Pinecone
- Integraciones complejas

---

# 17. Criterios de Aceptación

El MVP será considerado exitoso cuando:

✓ El usuario pueda iniciar una llamada.

✓ El agente mantenga una conversación natural.

✓ El sistema construya automáticamente un perfil.

✓ Se genere una recomendación personalizada.

✓ La recomendación explique su razonamiento.

✓ El Dashboard muestre el pipeline en tiempo real.

✓ Todos los eventos sean registrados.

✓ Se genere un resumen final.

---

# 18. Criterios de Calidad

El código deberá cumplir:

- Arquitectura Modular
- Event Driven
- Observer Pattern
- Singleton únicamente donde aplique
- Repository Pattern
- Adapter Pattern
- Strategy Pattern
- State Pattern

---

# 19. Supuestos

- Vapi y ElevenLabs estarán disponibles durante la demo.
- El modelo GPT-4o responderá dentro de los tiempos esperados para un MVP.
- La conexión a Internet será estable durante la presentación.
- El volumen de llamadas concurrentes será bajo (escenario de demostración).

---

# 20. Riesgos

| Riesgo | Impacto | Mitigación |
|----------|---------|------------|
| Latencia del LLM | Alto | Mostrar indicadores de procesamiento y usar timeouts. |
| Falla de Vapi | Alto | Preparar una conversación de respaldo grabada y modo demo. |
| Falla de ElevenLabs | Medio | Tener una voz alternativa configurada. |
| Error de WebSocket | Medio | Reconexión automática del dashboard. |
| Timeout de APIs | Medio | Reintentos limitados y mensajes claros al usuario. |

---

# 21. Definición de Terminado (Definition of Done)

Una funcionalidad se considera terminada cuando:

- Cumple los requisitos funcionales definidos.
- Emite los eventos correspondientes.
- Se refleja correctamente en Mission Control.
- Persiste la información requerida.
- Está desacoplada mediante interfaces cuando aplica.
- No introduce dependencias directas con proveedores externos.
- Es demostrable durante la presentación del hackathon.

---

# Principio Rector

> **Guardian AI no automatiza la venta de seguros; construye confianza mediante conversaciones inteligentes, decisiones explicables y observabilidad en tiempo real.**
