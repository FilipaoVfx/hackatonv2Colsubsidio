#!/usr/bin/env python3
"""Genera mock-protege/catalog.go desde sourceapi.json.

sourceapi.json trae el portafolio REAL publicado por Colsubsidio (21 productos,
segmentos, coberturas mencionadas y fuentes citadas). Lo que NO trae, porque
Colsubsidio no lo publica, es el precio: en el propio JSON aparece bajo
`condiciones_no_publicadas` junto con deducibles, topes y sumas aseguradas.

Por eso las primas de este catálogo son ESTIMACIONES DE DEMO y cada producto lo
declara en `metadata_json.price_source`. Las coberturas incluidas sí salen del
JSON (`coberturas_mencionadas`); las opcionales son añadidos de demo, marcados
como tales, y existen para que "ajustar coberturas" sea una operación real con
efecto en el precio y no un adorno.
"""
import json, re, uuid, pathlib

SRC = pathlib.Path("/tmp/claude-0/-root/f1e2483d-a729-460a-9a23-d0e98e72c442/scratchpad/repo/sourceapi.json")
OUT = pathlib.Path("/tmp/claude-0/-root/f1e2483d-a729-460a-9a23-d0e98e72c442/scratchpad/repo/guardian-ai/mock-protege/catalog.go")

# Prima mensual estimada (COP). Criterio: orden de magnitud del mercado
# colombiano de seguros masivos, coherente entre productos del mismo segmento.
PRICE = {
    "colsubsidio-seguro-vida": 25000,
    "colsubsidio-seguro-vida-ahorro": 58000,
    "colsubsidio-accidentes-personales": 18000,
    "colsubsidio-renta-hospitalizacion": 21000,
    "colsubsidio-diagnostico-cancer": 27000,
    "colsubsidio-poliza-salud": 96000,
    "colsubsidio-exequial-familiar": 16000,
    "colsubsidio-seguro-mascotas": 22000,
    "colsubsidio-autos-motos": 145000,
    "colsubsidio-hogar": 32000,
    "colsubsidio-bicicletas": 14000,
    "colsubsidio-arrendamiento": 39000,
    "colsubsidio-asistencia-medica-familiar": 19000,
    "colsubsidio-asistencia-mascotas": 12000,
    "colsubsidio-asistencia-juridica": 11000,
    "colsubsidio-seguro-vida-deudor": 13000,
    "colsubsidio-seguro-desempleo": 17000,
    "colsubsidio-seguro-incendio": 23000,
    "colsubsidio-asistencia-medica-viajes": 26000,
    "colsubsidio-empresas-colectivo": 240000,
    "colsubsidio-empresas-salud-colectiva": 310000,
}

# Coberturas opcionales por segmento: añadidos de demo con su sobreprima.
OPTIONAL = {
    "familia": [
        ("asistencia_medica_telefonica", "Asistencia médica telefónica 24/7", 4000),
        ("auxilio_exequial", "Auxilio exequial complementario", 6000),
    ],
    "patrimonio": [
        ("asistencia_domiciliaria", "Asistencia domiciliaria (plomería, cerrajería)", 5000),
        ("contenido_electronico", "Contenido electrónico y tecnología", 7000),
    ],
    "asistencias": [
        ("cobertura_ampliada", "Cobertura ampliada 24/7 sin límite de eventos", 3000),
    ],
    "credito": [
        ("proteccion_extendida", "Protección extendida por 12 meses adicionales", 5000),
    ],
    "empresas": [
        ("cobertura_colectiva_ampliada", "Ampliación de cobertura al grupo familiar", 12000),
    ],
}

# Etiquetas legibles para las coberturas que vienen en el JSON como claves.
LABEL = {
    "fallecimiento": "Fallecimiento",
    "incapacidad": "Incapacidad permanente",
    "proteccion_economica": "Protección económica a beneficiarios",
    "ahorro": "Componente de ahorro",
    "accidentes": "Accidentes personales",
    "invalidez": "Invalidez por accidente",
    "gastos_medicos": "Gastos médicos",
    "renta_diaria": "Renta diaria por hospitalización",
    "hospitalizacion": "Hospitalización",
    "diagnostico_cancer": "Diagnóstico de cáncer",
    "tratamiento": "Tratamiento",
    "salud": "Atención en salud",
    "consultas": "Consultas médicas",
    "examenes": "Exámenes diagnósticos",
    "exequias": "Servicio exequial",
    "traslados": "Traslados",
    "mascota": "Atención veterinaria",
    "responsabilidad_civil": "Responsabilidad civil",
    "vehiculo": "Daños al vehículo",
    "robo": "Hurto",
    "danos": "Daños materiales",
    "vivienda": "Estructura de la vivienda",
    "contenidos": "Contenidos del hogar",
    "bicicleta": "Bicicleta",
    "arrendamiento": "Canon de arrendamiento",
    "juridica": "Asesoría jurídica",
    "desempleo": "Pérdida de empleo",
    "incendio": "Incendio",
    "viajes": "Asistencia en viaje",
    "colectivo": "Cobertura colectiva",
}


def label_for(key: str) -> str:
    if key in LABEL:
        return LABEL[key]
    return key.replace("_", " ").capitalize()


def go_str(s: str) -> str:
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'


