package compliance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/store"
)

// Mailer e-mails each organisation's monthly report on its schedule.
type Mailer struct {
	Deps
	Senders   *alerts.Senders // the platform mail server (Mail(ctx))
	PublicURL string
	Now       func() time.Time
}

// Due reports whether a schedule should send now, and for which month. A
// month is sent once; a failed send is retried after an hour.
func Due(x store.Schedule, now time.Time) (string, bool) {
	loc, err := time.LoadLocation(x.Timezone)
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	if local.Day() < x.Day || (local.Day() == x.Day && local.Hour() < x.Hour) {
		return "", false
	}
	period := PreviousMonth(time.Date(local.Year(), local.Month(), 1, 12, 0, 0, 0, time.UTC))
	start, _ := Month(period)
	if x.LastPeriod != nil && !x.LastPeriod.Before(start) {
		return "", false
	}
	if x.LastStatus == "failed" && x.LastRunAt != nil && now.Sub(*x.LastRunAt) < time.Hour {
		return "", false
	}
	return period, true
}

// Tick sends every report that is due.
func (m *Mailer) Tick(ctx context.Context) error {
	if !m.Senders.Mail(ctx).Configured() {
		return nil
	}
	now := time.Now()
	if m.Now != nil {
		now = m.Now()
	}
	all, err := m.St.ReportSchedules(ctx)
	if err != nil {
		return err
	}
	for _, x := range all {
		period, due := Due(x, now)
		if !due {
			continue
		}
		start, _ := Month(period)
		status, msg := "sent", fmt.Sprintf("Sent to %d recipients.", len(x.Recipients))
		if err := m.Send(ctx, x, period); err != nil {
			status, msg = "failed", err.Error()
			slog.Warn("compliance report not sent", "org", x.OrgID, "period", period, "err", err)
		}
		if err := m.St.MarkReportRun(ctx, x.OrgID, start, status, msg); err != nil {
			return err
		}
	}
	return nil
}

// Send renders the month's report as PDF and e-mails it to the schedule's recipients.
func (m *Mailer) Send(ctx context.Context, x store.Schedule, period string) error {
	org, err := m.St.Org(ctx, x.OrgID)
	if err != nil {
		return err
	}
	rep, err := Report(ctx, m.Deps, org, period)
	if err != nil {
		return err
	}
	latest, err := m.St.LatestAttestations(ctx, org.ID)
	if err != nil {
		return err
	}
	state, _ := SignState(rep, latest, period)
	rep.Generated, rep.By = time.Now().UTC(), "Scheduled report"
	rep.Brand = Letterhead(ctx, m.St, org)
	var pdf bytes.Buffer
	if err := rep.Render(&pdf, "pdf"); err != nil {
		return err
	}
	lines := []string{fmt.Sprintf("The compliance report of %s for %s is attached.", org.Name, rep.Period)}
	for _, k := range rep.KPIs {
		lines = append(lines, fmt.Sprintf("%s: %s", k.Label, k.Value))
	}
	lines = append(lines, map[string]string{
		"signed":   "The report is signed by a responsible person.",
		"stale":    "It was signed earlier, but the data changed since: it needs signing again.",
		"unsigned": "It is not signed yet. Sign it under Compliance → Reports.",
	}[state])
	if rep.Notice != "" {
		lines = append(lines, rep.Notice)
	}
	msg := alerts.Message{Subject: fmt.Sprintf("Compliance report · %s · %s", rep.Period, org.Name), Text: strings.Join(lines, "\n"), Severity: "info",
		State: "report", Link: strings.TrimSuffix(m.PublicURL, "/") + "/compliance?tab=reports&period=" + period,
		Attachments: []alerts.Attachment{{Name: rep.FileName("pdf"), Type: "application/pdf", Data: pdf.Bytes()}}}
	var errs []error
	for _, to := range x.Recipients {
		if err := alerts.SendMail(ctx, m.Senders.Mail(ctx), to, msg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", to, err))
		}
	}
	return errors.Join(errs...)
}
