# Design

<!-- impeccable:design-schema 1 -->

## World

**La credencial de afiliación Colsubsidio.** El objeto que todo afiliado tiene
en la billetera es el mundo visual del producto: papel de seguridad, banda
institucional, campos rotulados, sellos de tinta, troquel y número de serie.

La metáfora no es decorativa, es funcional: cada conversación **construye la
credencial del cliente en vivo**. Los datos que el motor confirma se imprimen
en su campo; las etapas del lead se troquelan; los estados se sellan. Lo que en
un dashboard genérico sería "otra tarjeta con métricas", aquí es un documento
que se completa frente al operador.

Refuta explícitamente: el dashboard SaaS de tarjetas iguales con icono +
título + texto, sparklines decorativas y acento neón sobre gris.

## Color

Estrategia: **Restrained institucional** (neutros de papel + azul de marca),
con el amarillo reservado para lo que exige atención humana.

| Rol | Valor | Uso |
|---|---|---|
| `--ink` | `#1C1C1C` | Texto principal (tinta) |
| `--ink-2` | `#2C343A` | Texto secundario fuerte |
| `--ink-soft` | `#5C5C5C` | Rótulos, texto terciario |
| `--paper` | `#F4FAFD` | Fondo de la aplicación |
| `--paper-2` | `#F6F7FC` | Segunda capa: barras, paneles |
| `--card` | `#FFFFFF` | Superficie de credencial |
| `--rule` | `#D6D6D6` | Líneas de campo y troquel |
| `--rule-soft` | `#EBEBEB` | Separadores tenues |
| `--brand` | `#0067B1` | Azul Colsubsidio: banda, acción primaria, selección |
| `--brand-deep` | `#004A80` | Estado activo/hover del azul |
| `--brand-wash` | `#E8F2FA` | Fondo de selección y bandas suaves |
| `--attention` | `#FFD000` | Amarillo Colsubsidio: requiere atención humana |
| `--alert` | `#CF1132` | Error, rechazo, riesgo |
| `--seal-ok` | `#0F6B4F` | Sello confirmado |
| `--wa` | `#023727` | Verde WhatsApp institucional (solo canal) |

Reglas: el azul nunca decora, solo marca marca/acción/selección. El amarillo no
es un color de acento libre: aparece cuando el operador debe mirar (lead listo,
no leído, error recuperable). Los estados nunca se comunican solo por color:
llevan sello, rótulo o forma.

Modo por escena: la sala es una oficina con luz de día y una demo proyectada.
Un proyector lava los negros, el papel no. **Claro es el modo primario**; el
oscuro existe como "credencial bajo luz nocturna" y conserva la misma retícula.

## Type

**Poppins** (la tipografía del sitio de Colsubsidio) para toda la interfaz —
una sola familia, como corresponde a producto. Fallback:
`"Poppins", "Segoe UI", system-ui, -apple-system, sans-serif`.

Números de serie, valores, latencias, tokens y costos van en la mono del
sistema (`ui-monospace, "SF Mono", Menlo, monospace`): es medición real, no
disfraz técnico.

Dos escalones dentro de la interfaz y tres para títulos:

| Paso | Uso |
|---|---|
| **11px** `600` versalitas | rótulo de campo, metadato, sello, estado, hora, serie |
| **14px** | cuerpo: valores, mensajes, texto de evento, ayuda |
| **16 / 20 / 26px** | título de sección · título de tarjeta · título de página |

Nada intermedio: 12px y 13px quedan fuera del sistema a propósito. Los rótulos
son 11 y no 10 porque la sala de demo es proyectada. El título de sección se
distingue del rótulo por tamaño y tinta plena, nunca por ser otra versalita.
Sin tipografías display en controles ni datos.

## Components

- **Credencial** (`.cred`): superficie blanca, borde `--rule` 1px, radio 10px,
  con banda superior azul de 4px. Es el contenedor por defecto; sustituye a la
  tarjeta genérica. Nunca se anida una credencial dentro de otra.
- **Campo rotulado** (`.field`): rótulo en versalitas a la izquierda, valor a la
  derecha, línea punteada de guía entre ambos (la línea del formulario impreso).
- **Sello** (`.seal`): recuadro de 2px con texto en versalitas, rotación de
  -2deg, en `--seal-ok`, `--alert`, `--ink-soft` o `--brand`. Comunica estado
  confirmado/rechazado/pendiente sin depender del color.
- **Troquel** (`.perf-step`): fila de perforaciones ●○○○ que marca el avance de
  la etapa del lead. Es el stepper del sistema; reemplaza la barra de progreso.
  Las etapas de la máquina de voz (`.stage`) usan la misma lógica: ámbar la
  etapa en curso, azul sólido la cumplida.
- **Sello de calificación** (`.gauge` en Pipeline): el puntaje se estampa como
  sello de tinta rotado, verde/ámbar/rojo según el valor. No hay anillos de
  progreso en el sistema: la credencial se sella, no se llena.
- **Banda de datos** (`.band`): franja azul con texto blanco en versalitas para
  cabeceras de sección y encabezado de credencial.
- **Papel de seguridad**: patrón guilloché de líneas finas al 3-4% de opacidad,
  solo en el fondo de la aplicación. Jamás detrás de texto denso.
- Controles: misma forma en las tres vistas — botón radio 8px, altura 36px,
  foco visible con anillo `--brand` de 2px y offset. Estados completos
  (default, hover, focus, active, disabled, loading) para cada control.

## Motion

150–250 ms, ease-out. La única animación autorizada como firma es la
**impresión del dato**: cuando una variable se confirma, su campo entra con un
barrido corto desde la izquierda (como impresión de matriz), una sola vez. El
resto de la interfaz cambia de estado sin coreografía. El pulso solo marca lo
que está vivo (conexión, no leídos).

## Constraints

HTML + CSS + JS vanilla, sin build. Un único `guardian.css` gobierna las tres
vistas; lo específico de cada vista vive en su `<style>`. Las clases e `id`
que el JavaScript ya usa se conservan: el rediseño reemplaza apariencia y
estructura, nunca el comportamiento probado.
