import { Fragment, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  BadgeCheck, CircleAlert, CircleCheck, CircleDashed, CircleMinus, ExternalLink, FileSignature, Gauge, History, Mail, Pencil, Plus,
  ShieldCheck, Trash2, UserX, Wrench, Download as DownloadIcon, OctagonX, ChevronDown, ChevronRight,
} from "lucide-react";
import clsx from "clsx";
import { api, ApiError, qs } from "../lib/api";
import { useDirectory, useMe, useRange } from "../lib/hooks";
import { date, dateTime, num, timeZone } from "../lib/format";
import { fmtUnit, useCatalog } from "../lib/dashboards";
import { verifyEd25519 } from "../lib/verify";
import { TYPE } from "../lib/vocab";
import type { RecordType, User } from "../lib/types";
import { Card, Empty, ErrorBox, InfoTip, Modal, PageHeader, Skel } from "../components/ui";
import { HELP } from "../lib/help";
import { Download } from "../components/Download";

// ---------------------------------------------------------------- types

type Status = "met" | "partial" | "gap" | "open" | "not_applicable";
interface Attestation {
  id: string; requirementId: string; period?: string; status: string; note: string; evidenceUrl?: string; by: string; byName?: string;
  at: string; keyId?: string; signature?: string; publicKey?: string; message: string;
}
interface Item {
  id: string; regulation: string; ref: string; title: string; explain: string; attest: boolean;
  status: Status; auto?: Status; detail: string; attestation?: Attestation;
}
interface Regulation { Key: string; Name: string; Act: string; URL: string }
interface JurisdictionRef { code: string; name: string; scope: string }
interface RegisterView {
  register: { jurisdiction: string; jurisdictions: JurisdictionRef[]; items: Item[]; score: number; counts: Record<string, number>; simulated: boolean };
  regulations: Regulation[]; available: JurisdictionRef[]; org: { id: string; name: string; kind: string }; canAttest: boolean;
}
interface Limit {
  id: string; name: string; source: string; measure: string; types: string[]; siteId?: string; basis: "instant" | "daily_mean" | "monthly_mean";
  min?: number; max?: number; note: string; enabled: boolean; updatedAt?: string;
}
interface Exceedance { at: string; ref: string; site?: string; value: number; kind: "above" | "below"; excess: number }
interface LimitStatus { limit: Limit; label: string; unit: string; stat: { min: number; avg: number; max: number; count: number }; exceedances: Exceedance[]; truncated: boolean; error?: string }
interface Coverage { site: string; source: string; samples: number; gaps: number; gapMinutes: number; stops: number; percent: number }
interface Calibration { id: string; siteId?: string; parameter: string; at: string; kind: string; by: string; note: string }
interface StatusView { limits: LimitStatus[]; completeness: Coverage[]; calibrations: Calibration[] }

const STATUS: Record<Status, { label: string; chip: string; icon: typeof CircleCheck }> = {
  met: { label: "Met", chip: "chip-good", icon: CircleCheck },
  partial: { label: "Partly met", chip: "chip-warning", icon: CircleAlert },
  gap: { label: "Gap", chip: "chip-critical", icon: OctagonX },
  open: { label: "Statement needed", chip: "", icon: CircleDashed },
  not_applicable: { label: "Not applicable", chip: "", icon: CircleMinus },
};
const BASIS: Record<Limit["basis"], string> = { instant: "Every value", daily_mean: "Daily mean", monthly_mean: "Monthly mean" };

const fieldErr = (e: unknown, f: string) => e instanceof ApiError && Array.isArray(e.problem.errors)
  ? (e.problem.errors as { field: string; message: string }[]).filter((x) => x.field === f).map((x) => x.message).join(" ") : "";
const FieldError = ({ e, f }: { e: unknown; f: string }) => { const m = fieldErr(e, f); return m ? <p className="mt-1 text-[12px] text-critical">{m}</p> : null; };

// ---------------------------------------------------------------- page

type Tab = "limits" | "register" | "reports" | "privacy";

/**
 * Compliance: limit reporting (limits, exceedances, completeness,
 * calibrations), the regulatory register of the organisation's jurisdiction with signed statements, the signed
 * monthly report with its e-mail schedule, and data protection.
 */
