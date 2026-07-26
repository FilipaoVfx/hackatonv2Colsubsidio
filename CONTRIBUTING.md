# Contribuir

## Levantar el proyecto

```bash
git clone https://github.com/FilipaoVfx/hackatonv2Colsubsidio.git
cd hackatonv2Colsubsidio
cp .env.example guardian-ai/.env    # rellena las claves
make dev
make health                          # las 7 capabilities en verde
```

Si `make health` marca algo en rojo, falta la variable de entorno
correspondiente. No es un fallo de código.

Sin Docker necesitas Go 1.22+:

```bash
cd guardian-ai/backend && go run .
```

## Antes de abrir un PR

```bash
make lint    # go vet en backend y CLI
make test    # 22 suites; deben pasar todas
```

Los tests corren **sin red ni claves** — los clientes externos están
sustituidos por dobles. Si un test tuyo necesita internet, está mal planteado.

## Commits

[Conventional Commits](https://www.conventionalcommits.org). El tipo va en
inglés; la descripción, en español.

```
feat(cli): descubrimiento de endpoint desde el dominio
fix(backend): TOOL_CALLED leía la clave equivocada del payload
docs(adr): por qué no usamos base vectorial
refactor(rag): extraer el chunking por heading
test(statemachine): cubrir transiciones inválidas
chore: actualizar dependencias
```

Ámbitos: `backend`, `cli`, `landing`, `docs`, `adr`, `ci`.

**Explica el porqué, no el qué.** El diff ya dice qué cambió.

## Ramas

`main` siempre desplegable. Trabaja en rama y abre PR:

```
feat/nombre-corto
fix/nombre-corto
docs/nombre-corto
```

## Revisión

Un PR debería poder revisarse en menos de quince minutos. Si no, pártelo.

Qué se mira:

- ¿Los tests pasan y cubren el caso nuevo?
- ¿Un cambio de comportamiento actualizó la documentación en el mismo PR?
- ¿Una decisión de arquitectura necesita un [ADR](docs/adr/README.md)?
- ¿Se añadió una dependencia? El backend tiene **cuatro** directas. Sumar una
  quinta hay que argumentarla.

## Añadir un tipo de evento

Toca tres sitios, en este orden:

1. `guardian-ai/backend/events.go` — la constante
2. `guardian-ai/backend/projector.go` — cómo proyecta
3. `guardian-ai/cli/internal/api/types.go` — **la CLI duplica el envelope**
   (ver [ADR-0008](docs/adr/0008-monolito-go.md)); olvidarlo rompe el cliente en
   ejecución, no en compilación

Y documéntalo en `docs/architecture/event-catalog.md`.

## Documentación

La documentación y el código cambian en el **mismo PR**. Un `docs:` que llega
tres semanas después ya es ficción.

Los diagramas son **Mermaid**, nunca imágenes: se revisan en el diff.

## Estilo

- Go: `gofmt`. Sin excepciones.
- Comentarios que expliquen **por qué**, no qué. El código ya dice qué.
- Nombres en inglés en el código; documentación y mensajes al usuario, en
  español.
