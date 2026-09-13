package passport

import (
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/lca"
	"github.com/sensifai/roadpass/internal/proof"
	"github.com/sensifai/roadpass/internal/signing"
	"github.com/sensifai/roadpass/internal/store"
)

// Access is how much of a passport a viewer may see.
type Access string

const (
	// AccessFull: the viewer issued or received the record, or oversees the chain.
	AccessFull Access = "full"
	// AccessChain: the viewer took part in the record's lineage; it sees the whole
	// journey, with other companies' records summarised.
	AccessChain Access = "chain"
	// AccessPublic: anyone with the link; the product-passport public layer.
	AccessPublic Access = "public"
)

// OrgRef names an organisation.
type OrgRef struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Kind   domain.OrgKind `json:"kind"`
	City   string         `json:"city"`
	Region string         `json:"region"`
	Status string         `json:"status"`
	Logo   bool           `json:"logo,omitempty"` // served at /api/v1/orgs/{id}/logo
}

// Node is one record in the lineage graph.
type Node struct {
	ID         string            `json:"id"`
	Type       domain.RecordType `json:"type"`
	OrgID      string            `json:"orgId"`
	SiteID     string            `json:"siteId,omitempty"` // the plant or site, for maps
	Ref        string            `json:"ref,omitempty"`
	MassKg     float64           `json:"massKg"`
	OccurredAt time.Time         `json:"occurredAt"`
	Status     domain.Status     `json:"status"`
	Signed     bool              `json:"signed"`
	Sealed     bool              `json:"sealed"`
	Share      float64           `json:"share"` // share of the root's material that passed through this node
	Data       map[string]any    `json:"data,omitempty"`
}

// Stage aggregates the journey at one step of the chain.
type Stage struct {
	Stage   domain.Stage      `json:"stage"`
	Type    domain.RecordType `json:"type"`
	Name    string            `json:"name"`
	Records int               `json:"records"`
	MassKg  float64           `json:"massKg"` // contributed to the root
	Orgs    []string          `json:"orgs"`
	First   time.Time         `json:"first"`
	Last    time.Time         `json:"last"`
}

// Origin is one source of the root's plastic.
type Origin struct {
	OrgID  string  `json:"orgId"`
	Region string  `json:"region"`
	Kg     float64 `json:"kg"` // reject film contributed
	Lots   int     `json:"lots"`
	Route  string  `json:"route"`
}

// Event is one item of the timeline.
type Event struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"` // created | shipped | received | tested | finding | sealed | accepted | version | voided
	Record string    `json:"record,omitempty"`
	OrgID  string    `json:"orgId,omitempty"`
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
	ToSite string    `json:"toSiteId,omitempty"` // shipped: the receiving site (named only from the sender's own address book)
	OK     *bool     `json:"ok,omitempty"`
}

// Trust is what a verifier needs, for the root record.
type Trust struct {
	HashDoc     *domain.HashDoc `json:"hashDoc,omitempty"` // absent on the public layer
	ContentHash string          `json:"contentHash"`
	Algorithm   string          `json:"algorithm"`
	Signature   *SigView        `json:"signature,omitempty"`
	// ActorSignature is the submitting member's personal signature over the same
	// message; ReceiptActor the receiving member's over the receipt.
	ActorSignature *SigView      `json:"actorSignature,omitempty"`
	Receipt        *SigView      `json:"receipt,omitempty"`
	ReceiptActor   *SigView      `json:"receiptActor,omitempty"`
	Seal           *store.Seal   `json:"seal,omitempty"`
	LeafIndex      int           `json:"leafIndex"`
	Path           []proof.Step  `json:"path,omitempty"`
	Versions       []VersionView `json:"versions"`
}