export default function Compliance() {
  const me = useMe().data!;
  const admin = me.user.role === "admin";
  const [params, setParams] = useSearchParams();
  const dir = useDirectory();
  const tabs: { key: Tab; label: string; icon: typeof Gauge }[] = [
    { key: "limits", label: "Limits", icon: Gauge },
    { key: "register", label: "Regulatory register", icon: ShieldCheck },
    { key: "reports", label: "Reports & sign-off", icon: FileSignature },
    ...(admin ? [{ key: "privacy" as Tab, label: "Data protection", icon: UserX }] : []),
  ];
  const tab = tabs.find((t) => t.key === params.get("tab"))?.key ?? "limits";
  const set = (k: string, v: string | null) => setParams((p) => { const n = new URLSearchParams(p); if (v) n.set(k, v); else n.delete(k); return n; }, { replace: true });
  // Oversight bodies (reporters, auditors, the operator) look at one company at a time.
  const chainOrgs = useMemo(() => dir.orgs.filter((o) => !["operator", "reporter", "auditor"].includes(o.kind)).sort((a, b) => a.name.localeCompare(b.name)), [dir.orgs]);
  const org = me.oversight ? (params.get("org") ?? chainOrgs[0]?.id ?? "") : "";
  const orgQ: Record<string, string> = org && org !== me.org.id ? { org } : {};

  return (
    <div>
      <PageHeader title="Compliance" sub="Limits on your process data, the rules that apply where you operate with the evidence behind each, and the monthly report a responsible person signs."
        actions={<>
          {me.oversight && (
            <select className="input h-9 w-auto max-w-[260px]" value={org} onChange={(e) => set("org", e.target.value)} aria-label="Organisation">
              {chainOrgs.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
            </select>
          )}
          {tab === "register" && <Download kind="register" range={false} params={orgQ} label="Download register" />}
          {tab === "limits" && <Download kind="compliance" range={false} params={{ ...orgQ, period: params.get("period") ?? undefined }} label="Download report" />}
        </>} />
      <div className="mb-5 flex gap-1 overflow-x-auto border-b border-line" role="tablist" aria-label="Compliance sections">
        {tabs.map((t) => (
          <button key={t.key} role="tab" aria-selected={tab === t.key} onClick={() => set("tab", t.key)}
            className={clsx("-mb-px flex items-center gap-1.5 whitespace-nowrap border-b-2 px-3 pb-2.5 pt-1 text-[13.5px] font-medium transition",
              tab === t.key ? "border-accent text-text" : "border-transparent text-text-2 hover:text-text")}>
            <t.icon size={15} aria-hidden />{t.label}
          </button>
        ))}
      </div>
      {me.oversight && !org ? <Skel h={300} /> : (
        <>
          {tab === "limits" && <LimitsTab orgQ={orgQ} />}
          {tab === "register" && <RegisterTab orgQ={orgQ} />}
          {tab === "reports" && <ReportsTab orgQ={orgQ} />}
          {tab === "privacy" && admin && <DataProtection />}
        </>
      )}
    </div>
  );
}

// ---------------------------------------------------------------- limits

function LimitsTab({ orgQ }: { orgQ: Record<string, string> }) {
  const r = useRange();
  const dir = useDirectory();
  const qc = useQueryClient();
  const limits = useQuery({ queryKey: ["compliance-limits", orgQ], queryFn: () => api.get<{ limits: Limit[]; canEdit: boolean }>(`/compliance/limits${qs(orgQ)}`) });
  const status = useQuery({ queryKey: ["compliance-status", orgQ, r.from, r.to], queryFn: () => api.get<StatusView>(`/compliance/status${qs({ ...orgQ, from: r.from, to: r.to })}`) });
  const [edit, setEdit] = useState<Partial<Limit> | null>(null);
  const [cal, setCal] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const canEdit = !!limits.data?.canEdit;
  const refresh = () => { void qc.invalidateQueries({ queryKey: ["compliance-limits"] }); void qc.invalidateQueries({ queryKey: ["compliance-status"] }); };
  const del = useMutation({ mutationFn: (id: string) => api.send("DELETE", `/compliance/limits/${id}`), onSuccess: refresh });
  const delCal = useMutation({ mutationFn: (id: string) => api.send("DELETE", `/compliance/calibrations/${id}`), onSuccess: refresh });
  const st = status.data;
  const exceed = (st?.limits ?? []).reduce((s, l) => s + l.exceedances.length, 0);
  const cov = st?.completeness ?? [];
  const covAvg = cov.length ? cov.reduce((s, c) => s + c.percent, 0) / cov.length : null;
  const disabled = (limits.data?.limits ?? []).filter((l) => !l.enabled);

  return (
    <div className="flex flex-col gap-4">
      <div className="card grid grid-cols-2 divide-line sm:grid-cols-4 sm:divide-x">
        {[
          { label: "Limits checked", help: HELP.limit, value: num(st?.limits.length ?? 0), hint: disabled.length ? `${disabled.length} switched off` : "in this period" },
          { label: "Exceedances", help: HELP.exceedance, value: num(exceed), hint: exceed ? "values outside a limit" : "none in this period", tone: exceed ? "text-critical" : "" },
          { label: "Plant data complete", help: HELP.completeness, value: covAvg === null ? "—" : `${num(covAvg, 1)} %`, hint: "operating time without gaps" },
          { label: "Calibrations", help: HELP.calibration, value: num(st?.calibrations.length ?? 0), hint: "recorded in this period" },
        ].map((m) => (
          <div key={m.label} className="px-4 py-3">
            <div className="flex items-center gap-1.5 text-[12px] font-medium text-text-2">{m.label}<InfoTip text={m.help} /></div>
            {status.isLoading ? <Skel h={24} w={60} className="mt-1" /> : <div className={clsx("num mt-0.5 text-[22px] font-semibold tracking-tight", m.tone)}>{m.value}</div>}
            <div className="text-[11.5px] text-muted">{m.hint}</div>
          </div>
        ))}
      </div>

      <Card title="Limits" sub="Each limit is checked on every value, or on the daily or monthly mean, over the period picked at the top of the page." pad={false}
        action={canEdit && <button className="btn btn-primary btn-sm" onClick={() => setEdit({ basis: "instant", source: "records", types: [], enabled: true })}><Plus size={14} /> Add limit</button>}>
        {status.error ? <div className="p-4"><ErrorBox error={status.error} /></div> : status.isLoading ? <div className="p-4"><Skel h={140} /></div>
          : !st?.limits.length ? (
            <Empty icon={<Gauge size={20} />} title="No limits yet">{canEdit ? "Add the limits your permit or mix design sets, for example additive dosage or mixing temperature." : "Quality staff set the limits."}</Empty>
          ) : (
            <div className="overflow-x-auto">
              <table className="table">
                <thead><tr><th className="w-6" /><th>Limit</th><th>Checked on <InfoTip text={HELP.basis} /></th><th className="text-right">Allowed</th><th className="text-right">Lowest · average · highest</th><th className="text-right">Values</th><th>Result</th>{canEdit && <th />}</tr></thead>
                <tbody>
                  {st.limits.map((s) => {
                    const f = (v: number) => fmtUnit(v, s.unit, 2);
                    const isOpen = open === s.limit.id;
                    const allowed = s.limit.min !== undefined && s.limit.max !== undefined ? `${f(s.limit.min)} – ${f(s.limit.max)}` : s.limit.max !== undefined ? `≤ ${f(s.limit.max)}` : `≥ ${f(s.limit.min!)}`;
                    return (
                      <Fragment key={s.limit.id}>
                        <tr>
                          <td>{s.exceedances.length > 0 && <button className="text-muted hover:text-text" aria-expanded={isOpen} aria-label={`${isOpen ? "Hide" : "Show"} exceedances of ${s.limit.name}`} onClick={() => setOpen(isOpen ? null : s.limit.id)}>{isOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}</button>}</td>
                          <td><div className="font-medium">{s.limit.name}</div><div className="text-[12px] text-muted">{s.label}{s.limit.types.length ? ` · ${s.limit.types.map((t) => TYPE[t as RecordType]?.plural ?? t).join(", ")}` : ""}{s.limit.siteId ? ` · ${dir.site(s.limit.siteId)?.name ?? s.limit.siteId}` : ""}</div></td>
                          <td className="text-text-2">{BASIS[s.limit.basis]}</td>
                          <td className="num whitespace-nowrap text-right">{allowed}</td>
                          <td className="num whitespace-nowrap text-right text-text-2">{s.stat.count ? `${f(s.stat.min)} · ${f(s.stat.avg)} · ${f(s.stat.max)}` : "—"}</td>
                          <td className="num text-right">{num(s.stat.count)}</td>
                          <td>{s.error ? <span className="chip chip-warning" title={s.error}>Cannot check</span>
                            : s.exceedances.length ? <span className="chip chip-critical"><OctagonX size={11} />{num(s.exceedances.length)}{s.truncated ? "+" : ""} outside</span>
                            : s.stat.count ? <span className="chip chip-good"><CircleCheck size={11} />Within</span> : <span className="text-[12px] text-muted">No data</span>}</td>
                          {canEdit && <td className="whitespace-nowrap text-right">
                            <button className="btn btn-ghost btn-icon h-7" aria-label={`Edit ${s.limit.name}`} onClick={() => setEdit(s.limit)}><Pencil size={13} /></button>
                            <button className="btn btn-ghost btn-icon h-7 text-muted hover:text-critical" aria-label={`Delete ${s.limit.name}`} onClick={() => { if (confirm(`Delete the limit “${s.limit.name}”?`)) del.mutate(s.limit.id); }}><Trash2 size={13} /></button>
                          </td>}
                        </tr>
                        {isOpen && (
                          <tr><td colSpan={canEdit ? 8 : 7} className="bg-surface-2 p-0">
                            <div className="max-h-[280px] overflow-auto scroll-thin">
                              <table className="table text-[12.5px]">
                                <thead><tr><th>When</th><th>Reference</th><th>Site</th><th className="text-right">Value</th><th className="text-right">Outside by</th></tr></thead>
                                <tbody>{s.exceedances.map((e, i) => (
                                  <tr key={i}><td className="whitespace-nowrap">{s.limit.basis === "instant" ? dateTime(e.at) : e.ref}</td><td className="mono">{s.limit.basis === "instant" ? e.ref : "mean"}</td>
                                    <td className="text-text-2">{e.site ? dir.site(e.site)?.name ?? e.site : "—"}</td><td className="num text-right">{f(e.value)}</td>
                                    <td className={clsx("num text-right", e.kind === "above" ? "text-critical" : "text-warning")}>{e.kind === "above" ? "+" : "−"}{f(e.excess)}</td></tr>
                                ))}</tbody>
                              </table>
                              {s.truncated && <p className="px-4 py-2 text-[12px] text-muted">Showing the first 500. Download the report for the full list.</p>}
                            </div>
                          </td></tr>
                        )}
                      </Fragment>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        {disabled.length > 0 && <p className="border-t border-line px-5 py-2.5 text-[12px] text-muted">Switched off: {disabled.map((l) => <button key={l.id} disabled={!canEdit} className="mr-2 underline decoration-dotted" onClick={() => setEdit(l)}>{l.name}</button>)}</p>}
      </Card>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card title="Plant data completeness" sub="Gaps are silences longer than three usual intervals. Silences over six hours count as stops, not missing data." pad={false}>
          {status.isLoading ? <div className="p-4"><Skel h={100} /></div> : !cov.length ? <Empty title="No plant data">No extruder or mixer data in this period.</Empty> : (
            <table className="table">
              <thead><tr><th>Site</th><th className="text-right">Samples</th><th className="text-right">Gaps</th><th className="text-right">Stops</th><th className="w-[30%]">Complete</th></tr></thead>
              <tbody>{cov.map((c) => (
                <tr key={c.site + c.source}>
                  <td><div className="font-medium">{dir.site(c.site)?.name ?? c.site}</div><div className="text-[12px] text-muted">{c.source === "extruder_readings" ? "Extruder" : "Mixer"}</div></td>
                  <td className="num text-right">{num(c.samples)}</td>
                  <td className="num text-right">{num(c.gaps)}<div className="text-[11.5px] text-muted">{num(c.gapMinutes)} min</div></td>
                  <td className="num text-right">{num(c.stops)}</td>
                  <td><div className="flex items-center gap-2"><div className="h-[6px] flex-1 overflow-hidden rounded-full bg-surface-3"><div className="h-full rounded-full" style={{ width: `${c.percent}%`, background: c.percent >= 99 ? "var(--good)" : c.percent >= 95 ? "var(--warning)" : "var(--critical)" }} /></div><span className="num w-12 text-right text-[12.5px]">{num(c.percent, 1)} %</span></div></td>
                </tr>
              ))}</tbody>
            </table>
          )}
        </Card>
        <Card title="Calibrations and checks" sub="Instrument calibrations, checks and replacements; factory production control needs them on record." pad={false}
          action={canEdit && <button className="btn btn-sm" onClick={() => setCal(true)}><Wrench size={13} /> Record</button>}>
          {!st?.calibrations.length ? <Empty title="Nothing recorded in this period" /> : (
            <ul className="divide-y divide-line">
              {st.calibrations.map((c) => (
                <li key={c.id} className="flex items-start gap-3 px-5 py-2.5 text-[13px]">
                  <span className="chip shrink-0 capitalize">{c.kind}</span>
                  <span className="min-w-0 flex-1"><span className="font-medium">{c.parameter}</span>{c.siteId && <span className="text-text-2"> · {dir.site(c.siteId)?.name}</span>}
                    <span className="block text-[12px] text-muted">{date(c.at)} · {c.by}{c.note ? ` · ${c.note}` : ""}</span></span>
                  {canEdit && <button className="text-muted hover:text-critical" aria-label={`Delete calibration of ${c.parameter}`} onClick={() => { if (confirm("Delete this entry?")) delCal.mutate(c.id); }}><Trash2 size={13} /></button>}
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>
      {edit && <LimitModal limit={edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); refresh(); }} />}
      {cal && <CalibrationModal onClose={() => setCal(false)} onSaved={() => { setCal(false); refresh(); }} />}
    </div>
  );
}

function LimitModal({ limit, onClose, onSaved }: { limit: Partial<Limit>; onClose: () => void; onSaved: () => void }) {
  const cat = useCatalog().data;
  const me = useMe().data!;
  const dir = useDirectory();
  const [l, setL] = useState<Partial<Limit>>(limit);
  const src = cat?.sources.find((s) => s.key === l.source);
  const measures = (src?.fields ?? []).filter((f) => f.kind === "measure" && f.key !== "records");
  useEffect(() => { if (src && !measures.some((m) => m.key === l.measure)) setL((x) => ({ ...x, measure: measures[0]?.key })); }, [src]); // eslint-disable-line react-hooks/exhaustive-deps
  const unit = measures.find((m) => m.key === l.measure)?.unit;
  const save = useMutation({
    mutationFn: () => api.send<Limit>(l.id ? "PUT" : "POST", l.id ? `/compliance/limits/${l.id}` : "/compliance/limits",
      { name: l.name ?? "", source: l.source, measure: l.measure, types: l.types ?? [], siteId: l.siteId ?? "", basis: l.basis, min: l.min ?? null, max: l.max ?? null, note: l.note ?? "", enabled: l.enabled ?? true }),
    onSuccess: onSaved,
  });
  const numIn = (k: "min" | "max") => (
    <label className="block"><span className="label">{k === "min" ? "Lower bound" : "Upper bound"}{unit ? <span className="font-normal text-muted"> · {unit === "pct" ? "%" : unit === "c" ? "°C" : unit}</span> : null}</span>
      <input className="input num" type="number" step="any" value={l[k] ?? ""} placeholder="none" onChange={(e) => setL({ ...l, [k]: e.target.value === "" ? undefined : Number(e.target.value) })} />
      <FieldError e={save.error} f={k} /></label>
  );
  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title={l.id ? `Edit “${limit.name}”` : "New limit"} sub="Every change is kept in the audit log.">
      <form className="flex flex-col gap-3.5" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
        <label className="block"><span className="label">Name</span><input className="input" value={l.name ?? ""} maxLength={80} required placeholder="Additive dosage" onChange={(e) => setL({ ...l, name: e.target.value })} /><FieldError e={save.error} f="name" /></label>
        <div className="grid grid-cols-2 gap-3">
          <label className="block"><span className="label">Data from</span>
            <select className="input" value={l.source} onChange={(e) => setL({ ...l, source: e.target.value, types: [], measure: undefined })}>{cat?.sources.map((s) => <option key={s.key} value={s.key}>{s.label}</option>)}</select></label>
          <label className="block"><span className="label">Measure</span>
            <select className="input" value={l.measure ?? ""} onChange={(e) => setL({ ...l, measure: e.target.value })}>{measures.map((m) => <option key={m.key} value={m.key}>{m.label}</option>)}</select>
            <FieldError e={save.error} f="measure" /></label>
        </div>
        {src?.types && (
          <div><span className="label">Record types <span className="font-normal text-muted">· none selected = all</span></span>
            <div className="flex flex-wrap gap-1">{src.types.map((t) => { const on = l.types?.includes(t); return (
              <button key={t} type="button" aria-pressed={!!on} onClick={() => setL({ ...l, types: on ? l.types!.filter((x) => x !== t) : [...(l.types ?? []), t] })}
                className={clsx("chip", on ? "chip-accent" : "opacity-70 hover:opacity-100")}>{TYPE[t as RecordType]?.plural ?? t}</button>); })}</div></div>
        )}
        <div className="grid grid-cols-2 gap-3">
          <label className="block"><span className="label">Check</span>
            <select className="input" value={l.basis} onChange={(e) => setL({ ...l, basis: e.target.value as Limit["basis"] })}>
              <option value="instant">Every value</option><option value="daily_mean">The daily mean</option><option value="monthly_mean">The monthly mean</option></select></label>
          <label className="block"><span className="label">Site</span>
            <select className="input" value={l.siteId ?? ""} onChange={(e) => setL({ ...l, siteId: e.target.value || undefined })}>
              <option value="">All my sites</option>{dir.sites.filter((s) => s.orgId === me.org.id).map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}</select>
            <FieldError e={save.error} f="siteId" /></label>
        </div>
        <div className="grid grid-cols-2 gap-3">{numIn("min")}{numIn("max")}</div>
        <label className="block"><span className="label">Source of the limit <span className="font-normal text-muted">· permit, mix design, standard</span></span>
          <input className="input" value={l.note ?? ""} maxLength={500} placeholder="EN 13108-1 mix design AC 16 surf, rev. 3" onChange={(e) => setL({ ...l, note: e.target.value })} /></label>
        {l.id && <label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={l.enabled ?? true} onChange={(e) => setL({ ...l, enabled: e.target.checked })} /> Check this limit</label>}
        {save.error && !(save.error instanceof ApiError && save.error.problem.errors) ? <ErrorBox error={save.error} /> : null}
        <div className="flex justify-end gap-2"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={save.isPending}>Save limit</button></div>
      </form>
    </Modal>
  );
}

function CalibrationModal({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const me = useMe().data!;
  const dir = useDirectory();
  const today = new Date().toISOString().slice(0, 10);
  const [c, setC] = useState({ parameter: "", kind: "calibration", at: today, by: "", siteId: "", note: "" });
  const save = useMutation({
    mutationFn: () => api.send("POST", "/compliance/calibrations", { ...c, at: new Date(`${c.at}T12:00:00Z`).toISOString() }),
    onSuccess: onSaved,
  });
  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title="Record a calibration or check">
      <form className="flex flex-col gap-3.5" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
        <label className="block"><span className="label">Instrument or parameter</span><input className="input" required maxLength={80} placeholder="Additive feeder load cell" value={c.parameter} onChange={(e) => setC({ ...c, parameter: e.target.value })} /><FieldError e={save.error} f="parameter" /></label>
        <div className="grid grid-cols-2 gap-3">
          <label className="block"><span className="label">What was done</span>
            <select className="input" value={c.kind} onChange={(e) => setC({ ...c, kind: e.target.value })}><option value="calibration">Calibration</option><option value="check">Check</option><option value="maintenance">Maintenance</option><option value="replacement">Replacement</option></select></label>
          <label className="block"><span className="label">On</span><input className="input" type="date" max={today} required value={c.at} onChange={(e) => setC({ ...c, at: e.target.value })} /><FieldError e={save.error} f="at" /></label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="block"><span className="label">By</span><input className="input" required maxLength={120} placeholder="Person or service company" value={c.by} onChange={(e) => setC({ ...c, by: e.target.value })} /><FieldError e={save.error} f="by" /></label>
          <label className="block"><span className="label">Site</span>
            <select className="input" value={c.siteId} onChange={(e) => setC({ ...c, siteId: e.target.value })}><option value="">—</option>{dir.sites.filter((s) => s.orgId === me.org.id).map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}</select></label>
        </div>
        <label className="block"><span className="label">Note</span><input className="input" maxLength={1000} placeholder="Certificate number, deviation found" value={c.note} onChange={(e) => setC({ ...c, note: e.target.value })} /></label>
        {save.error && !(save.error instanceof ApiError && save.error.problem.errors) ? <ErrorBox error={save.error} /> : null}
        <div className="flex justify-end gap-2"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={save.isPending}>Record</button></div>
      </form>
    </Modal>
  );
}

// ---------------------------------------------------------------- register

function RegisterTab({ orgQ }: { orgQ: Record<string, string> }) {
  const q = useQuery({ queryKey: ["compliance-register", orgQ], queryFn: () => api.get<RegisterView>(`/compliance/register${qs(orgQ)}`) });
  const [attest, setAttest] = useState<Item | null>(null);
  const [history, setHistory] = useState<Item | null>(null);
  const [filter, setFilter] = useState<"all" | "todo">("all");
  if (q.error) return <ErrorBox error={q.error} />;
  if (q.isLoading || !q.data) return <Skel h={360} />;
  const { register: reg, regulations, available, canAttest } = q.data;
  if (!reg.jurisdiction) {
    return <Card><Empty icon={<ShieldCheck size={20} />} title="No regulatory register for this country yet">
      ROADPASS carries registers for {available.map((j) => j.name).join(", ")}. The rules of this organisation's country or state are not in the platform yet,
      so nothing is scored here; limits, exceedances and the signed monthly report still apply. Ask the platform operator to add your jurisdiction.
    </Empty></Card>;
  }
  const todo = (s: Status) => s === "gap" || s === "open" || s === "partial";
  const items = reg.items.filter((it) => filter === "all" || todo(it.status));
  return (
    <div className="flex flex-col gap-4">
      <div className="card flex flex-wrap items-center gap-x-8 gap-y-4 p-5">
        <div className="flex items-center gap-4">
          <ScoreRing value={reg.score} />
          <div><div className="flex items-center gap-1.5 text-[15px] font-semibold">Register score<InfoTip text={HELP.registerScore} /></div><div className="text-[12.5px] text-text-2">Met counts fully, partly met half; not-applicable items are left out.</div></div>
        </div>
        <div className="flex flex-wrap gap-2">
          {(["met", "partial", "gap", "open", "not_applicable"] as Status[]).filter((s) => reg.counts[s]).map((s) => { const S = STATUS[s]; return <span key={s} className={clsx("chip", S.chip)}><S.icon size={11} />{reg.counts[s]} {S.label.toLowerCase()}</span>; })}
        </div>
        <div className="ml-auto seg" role="group" aria-label="Which requirements">
          <button type="button" aria-pressed={filter === "all"} onClick={() => setFilter("all")}>All</button>
          <button type="button" aria-pressed={filter === "todo"} onClick={() => setFilter("todo")}>To do</button>
        </div>
      </div>
      <div className="flex flex-col gap-1 px-1 text-[12.5px] text-text-2">
        {reg.jurisdictions.map((j) => <p key={j.code}><span className="font-semibold text-text">{j.name}</span> · {j.scope}</p>)}
      </div>
      {reg.simulated && <div className="rounded-2xl bg-[var(--warning-soft)] px-4 py-3 text-[13px] text-warning">These figures come from simulated demo data, so automatic checks count as partly met at most. Operational records lift them to met.</div>}
      {regulations.map((rg) => {
        const rows = items.filter((it) => it.regulation === rg.Key);
        if (!rows.length) return null;
        return (
          <Card key={rg.Key} pad={false} title={rg.Name} sub={<a href={rg.URL} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:text-accent-ink">{rg.Act}<ExternalLink size={11} /></a>}>
            <ul className="divide-y divide-line">
              {rows.map((it) => {
                const S = STATUS[it.status];
                const signable = canAttest && (it.attest || it.status !== "met");
                return (
                  <li key={it.id} className="grid gap-x-4 gap-y-1.5 px-5 py-3.5 md:grid-cols-[150px_minmax(0,1fr)_auto]">
                    <div><span className={clsx("chip", S.chip)}><S.icon size={11} />{S.label}</span>
                      <div className="mt-1 flex items-center gap-1 text-[11.5px] text-muted">{it.attest ? "Signed statement" : "Checked from data"}<InfoTip size={11} text={it.attest ? HELP.attestation : HELP.autoCheck} /></div></div>
                    <div className="min-w-0">
                      <div className="text-[13.5px] font-semibold">{it.title} <span className="font-normal text-muted">· {it.ref}</span></div>
                      <p className="mt-0.5 text-[12.5px] text-text-2">{it.explain}</p>
                      <p className="mt-1.5 text-[12.5px]">{it.detail}</p>
                      {it.attestation && <AttestationLine a={it.attestation} />}
                    </div>
                    <div className="flex items-start gap-1.5 md:justify-end">
                      {it.attestation && <button className="btn btn-ghost btn-sm" onClick={() => setHistory(it)}><History size={13} /> History</button>}
                      {signable && (it.attest
                        ? <button className="btn btn-sm" onClick={() => setAttest(it)}><FileSignature size={13} /> Sign statement</button>
                        : <button className="btn btn-ghost btn-sm text-muted" onClick={() => setAttest(it)}>Not applicable?</button>)}
                    </div>
                  </li>
                );
              })}
            </ul>
          </Card>
        );
      })}
      {filter === "todo" && !items.length && <Card><Empty icon={<BadgeCheck size={20} />} title="Nothing to do">Every requirement is met or not applicable.</Empty></Card>}
      {attest && <AttestModal item={attest} onClose={() => setAttest(null)} />}
      {history && <HistoryModal item={history} orgQ={orgQ} onClose={() => setHistory(null)} />}
    </div>
  );
}

function ScoreRing({ value }: { value: number }) {
  const r = 26, c = 2 * Math.PI * r;
  const color = value >= 90 ? "var(--good)" : value >= 60 ? "var(--warning)" : "var(--critical)";
  return (
    <svg width={68} height={68} viewBox="0 0 68 68" role="img" aria-label={`Score ${Math.round(value)} percent`}>
      <circle cx={34} cy={34} r={r} fill="none" stroke="var(--surface-3)" strokeWidth={7} />
      <circle cx={34} cy={34} r={r} fill="none" stroke={color} strokeWidth={7} strokeLinecap="round" strokeDasharray={`${(value / 100) * c} ${c}`} transform="rotate(-90 34 34)" />
      <text x={34} y={39} textAnchor="middle" style={{ fill: "var(--text)", fontSize: 15, fontWeight: 650 }}>{Math.round(value)}%</text>
    </svg>
  );
}

/** Who signed a statement, with the signature checked in this browser. */
function AttestationLine({ a }: { a: Attestation }) {
  const [ok, setOk] = useState<boolean | null | undefined>(undefined);
  useEffect(() => {
    let live = true;
    if (a.publicKey && a.signature) void verifyEd25519(a.publicKey, a.signature, a.message).then((v) => live && setOk(v));
    return () => { live = false; };
  }, [a]);
  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 rounded-lg bg-surface-2 px-2.5 py-1.5 text-[12px] text-text-2">
      <FileSignature size={12} className="text-muted" />
      <span>{a.byName || "A former member"} · {dateTime(a.at)}</span>
      {a.evidenceUrl && <a className="inline-flex items-center gap-0.5 text-accent-ink hover:underline" href={a.evidenceUrl} target="_blank" rel="noreferrer">document<ExternalLink size={10} /></a>}
      {ok === true && <span className="chip chip-good"><BadgeCheck size={11} />signature valid</span>}
      {ok === false && <span className="chip chip-critical">signature does not match</span>}
      {ok === null && <span className="text-muted">signature not checkable in this browser</span>}
    </div>
  );
}

function AttestModal({ item, onClose }: { item: Item; onClose: () => void }) {
  const qc = useQueryClient();
  const [x, setX] = useState({ status: item.attest ? "met" : "not_applicable", note: "", evidenceUrl: "" });
  const save = useMutation({
    mutationFn: () => api.send("POST", "/compliance/attestations", { requirementId: item.id, ...x }),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["compliance-register"] }); onClose(); },
  });
  const options = item.attest ? [["met", "Met"], ["partial", "Partly met"], ["not_met", "Not met"], ["not_applicable", "Not applicable"]] : [["not_applicable", "Not applicable to us"]];
  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title={item.title} sub={`${item.ref} · signed with your personal key; it cannot be changed afterwards, only superseded.`}>
      <form className="flex flex-col gap-3.5" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
        <p className="text-[13px] text-text-2">{item.explain}</p>
        {!item.attest && <p className="rounded-xl bg-surface-2 px-3 py-2 text-[12.5px] text-text-2">This requirement is checked from your data. A statement can only record that it does not apply to you, and why.</p>}
        <div><span className="label">Status</span>
          <div className="flex flex-wrap gap-1.5" role="radiogroup" aria-label="Status">
            {options.map(([v, l]) => <button key={v} type="button" role="radio" aria-checked={x.status === v} onClick={() => setX({ ...x, status: v })} className={clsx("chip", x.status === v ? "chip-accent" : "opacity-70 hover:opacity-100")}>{l}</button>)}
          </div><FieldError e={save.error} f="status" /></div>
        <label className="block"><span className="label">Statement</span>
          <textarea className="input min-h-[110px] py-2" required minLength={10} maxLength={2000} value={x.note} onChange={(e) => setX({ ...x, note: e.target.value })}
            placeholder={item.attest ? "What is in place, where it is documented, and until when it is valid." : "Why this does not apply to your organisation."} />
          <FieldError e={save.error} f="note" /></label>
        <label className="block"><span className="label">Link to the document <span className="font-normal text-muted">· optional</span></span>
          <input className="input" type="url" placeholder="https://" value={x.evidenceUrl} onChange={(e) => setX({ ...x, evidenceUrl: e.target.value })} /><FieldError e={save.error} f="evidenceUrl" /></label>
        {save.error && !(save.error instanceof ApiError && save.error.problem.errors) ? <ErrorBox error={save.error} /> : null}
        <div className="flex justify-end gap-2"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={save.isPending}><FileSignature size={14} /> Sign</button></div>
      </form>
    </Modal>
  );
}

function HistoryModal({ item, orgQ, onClose }: { item: Item; orgQ: Record<string, string>; onClose: () => void }) {
  const q = useQuery({ queryKey: ["attest-history", item.id, orgQ], queryFn: () => api.get<Attestation[]>(`/compliance/requirements/${item.id}/history${qs(orgQ)}`) });
  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title={`History · ${item.title}`} sub="Every signed statement, newest first. Earlier ones are kept unchanged.">
      {q.isLoading ? <Skel h={120} /> : (
        <ul className="flex max-h-[60vh] flex-col gap-2 overflow-y-auto scroll-thin">
          {(q.data ?? []).map((a) => (
            <li key={a.id} className="rounded-xl border border-line p-3 text-[13px]">
              <div className="flex items-center justify-between gap-2"><span className={clsx("chip", STATUS[(a.status === "not_met" ? "gap" : a.status) as Status]?.chip)}>{a.status.replace("_", " ")}</span><span className="text-[12px] text-muted">{dateTime(a.at)}</span></div>
              <p className="mt-1.5">{a.note}</p>
              <AttestationLine a={a} />
            </li>
          ))}
        </ul>
      )}
    </Modal>
  );
}

// ---------------------------------------------------------------- reports

interface ReportState {
  period: string; state: "signed" | "stale" | "unsigned"; attestation?: Attestation; fingerprint: string;
  kpis: { label: string; value: string; hint?: string }[]; notice?: string; canSign: boolean;
}

function months(n: number) {
  const out: { value: string; label: string }[] = [];
  const d = new Date();
  for (let i = 1; i <= n; i++) {
    const m = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() - i, 1));
    out.push({ value: m.toISOString().slice(0, 7), label: m.toLocaleDateString("en-GB", { month: "long", year: "numeric", timeZone: "UTC" }) });
  }
  return out;
}

function ReportsTab({ orgQ }: { orgQ: Record<string, string> }) {
  const me = useMe().data!;
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const list = useMemo(() => months(12), []);
  const period = params.get("period") ?? list[0].value;
  const q = useQuery({ queryKey: ["compliance-report", period, orgQ], queryFn: () => api.get<ReportState>(`/compliance/report${qs({ ...orgQ, period })}`) });
  const [signing, setSigning] = useState(false);
  const [note, setNote] = useState("");
  const sign = useMutation({
    mutationFn: () => api.send("POST", "/compliance/report/sign", { period, note, fingerprint: q.data!.fingerprint }),
    onSuccess: () => { setSigning(false); setNote(""); void qc.invalidateQueries({ queryKey: ["compliance-report"] }); },
  });
  const changed = sign.error instanceof ApiError && sign.error.status === 409;
  const d = q.data;
  const label = list.find((m) => m.value === period)?.label ?? period;
  const badge = d && {
    signed: <span className="chip chip-good"><BadgeCheck size={12} />Signed</span>,
    stale: <span className="chip chip-warning"><CircleAlert size={12} />Data changed since signing</span>,
    unsigned: <span className="chip"><CircleDashed size={12} />Not signed</span>,
  }[d.state];

  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
      <Card title="Monthly compliance report" help={HELP.reportSignOff} sub="Limits, exceedances, plant data completeness and calibrations for one month. A responsible person signs it with their personal key."
        action={<select className="input h-8 w-auto" value={period} onChange={(e) => setParams((p) => { const n = new URLSearchParams(p); n.set("period", e.target.value); return n; }, { replace: true })} aria-label="Month">
          {list.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}</select>}>
        {q.error ? <ErrorBox error={q.error} /> : !d ? <Skel h={180} /> : (
          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center gap-2">{badge}<span className="text-[12.5px] text-text-2">{label}</span></div>
            {d.notice && <p className="rounded-xl bg-[var(--warning-soft)] px-3 py-2 text-[12.5px] text-warning">{d.notice}</p>}
            <div className="grid grid-cols-3 gap-3">
              {d.kpis.map((k) => <div key={k.label} className="rounded-xl bg-surface-2 p-3"><div className="text-[12px] text-muted">{k.label}</div><div className="num mt-0.5 text-[20px] font-semibold">{k.value}</div></div>)}
            </div>
            {d.attestation && (
              <div className="text-[12.5px] text-text-2">
                {d.state === "signed" ? "Signed" : "Signed for earlier figures"} by <b>{d.attestation.byName || "a former member"}</b> on {dateTime(d.attestation.at)}{d.attestation.note ? ` — “${d.attestation.note}”` : ""}.
                {d.state === "stale" && " Records for this month changed afterwards; sign again to confirm the current figures."}
              </div>
            )}
            <div className="flex items-start gap-1.5"><InfoTip size={12} text={HELP.fingerprint} /><span className="mono break-all text-[11px] text-muted">Fingerprint sha256:{d.fingerprint}</span></div>
            <div className="flex flex-wrap gap-2">
              <Download kind="compliance" range={false} params={{ ...orgQ, period }} label="Download" />
              {d.canSign && d.state !== "signed" && <button className="btn btn-primary" onClick={() => { sign.reset(); setSigning(true); }}><FileSignature size={14} /> Sign {label}</button>}
            </div>
          </div>
        )}
      </Card>
      {me.user.role === "admin" && !orgQ.org && <ScheduleCard />}
      {signing && d && (
        <Modal open onOpenChange={(o) => !o && setSigning(false)} title={`Sign the report for ${label}`} sub="Your personal key signs the report's fingerprint. If the month's data change later, the report shows that it needs signing again.">
          <form className="flex flex-col gap-3.5" onSubmit={(e) => { e.preventDefault(); sign.mutate(); }}>
            <div className="rounded-xl bg-surface-2 p-3 text-[13px]">I confirm that this report for <b>{label}</b> is complete and correct to the best of my knowledge.</div>
            <label className="block"><span className="label">Note <span className="font-normal text-muted">· optional</span></span>
              <input className="input" maxLength={1000} value={note} onChange={(e) => setNote(e.target.value)} placeholder="Exceedances on 14 Sep explained in deviation report DR-26-031" /></label>
            <div className="mono break-all text-[11px] text-muted">sha256:{d.fingerprint}</div>
            {changed ? <div className="rounded-xl bg-[var(--warning-soft)] px-3 py-2 text-[12.5px] text-warning">New data arrived for this month. <button type="button" className="font-semibold underline" onClick={() => { void q.refetch(); setSigning(false); }}>Review the new figures</button></div>
              : sign.error ? <ErrorBox error={sign.error} /> : null}
            <div className="flex justify-end gap-2"><button type="button" className="btn" onClick={() => setSigning(false)}>Cancel</button><button className="btn btn-primary" disabled={sign.isPending || changed}><FileSignature size={14} /> Sign</button></div>
          </form>
        </Modal>
      )}
    </div>
  );
}

interface Schedule { recipients: string[]; day: number; hour: number; timezone: string; enabled: boolean; lastRunAt?: string; lastStatus?: string; lastMessage?: string; lastPeriod?: string }

function ScheduleCard() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["compliance-schedule"], queryFn: () => api.get<{ schedule: Schedule | null; mail: boolean }>("/compliance/schedule") });
  const [x, setX] = useState<{ recipients: string; day: number; hour: number; timezone: string; enabled: boolean } | null>(null);
  useEffect(() => {
    if (q.data && !x) {
      const s = q.data.schedule;
      setX(s ? { recipients: s.recipients.join(", "), day: s.day, hour: s.hour, timezone: s.timezone, enabled: s.enabled } : { recipients: "", day: 3, hour: 8, timezone: timeZone(), enabled: true });
    }
  }, [q.data, x]);
  const save = useMutation({
    mutationFn: () => api.send("PUT", "/compliance/schedule", { ...x!, recipients: x!.recipients.split(/[,;\s]+/).filter(Boolean) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["compliance-schedule"] }),
  });
  const del = useMutation({ mutationFn: () => api.send("DELETE", "/compliance/schedule"), onSuccess: () => { setX(null); void qc.invalidateQueries({ queryKey: ["compliance-schedule"] }); } });
  const s = q.data?.schedule;
  return (
    <Card title="E-mail it every month" sub="The previous month's report goes out as a PDF, signed or not; the e-mail says which.">
      {!q.data || !x ? <Skel h={160} /> : (
        <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
          {!q.data.mail && <p className="rounded-xl bg-[var(--warning-soft)] px-3 py-2 text-[12.5px] text-warning">This platform has no mail server configured yet; nothing will be sent until it does.</p>}
          <label className="block"><span className="label">To</span><input className="input" value={x.recipients} onChange={(e) => setX({ ...x, recipients: e.target.value })} placeholder="quality@company.example, authority@region.example" /><FieldError e={save.error} f="recipients" /></label>
          <div className="grid grid-cols-3 gap-2">
            <label className="block"><span className="label">Day</span><select className="input" value={x.day} onChange={(e) => setX({ ...x, day: Number(e.target.value) })}>{Array.from({ length: 28 }, (_, i) => <option key={i + 1} value={i + 1}>{i + 1}</option>)}</select></label>
            <label className="block"><span className="label">At</span><select className="input" value={x.hour} onChange={(e) => setX({ ...x, hour: Number(e.target.value) })}>{Array.from({ length: 24 }, (_, i) => <option key={i} value={i}>{String(i).padStart(2, "0")}:00</option>)}</select></label>
            <label className="block"><span className="label">Zone</span><input className="input" value={x.timezone} onChange={(e) => setX({ ...x, timezone: e.target.value })} /><FieldError e={save.error} f="timezone" /></label>
          </div>
          <label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={x.enabled} onChange={(e) => setX({ ...x, enabled: e.target.checked })} /> Send automatically</label>
          {s?.lastRunAt && <p className={clsx("text-[12.5px]", s.lastStatus === "failed" ? "text-critical" : "text-text-2")}><Mail size={12} className="mr-1 inline" />Last run {dateTime(s.lastRunAt)}: {s.lastMessage}</p>}
          {save.error && !(save.error instanceof ApiError && save.error.problem.errors) ? <ErrorBox error={save.error} /> : null}
          <div className="flex items-center justify-end gap-2">
            {save.isSuccess && <span className="text-[12.5px] text-[var(--good-ink)]">Saved</span>}
            {s && <button type="button" className="btn btn-ghost" onClick={() => { if (confirm("Stop the monthly e-mail?")) del.mutate(); }}>Remove</button>}
            <button className="btn btn-primary" disabled={save.isPending}>Save</button>
          </div>
        </form>
      )}
    </Card>
  );
}

