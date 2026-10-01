package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/compliance"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/report"
	"github.com/sensifai/roadpass/internal/store"
)

// Compliance: limit reporting (limits, exceedances, completeness,
// calibrations), the EU regulatory register with signed statements, the signed
// monthly report and its e-mail schedule, and data protection (GDPR).
//
// Who may do what:
//   - everyone in the organisation reads; oversight bodies read any organisation (?org=)
//   - quality (admins, or members holding the test or release duty) edits limits and calibrations
//   - responsible people (admins, or the release duty) sign statements and reports
//   - admins schedule reports and handle data-protection requests

func (p *Principal) quality() bool {
	return p.can("admin") || (p.can("editor") && (domain.HasDuty(p.Duties(), domain.DutyTest) || domain.HasDuty(p.Duties(), domain.DutyRelease)))
}

func (p *Principal) attests() bool {
	return p.User != nil && (p.can("admin") || (p.can("editor") && domain.HasDuty(p.Duties(), domain.DutyRelease)))
}

// complianceOrg is the organisation a compliance read is about: the caller's,
// or for oversight bodies the one named by ?org=.
func (s *Server) complianceOrg(r *http.Request) (domain.Org, error) {
	p := principal(r.Context())
	if id := r.URL.Query().Get("org"); id != "" && id != p.Org.ID {
		if !p.Viewer.Oversight {
			return domain.Org{}, store.ErrNotFound
		}
		return s.St.Org(r.Context(), id)
	}
	return p.Org, nil
}

// ownSite reports whether id is empty or one of the organisation's sites.
func (s *Server) ownSite(ctx context.Context, orgID, id string) (bool, error) {
	if id == "" {
		return true, nil
	}
	sites, err := s.St.Sites(ctx, orgID)
	return slices.ContainsFunc(sites, func(x domain.Site) bool { return x.ID == id }), err
}

// ---------------------------------------------------------------- register

func (s *Server) register(ctx context.Context, org domain.Org) (compliance.Register, error) {
	ev, err := s.St.ComplianceEvidence(ctx, org.ID, time.Now().AddDate(-1, 0, 0))
	if err != nil {
		return compliance.Register{}, err
	}
	latest, err := s.St.LatestAttestations(ctx, org.ID)
	if err != nil {
		return compliance.Register{}, err
	}
	return compliance.ForOrg(org.Country, org.Region, org.Kind, ev, latest), nil
}

func (s *Server) complianceRegister(w http.ResponseWriter, r *http.Request) {
	org, err := s.complianceOrg(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	reg, err := s.register(r.Context(), org)
	if err != nil {
		fail(w, r, err)
		return
	}
	p := principal(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"register": reg, "regulations": reg.Regulations, "available": compliance.Available(), "org": map[string]any{"id": org.ID, "name": org.Name, "kind": org.Kind},
		"canAttest": org.ID == p.Org.ID && p.attests(), "attestStatuses": compliance.AttestStatuses})
}

