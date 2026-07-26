# 08 — Afiliado 360 (base de afiliados del Reto Seguros)

> Insumo: `recursos_seguros/Usos_Productos_Afiliados_SIN_ID (1).xlsx` — base
> anonimizada de 500.000 afiliados (demografía + 5 marcas de consumo sí/no).
> Objetivo del reto: *"perfilar a los afiliados para identificar qué seguros
> ofrecerles"*.

## 1. Qué hace

Cuando el Guardian abre una conversación con un usuario nuevo, busca su perfil
en la base de afiliados y **precarga** sus variables (ciudad, rango de edad,
rango salarial, ingreso estimado, marcas de consumo) en la Protege API vía
`PUT /users/{id}/variables` con `source: "colsubsidio_360"`. Resultado: el
asesor abre sabiendo lo que Colsubsidio ya sabe — menos interrogatorio, más
conversación (principio del spec `retrieval.md`).

Además, siembra en la API **reglas de afinidad derivadas de la base** (weights
con evidencia real) usando el CRUD propio de la API (`POST /api/v1/rules`) —
mismo camino para el mock y la API real: cero cambio de contrato.

## 2. Antes vs Después — impacto de la mejora

| Dimensión | ANTES (Guardian sin 360) | DESPUÉS (Guardian + Afiliado 360) |
|---|---|---|
| Apertura de conversación | El asesor arranca de cero: pregunta nombre, edad, ingresos, ciudad… | El asesor abre con **7 variables precargadas** de la base real (edad, salario, ciudad, género, segmento, hábitos) |
| Preguntas necesarias hasta recomendar | ~8-10 turnos (todo el cuestionario por chat) | ~3-4 turnos (solo confirma y completa lo que falta) — `MissingQuestions` ve menos faltantes |
| Sensación de interrogatorio | Alta: cada dato se pregunta | Baja: *"veo que estás en Bogotá y usas nuestras droguerías…"* — cumple el principio del spec "nunca formulario" |
| Reglas de recomendación | 12 reglas con pesos **inventados a mano** (réplica plausible) | +4 reglas 360 con pesos **derivados de 500.000 afiliados reales** (gradientes medidos) |
| Justificación ante el cliente | "Te recomiendo X porque tienes mascota" | "El 17.6% de afiliados como tú usa droguerías; en tu rango salarial el uso sube al 31%…" — evidencia citable |
| Personalización del ingreso | Solo si el cliente lo dice en el chat | `monthly_income` estimado desde `RANGO_SALARIAL` (punto medio × SMLV) ya disponible para las reglas de capacidad |
| Fuente de memoria | Solo lo conversado | Lo conversado + el maestro de afiliados (misma vía: `GET/PUT /variables`) |
| Contrato con la Protege API | — | **Sin cambios**: todo entra por `PUT /variables` (source) y `POST /rules` (CRUD del spec) |
| KPI demo (variables por lead) | 0 al abrir | 7 al abrir (visibles al instante en el panel "Perfil capturado" de /chat) |

Nada del flujo anterior se pierde: sin CSV (`AFFILIATES_CSV` ausente) el sistema
corre exactamente como antes — degradación elegante, mismo patrón del resto del
stack.

## 3. Integración con la Protege API — por qué NO es contraproducente

La API es *variable-driven*: puntúa `variables` contra `rules` sin importar la
fuente (`source` existe para eso). El 360 solo añade variables y reglas por los
**puntos de extensión oficiales**:

- Variables: `PUT /users/{id}/variables` (`source: colsubsidio_360`, confidence 0.85).
- Reglas: `POST /api/v1/rules` (upsert por Name en el mock; nombres prefijados "360 —").
- Mapeo canónico: la base se traduce al vocabulario de la API, nunca al revés
  (`RANGO_SALARIAL` → `monthly_income` estimado por punto medio × SMLV, para que
  las reglas de ingreso existentes disparen; el rango crudo se guarda aparte).
- La precarga hace que `MissingQuestions` vea menos faltantes → el flujo avanza
  más rápido: comportamiento deseado, no conflicto.

