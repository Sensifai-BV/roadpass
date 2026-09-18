package compliance

import (
	"context"
	"strings"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/report"
	"github.com/sensifai/roadpass/internal/store"
)

// Letterhead is an organisation's brand for its reports: the details its
// admins entered and its logo (nil when it has set nothing beyond its name).
func Letterhead(ctx context.Context, st *store.Store, o domain.Org) *report.Brand {
	b := &report.Brand{Name: o.Name, Address: o.Address, Footer: o.ReportFooter}
	if o.LegalName != "" && o.LegalName != o.Name {
		b.Legal = o.LegalName
	}
	if o.TaxID != "" {
		b.Legal = strings.Join(nonEmptyStr(b.Legal, "VAT/reg. "+o.TaxID), " · ")
	}
	b.Contact = strings.Join(nonEmptyStr(o.ContactEmail, o.Phone, o.Website), " · ")
	if o.HasLogo {
		if ct, data, _, err := st.OrgLogo(ctx, o.ID); err == nil {
			b.Logo, b.LogoType = data, ct
		}
	}
	if b.Legal == "" && b.Address == "" && b.Contact == "" && b.Footer == "" && b.Logo == nil {
		return nil
	}
	return b
}

func nonEmptyStr(xs ...string) []string {
	out := xs[:0:0]
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}
