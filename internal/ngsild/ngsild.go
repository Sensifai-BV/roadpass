// Package ngsild maps ROADPASS records to and from NGSI-LD entities (ETSI GS CIM
// 009), so partners can exchange passports in the format the CIRCULOOS Data
// Platform and other FIWARE context brokers speak.
//
// Both the normalized and the concise representation are accepted on input; the
// normalized form, or keyValues with options=keyValues, is produced on output.
// Whatever the input format, an entity becomes the same domain.Record before it is
// hashed and signed, so a record's fingerprint does not depend on how it arrived.
//
// JSON-LD expansion is not performed against arbitrary remote contexts: terms are
// resolved against the ROADPASS context (served at ContextPath) and the NGSI-LD
// core context. A request whose context is neither gets LdContextNotAvailable
// rather than a silently wrong interpretation.
package ngsild

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/sensifai/roadpass/internal/domain"
)

const (
	// CoreContext is the NGSI-LD core context (v1.8).
	CoreContext = "https://uri.etsi.org/ngsi-ld/v1/ngsi-ld-core-context-v1.8.jsonld"
	// Vocab is the IRI base of ROADPASS terms.
	Vocab = "https://roadpass.sensifai.com/ns/"
	// ContextPath is where the service serves its @context document.
	ContextPath = "/ngsi-ld/v1/context.jsonld"
	// MediaLD and MediaJSON are the two accepted content types.
	MediaLD   = "application/ld+json"
	MediaJSON = "application/json"
)

// Entity is a decoded NGSI-LD entity.
type Entity = map[string]any

// EntityTypes maps record types to NGSI-LD entity type names.
var EntityTypes = map[domain.RecordType]string{
	domain.TypeRejectLot:     "PlasticRejectLot",
	domain.TypeRecycledLot:   "RecycledPlasticLot",
	domain.TypeAdditiveBatch: "AsphaltAdditiveBatch",
	domain.TypeAsphaltLot:    "AsphaltLot",
	domain.TypeDelivery:      "AsphaltDelivery",
	domain.TypeRoadSection:   "RoadSection",
	domain.TypeShipment:      "MaterialShipment",
	domain.TypeLabTest:       "LabTest",
	domain.TypeAcceptance:    "FindingAcceptance",
	domain.TypeRelease:       "QualityRelease",
}

// RecordTypeOf is the reverse of EntityTypes; full IRIs are accepted too.
func RecordTypeOf(entityType string) (domain.RecordType, bool) {
	entityType = strings.TrimPrefix(entityType, Vocab)
	for rt, et := range EntityTypes {
		if et == entityType {
			return rt, true
		}
	}
	return "", false
}

// UnitCodes maps the units used in domain.Fields to UN/CEFACT common codes.
var UnitCodes = map[string]string{
	"kg": "KGM", "%": "P1", "% of mix": "P1", "% of binder": "P1", "°C": "CEL", "km": "KMT",
	"m²": "MTK", "m³": "MTQ", "cm": "CMT", "kWh": "KWH", "kN": "B47", "t/m³": "D41", "": "",
}

// Envelope attribute names.
const (
	AttrReference  = "reference"
	AttrDate       = "dateObserved"
	AttrMass       = "mass"
	AttrIssuedBy   = "issuedBy"
	AttrSite       = "atSite"
	AttrReceiver   = "receiver"
	AttrHasInput   = "hasInput"
	AttrProvenance = "provenance"
	AttrNote       = "signerNote"
	AttrVersion    = "version"
	AttrPrevHash   = "previousHash"
	AttrLocation   = "location"
	AttrProof      = "proof"
)

// EntityID is the NGSI-LD URN of a record.
func EntityID(t domain.RecordType, recordID string) string {
	return "urn:ngsi-ld:" + EntityTypes[t] + ":" + recordID
}

// OrgURN and SiteURN name organisations and sites.
func OrgURN(id string) string  { return "urn:ngsi-ld:Organization:" + id }
func SiteURN(id string) string { return "urn:ngsi-ld:Site:" + id }

// RecordIDOf extracts the record id from an entity URN, or returns the input when
// it is already a bare id.
func RecordIDOf(urn string) string {
	if !strings.HasPrefix(urn, "urn:ngsi-ld:") {
		return urn
	}
	parts := strings.SplitN(strings.TrimPrefix(urn, "urn:ngsi-ld:"), ":", 2)
	if len(parts) != 2 {
		return urn
	}
	return parts[1]
}