// SigView is a signature with its public key and server-side verdict.
type SigView struct {
	KeyID      string    `json:"keyId"`
	OrgID      string    `json:"orgId"`
	Algorithm  string    `json:"algorithm"`
	Value      string    `json:"value"`
	PublicKey  string    `json:"publicKey"`
	OnLedger   bool      `json:"onLedger"`
	LedgerTxID string    `json:"ledgerTxId,omitempty"`
	SignedAt   time.Time `json:"signedAt"`
	Valid      bool      `json:"valid"`
	// Receipt fields (shipments)
	ReceivedAt *time.Time `json:"receivedAt,omitempty"`
	MassKg     float64    `json:"massKg,omitempty"`
	// Personal keys: the member (a pseudonymous id; the name only for viewers in
	// the same organisation).
	HolderID   string `json:"holderId,omitempty"`
	HolderName string `json:"holderName,omitempty"`
	Decision   string `json:"decision,omitempty"` // intake review (v2 receipts)
	Note       string `json:"note,omitempty"`
	// NoteHidden: the note exists but this view may not show it, so the receipt
	// signature cannot be re-checked here.
	NoteHidden bool `json:"noteHidden,omitempty"`
}

// VersionView is one version of the root.
type VersionView struct {
	Version     int       `json:"version"`
	ContentHash string    `json:"contentHash"`
	PrevHash    string    `json:"prevHash,omitempty"`
	OccurredAt  time.Time `json:"occurredAt"`
	SealID      string    `json:"sealId,omitempty"`
}

// Passport is the assembled view of one record.
type Passport struct {
	Access     Access            `json:"access"`
	Record     *domain.Record    `json:"record,omitempty"`
	Root       Node              `json:"root"`
	Org        OrgRef            `json:"org"`
	Orgs       map[string]OrgRef `json:"orgs"`
	Carbon     lca.Result        `json:"carbon"`
	Scenarios  []lca.Result      `json:"scenarios"`
	Stages     []Stage           `json:"stages"`
	Origins    []Origin          `json:"origins"`
	Nodes      []Node            `json:"nodes,omitempty"`
	Links      []domain.Link     `json:"links,omitempty"`
	Downstream []Node            `json:"downstream,omitempty"`
	Timeline   []Event           `json:"timeline,omitempty"`
	Tests      []Node            `json:"tests,omitempty"`
	Findings   []store.Finding   `json:"findings,omitempty"`
	Trust      *Trust            `json:"trust,omitempty"`
	Truncated  bool              `json:"truncated"`
	Simulated  bool              `json:"simulated"`
	// TracedKg is, for a road section, the asphalt of the accepted deliveries laid
	// on it: what the chain can trace. It differs from the section's own mass when
	// the as-laid claim was accepted as an exception (check C5) or deliveries are
	// still missing; the passport shows both rather than one unqualified figure.
	TracedKg *float64  `json:"tracedKg,omitempty"`
	BuiltAt  time.Time `json:"builtAt"`
}

// Builder assembles passports.
type Builder struct {
	St *store.Store
}

// ErrNotPassport is returned for record types that are evidence, not passports.
var ErrNotPassport = errors.New("this record type has no passport")

