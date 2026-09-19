package compliance

// Retention is how long one kind of data is kept, and why. It documents what
// the code does (see the prune loops in internal/worker and internal/alerts).
type Retention struct {
	What     string `json:"what"`
	Kept     string `json:"kept"`
	Why      string `json:"why"`
	Personal bool   `json:"personal"`
}

// Policy is the platform's retention schedule.
var Policy = []Retention{
	{"Records, passports and their versions", "For the life of the product; never deleted", "Passports must stay available for the expected life of the road (ESPR art. 10). Corrections add versions; nothing is overwritten.", false},
	{"Ledger seals and registered keys", "Permanent", "The ledger is append-only. It holds hashes and public keys only, never personal data.", false},
	{"Audit log", "For the life of the account", "Shows who changed what (GDPR art. 30, ESPR traceability). Refers to people by pseudonymous id.", true},
	{"Read notifications", "90 days", "Pruned automatically; unread ones after one year.", true},
	{"Alert events", "1 year", "Pruned automatically.", false},
	{"Member accounts", "While active; personal data erased on request after removal", "Removal disables sign-in at once. Erasure replaces name and e-mail and deletes preferences, devices and notifications; the pseudonymous id stays so past signatures still verify.", true},
	{"Sessions", "Up to 12 hours, or until sign-out", "Held in Redis only.", true},
	{"Uploaded spreadsheets", "Not kept", "Rows become records; the file itself is discarded after import.", false},
}
