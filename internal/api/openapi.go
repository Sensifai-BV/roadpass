package api

import (
	"net/http"

	"github.com/sensifai/roadpass/internal/domain"
)

// openapi serves an OpenAPI 3.1 description of the public API, generated from the
// same field catalogue that validates submissions.
func (s *Server) openapi(w http.ResponseWriter, _ *http.Request) {
	recordData := map[string]any{}
	for _, t := range domain.RecordTypes {
		props := map[string]any{}
		var required []string
		for _, f := range domain.Fields[t] {
			p := map[string]any{"description": f.Label}
			if f.Legacy {
				p["deprecated"] = true
			}
			switch f.Kind {
			case domain.FNumber:
				p["type"] = "number"
				if f.Min != nil {
					p["minimum"] = *f.Min
				}
				if f.Max != nil {
					p["maximum"] = *f.Max
				}
			case domain.FEnum:
				p["type"], p["enum"] = "string", f.Enum
			case domain.FTime:
				p["type"], p["format"] = "string", "date-time"
			default:
				p["type"] = "string"
			}
			if f.Unit != "" {
				p["description"] = f.Label + " (" + f.Unit + ")"
			}
			p["example"] = f.Example
			props[f.Name] = p
			if f.Required {
				required = append(required, f.Name)
			}
		}
		recordData[string(t)] = map[string]any{"type": "object", "title": domain.Types[t].Name,
			"description": domain.Types[t].Summary, "properties": props, "required": required, "additionalProperties": false}
	}
	op := func(summary, tag string) map[string]any {
		return map[string]any{"summary": summary, "tags": []string{tag}, "responses": map[string]any{"200": map[string]any{"description": "OK"}}}
	}
	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{"title": "ROADPASS API", "version": s.Version,
			"description": "Digital product passports for recycled plastic in asphalt roads. Authenticate with `Authorization: Bearer rpk_…` (an organisation API key). Every write is validated, fingerprinted (SHA-256 over RFC 8785 JSON), signed with your organisation's key, checked (C1–C8) and sealed on a blockchain ledger. The same data is available as NGSI-LD under /ngsi-ld/v1."},
		"servers": []any{map[string]any{"url": s.Cfg.PublicURL}},
		"components": map[string]any{
			"securitySchemes": map[string]any{"apiKey": map[string]any{"type": "http", "scheme": "bearer"}},
			"schemas": map[string]any{
				"RecordData": map[string]any{"oneOf": func() []any {
					var out []any
					for _, t := range domain.RecordTypes {
						out = append(out, map[string]any{"$ref": "#/components/schemas/" + string(t)})
					}
					return out
				}()},
				"Input": map[string]any{"type": "object", "required": []string{"id", "massKg"},
					"properties": map[string]any{"id": map[string]any{"type": "string", "example": "RPL-26-000123"}, "massKg": map[string]any{"type": "number"}}},
				"RecordInput": map[string]any{"type": "object", "required": []string{"type", "ref", "occurredAt", "data"},
					"properties": map[string]any{
						"id":         map[string]any{"type": "string", "description": "Only for a correction (creates the next version)"},
						"type":       map[string]any{"type": "string", "enum": domain.RecordTypes},
						"ref":        map[string]any{"type": "string", "description": "Your own reference; unique per type"},
						"siteId":     map[string]any{"type": "string"},
						"occurredAt": map[string]any{"type": "string", "format": "date-time"},
						"massKg":     map[string]any{"type": "number"},
						"inputs":     map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Input"}},
						"data":       map[string]any{"$ref": "#/components/schemas/RecordData"},
						"signature":  map[string]any{"type": "string", "description": "Optional base64 Ed25519 signature with your own key"},
						"keyId":      map[string]any{"type": "string"},
					}},
			},
		},
		"security": []any{map[string]any{"apiKey": []string{}}},
		"paths": map[string]any{
			"/api/v1/records": map[string]any{
				"get": op("List records you issued or received", "Records"),
				"post": map[string]any{"summary": "Submit 1–1,000 records (all or nothing)", "tags": []string{"Records"},
					"parameters": []any{map[string]any{"name": "Idempotency-Key", "in": "header", "schema": map[string]any{"type": "string"}}},
					"requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object",
						"properties": map[string]any{"records": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/RecordInput"}}}}}}},
					"responses": map[string]any{"201": map[string]any{"description": "Stored, signed and checked"}, "422": map[string]any{"description": "Invalid rows; nothing stored"}}},
			},
			"/api/v1/records/{id}":                map[string]any{"get": op("Read a record", "Records")},
			"/api/v1/records/{id}/proof":          map[string]any{"get": op("Hash document, signature and Merkle path to the ledger", "Trust")},
			"/api/v1/shipments/{id}/receipt":      map[string]any{"post": op("Confirm receipt of a shipment (signed by the receiver)", "Records")},
			"/api/v1/records/{id}/void":           map[string]any{"post": op("Void one of your records (a signed new version with no mass)", "Records")},
			"/api/v1/passports/{id}":              map[string]any{"get": op("Assembled passport: lineage, origins, carbon, trust", "Passports")},
			"/api/v1/public/passports/{id}":       map[string]any{"get": op("Public passport layer (no authentication)", "Passports")},
			"/api/v1/imports":                     map[string]any{"post": op("Upload a CSV (multipart, ?type=…&dryRun=1)", "CSV")},
			"/api/v1/templates/{type}.csv":        map[string]any{"get": op("CSV template for a record type", "CSV")},
			"/api/v1/telemetry":                   map[string]any{"post": op("Queue extruder readings and mixer cycles (202)", "Plant data")},
			"/api/v1/findings":                    map[string]any{"get": op("Data-check findings", "Data checks")},
			"/api/v1/analytics/overview":          map[string]any{"get": op("Headline figures", "Analytics")},
			"/api/v1/ledger/verify":               map[string]any{"post": op("Verify a fingerprint or record against the ledger (public)", "Trust")},
			"/api/v1/exports/greta.csv":           map[string]any{"get": op("GRETA-ready LCA input (?scenario=baseline|additive|sensitivity)", "Exports")},
			"/ngsi-ld/v1/entities":                map[string]any{"get": op("NGSI-LD query", "NGSI-LD"), "post": op("NGSI-LD create", "NGSI-LD")},
			"/ngsi-ld/v1/entities/{id}":           map[string]any{"get": op("NGSI-LD retrieve", "NGSI-LD")},
			"/ngsi-ld/v1/entities/{id}/attrs":     map[string]any{"patch": op("NGSI-LD update (creates the next version)", "NGSI-LD")},
			"/ngsi-ld/v1/entityOperations/upsert": map[string]any{"post": op("NGSI-LD batch upsert", "NGSI-LD")},
			"/ngsi-ld/v1/types":                   map[string]any{"get": op("NGSI-LD entity types", "NGSI-LD")},
		},
	}
	comps := spec["components"].(map[string]any)["schemas"].(map[string]any)
	for k, v := range recordData {
		comps[k] = v
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, spec)
}