func (s *Server) attestationHistory(w http.ResponseWriter, r *http.Request) {
	org, err := s.complianceOrg(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	h, err := s.St.AttestationHistory(r.Context(), org.ID, r.PathValue("id"))
	answer(w, r, h, err)
}

// sign signs msg with the caller's personal key.
func (s *Server) sign(ctx context.Context, p *Principal, msg []byte) (keyID string, sig []byte, err error) {
	signer, err := s.Keyring.UserSigner(ctx, p.Org.ID, p.User.ID)
	if err != nil {
		return "", nil, err
	}
	sig, err = signer.SignMessage(ctx, msg)
	return signer.KeyID(), sig, err
}

func (s *Server) attest(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if !p.attests() {
		writeProblem(w, r, http.StatusForbidden, "Not allowed", "Statements are signed by administrators or members responsible for quality release.")
		return
	}
	var in struct {
		RequirementID string `json:"requirementId"`
		Status        string `json:"status"`
		Note          string `json:"note"`
		EvidenceURL   string `json:"evidenceUrl"`
	}
	if !decode(w, r, &in, 8192) {
		return
	}
	var errs []fieldErr
	req, ok := compliance.Find(p.Org.Country, p.Org.Region, in.RequirementID)
	if !ok || !req.Applies(p.Org.Kind) {
		errs = append(errs, fieldErr{"requirementId", "Pick a requirement from your register."})
	}
	if !slices.Contains(compliance.AttestStatuses, in.Status) {
		errs = append(errs, fieldErr{"status", "Choose met, partly met, not met or not applicable."})
	} else if ok && !req.Attest && in.Status != "not_applicable" {
		errs = append(errs, fieldErr{"status", "This requirement is checked from your data; a statement can only mark it as not applicable."})
	}
	in.Note = strings.TrimSpace(in.Note)
	if l := len([]rune(in.Note)); l < 10 || l > 2000 {
		errs = append(errs, fieldErr{"note", "Explain the statement in 10 to 2000 characters: what, where it is documented, until when."})
	}
	if in.EvidenceURL = strings.TrimSpace(in.EvidenceURL); in.EvidenceURL != "" {
		if u, err := url.Parse(in.EvidenceURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(in.EvidenceURL) > 500 {
			errs = append(errs, fieldErr{"evidenceUrl", "Use a web link (https://…) to the document."})
		}
	}
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Statement not signed", "Fix the highlighted fields.", errs)
		return
	}
	a := store.Attestation{ID: auth.ID("att_"), OrgID: p.Org.ID, RequirementID: in.RequirementID, Status: in.Status, Note: in.Note,
		EvidenceURL: in.EvidenceURL, By: p.User.ID, At: time.Now().UTC().Truncate(time.Second)}
	a.Message = compliance.AttestationMessage(a)
	var err error
	if a.KeyID, a.Signature, err = s.sign(r.Context(), p, []byte(a.Message)); err != nil {
		fail(w, r, err)
		return
	}
	if err := s.St.AddAttestation(r.Context(), a); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "compliance.attest", in.RequirementID, map[string]string{"status": in.Status, "key": a.KeyID})
	writeJSON(w, http.StatusCreated, a)
}

// ---------------------------------------------------------------- limits

func (s *Server) complianceLimits(w http.ResponseWriter, r *http.Request) {
	org, err := s.complianceOrg(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	l, err := s.St.Limits(r.Context(), org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"limits": l, "canEdit": org.ID == principal(r.Context()).Org.ID && principal(r.Context()).quality()})
}

