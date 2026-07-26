# ADR-0007 — Guarda de solo lectura en la capa de datos

## Estado

Aceptada.

## Contexto

Para que el jurado pruebe la CLI sin instalar nada, se sirve por `ttyd` en el
navegador. Esa sesión es **pública**: cualquiera con el enlace entra.

El problema: dos de los ocho módulos escriben en producción. Settings publica
configuración del agente; Prompt hace rollback a una versión anterior. Un jurado
curioso podía revertir el prompt en mitad del pitch.

## Opciones consideradas

**A. Dejarlo abierto.**
Cero trabajo. Riesgo inaceptable: escrituras destructivas expuestas a internet.

**B. Ocultar los módulos Settings y Prompt en la sesión pública.**
Simple. Pero se pierden dos de los ocho módulos justo en la demo, y ocultar en
la UI no impide llegar por otro camino.

**C. Guarda en el manejador de teclas.**
Interceptar la tecla que dispara el publish. Bypasseable: la paleta de comandos
llega al mismo comando, y un subcomando futuro no pasa por ahí.

**D. Guarda en la capa de datos.**

## Decisión

**Opción D.** El flag `--read-only` marca la `LiveSource`
([`source.go`](../../guardian-ai/cli/internal/api/source.go)) — el único punto
por el que pasa cualquier mutación. Los tres métodos que escriben
(`StudioSaveDraft`, `StudioPublish`, `StudioRollback`) devuelven `ErrReadOnly`
antes de tocar la red.

La UI sigue estando: los módulos se ven y se navegan, y un intento de escritura
produce un aviso claro. Un chip `◇ solo lectura` en la cabecera explica por qué,
para que no se lea como un fallo.

Cubierto por `TestReadOnlyBlocksEveryWrite`.

## Consecuencias

**A favor**

- **No se puede esquivar.** No hay ruta —paleta de comandos, deep link,
  subcomando futuro— que llegue a la API sin pasar por `LiveSource`.
- La demo conserva los ocho módulos.
- El mismo binario sirve para el operador y para el público; solo cambia el
  flag. Nada de builds especiales que divergen.
- Un test protege la propiedad: añadir un método de escritura sin guarda rompe
  CI.

**En contra**

- El test lista los tres métodos **a mano**. Uno nuevo no falla solo — hay que
  acordarse de añadirlo.
- La guarda es del lado del cliente. **El backend sigue sin autenticación**:
  cualquiera con la URL del túnel puede hacer publish con `curl`. Esto no
  protege la API, protege la sesión pública de la CLI. Ver
  [seguridad](../security.md).
- `--read-only` no bloquea la tecla `w` (disparar WhatsApp real), a propósito:
  es la demo. Pero **gasta tokens**, ~$0,03 por conversación, y nada limita el
  ritmo.

**Cuándo revisar**

En cuanto el backend tenga autenticación, la guarda correcta pasa a ser un rol
del lado servidor. Esta se queda como defensa en profundidad.

## Ver también

- [Seguridad](../security.md) · [Despliegue](../deployment.md)
