# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

**Asesor / supervisor comercial de Colsubsidio** (primario): opera desde un
escritorio en oficina, con varias conversaciones autónomas corriendo a la vez.
Su trabajo no es escribir mensajes: es vigilar que el sistema perfile bien,
detectar cuándo un lead está listo y tomarlo. Necesita ver el estado de varias
conversaciones simultáneas de un vistazo y auditar por qué el sistema
recomendó lo que recomendó.

**Jurado del hackathon** (co-primario, confirmado): evalúa el sistema en vivo
durante una demostración corta y proyectada. Debe entender en segundos que el
sistema conversa solo, extrae perfil real y decide con reglas de negocio, no
con improvisación del modelo.

Ambas audiencias pesan igual: la interfaz debe sostenerse como herramienta de
operación diaria y como escenario de demostración.

## Job

Convertir un contacto de WhatsApp o voz en un **Lead Ready for Advisor**:
perfilado, calificado por reglas de negocio y entregado al asesor humano con
todo el contexto. El operador acompaña y audita; el sistema conduce.

## Capabilities

- Contacto autónomo saliente y entrante por WhatsApp (gateway Kapso) y por voz
  (Vapi / ElevenLabs / Web Speech).
- Motor conversacional Guardian: el LLM entiende, extrae y explica; toda
  decisión de negocio viene de la API Colsubsidio Protege.
- Máquina de estados comercial del lead: `NEW → AFFILIATION_CHECK →
  PROFILE_DISCOVERY → FINANCIAL_QUALIFICATION → PROJECT_MATCHING →
  READY_FOR_ADVISOR | NURTURING → COMPLETED`. Sin transiciones arbitrarias.
- Persistencia inmediata del perfil en la API (cada dato confirmado se guarda
  en el turno en que se dice).
- Perfil "Afiliado 360" precargado desde la base de afiliados.
- Tool calling con registro cerrado; RAG solo sobre documentación.
- Observabilidad por mensaje: estado, intención, confianza, latencia, tokens,
  costo, herramientas ejecutadas, variables nuevas.

## Surfaces

- **Mission Control** (`/`): tablero de operación en vivo — llamada de voz en
  curso, eventos, perfil y telemetría.
- **Chat WhatsApp** (`/chat`): conversaciones de texto simultáneas en vivo, con
  pestañas por cliente, etapa del lead, perfil capturándose y actividad del
  motor.
- **Pipeline** (`/pipeline`): resultado post-conversación — conversaciones
  cerradas, fases, transcripción, scoring, recomendaciones, costo.

Las tres forman un solo sistema; ninguna domina (confirmado).

## Terminology

Lead, afiliado, asesor, perfil, variable, regla, recomendación, etapa del lead,
conversación, turno. "Guardian" es el asesor autónomo; "Sofía" es el nombre de
la voz del agente. `estado_pipeline` es el estado en el motor de ventas, no la
afiliación a la caja.

## Constraints

- Frontend sin framework ni build step: HTML + CSS + JavaScript vanilla
  servido por nginx. Debe seguir así.
- Datos en vivo por WebSocket (`/ws`); el backend es la única fuente de verdad.
- Densidad real: varias conversaciones simultáneas, transcripciones largas,
  feeds de eventos continuos.
- Español de Colombia en toda la interfaz.
- Debe verse íntegro proyectado en una sala (demo) y en un monitor de trabajo.

## Brand commitments

**Obligatorio respetar la identidad de Colsubsidio** (confirmado por el
usuario). Evidencia tomada del sitio oficial `colsubsidio.com`:

- Azul institucional `#0067B1` (variable propia `--color-blue`).
- Amarillo `#FFD000`, rojo `#CF1132`, rosa `#C53465` como colores de sistema.
- Neutros: `#1C1C1C`, `#2C343A`, `#F4FAFD`, `#F6F7FC`; oscuro `#121212`,
  `#1F2429`.
- Tipografía del sitio: **Poppins**.
- Tono de marca: cercano, familiar, accesible ("Con todo lo que te mereces").

Guardian AI es producto interno de Colsubsidio: la identidad de la caja manda;
el producto puede tener carácter propio dentro de ella.

## Accessibility

Interfaz operativa de uso prolongado: contraste de texto conforme, foco de
teclado visible, estados no comunicados solo por color, y legibilidad a
distancia de proyección.

## Open decisions

- Idioma adicional (inglés) para jurados no hispanohablantes: no decidido.
- Modo oscuro: hoy existe en `/chat`; falta decidir si es el modo de la sala de
  operación o una preferencia por usuario.