func (s *Server) saveLimit(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if !p.quality() {
		writeProblem(w, r, http.StatusForbidden, "Not allowed", "Limits are set by administrators and quality staff.")
		return
	}
	var in store.Limit
	if !decode(w, r, &in, 8192) {
		return
	}
	in.ID = r.PathValue("id")
	var errs []fieldErr
	add := func(f, m string) { errs = append(errs, fieldErr{f, m}) }
	in.Name = strings.TrimSpace(in.Name)
	if l := len([]rune(in.Name)); l < 1 || l > 80 {
		add("name", "Name the limit (up to 80 characters).")
	}
	if in.Types == nil {
		in.Types = []string{}
	}
	if err := compliance.Spec(in).Validate(); err != nil {
		add("measure", strings.TrimPrefix(err.Error(), analytics.ErrQuery.Error()+": "))
	}
	if in.Min == nil && in.Max == nil {
		add("max", "Set a lower bound, an upper bound, or both.")
	}
	for _, v := range []*float64{in.Min, in.Max} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0)) {
			add("max", "Bounds must be numbers.")
		}
	}
	if in.Min != nil && in.Max != nil && *in.Min >= *in.Max {
		add("min", "The lower bound must be below the upper bound.")
	}
	if ok, err := s.ownSite(r.Context(), p.Org.ID, in.SiteID); err != nil {
		fail(w, r, err)
		return
	} else if !ok {
		add("siteId", "Pick one of your sites.")
	}
	if len([]rune(in.Note)) > 500 {
		add("note", "Keep the note under 500 characters.")
	}
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Limit not saved", "Fix the highlighted fields.", errs)
		return
	}
	if in.ID == "" {
		n, err := s.St.CountLimits(r.Context(), p.Org.ID)
		if err != nil {
			fail(w, r, err)
			return
		}
		if n >= compliance.MaxLimits {
			writeProblem(w, r, http.StatusConflict, "Too many limits", fmt.Sprintf("An organisation can set up to %d limits.", compliance.MaxLimits))
			return
		}
		in.ID, in.Enabled = auth.ID("lim_"), true
	}
	in.OrgID, in.CreatedBy = p.Org.ID, p.Actor()
	l, err := s.St.SaveLimit(r.Context(), in)
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "compliance.limit", l.ID, map[string]any{"name": l.Name, "min": l.Min, "max": l.Max, "enabled": l.Enabled})
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) deleteLimit(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if !p.quality() {
		writeProblem(w, r, http.StatusForbidden, "Not allowed", "")
		return
	}
	if err := s.St.DeleteLimit(r.Context(), p.Org.ID, r.PathValue("id")); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "compliance.limit.delete", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

// limitsStatus runs every limit over the window, with completeness and calibrations.
func (s *Server) limitsStatus(w http.ResponseWriter, r *http.Request) {
	org, err := s.complianceOrg(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	rg := window(r)
	limits, err := s.St.Limits(r.Context(), org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	cov, err := s.CH.Completeness(r.Context(), analytics.Scope{OrgID: org.ID}, rg)
	if err != nil {
		fail(w, r, err)
		return
	}
	cals, err := s.St.Calibrations(r.Context(), org.ID, rg.From, rg.To)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": rg.From, "to": rg.To, "limits": compliance.CheckLimits(r.Context(), s.CH, org.ID, limits, rg),
		"completeness": nonNil(cov), "calibrations": cals})
}

func (s *Server) addCalibration(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if !p.quality() {
		writeProblem(w, r, http.StatusForbidden, "Not allowed", "Calibrations are recorded by administrators and quality staff.")
		return
	}
	var in store.Calibration
	if !decode(w, r, &in, 4096) {
		return
	}
	var errs []fieldErr
	in.Parameter, in.By = strings.TrimSpace(in.Parameter), strings.TrimSpace(in.By)
	if l := len([]rune(in.Parameter)); l < 1 || l > 80 {
		errs = append(errs, fieldErr{"parameter", "Name the instrument or parameter."})
	}
	if !slices.Contains([]string{"calibration", "check", "maintenance", "replacement"}, in.Kind) {
		errs = append(errs, fieldErr{"kind", "Choose calibration, check, maintenance or replacement."})
	}
	if in.At.IsZero() || in.At.After(time.Now().Add(time.Hour)) {
		errs = append(errs, fieldErr{"at", "When was it done? Not in the future."})
	}
	if l := len([]rune(in.By)); l < 1 || l > 120 {
		errs = append(errs, fieldErr{"by", "Who did it (person or company)?"})
	}
	if len([]rune(in.Note)) > 1000 {
		errs = append(errs, fieldErr{"note", "Keep the note under 1000 characters."})
	}
	if ok, err := s.ownSite(r.Context(), p.Org.ID, in.SiteID); err != nil {
		fail(w, r, err)
		return
	} else if !ok {
		errs = append(errs, fieldErr{"siteId", "Pick one of your sites."})
	}
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Not recorded", "Fix the highlighted fields.", errs)
		return
	}
	in.ID, in.OrgID, in.CreatedBy = auth.ID("cal_"), p.Org.ID, p.Actor()
	if err := s.St.AddCalibration(r.Context(), in); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "compliance.calibration", in.ID, map[string]string{"parameter": in.Parameter, "kind": in.Kind})
	writeJSON(w, http.StatusCreated, in)
}