// Camel converts snake_case field names to the NGSI-LD attribute style.
func Camel(s string) string {
	var b strings.Builder
	up := false
	for _, r := range s {
		if r == '_' {
			up = true
			continue
		}
		if up {
			r = unicode.ToUpper(r)
			up = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Location is the GeoJSON point of a record's site, when known.
type Location struct{ Lat, Lon float64 }

// Proof is the server-assigned trust state shown on output.
type Proof struct {
	ContentHash string `json:"contentHash"`
	Algorithm   string `json:"algorithm"`
	Status      string `json:"status"`
	KeyID       string `json:"keyId,omitempty"`
	Signature   string `json:"signature,omitempty"`
	CoKeyID     string `json:"coKeyId,omitempty"`
	CoSignature string `json:"coSignature,omitempty"`
	SealID      string `json:"sealId,omitempty"`
	LeafIndex   *int   `json:"leafIndex,omitempty"`
}

func prop(v any, unit string, at string) map[string]any {
	p := map[string]any{"type": "Property", "value": v}
	if code := UnitCodes[unit]; code != "" {
		p["unitCode"] = code
	}
	if at != "" {
		p["observedAt"] = at
	}
	return p
}

func rel(obj string) map[string]any { return map[string]any{"type": "Relationship", "object": obj} }

// FromRecord renders a record as a normalized NGSI-LD entity. resolve maps a
// referenced record id to its type (for Relationship URNs); nil uses bare ids.
func FromRecord(r *domain.Record, loc *Location, pr *Proof, resolve func(id string) (domain.RecordType, bool)) Entity {
	at := r.OccurredAt.UTC().Format(time.RFC3339)
	refURN := func(id string) string {
		if resolve != nil {
			if t, ok := resolve(id); ok {
				return EntityID(t, id)
			}
		}
		return "urn:ngsi-ld:Record:" + id
	}
	e := Entity{
		"id":           EntityID(r.Type, r.ID),
		"type":         EntityTypes[r.Type],
		AttrReference:  prop(r.Ref, "", ""),
		AttrDate:       prop(map[string]any{"@type": "DateTime", "@value": at}, "", ""),
		AttrMass:       prop(r.MassKg, "kg", at),
		AttrIssuedBy:   rel(OrgURN(r.OrgID)),
		AttrProvenance: prop(r.Provenance, "", ""),
		AttrVersion:    prop(r.Version, "", ""),
	}
	if r.PrevHash != "" {
		e[AttrPrevHash] = prop(r.PrevHash, "", "")
	}
	if r.Note != "" {
		e[AttrNote] = prop(r.Note, "", "")
	}
	if r.SiteID != "" {
		e[AttrSite] = rel(SiteURN(r.SiteID))
	}
	if r.Counterparty != "" {
		e[AttrReceiver] = rel(OrgURN(r.Counterparty))
	}
	if len(r.Inputs) > 0 {
		ins := make([]any, len(r.Inputs))
		for i, in := range r.Inputs {
			x := rel(refURN(in.ID))
			x["datasetId"] = fmt.Sprintf("urn:ngsi-ld:Dataset:input-%d", i+1)
			x[AttrMass] = prop(in.MassKg, "kg", "")
			ins[i] = x
		}
		if len(ins) == 1 {
			e[AttrHasInput] = ins[0]
		} else {
			e[AttrHasInput] = ins
		}
	}
	for _, fd := range domain.Fields[r.Type] {
		v, ok := r.Body[fd.Name]
		if !ok {
			continue
		}
		name := Camel(fd.Name)
		switch fd.Kind {
		case domain.FRef:
			e[name] = rel(refURN(fmt.Sprint(v)))
		case domain.FOrg:
			e[name] = rel(OrgURN(fmt.Sprint(v)))
		case domain.FSite:
			e[name] = rel(SiteURN(fmt.Sprint(v)))
		case domain.FTime:
			e[name] = prop(map[string]any{"@type": "DateTime", "@value": v}, "", "")
		default:
			e[name] = prop(v, fd.Unit, "")
		}
	}
	if loc != nil {
		e[AttrLocation] = map[string]any{"type": "GeoProperty",
			"value": map[string]any{"type": "Point", "coordinates": []float64{loc.Lon, loc.Lat}}}
	}
	if pr != nil {
		e[AttrProof] = prop(pr, "", "")
	}
	return e
}

// KeyValues simplifies a normalized entity (options=keyValues).
func KeyValues(e Entity) Entity {
	out := make(Entity, len(e))
	for k, v := range e {
		out[k] = simplify(v)
	}
	return out
}

func simplify(v any) any {
	switch a := v.(type) {
	case map[string]any:
		switch a["type"] {
		case "Relationship":
			return a["object"]
		case "Property", "GeoProperty":
			if dt, ok := a["value"].(map[string]any); ok && dt["@type"] == "DateTime" {
				return dt["@value"]
			}
			return a["value"]
		}
		return a
	case []any:
		out := make([]any, len(a))
		for i, x := range a {
			out[i] = simplify(x)
		}
		return out
	}
	return v
}

// Error types (ETSI GS CIM 009 §5.5.2).
const (
	ErrInvalidRequest      = "https://uri.etsi.org/ngsi-ld/errors/InvalidRequest"
	ErrBadRequestData      = "https://uri.etsi.org/ngsi-ld/errors/BadRequestData"
	ErrAlreadyExists       = "https://uri.etsi.org/ngsi-ld/errors/AlreadyExists"
	ErrResourceNotFound    = "https://uri.etsi.org/ngsi-ld/errors/ResourceNotFound"
	ErrOperationNotSupport = "https://uri.etsi.org/ngsi-ld/errors/OperationNotSupported"
	ErrLdContextNotAvail   = "https://uri.etsi.org/ngsi-ld/errors/LdContextNotAvailable"
)

// Error is an NGSI-LD problem.
type Error struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

func (e *Error) Error() string { return e.Title + ": " + e.Detail }

func bad(format string, a ...any) *Error {
	return &Error{Type: ErrBadRequestData, Title: "Bad request data", Detail: fmt.Sprintf(format, a...)}
}

// CheckContext accepts the ROADPASS context URL, the core context, or an inline
// list/object made only of those. selfURL is the absolute URL of ContextPath.
func CheckContext(ctx any, selfURL string) error {
	ok := func(s string) bool {
		return s == selfURL || strings.HasSuffix(s, ContextPath) || strings.HasPrefix(s, "https://uri.etsi.org/ngsi-ld/")
	}
	switch c := ctx.(type) {
	case nil:
		return nil
	case string:
		if ok(c) {
			return nil
		}
		return &Error{Type: ErrLdContextNotAvail, Title: "LD context not available",
			Detail: fmt.Sprintf("context %q is not supported; use %s", c, selfURL)}
	case []any:
		for _, x := range c {
			if err := CheckContext(x, selfURL); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		return nil // inline term definitions: terms are matched by name below
	}
	return bad("@context must be a URL, a list or an object")
}

// attrValue returns the value of a normalized or concise attribute.
func attrValue(v any) (any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return v, true // concise property
	}
	if t, _ := m["type"].(string); t == "Relationship" || (t == "" && m["object"] != nil) {
		return m["object"], true
	}
	if val, ok := m["value"]; ok {
		if dt, ok := val.(map[string]any); ok && dt["@type"] == "DateTime" {
			return dt["@value"], true
		}
		return val, true
	}
	if _, isGeo := m["coordinates"]; isGeo {
		return m, true
	}
	return nil, false
}

func str(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// ToRecord converts an incoming entity to a record owned by orgID. The record's
// version, hash, status and signatures are assigned by the ingest pipeline.
func ToRecord(e Entity, orgID string) (*domain.Record, error) {
	et, _ := e["type"].(string)
	rt, ok := RecordTypeOf(et)
	if !ok {
		return nil, bad("unknown entity type %q; supported: %s", et, strings.Join(sortedTypes(), ", "))
	}
	id, _ := e["id"].(string)
	r := &domain.Record{Type: rt, OrgID: orgID, Body: map[string]any{}} // no provenance attribute: ingest uses the organisation's
	if id != "" {
		if want := "urn:ngsi-ld:" + EntityTypes[rt] + ":"; !strings.HasPrefix(id, want) {
			return nil, bad("id %q must start with %s", id, want)
		}
		r.ID = RecordIDOf(id)
	}
	fields := map[string]domain.Field{}
	for _, fd := range domain.Fields[rt] {
		fields[Camel(fd.Name)] = fd
	}
	for k, raw := range e {
		k = strings.TrimPrefix(k, Vocab)
		switch k {
		case "id", "type", "@context", AttrProof, AttrLocation, "createdAt", "modifiedAt", "scope":
			continue
		case AttrReference:
			v, _ := attrValue(raw)
			s, ok := str(v)
			if !ok {
				return nil, bad("reference must be text")
			}
			r.Ref = s
		case AttrDate:
			v, _ := attrValue(raw)
			s, _ := str(v)
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return nil, bad("dateObserved must be an RFC 3339 date-time")
			}
			r.OccurredAt = t.UTC()
		case AttrMass:
			v, _ := attrValue(raw)
			x, ok := v.(float64)
			if !ok {
				return nil, bad("mass must be a number in kilograms (unitCode KGM)")
			}
			if m, ok := raw.(map[string]any); ok {
				if uc, _ := m["unitCode"].(string); uc != "" && uc != "KGM" {
					if uc != "TNE" {
						return nil, bad("mass unitCode %q not supported (use KGM or TNE)", uc)
					}
					x *= 1000
				}
			}
			r.MassKg = x
		case AttrProvenance:
			v, _ := attrValue(raw)
			s, _ := str(v)
			r.Provenance = s
		case AttrNote:
			v, _ := attrValue(raw)
			r.Note, _ = str(v)
		case AttrPrevHash:
			v, _ := attrValue(raw)
			r.PrevHash, _ = str(v)
		case AttrVersion:
			v, _ := attrValue(raw)
			if x, ok := v.(float64); ok {
				r.Version = int(x)
			}
		case AttrIssuedBy:
			v, _ := attrValue(raw)
			if s, _ := str(v); s != "" && RecordIDOf(s) != orgID {
				return nil, bad("issuedBy %s does not match the authenticated organisation", s)
			}
		case AttrSite:
			v, _ := attrValue(raw)
			s, _ := str(v)
			r.SiteID = RecordIDOf(s)
		case AttrReceiver:
			v, _ := attrValue(raw)
			s, _ := str(v)
			r.Counterparty = RecordIDOf(s)
		case AttrHasInput:
			ins, err := inputs(raw)
			if err != nil {
				return nil, err
			}
			r.Inputs = ins
		default:
			fd, ok := fields[k]
			if !ok {
				return nil, bad("attribute %q is not defined for %s", k, et)
			}
			v, ok := attrValue(raw)
			if !ok {
				return nil, bad("attribute %q has no value", k)
			}
			if fd.Kind == domain.FRef || fd.Kind == domain.FOrg || fd.Kind == domain.FSite {
				s, _ := str(v)
				v = RecordIDOf(s)
			}
			r.Body[fd.Name] = v
		}
	}
	return r, nil
}

func inputs(raw any) ([]domain.Input, error) {
	list, ok := raw.([]any)
	if !ok {
		list = []any{raw}
	}
	out := make([]domain.Input, 0, len(list))
	for i, x := range list {
		m, ok := x.(map[string]any)
		if !ok {
			return nil, bad("hasInput[%d] must be a Relationship", i)
		}
		obj, _ := str(m["object"])
		if obj == "" {
			return nil, bad("hasInput[%d] has no object", i)
		}
		mass, _ := attrValue(m[AttrMass])
		kg, ok := mass.(float64)
		if !ok {
			return nil, bad("hasInput[%d] needs a mass sub-property in kilograms", i)
		}
		out = append(out, domain.Input{ID: RecordIDOf(obj), MassKg: kg})
	}
	return out, nil
}

func sortedTypes() []string {
	out := make([]string, 0, len(EntityTypes))
	for _, v := range EntityTypes {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// Context returns the ROADPASS @context document.
func Context() map[string]any {
	terms := map[string]any{"rp": Vocab}
	for _, et := range EntityTypes {
		terms[et] = "rp:" + et
	}
	for _, a := range []string{AttrReference, AttrMass, AttrIssuedBy, AttrSite, AttrReceiver, AttrHasInput,
		AttrProvenance, AttrNote, AttrVersion, AttrPrevHash, AttrProof} {
		terms[a] = "rp:" + a
	}
	terms[AttrDate] = "https://smartdatamodels.org/dateObserved"
	for _, fs := range domain.Fields {
		for _, fd := range fs {
			name := Camel(fd.Name)
			if fd.Kind == domain.FRef || fd.Kind == domain.FOrg {
				terms[name] = map[string]any{"@id": "rp:" + name, "@type": "@id"}
			} else {
				terms[name] = "rp:" + name
			}
		}
	}
	return map[string]any{"@context": []any{terms, CoreContext}}
}

// AsError unwraps an *Error.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