// Build assembles the passport of id for a viewer (nil viewer: public).
func (b *Builder) Build(ctx context.Context, id string, v *store.Viewer) (*Passport, error) {
	root, err := b.St.RecordAny(ctx, id)
	if err != nil {
		return nil, err
	}
	g, err := b.St.Upstream(ctx, id, 1500)
	if err != nil {
		return nil, err
	}
	g, dropped := counted(g, id, true)
	orgs, err := b.orgs(ctx)
	if err != nil {
		return nil, err
	}
	p := &Passport{Truncated: g.Truncated, BuiltAt: time.Now().UTC(), Simulated: root.Provenance == domain.ProvSimulated,
		Orgs: map[string]OrgRef{}}
	participants := map[string]bool{}
	for _, n := range g.Nodes {
		participants[n.OrgID] = true
		if n.Counterparty != "" {
			participants[n.Counterparty] = true
		}
	}
	p.Access = AccessPublic
	if v != nil {
		switch {
		case v.All() || root.OrgID == v.OrgID || root.Counterparty == v.OrgID || domain.Str(root.Body, "owner_org_id") == v.OrgID:
			p.Access = AccessFull
		case participants[v.OrgID]:
			p.Access = AccessChain
		}
	}

	// Carbon: mass-allocated over the lineage, with shipments' transport.
	ids := make([]string, len(g.Nodes))
	for i, n := range g.Nodes {
		ids[i] = n.ID
	}
	ships, err := b.St.ShipmentsOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, sc := range lca.Scenarios {
		shipFoot := map[string]lca.Intensity{}
		for item, list := range ships {
			for _, s := range list {
				if s.Shipment.ID == root.ID {
					continue // a shipment's own transport is added once, on the shipment itself
				}
				shipFoot[item] = shipFoot[item].Add(lca.ShipmentShare(&s.Shipment, s.ItemKg, sc))
			}
		}
		res := lca.Summarise(&root, lca.Flow(g.Nodes, g.Links, shipFoot, sc), sc)
		p.Scenarios = append(p.Scenarios, res)
		if sc.Key == lca.DefaultScenario.Key {
			p.Carbon = res
		}
	}

	if root.Type == domain.TypeRoadSection {
		var laid float64
		for _, l := range g.Links { // counted() has dropped loads rejected at the paver and voids
			if l.Kind == domain.LinkLaid && l.Child == root.ID {
				laid += l.MassKg
			}
		}
		p.TracedKg = &laid
	}

	shares := shareOf(root.ID, g)
	byID := map[string]*domain.Record{}
	for i := range g.Nodes {
		byID[g.Nodes[i].ID] = &g.Nodes[i]
	}
	full := func(r *domain.Record) bool {
		if p.Access == AccessFull && r.ID == root.ID {
			return true
		}
		return v != nil && (v.All() || r.OrgID == v.OrgID || r.Counterparty == v.OrgID)
	}
	nodeOf := func(r *domain.Record) Node {
		n := Node{ID: r.ID, Type: r.Type, OrgID: r.OrgID, SiteID: r.SiteID, MassKg: r.MassKg, OccurredAt: r.OccurredAt, Status: r.Status,
			Signed: r.Signature != nil, Sealed: r.Sealed, Share: shares[r.ID]}
		if full(r) {
			n.Ref, n.Data = r.Ref, r.Body
		}
		return n
	}
	p.Root = nodeOf(&root)
	p.Root.Share = 1
	p.Org = orgs[root.OrgID]

	// Journey by stage and origins.
	stageOf := map[domain.RecordType]*Stage{}
	for i := range g.Nodes {
		r := &g.Nodes[i]
		ti := domain.Types[r.Type]
		st := stageOf[r.Type]
		if st == nil {
			st = &Stage{Stage: ti.Stage, Type: r.Type, Name: ti.Plural, First: r.OccurredAt, Last: r.OccurredAt}
			stageOf[r.Type] = st
		}
		st.Records++
		st.MassKg += shares[r.ID] * r.MassKg
		// The company that does the step: the contractor lays a delivery (the
		// plant only issues the ticket); the road owner owns the section.
		who := r.OrgID
		switch {
		case r.Type == domain.TypeDelivery && r.Counterparty != "":
			who = r.Counterparty
		case r.Type == domain.TypeRoadSection && domain.Str(r.Body, "owner_org_id") != "":
			who = domain.Str(r.Body, "owner_org_id")
		}
		if !slices.Contains(st.Orgs, who) {
			st.Orgs = append(st.Orgs, who)
		}
		if o, ok := orgs[who]; ok {
			p.Orgs[who] = o
		}
		if r.OccurredAt.Before(st.First) {
			st.First = r.OccurredAt
		}
		if r.OccurredAt.After(st.Last) {
			st.Last = r.OccurredAt
		}
	}
	for _, st := range stageOf {
		p.Stages = append(p.Stages, *st)
	}
	sort.Slice(p.Stages, func(i, j int) bool { return p.Stages[i].Stage < p.Stages[j].Stage })
	org := map[string]*Origin{}
	for i := range g.Nodes {
		r := &g.Nodes[i]
		if r.Type != domain.TypeRejectLot {
			continue
		}
		o := org[r.OrgID]
		if o == nil {
			o = &Origin{OrgID: r.OrgID, Region: orgs[r.OrgID].Region, Route: domain.Str(r.Body, "avoided_route")}
			org[r.OrgID] = o
		}
		o.Kg += shares[r.ID] * r.MassKg
		o.Lots++
	}
	p.Origins = make([]Origin, 0, len(org)) // [] rather than null: an empty or truncated lineage has none
	for _, o := range org {
		p.Origins = append(p.Origins, *o)
	}
	sort.Slice(p.Origins, func(i, j int) bool { return p.Origins[i].Kg > p.Origins[j].Kg })

	for id := range participants {
		if o, ok := orgs[id]; ok {
			p.Orgs[id] = o
		}
	}
	if owner := domain.Str(root.Body, "owner_org_id"); owner != "" {
		p.Orgs[owner] = orgs[owner]
	}
	if p.Access == AccessPublic {
		p.Record = nil
		return p, b.trust(ctx, p, &root, false, v)
	}

	// Members: the graph, downstream use, timeline and evidence.
	rec := root
	if p.Access != AccessFull {
		rec.Body = nil
	}
	p.Record = &rec
	for i := range g.Nodes {
		p.Nodes = append(p.Nodes, nodeOf(&g.Nodes[i]))
	}
	p.Links = g.Links
	if down, err := b.St.Downstream(ctx, id, 300); err == nil {
		down, _ = counted(down, id, false)
		for i := range down.Nodes {
			if down.Nodes[i].ID != id {
				p.Downstream = append(p.Downstream, nodeOf(&down.Nodes[i]))
			}
		}
		for _, n := range p.Downstream {
			if o, ok := orgs[n.OrgID]; ok {
				p.Orgs[n.OrgID] = o
			}
		}
	}
	evidence, links, err := b.St.Evidence(ctx, ids)
	if err != nil {
		return nil, err
	}
	evByID := map[string]*domain.Record{}
	for i := range evidence {
		evByID[evidence[i].ID] = &evidence[i]
		if o, ok := orgs[evidence[i].OrgID]; ok {
			p.Orgs[o.ID] = o
		}
	}
	for i := range g.Nodes {
		r := &g.Nodes[i]
		p.Timeline = append(p.Timeline, Event{At: r.OccurredAt, Kind: "created", Record: r.ID, OrgID: r.OrgID,
			Title: domain.Types[r.Type].Name + " recorded"})
		if r.Sealed {
			p.Timeline = append(p.Timeline, Event{At: r.CreatedAt, Kind: "sealed", Record: r.ID, Title: "Sealed on the ledger", Detail: r.SealID})
		}
		if r.Void() {
			ev := Event{At: r.CreatedAt, Kind: "voided", Record: r.ID, OrgID: r.OrgID, Title: "Voided: " + r.ID, OK: new(false)}
			if p.Access != AccessPublic {
				ev.Detail = r.VoidReason()
			}
			p.Timeline = append(p.Timeline, ev)
		}
		if r.Type == domain.TypeDelivery && r.Receipt != nil {
			ev := Event{At: r.Receipt.ReceivedAt, Kind: "received", Record: r.ID, OrgID: r.Counterparty, Title: "Taken at the paver: " + r.ID}
			switch r.Receipt.Decision {
			case domain.Rejected:
				ev.Title, ev.OK = "Rejected at the paver: "+r.ID, new(false)
			case domain.AcceptedWithNote:
				ev.Title = "Taken at the paver with a note: " + r.ID
			}
			if p.Access != AccessPublic {
				ev.Detail = r.Receipt.Note
			}
			p.Timeline = append(p.Timeline, ev)
		}
	}
	// Voided records and loads rejected at the paver are not part of the lineage,
	// but the void or the rejection stays on the timeline.
	for i := range dropped {
		r := &dropped[i]
		if r.Void() {
			ev := Event{At: r.CreatedAt, Kind: "voided", Record: r.ID, OrgID: r.OrgID, Title: "Voided: " + r.ID, OK: new(false)}
			if p.Access != AccessPublic {
				ev.Detail = r.VoidReason()
			}
			p.Timeline = append(p.Timeline, ev)
			continue
		}
		if r.Type != domain.TypeDelivery || r.Receipt == nil || r.Receipt.Decision != domain.Rejected {
			continue
		}
		ev := Event{At: r.Receipt.ReceivedAt, Kind: "received", Record: r.ID, OrgID: r.Counterparty,
			Title: "Rejected at the paver, not laid: " + r.ID, OK: new(false)}
		if full(r) {
			ev.Detail = r.Receipt.Note
		}
		p.Timeline = append(p.Timeline, ev)
	}
	for _, l := range links {
		e := evByID[l.Child]
		if e == nil {
			continue
		}
		switch l.Kind {
		case domain.LinkShipped:
			p.Timeline = append(p.Timeline, Event{At: e.OccurredAt, Kind: "shipped", Record: e.ID, OrgID: e.OrgID,
				Title: "Shipped " + l.Parent, Detail: e.Counterparty, ToSite: domain.Str(e.Body, "to_site_id")})
			if e.Receipt != nil {
				ev := Event{At: e.Receipt.ReceivedAt, Kind: "received", Record: e.ID, OrgID: e.Counterparty, Title: "Received " + l.Parent}
				switch e.Receipt.Decision {
				case domain.Rejected:
					ev.Title, ev.OK = "Rejected at intake: "+l.Parent, new(false)
				case domain.AcceptedWithNote:
					ev.Title = "Received with a note: " + l.Parent
				}
				if p.Access != AccessPublic {
					ev.Detail = e.Receipt.Note
				}
				p.Timeline = append(p.Timeline, ev)
			}
		case domain.LinkTested:
			ok := domain.Str(e.Body, "result") == "pass"
			n := nodeOf(e)
			if p.Access == AccessFull || full(e) || byID[l.Parent] != nil {
				n.Data = e.Body // test results travel with the passport
			}
			p.Tests = append(p.Tests, n)
			p.Timeline = append(p.Timeline, Event{At: e.OccurredAt, Kind: "tested", Record: e.ID, OrgID: e.OrgID,
				Title: domain.LabTests[domain.Str(e.Body, "test")].Name + " on " + l.Parent, OK: &ok})
		case domain.LinkReleases:
			ok := domain.Str(e.Body, "decision") == domain.Released
			title := "Released by quality: " + l.Parent
			if !ok {
				title = "Quarantined by quality: " + l.Parent
			}
			p.Timeline = append(p.Timeline, Event{At: e.OccurredAt, Kind: "released", Record: e.ID, OrgID: e.OrgID,
				Title: title, Detail: domain.Str(e.Body, "note"), OK: &ok})
		case domain.LinkAccepts:
			p.Timeline = append(p.Timeline, Event{At: e.OccurredAt, Kind: "accepted", Record: e.ID, OrgID: e.OrgID,
				Title: "Finding " + domain.Str(e.Body, "finding_code") + " accepted on " + l.Parent, Detail: domain.Str(e.Body, "reason")})
		}
	}
	sort.SliceStable(p.Timeline, func(i, j int) bool { return p.Timeline[i].At.Before(p.Timeline[j].At) })
	if len(p.Timeline) > 400 {
		p.Timeline = p.Timeline[len(p.Timeline)-400:]
	}
	if fs, _, err := b.St.Findings(ctx, store.SystemViewer, store.FindingFilter{RecordID: root.ID, Limit: 50}); err == nil {
		p.Findings = fs
	}
	return p, b.trust(ctx, p, &root, true, v)
}