func (s *Server) deleteCalibration(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if !p.quality() {
		writeProblem(w, r, http.StatusForbidden, "Not allowed", "")
		return
	}
	if err := s.St.DeleteCalibration(r.Context(), p.Org.ID, r.PathValue("id")); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "compliance.calibration.delete", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- monthly report

func periodParam(r *http.Request) (string, error) {
	period := r.URL.Query().Get("period")
	if period == "" {
		return compliance.PreviousMonth(time.Now()), nil
	}
	m, err := compliance.Month(period)
	if err != nil {
		return "", err
	}
	if !m.Before(time.Now()) {
		return "", errors.New("the month has not started yet")
	}
	return period, nil
}

func (s *Server) complianceDeps() compliance.Deps { return compliance.Deps{St: s.St, CH: s.CH} }

// reportState says whether a month's report is signed over its current figures.
func (s *Server) reportState(w http.ResponseWriter, r *http.Request) {
	org, err := s.complianceOrg(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	period, err := periodParam(r)
	if err != nil {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Invalid month", err.Error())
		return
	}
	rep, err := compliance.Report(r.Context(), s.complianceDeps(), org, period)
	if err != nil {
		fail(w, r, err)
		return
	}
	latest, err := s.St.LatestAttestations(r.Context(), org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	state, a := compliance.SignState(rep, latest, period)
	p := principal(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"period": period, "state": state, "attestation": a, "fingerprint": rep.Fingerprint(),
		"kpis": rep.KPIs, "notice": rep.Notice, "canSign": org.ID == p.Org.ID && p.attests()})
}

func (s *Server) signReport(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if !p.attests() {
		writeProblem(w, r, http.StatusForbidden, "Not allowed", "Reports are signed by administrators or members responsible for quality release.")
		return
	}
	var in struct {
		Period      string `json:"period"`
		Note        string `json:"note"`
		Fingerprint string `json:"fingerprint"`
	}
	if !decode(w, r, &in, 4096) {
		return
	}
	m, err := compliance.Month(in.Period)
	if err != nil || !m.AddDate(0, 1, 0).Before(time.Now().Add(time.Hour)) {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Month not signable", "Only a complete month can be signed.")
		return
	}
	rep, err := compliance.Report(r.Context(), s.complianceDeps(), p.Org, in.Period)
	if err != nil {
		fail(w, r, err)
		return
	}
	// The signer saw these figures; if they changed meanwhile, show them again.
	fp := rep.Fingerprint()
	if in.Fingerprint != fp {
		writeProblem(w, r, http.StatusConflict, "The figures changed", "New data arrived for this month since you opened it. Review the report again before signing.")
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if len([]rune(in.Note)) > 1000 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Note too long", "Keep it under 1000 characters.")
		return
	}
	a := store.Attestation{ID: auth.ID("att_"), OrgID: p.Org.ID, RequirementID: compliance.ReportRequirement, Period: in.Period, Status: "met",
		Note: in.Note, By: p.User.ID, At: time.Now().UTC().Truncate(time.Second), Message: string(report.SigningMessage(fp))}
	if a.KeyID, a.Signature, err = s.sign(r.Context(), p, []byte(a.Message)); err != nil {
		fail(w, r, err)
		return
	}
	if err := s.St.AddAttestation(r.Context(), a); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "compliance.report.sign", in.Period, map[string]string{"fingerprint": fp, "key": a.KeyID})
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) reportCompliance(r *http.Request) (*report.Report, error) {
	org, err := s.complianceOrg(r)
	if err != nil {
		return nil, err
	}
	period, err := periodParam(r)
	if err != nil {
		return nil, err
	}
	return compliance.Report(r.Context(), s.complianceDeps(), org, period)
}

func (s *Server) reportRegister(r *http.Request) (*report.Report, error) {
	org, err := s.complianceOrg(r)
	if err != nil {
		return nil, err
	}
	reg, err := s.register(r.Context(), org)
	if err != nil {
		return nil, err
	}
	return compliance.RegisterReport(org, reg, time.Now()), nil
}

// ---------------------------------------------------------------- schedule

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	x, err := s.St.ReportSchedule(r.Context(), principal(r.Context()).Org.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"schedule": nil, "mail": s.Notify != nil && s.Notify.Senders != nil && s.Notify.Senders.Mail(r.Context()).Configured()})
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedule": x, "mail": s.Notify != nil && s.Notify.Senders != nil && s.Notify.Senders.Mail(r.Context()).Configured()})
}

