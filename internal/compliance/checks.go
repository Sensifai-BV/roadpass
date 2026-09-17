package compliance

import (
	"fmt"

	"github.com/sensifai/roadpass/internal/store"
)

// Automatic checks, judged from an organisation's own data. They are shared by
// the jurisdictions: the duty differs by law, the evidence that proves it often
// does not (a load reviewed at intake, a lot tested before dispatch).

// share judges a ratio: ≥ 99 % met, ≥ 80 % partial, else gap.
func share(n, of int, what string) (string, string) {
	if of == 0 {
		return Partial, "Nothing to measure in the last 12 months yet."
	}
	p := 100 * float64(n) / float64(of)
	detail := fmt.Sprintf("%s: %d of %d (%.1f %%).", what, n, of, p)
	switch {
	case p >= 99:
		return Met, detail
	case p >= 80:
		return Partial, detail
	}
	return Gap, detail
}

func checkIdentifier(e store.Evidence) (string, string) {
	if e.Passports == 0 {
		return Partial, "No passports issued in the last 12 months yet."
	}
	return Met, fmt.Sprintf("%d passports issued, each with a unique identifier and a public page.", e.Passports)
}

func checkIntegrity(e store.Evidence) (string, string) {
	st, d := share(e.Sealed, e.Signed, "Signed records sealed on the ledger")
	if e.Signed < e.Records {
		return worst(st, Partial), d + fmt.Sprintf(" %d records are not signed yet.", e.Records-e.Signed)
	}
	return st, d
}

func checkTraceability(e store.Evidence) (string, string) {
	st, d := share(e.Linked, e.Linkable, "Passports linked to their inputs")
	if e.Linkable > 0 {
		d += " Sorted reject lots are the origin of the chain and are not counted."
	}
	return st, d
}

func checkOrigin(e store.Evidence) (string, string) {
	return share(e.RecycledLinked, e.Recycled, "Recycled lots and batches linked to their waste input")
}

func checkAccess(e store.Evidence) (string, string) {
	if e.Admins == 0 {
		return Gap, "The organisation has no active administrator."
	}
	return Met, fmt.Sprintf("%d active members with named roles, %d administrators.", e.Members, e.Admins)
}

func checkIntake(e store.Evidence) (string, string) {
	st, d := share(e.Reviewed, e.ShipmentsIn, "Incoming loads reviewed and signed")
	if e.Rejected > 0 {
		d += fmt.Sprintf(" %d rejected.", e.Rejected)
	}
	return st, d
}

func checkTested(e store.Evidence) (string, string) {
	st, d := share(e.LotsTested, e.Lots, "Lots with a lab test")
	if e.LabTests > 0 {
		d += fmt.Sprintf(" Pass rate %.1f %%.", 100*float64(e.LabPassed)/float64(e.LabTests))
	}
	return st, d
}

func checkReleased(e store.Evidence) (string, string) {
	return share(e.LotsReleased, e.Lots, "Lots with a signed release decision")
}

func checkFindings(e store.Evidence) (string, string) {
	switch {
	case e.StaleBlockers > 0:
		return Gap, fmt.Sprintf("%d blocking findings open for more than 30 days.", e.StaleBlockers)
	case e.OpenBlockers > 0:
		return Partial, fmt.Sprintf("%d blocking findings open, none older than 30 days.", e.OpenBlockers)
	}
	return Met, fmt.Sprintf("No open blocking findings (%d warnings open).", e.OpenWarnings)
}

func checkFPC(e store.Evidence) (string, string) {
	st, d := share(e.LotsTested, e.Lots, "Asphalt lots with a lab test")
	if e.Calibrations == 0 {
		return worst(st, Partial), d + " No calibration recorded in the last 12 months."
	}
	return st, d + fmt.Sprintf(" %d calibrations recorded.", e.Calibrations)
}

func checkLimits(e store.Evidence) (string, string) {
	if e.Limits == 0 {
		return Gap, "No limits set. Add the mix's declared limits under Limits."
	}
	return Met, fmt.Sprintf("%d %s checked continuously; see the Limits tab for exceedances.", e.Limits, map[bool]string{true: "limit", false: "limits"}[e.Limits == 1])
}

func checkMasses(e store.Evidence) (string, string) {
	return share(e.Signed, e.Records, "Records with masses signed by the company")
}

func checkDeliveries(e store.Evidence) (string, string) {
	if e.Deliveries == 0 {
		return Partial, "No deliveries in the last 12 months to calculate from."
	}
	return Met, fmt.Sprintf("%d deliveries carry the data for the GRETA-ready carbon calculation.", e.Deliveries)
}

func checkMinimisation(store.Evidence) (string, string) {
	return Met, "By design: signatures name a key holder id, never a person's name or e-mail."
}

func checkRights(e store.Evidence) (string, string) {
	if e.DisabledNotErased > 0 {
		return Partial, fmt.Sprintf("%d former members disabled for over 30 days still hold personal data. Erase them under Data protection.", e.DisabledNotErased)
	}
	return Met, "Export and erasure are available under Data protection; no former member's data is pending."
}

func checkSecurity(e store.Evidence) (string, string) {
	if !e.OrgKeyOnLedger {
		return Partial, "The company's signing key is not registered on the ledger yet."
	}
	return share(e.MembersWithKey, e.Members, "Members with a personal key on the ledger")
}

func checkAuditLog(e store.Evidence) (string, string) {
	return Met, fmt.Sprintf("%d audited changes in the last 12 months.", e.AuditEntries)
}