## 4. Evidencia del ETL (2026-07-24, 500.000 filas)

| Señal | Valor |
|---|---|
| DROGUERÍA global | 17.6% (única marca con señal) |
| DROGUERÍA por salario | 7% (<SMLV) → 35% (2.5-3 SMLV) |
| DROGUERÍA 20-35 años | 24% |
| HOTELES / PISCILAGO / AGENCIAS / VIVIENDA | ≈0% en la muestra (sin señal) |
| 20-35 años | 49.6% de la base |
| 1-1.5 SMLV | 60% de la base |
| Ciudad top | Bogotá (≈174k con ciudad registrada) |

Reglas 360 sembradas (weights modestos — complementan la conversación):
`uses_drugstore=true → Vida (0.3)`, `age_range=20-35 → Accidentes (0.15)`,
`monthly_income ≥ 2 SMLV → Hogar (0.2)`, `city=Bogotá → Vida (0.1)`.

## 5. Honestidad metodológica (declarar a jurados)

- **Sin target de compra de seguros**: las marcas son consumo de OTROS
  servicios. La relación consumo→seguro es **afinidad heurística con evidencia
  de gradientes reales**, no un modelo de propensión validado.
- **Dos vías de vinculación, declaradas en la variable `fuente_perfil`**:
  1. Al abrir: **estimación demo** (hash determinístico del teléfono — la base
     es anónima, sin teléfonos). `fuente_perfil = "estimación demo (hash de
     teléfono)"`.
  2. Cuando el cliente comparte su **número de afiliado** en la conversación:
     lookup **REAL** por SERIE contra el maestro (`BySerie`), que reemplaza la
     estimación. `fuente_perfil = "maestro de afiliados (serie confirmada)"`.
  En producción la vía 1 desaparece (el maestro real vincula por
  teléfono/cédula).
- **Semántica del pipeline**: la variable `estado_pipeline` (`nuevo|conocido`)
  indica si el usuario existía en la **Protege API** (pipeline de ventas), NO
  su afiliación a Colsubsidio. Ser nuevo en el pipeline y a la vez afiliado
  conocido del maestro es el caso de negocio normal.
- Segmentos ofuscados (letras griegas) se guardan como categorías sin
  interpretarse.
- 4 de las 5 marcas no tienen señal en la muestra entregada; solo droguería
  aporta discriminación real.

## 6. Piezas

- `backend/affiliates.go` — store CSV en memoria (`AFFILIATES_CSV`, default
  `data/affiliates_sample.csv`, subset determinístico 10k del ETL), `ForPhone`
  (hash determinístico), `Variables()` (mapeo canónico), `derivedRules()`.
- `backend/guardian.go` — precarga en `start()` para usuarios nuevos
  (tool `save_variable` + `FEATURE_UPDATED` con source `colsubsidio_360`).
- `backend/main.go` — goroutine de seed de reglas al arranque.
- `mock-protege` — `POST /api/v1/rules` (upsert por Name, spec real).
- `backend/knowledge/insights_afiliados.md` — hallazgos agregados para el RAG.
- ETL reproducible: `scratchpad/etl_afiliados.py` (stdlib, streaming).

## 7. Cómo verificar

```bash
docker compose up -d --build
docker logs guardian-ai-backend-1 | grep afiliado360
# -> "afiliado360: 10000 afiliados cargados" y "4 regla(s) 360 sembradas"

curl -s localhost:9000/api/v1/rules | grep -c '360 —'   # 4

# abrir conversación: el perfil 360 aparece precargado en /chat
curl -X POST localhost:8099/api/chat/start -H 'Content-Type: application/json' \
  -d '{"to":"+573001112233"}'
curl -s localhost:9000/api/v1/users/<user_id>/variables   # variables colsubsidio_360
```

## 8. Sin Pinecone

El RAG sigue en memoria (embeddings OpenAI + cosine). La tabla de afinidad son
mapas Go. Un vector DB externo no aporta nada a este tamaño de corpus.