func (s *Server) saveSchedule(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in store.Schedule
	if !decode(w, r, &in, 4096) {
		return
	}
	var errs []fieldErr
	if len(in.Recipients) == 0 || len(in.Recipients) > 10 {
		errs = append(errs, fieldErr{"recipients", "Add between 1 and 10 e-mail addresses."})
	}
	for i, to := range in.Recipients {
		a, err := mail.ParseAddress(strings.TrimSpace(to))
		if err != nil || strings.ContainsAny(to, "\r\n") {
			errs = append(errs, fieldErr{"recipients", fmt.Sprintf("“%s” is not an e-mail address.", to)})
			continue
		}
		in.Recipients[i] = a.Address
	}
	if in.Day < 1 || in.Day > 28 {
		errs = append(errs, fieldErr{"day", "Pick a day between the 1st and the 28th."})
	}
	if in.Hour < 0 || in.Hour > 23 {
		errs = append(errs, fieldErr{"hour", "Pick an hour between 0 and 23."})
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		errs = append(errs, fieldErr{"timezone", "Unknown time zone."})
	}
	if errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Schedule not saved", "Fix the highlighted fields.", errs)
		return
	}
	in.ID, in.OrgID, in.CreatedBy = auth.ID("sch_"), p.Org.ID, p.Actor()
	x, err := s.St.SaveReportSchedule(r.Context(), in)
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "compliance.schedule", x.ID, map[string]any{"recipients": len(x.Recipients), "day": x.Day, "enabled": x.Enabled})
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if err := s.St.DeleteReportSchedule(r.Context(), p.Org.ID); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "compliance.schedule.delete", "", nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- data protection

// privacyTarget is the member a data-protection request concerns: one of the
// caller's organisation, or for the platform manager anyone's.
func (s *Server) privacyTarget(r *http.Request) (store.User, error) {
	p := principal(r.Context())
	u, err := s.St.UserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		return u, err
	}
	if u.OrgID != p.Org.ID && !p.Operator() {
		return u, store.ErrNotFound
	}
	return u, nil
}

func (s *Server) privacyOverview(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	orgID := p.Org.ID
	if p.Operator() && r.URL.Query().Get("all") == "1" {
		orgID = ""
	}
	former, err := s.St.FormerMembers(r.Context(), orgID)
	if err != nil {
		fail(w, r, err)
		return
	}
	reqs, err := s.St.PrivacyRequests(r.Context(), p.Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"former": former, "requests": reqs, "retention": compliance.Policy, "sessionHours": s.Cfg.SessionTTL.Hours()})
}

func (s *Server) exportPerson(w http.ResponseWriter, r *http.Request, u store.User) {
	p := principal(r.Context())
	d, err := s.St.ExportPersonalData(r.Context(), u.OrgID, u.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.LogPrivacyRequest(r.Context(), store.PrivacyRequest{ID: auth.ID("prv_"), OrgID: u.OrgID, SubjectUser: u.ID, Kind: "export", RequestedBy: p.Actor()}, nil)
	_ = s.St.Audit(r.Context(), u.OrgID, p.Actor(), "privacy.export", u.ID, nil)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="roadpass-personal-data-%s.json"`, u.ID))
	w.Header().Set("Cache-Control", "private, no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{"exportedAt": time.Now().UTC(), "controller": u.OrgID, "data": d})
}

// exportMyData is a member's own export (GDPR art. 15 and 20).
func (s *Server) exportMyData(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if p.User == nil {
		writeProblem(w, r, http.StatusForbidden, "Members only", "API keys hold no personal data.")
		return
	}
	s.exportPerson(w, r, *p.User)
}

func (s *Server) exportMember(w http.ResponseWriter, r *http.Request) {
	u, err := s.privacyTarget(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	s.exportPerson(w, r, u)
}

func (s *Server) eraseMember(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	u, err := s.privacyTarget(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if u.DisabledAt == nil {
		writeProblem(w, r, http.StatusConflict, "Remove the member first", "Personal data can be erased only after the member has been removed from the team.")
		return
	}
	if err := s.St.ErasePerson(r.Context(), u.OrgID, u.ID); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.LogPrivacyRequest(r.Context(), store.PrivacyRequest{ID: auth.ID("prv_"), OrgID: u.OrgID, SubjectUser: u.ID, Kind: "erase", RequestedBy: p.Actor()}, nil)
	_ = s.St.Audit(r.Context(), u.OrgID, p.Actor(), "privacy.erase", u.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}