func (b *Builder) orgs(ctx context.Context) (map[string]OrgRef, error) {
	list, err := b.St.Orgs(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]OrgRef, len(list))
	cs := make(map[string]string, len(list))
	for _, o := range list {
		out[o.ID] = OrgRef{ID: o.ID, Name: o.Name, Kind: o.Kind, City: o.City, Region: o.Region, Status: o.Status, Logo: o.HasLogo}
		cs[o.ID] = o.Country
	}
	lca.SetOrgCountries(cs) // electricity factors follow each plant's country
	return out, nil
}

// counted removes from a lineage graph what did not count as material: the
// links out of a void record or a delivery rejected at the paver (other than the
// root, whose own passport still shows it), and the records only reachable
// through them. up is the walk's direction (Upstream: towards origins). It
// returns the pruned graph and the dropped records that were cut off at a
// non-counting link (for the timeline).
func counted(g *store.Graph, root string, up bool) (*store.Graph, []domain.Record) {
	byID := make(map[string]*domain.Record, len(g.Nodes))
	for i := range g.Nodes {
		byID[g.Nodes[i].ID] = &g.Nodes[i]
	}
	cut := func(id string) bool {
		r := byID[id]
		return id != root && r != nil && !r.CountsMass()
	}
	next := map[string][]string{}
	var links []domain.Link
	for _, l := range g.Links {
		if cut(l.Parent) {
			continue
		}
		links = append(links, l)
		if up {
			next[l.Child] = append(next[l.Child], l.Parent)
		} else {
			next[l.Parent] = append(next[l.Parent], l.Child)
		}
	}
	keep := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, n := range next[id] {
			if !keep[n] {
				keep[n] = true
				queue = append(queue, n)
			}
		}
	}
	if len(links) == len(g.Links) && len(keep) >= len(g.Nodes) {
		return g, nil
	}
	out := &store.Graph{Truncated: g.Truncated}
	var dropped []domain.Record
	for _, n := range g.Nodes {
		switch {
		case keep[n.ID]:
			out.Nodes = append(out.Nodes, n)
		case cut(n.ID):
			dropped = append(dropped, n)
		}
	}
	for _, l := range links {
		if keep[l.Parent] && keep[l.Child] {
			out.Links = append(out.Links, l)
		}
	}
	return out, dropped
}