// ---------------------------------------------------------------- data protection

interface Privacy {
  former: User[]; requests: { id: string; subjectUser: string; kind: "export" | "erase"; requestedBy: string; at: string }[];
  retention: { what: string; kept: string; why: string; personal: boolean }[];
}

/** GDPR: retention, export and erasure. The platform manager sees every organisation's former members. */
export function DataProtection({ platform = false }: { platform?: boolean }) {
  const qc = useQueryClient();
  const dir = useDirectory();
  const q = useQuery({ queryKey: ["privacy", platform], queryFn: () => api.get<Privacy>(`/compliance/privacy${platform ? "?all=1" : ""}`) });
  const [erase, setErase] = useState<User | null>(null);
  const del = useMutation({
    mutationFn: (id: string) => api.send("POST", `/compliance/privacy/${id}/erase`),
    onSuccess: () => { setErase(null); void qc.invalidateQueries({ queryKey: ["privacy"] }); },
  });
  if (q.error) return <ErrorBox error={q.error} />;
  if (!q.data) return <Skel h={300} />;
  const erased = (u: User) => u.email.startsWith("erased+");
  return (
    <div className="flex flex-col gap-4">
      <Card title="Former members" sub="Removed members can no longer sign in. Export their data on request, or erase it: name and e-mail are replaced and their preferences, devices and notifications deleted. Records they signed stay, under a pseudonymous id, so the signatures still verify." pad={false}>
        {!q.data.former.length ? <Empty title="No former members" /> : (
          <table className="table">
            <thead><tr><th>Person</th>{platform && <th>Organisation</th>}<th>Removed</th><th /></tr></thead>
            <tbody>{q.data.former.map((u) => (
              <tr key={u.id}>
                <td><div className="font-medium">{u.name}</div><div className="mono text-[12px] text-muted">{erased(u) ? u.id : u.email}</div></td>
                {platform && <td className="text-text-2">{dir.orgName(u.orgId)}</td>}
                <td className="text-text-2">{u.disabledAt ? date(u.disabledAt) : "—"}</td>
                <td className="whitespace-nowrap text-right">
                  {erased(u) ? <span className="chip">erased</span> : <>
                    <a className="btn btn-sm btn-ghost" href={`/api/v1/compliance/privacy/${u.id}/export`} download><DownloadIcon size={13} /> Export</a>
                    <button className="btn btn-sm btn-danger" onClick={() => setErase(u)}><UserX size={13} /> Erase</button>
                  </>}
                </td>
              </tr>
            ))}</tbody>
          </table>
        )}
      </Card>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <Card title="How long data are kept" pad={false}>
          <table className="table">
            <thead><tr><th>Data</th><th>Kept</th></tr></thead>
            <tbody>{q.data.retention.map((r) => (
              <tr key={r.what}><td><div className="font-medium">{r.what}{r.personal && <span className="chip ml-1.5">personal data</span>}</div><div className="text-[12px] text-muted">{r.why}</div></td><td className="text-text-2">{r.kept}</td></tr>
            ))}</tbody>
          </table>
        </Card>
        <Card title="Requests handled" pad={false}>
          {!q.data.requests.length ? <Empty title="None yet" /> : (
            <ul className="divide-y divide-line">{q.data.requests.map((r) => (
              <li key={r.id} className="flex items-center justify-between gap-2 px-5 py-2.5 text-[13px]">
                <span><span className={clsx("chip", r.kind === "erase" && "chip-critical")}>{r.kind === "erase" ? "Erasure" : "Export"}</span> <span className="mono text-[12px] text-text-2">{r.subjectUser}</span></span>
                <span className="text-[12px] text-muted">{dateTime(r.at)}</span>
              </li>
            ))}</ul>
          )}
          <p className="border-t border-line px-5 py-2.5 text-[12px] text-muted">Members download their own data under Settings → Profile.</p>
        </Card>
      </div>
      {erase && (
        <Modal open onOpenChange={(o) => !o && setErase(null)} title={`Erase ${erase.name}'s personal data?`} sub="This cannot be undone.">
          <div className="flex flex-col gap-3 text-[13px]">
            <p>Their name and e-mail are replaced, and their preferences, devices, notifications and saved layouts deleted. The records they signed stay part of the passports, under the pseudonymous id <span className="mono">{erase.id}</span>.</p>
            <p className="text-text-2">Export their data first if they asked for a copy.</p>
            {del.error ? <ErrorBox error={del.error} /> : null}
            <div className="flex justify-end gap-2"><button className="btn" onClick={() => setErase(null)}>Cancel</button><button className="btn btn-danger" disabled={del.isPending} onClick={() => del.mutate(erase.id)}>Erase</button></div>
          </div>
        </Modal>
      )}
    </div>
  );
}

