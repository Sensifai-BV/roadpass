package domain

// A void is a correction that says a record should not exist (a load that was
// never laid, a lot entered twice). It is a new version like any correction:
// signed, checked and sealed, naming the previous fingerprint, so the history
// stays. The void version carries no mass, and its inputs carry none either, so
// mass balances, passports and KPIs treat it as zero. The flag and the reason live
// in the body, so they are covered by the content hash.
const (
	FieldVoid       = "void"
	FieldVoidReason = "void_reason"
)

// Void reports whether this version voids the record.
func (r *Record) Void() bool {
	v, _ := r.Body[FieldVoid].(bool)
	return v
}

// VoidReason is the signer's reason for a void ("" when not void).
func (r *Record) VoidReason() string { return Str(r.Body, FieldVoidReason) }

// CountsMass reports whether a record's mass counts towards what reached the
// road: not when it is void, nor a delivery the contractor rejected at the paver.
func (r *Record) CountsMass() bool {
	if r.Void() {
		return false
	}
	return r.Type != TypeDelivery || r.Receipt == nil || r.Receipt.Decision != Rejected
}

// validateVoid checks the void fields; it removes them from the body before
// the per-type field check and returns whether the record is a void.
func validateVoid(r *Record, add func(field, msg string, a ...any)) bool {
	v, present := r.Body[FieldVoid]
	reason, hasReason := r.Body[FieldVoidReason]
	if !present && !hasReason {
		return false
	}
	if v != true {
		add(FieldVoid, "must be true (omit it for a record that is not void)")
		return false
	}
	s, _ := reason.(string)
	switch {
	case s == "":
		add(FieldVoidReason, "say why the record is void")
	case len([]rune(s)) > 500:
		add(FieldVoidReason, "at most 500 characters")
	}
	if r.ID == "" {
		add(FieldVoid, "only an existing record can be voided")
	}
	if r.MassKg != 0 {
		add("mass_kg", "a void record carries no mass")
	}
	for i, in := range r.Inputs {
		if in.MassKg != 0 {
			add("inputs", "a void record takes nothing from its inputs (inputs[%d] has %g kg)", i, in.MassKg)
		}
	}
	return true
}