// shareOf returns, for each node, the fraction of that node's mass that ended up
// in the root: share(parent) += share(child) × link mass / parent mass. The root's
// share is 1; share × mass is the kilograms a node contributed.
func shareOf(root string, g *store.Graph) map[string]float64 {
	mass := map[string]float64{}
	for _, n := range g.Nodes {
		mass[n.ID] = n.MassKg
	}
	parents := map[string][]domain.Link{}
	children := map[string]int{}
	for _, l := range g.Links {
		parents[l.Child] = append(parents[l.Child], l)
		children[l.Parent]++
	}
	// Kahn's order from the root downwards (towards origins).
	share := map[string]float64{root: 1}
	pending := map[string]int{}
	for id, n := range children {
		pending[id] = n
	}
	queue := []string{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, l := range parents[id] {
			if pm := mass[l.Parent]; pm > 0 {
				share[l.Parent] += share[id] * l.MassKg / pm
			}
			pending[l.Parent]--
			if pending[l.Parent] == 0 {
				queue = append(queue, l.Parent)
			}
		}
	}
	return share
}

// trust fills the verification block for the root record.
func (b *Builder) trust(ctx context.Context, p *Passport, r *domain.Record, members bool, v *store.Viewer) error {
	var ids []string
	for _, sg := range []*domain.Signature{r.Signature, r.ActorSignature} {
		if sg != nil {
			ids = append(ids, sg.KeyID)
		}
	}
	if rc := r.Receipt; rc != nil {
		ids = append(ids, rc.Signature.KeyID)
		if rc.ActorSignature != nil {
			ids = append(ids, rc.ActorSignature.KeyID)
		}
	}
	keys, err := b.St.SigningKeysByID(ctx, ids)
	if err != nil {
		return err
	}
	kmap := map[string]store.SigningKey{}
	for _, k := range keys {
		kmap[k.ID] = k
	}
	doc := r.HashDoc()
	t := &Trust{HashDoc: &doc, ContentHash: r.ContentHash, Algorithm: "SHA-256 over RFC 8785 (JCS) canonical JSON"}
	if s := r.Signature; s != nil {
		k := kmap[s.KeyID]
		t.Signature = &SigView{KeyID: s.KeyID, OrgID: s.OrgID, Algorithm: signing.Algorithm,
			Value: base64.StdEncoding.EncodeToString(s.Value), PublicKey: base64.StdEncoding.EncodeToString(k.Public),
			OnLedger: k.LedgerTxID != "", LedgerTxID: k.LedgerTxID, SignedAt: s.SignedAt,
			Valid: signing.Verify(k.Public, r.ContentHash, s.Value) == nil}
	}
	if rc := r.Receipt; rc != nil {
		k := kmap[rc.Signature.KeyID]
		at := rc.ReceivedAt
		t.Receipt = &SigView{KeyID: rc.Signature.KeyID, OrgID: rc.Signature.OrgID, Algorithm: signing.Algorithm,
			Value: base64.StdEncoding.EncodeToString(rc.Signature.Value), PublicKey: base64.StdEncoding.EncodeToString(k.Public),
			OnLedger: k.LedgerTxID != "", LedgerTxID: k.LedgerTxID, SignedAt: rc.ReceivedAt, ReceivedAt: &at, MassKg: rc.MassKg,
			Decision: rc.Decision, Note: rc.Note,
			Valid: signing.VerifyReceipt(k.Public, r.ContentHash, signing.ReceiptFacts{ReceivedAt: rc.ReceivedAt, MassKg: rc.MassKg, Decision: rc.Decision, Note: rc.Note}, rc.Signature.Value) == nil}
	}
	person := func(sig *domain.Signature, msgOK func(pub []byte) bool) *SigView {
		if sig == nil {
			return nil
		}
		k := kmap[sig.KeyID]
		sv := &SigView{KeyID: sig.KeyID, OrgID: sig.OrgID, Algorithm: signing.Algorithm, Value: base64.StdEncoding.EncodeToString(sig.Value),
			PublicKey: base64.StdEncoding.EncodeToString(k.Public), OnLedger: k.LedgerTxID != "", LedgerTxID: k.LedgerTxID,
			SignedAt: sig.SignedAt, HolderID: k.UserID, Valid: len(k.Public) > 0 && msgOK(k.Public)}
		if v != nil && v.OrgID == sig.OrgID && k.UserID != "" {
			if u, err := b.St.UserByID(ctx, k.UserID); err == nil {
				sv.HolderName = u.Name
			}
		}
		return sv
	}
	t.ActorSignature = person(r.ActorSignature, func(pub []byte) bool { return signing.Verify(pub, r.ContentHash, r.ActorSignature.Value) == nil })
	if rc := r.Receipt; rc != nil && rc.ActorSignature != nil {
		facts := signing.ReceiptFacts{ReceivedAt: rc.ReceivedAt, MassKg: rc.MassKg, Decision: rc.Decision, Note: rc.Note}
		t.ReceiptActor = person(rc.ActorSignature, func(pub []byte) bool {
			return signing.VerifyReceipt(pub, r.ContentHash, facts, rc.ActorSignature.Value) == nil
		})
		if t.Receipt != nil {
			t.ReceiptActor.Decision, t.ReceiptActor.Note, t.ReceiptActor.ReceivedAt, t.ReceiptActor.MassKg = t.Receipt.Decision, t.Receipt.Note, t.Receipt.ReceivedAt, t.Receipt.MassKg
		}
	}
	if r.SealID != "" {
		if s, err := b.St.GetSeal(ctx, r.SealID); err == nil {
			t.Seal = &s
			t.LeafIndex = r.LeafIndex
			if s.Status == "sealed" {
				if leaves, err := b.St.SealLeaves(ctx, r.SealID); err == nil && r.LeafIndex < len(leaves) {
					if _, path, err := proof.InclusionProof(leaves, r.LeafIndex); err == nil {
						t.Path = path
					}
				}
			}
		}
	}
	if vs, err := b.St.Versions(ctx, store.SystemViewer, r.ID); err == nil {
		for _, x := range vs {
			t.Versions = append(t.Versions, VersionView{Version: x.Version, ContentHash: x.ContentHash, PrevHash: x.PrevHash,
				OccurredAt: x.OccurredAt, SealID: x.SealID})
		}
	}
	if !members {
		// The public layer shows the fingerprint and the seal, not the record body
		// or the receiver's note.
		t.HashDoc = nil // an empty document would hash to a different fingerprint
		for _, rc := range []*SigView{t.Receipt, t.ReceiptActor} {
			if rc != nil && rc.Note != "" {
				rc.Note, rc.NoteHidden = "", true
			}
		}
	}
	p.Trust = t
	return nil
}
