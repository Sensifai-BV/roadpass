package domain

// Link is one lineage edge: Parent was consumed in, carried by, or laid as Child.
type Link struct {
	Parent string  `json:"parent"`
	Child  string  `json:"child"`
	Kind   string  `json:"kind"`
	MassKg float64 `json:"massKg"`
}

// Link kinds. Material flows along input, delivered and laid; shipped, tested and
// accepts attach evidence to a node without being part of the material path.
const (
	LinkInput     = "input"
	LinkShipped   = "shipped"
	LinkDelivered = "delivered"
	LinkLaid      = "laid"
	LinkTested    = "tested"
	LinkAccepts   = "accepts"
	LinkReleases  = "releases"
)

// MaterialLinks are the kinds a passport walks to find where material came from.
var MaterialLinks = []string{LinkInput, LinkDelivered, LinkLaid}

// Links derives the lineage edges a record creates.
func (r *Record) Links() []Link {
	var out []Link
	kind := LinkInput
	switch r.Type {
	case TypeShipment:
		kind = LinkShipped
	case TypeDelivery:
		kind = LinkDelivered
	}
	for _, in := range r.Inputs {
		out = append(out, Link{Parent: in.ID, Child: r.ID, Kind: kind, MassKg: in.MassKg})
	}
	switch r.Type {
	case TypeDelivery:
		if s := Str(r.Body, "section_id"); s != "" {
			out = append(out, Link{Parent: r.ID, Child: s, Kind: LinkLaid, MassKg: r.MassKg})
		}
	case TypeLabTest:
		if s := Str(r.Body, "subject_id"); s != "" {
			out = append(out, Link{Parent: s, Child: r.ID, Kind: LinkTested})
		}
	case TypeAcceptance:
		if s := Str(r.Body, "subject_id"); s != "" {
			out = append(out, Link{Parent: s, Child: r.ID, Kind: LinkAccepts})
		}
	case TypeRelease:
		if s := Str(r.Body, "subject_id"); s != "" {
			out = append(out, Link{Parent: s, Child: r.ID, Kind: LinkReleases})
		}
	}
	return out
}