def main():
    data = json.loads(SRC.read_text())
    ns = uuid.UUID("6f1b6a2e-0000-4000-8000-000000000000")

    lines = []
    lines.append("package main")
    lines.append("")
    lines.append("// catalog.go — portafolio Colsubsidio Protege para la demo.")
    lines.append("//")
    lines.append("// GENERADO desde sourceapi.json (portafolio real publicado por Colsubsidio:")
    lines.append("// 21 productos, segmentos, coberturas mencionadas y fuentes citadas). No se")
    lines.append("// edita a mano: se regenera con scripts/gen_catalog.py.")
    lines.append("//")
    lines.append("// HONESTIDAD: sourceapi.json NO trae precios — Colsubsidio no los publica, y el")
    lines.append("// propio JSON los lista bajo `condiciones_no_publicadas` junto a deducibles,")
    lines.append("// topes y sumas aseguradas. Las primas de aquí son ESTIMACIONES DE DEMO y cada")
    lines.append("// producto lo declara en metadata_json.price_source. Las coberturas INCLUIDAS")
    lines.append("// salen del JSON; las OPCIONALES son añadidos de demo, marcados como tales,")
    lines.append("// para que ajustar coberturas tenga un efecto real sobre el precio.")
    lines.append("")
    lines.append("// Coverage es una cobertura de un producto. Included distingue lo que ya trae")
    lines.append("// la prima base de lo que el cliente puede añadir pagando PriceDelta más.")
    lines.append("type Coverage struct {")
    lines.append("\tKey        string  `json:\"key\"`")
    lines.append("\tLabel      string  `json:\"label\"`")
    lines.append("\tIncluded   bool    `json:\"included\"`")
    lines.append("\tPriceDelta float64 `json:\"price_delta\"`")
    lines.append("\tSource     string  `json:\"source\"` // portafolio_publicado | estimacion_demo")
    lines.append("}")
    lines.append("")
    lines.append("var products = []map[string]interface{}{")

    for p in data["productos"]:
        pid = str(uuid.uuid5(ns, p["id_producto"]))
        seg = p["segmento"]
        price = PRICE[p["id_producto"]]
        code = re.sub(r"[^A-Z0-9]+", "_", p["nombre_comercial"].upper()).strip("_")

        covs = []
        for c in p.get("coberturas_mencionadas", []):
            covs.append((c, label_for(c), True, 0, "portafolio_publicado"))
        for key, lab, delta in OPTIONAL.get(seg, []):
            covs.append((key, lab, False, delta, "estimacion_demo"))

        lines.append("\t{")
        lines.append(f'\t\t"id": {go_str(pid)}, "code": {go_str(code)},')
        lines.append(f'\t\t"name": {go_str(p["nombre_comercial"])},')
        lines.append(f'\t\t"description": {go_str(p["descripcion_comercial"])},')
        lines.append(f'\t\t"category": {go_str(seg)}, "active": true, "base_price": {price}.0,')
        lines.append("\t\t\"coverages\": []Coverage{")
        for key, lab, inc, delta, src in covs:
            lines.append(
                f'\t\t\t{{Key: {go_str(key)}, Label: {go_str(lab)}, '
                f'Included: {"true" if inc else "false"}, PriceDelta: {delta}, Source: {go_str(src)}}},'
            )
        lines.append("\t\t},")
        lines.append("\t\t\"metadata_json\": map[string]interface{}{")
        lines.append(f'\t\t\t"segmento": {go_str(seg)}, "subsegmento": {go_str(p.get("subsegmento", ""))},')
        lines.append(f'\t\t\t"modalidad": {go_str(p.get("modalidad", ""))}, "alianza": {go_str(p.get("alianza", ""))},')
        benefits = ", ".join(go_str(b) for b in p.get("beneficios_destacados", []))
        lines.append(f'\t\t\t"beneficios": []string{{{benefits}}},')
        channels = ", ".join(go_str(c) for c in p.get("canal_ofrecimiento", []))
        lines.append(f'\t\t\t"canales": []string{{{channels}}},')
        srcs = ", ".join(go_str(f["url"]) for f in p.get("fuentes", []))
        lines.append(f'\t\t\t"fuentes": []string{{{srcs}}},')
        lines.append('\t\t\t"price_source": "estimación de demo — Colsubsidio no publica primas (ver condiciones_no_publicadas en sourceapi.json)",')
        no_pub = ", ".join(go_str(c) for c in p.get("condiciones_no_publicadas", []))
        lines.append(f'\t\t\t"condiciones_no_publicadas": []string{{{no_pub}}},')
        lines.append("\t\t},")
        lines.append('\t\t"created_at": "2026-07-25T00:00:00Z", "updated_at": "2026-07-25T00:00:00Z",')
        lines.append("\t},")

    lines.append("}")
    lines.append("")
    lines.append("// productIDs expone los identificadores por clave del portafolio, para que las")
    lines.append("// reglas se escriban contra un nombre legible y no contra un UUID suelto.")
    lines.append("var productIDs = map[string]string{")
    for p in data["productos"]:
        pid = str(uuid.uuid5(ns, p["id_producto"]))
        short = p["id_producto"].replace("colsubsidio-", "")
        lines.append(f'\t{go_str(short)}: {go_str(pid)},')
    lines.append("}")
    lines.append("")

    OUT.write_text("\n".join(lines) + "\n")
    print(f"escrito {OUT} — {len(data['productos'])} productos")


if __name__ == "__main__":
    main()
