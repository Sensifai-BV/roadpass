import { useMemo, useState, type ReactNode } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlarmClock, BellOff, CheckCircle2, CircleAlert, Hand, Hash, Mail, MessageCircle, MessageSquare, Pencil, Plus, Radio, Send, Siren, Trash2, UserCheck, Webhook,
} from "lucide-react";
import clsx from "clsx";
import { api, ApiError } from "../lib/api";
import { useMe } from "../lib/hooks";
import { DUTY } from "../lib/vocab";
import { ago, dateTime, num } from "../lib/format";
import type { Duty, User } from "../lib/types";
import { AGGS, useCatalog, type Query } from "../lib/dashboards";
import { Card, Copy_, Empty, ErrorBox, Modal, PageHeader, Skel, Stat } from "../components/ui";

type Severity = "info" | "warning" | "critical";
type Kind = "metric" | "finding" | "receipt_overdue" | "rejected" | "telemetry_gap" | "seal_backlog" | "plan_limit";
interface Params { query?: Query; windowMinutes?: number; op?: string; value?: number; minSeverity?: string; hours?: number; minutes?: number; percent?: number }
interface Step { afterMinutes: number; channelIds: string[]; audience: string[] }
interface Rule {
  id: string; name: string; kind: Kind; params: Params; severity: Severity; audience: string[]; channelIds: string[];
  forMinutes: number; repeatMinutes: number; notifyResolved: boolean; escalation: Step[]; enabled: boolean; version: number; updatedAt?: string;
}
interface Channel { id: string; type: string; name: string; config: Record<string, string>; verified: boolean; lastStatus?: string; lastError?: string; lastSentAt?: string }
interface ActiveAlert {
  ruleId: string; key: string; since: string; firedAt?: string; value: number; ackBy?: string; ackAt?: string; snoozeUntil?: string; assignedTo?: string;
  ruleName: string; kind: Kind; severity: Severity; title: string; body: string; link?: string; escLevel: number;
}
interface Event { id: number; ruleId: string; key: string; state: string; severity: Severity; title: string; body: string; at: string; deliveries: { channel: string; type: string; ok: boolean; error?: string; to?: number }[]; ackAt?: string }
interface Maintenance { id: string; startsAt: string; endsAt: string; ruleIds: string[]; note: string }

const KIND: Record<Kind, { name: string; explain: string }> = {
  metric: { name: "A measure crosses a threshold", explain: "Any measure from the data catalogue, over a recent window, compared with a value." },
  finding: { name: "A data check fails", explain: "A new finding on one of your records." },
  receipt_overdue: { name: "A shipment is not received", explain: "A shipment to you still waits for an intake review after N hours." },
  rejected: { name: "Our load is rejected", explain: "A receiver rejected one of your shipments at intake." },
  telemetry_gap: { name: "Plant data stops", explain: "A plant control system sent nothing for N minutes." },
  seal_backlog: { name: "Records wait for a seal", explain: "Records wait longer than N minutes for the ledger (platform operator)." },
  plan_limit: { name: "Plan limit approaches", explain: "Members or records reach N % of your plan." },
};
const SEV: Record<Severity, { label: string; chip: string; color: string }> = {
  critical: { label: "Critical", chip: "chip-critical", color: "var(--critical)" }, warning: { label: "Warning", chip: "chip-warning", color: "var(--warning)" },
  info: { label: "Info", chip: "chip-accent", color: "var(--accent)" },
};
const CH_ICON: Record<string, typeof Mail> = { email: Mail, webhook: Webhook, slack: Hash, teams: MessageSquare, sms: MessageCircle, telegram: Send };
const CH_NAME: Record<string, string> = { email: "E-mail", webhook: "Webhook", slack: "Slack", teams: "Microsoft Teams", sms: "SMS", telegram: "Telegram" };

type Tab = "active" | "rules" | "channels" | "history" | "maintenance";

export default function Alerts() {
  const me = useMe().data!;
  const admin = me.user.role === "admin";
  const [params, setParams] = useSearchParams();
  const tabs: { key: Tab; label: string; admin?: boolean }[] = [
    { key: "active", label: "Active" }, { key: "rules", label: "Rules" }, { key: "channels", label: "Channels", admin: true },
    { key: "history", label: "History" }, { key: "maintenance", label: "Maintenance" },
  ];
  const visible = tabs.filter((t) => !t.admin || admin);
  const tab = (visible.find((t) => t.key === params.get("tab"))?.key ?? "active") as Tab;
  return (
    <div>
      <PageHeader title="Alerts" sub="Rules watch the chain and tell the right people, in ROADPASS, in their browser, and through your channels." />
      <div className="mb-5 flex gap-1 overflow-x-auto border-b border-line" role="tablist">
        {visible.map((t) => (
          <button key={t.key} role="tab" aria-selected={tab === t.key} onClick={() => setParams({ tab: t.key }, { replace: true })}
            className={clsx("-mb-px whitespace-nowrap border-b-2 px-3 pb-2.5 pt-1 text-[13.5px] font-medium", tab === t.key ? "border-accent text-text" : "border-transparent text-text-2 hover:text-text")}>{t.label}</button>
        ))}
      </div>
      {tab === "active" && <ActiveTab />}
      {tab === "rules" && <RulesTab admin={admin} />}
      {tab === "channels" && <ChannelsTab />}
      {tab === "history" && <HistoryTab />}
      {tab === "maintenance" && <MaintenanceTab admin={admin} />}
    </div>
  );
}

function useMembers() {
  return useQuery({ queryKey: ["members"], queryFn: () => api.get<User[]>("/org/members") });
}

function ActiveTab() {
  const qc = useQueryClient();
  const me = useMe().data!;
  const q = useQuery({ queryKey: ["alerts-active"], queryFn: () => api.get<ActiveAlert[]>("/alerts/active"), refetchInterval: 30_000 });
  const members = useMembers();
  const name = (id?: string) => members.data?.find((u) => u.id === id)?.name ?? id;
  const act = useMutation({
    mutationFn: (x: { ruleId: string; key: string; action: string; minutes?: number; userId?: string }) => api.send("POST", "/alerts/action", x),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["alerts-active"] }),
  });
  const canAct = me.user.role !== "viewer";
  if (q.isLoading) return <Skel h={160} />;
  if (!q.data?.length) return <Card><Empty icon={<CheckCircle2 size={20} />} title="Nothing is firing">When a rule fires it appears here until the condition clears.</Empty></Card>;
  return (
    <div className="flex flex-col gap-3">
      {act.error ? <ErrorBox error={act.error} /> : null}
      {q.data.map((a) => {
        const snoozed = a.snoozeUntil && new Date(a.snoozeUntil) > new Date();
        return (
          <div key={a.ruleId + a.key} className="card flex flex-col gap-2 rounded-2xl p-4 sm:flex-row sm:items-start">
            <span className="mt-1 size-2.5 shrink-0 rounded-full" style={{ background: SEV[a.severity].color, boxShadow: `0 0 0 4px color-mix(in srgb, ${SEV[a.severity].color} 18%, transparent)` }} aria-hidden />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2"><span className="text-[14px] font-semibold">{a.title}</span><span className={clsx("chip", SEV[a.severity].chip)}>{SEV[a.severity].label}</span>
                {a.ackAt && <span className="chip chip-good"><Hand size={11} />acknowledged by {name(a.ackBy)}</span>}
                {snoozed && <span className="chip"><AlarmClock size={11} />snoozed until {dateTime(a.snoozeUntil)}</span>}
                {a.assignedTo && <span className="chip"><UserCheck size={11} />{name(a.assignedTo)}</span>}
                {a.escLevel > 0 && <span className="chip chip-critical">escalated ×{a.escLevel}</span>}</div>
              <p className="mt-1 text-[13px] text-text-2">{a.body}</p>
              <p className="mt-1 text-[12px] text-muted">{a.ruleName} · firing since {a.firedAt ? ago(a.firedAt) : "—"}{a.link ? <> · <a className="text-accent-ink hover:underline" href={a.link}>open</a></> : null}</p>
            </div>
            {canAct && (
              <div className="flex shrink-0 flex-wrap gap-1.5">
                {!a.ackAt && <button className="btn btn-sm" onClick={() => act.mutate({ ruleId: a.ruleId, key: a.key, action: "ack" })}><Hand size={13} /> Acknowledge</button>}
                <select className="input h-8 w-auto text-[12.5px]" aria-label="Snooze" value="" onChange={(e) => e.target.value && act.mutate({ ruleId: a.ruleId, key: a.key, action: "snooze", minutes: Number(e.target.value) })}>
                  <option value="">Snooze…</option><option value="60">1 hour</option><option value="240">4 hours</option><option value="1440">24 hours</option>
                </select>
                <select className="input h-8 w-auto text-[12.5px]" aria-label="Assign" value={a.assignedTo ?? ""} onChange={(e) => act.mutate({ ruleId: a.ruleId, key: a.key, action: "assign", userId: e.target.value })}>
                  <option value="">Assign…</option>{(members.data ?? []).map((u) => <option key={u.id} value={u.id}>{u.name}</option>)}
                </select>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function audienceText(a: string[], members: User[] | undefined): string {
  if (!a.length) return "Admins";
  return a.map((x) => x === "all" ? "Everyone" : x.startsWith("role:") ? `${x.slice(5)}s` : x.startsWith("duty:") ? DUTY[x.slice(5) as Duty]?.name ?? x : members?.find((u) => `user:${u.id}` === x)?.name ?? x).join(", ");
}

function RulesTab({ admin }: { admin: boolean }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["alert-rules"], queryFn: () => api.get<{ rules: Rule[]; kinds: Kind[] }>("/alerts/rules") });
  const members = useMembers();
  const channels = useQuery({ queryKey: ["alert-channels"], queryFn: () => api.get<{ channels: Channel[] }>("/alerts/channels"), enabled: admin });
  const [edit, setEdit] = useState<Rule | null>(null);
  const del = useMutation({ mutationFn: (id: string) => api.send("DELETE", `/alerts/rules/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ["alert-rules"] }) });
  const toggle = useMutation({ mutationFn: (r: Rule) => api.send("PUT", `/alerts/rules/${r.id}`, { ...r, enabled: !r.enabled }), onSuccess: () => qc.invalidateQueries({ queryKey: ["alert-rules"] }) });
  const blank: Rule = { id: "", name: "", kind: "metric", params: { query: { source: "records", measure: "mass_kg", agg: "sum" }, windowMinutes: 60, op: ">", value: 0 },
    severity: "warning", audience: ["role:admin"], channelIds: [], forMinutes: 0, repeatMinutes: 0, notifyResolved: true, escalation: [], enabled: true, version: 0 };
  return (
    <Card pad={false} title="Rules" sub="Each rule watches one condition. New organisations start with a few; change them to suit you."
      action={admin && <button className="btn btn-sm btn-primary" onClick={() => setEdit(blank)}><Plus size={13} /> New rule</button>}>
      {(del.error || toggle.error) ? <div className="p-4"><ErrorBox error={del.error ?? toggle.error} /></div> : null}
      <div className="overflow-x-auto">
        <table className="table">
          <thead><tr><th>Rule</th><th>Severity</th><th>Tells</th><th>Channels</th><th>Timing</th><th /></tr></thead>
          <tbody>
            {(q.data?.rules ?? []).map((r) => (
              <tr key={r.id} className={r.enabled ? undefined : "opacity-55"}>
                <td><div className="font-medium">{r.name}</div><div className="text-[12px] text-muted">{KIND[r.kind]?.name}</div></td>
                <td><span className={clsx("chip", SEV[r.severity].chip)}>{SEV[r.severity].label}</span></td>
                <td className="text-text-2">{audienceText(r.audience, members.data)}</td>
                <td className="text-text-2">{r.channelIds.length ? r.channelIds.map((id) => channels.data?.channels.find((c) => c.id === id)?.name ?? id).join(", ") : "In-app only"}</td>
                <td className="text-[12.5px] text-text-2">{r.forMinutes ? `after ${r.forMinutes} min` : "at once"}{r.repeatMinutes ? ` · every ${r.repeatMinutes} min` : ""}{r.escalation.length ? ` · ${r.escalation.length} escalation${r.escalation.length > 1 ? "s" : ""}` : ""}</td>
                <td className="whitespace-nowrap text-right">{admin && <>
                  <button className="btn btn-sm btn-ghost" onClick={() => toggle.mutate(r)}>{r.enabled ? <><BellOff size={13} /> Pause</> : <><Radio size={13} /> Resume</>}</button>
                  <button className="btn btn-sm btn-ghost" aria-label={`Edit ${r.name}`} onClick={() => setEdit(r)}><Pencil size={13} /></button>
                  <button className="btn btn-sm btn-ghost" aria-label={`Delete ${r.name}`} onClick={() => { if (confirm(`Delete “${r.name}”?`)) del.mutate(r.id); }}><Trash2 size={13} /></button>
                </>}</td>
              </tr>
            ))}
            {!q.isLoading && !q.data?.rules.length && <tr><td colSpan={6} className="text-center text-muted">No rules yet.</td></tr>}
          </tbody>
        </table>
      </div>
      {edit && <RuleEditor rule={edit} channels={channels.data?.channels ?? []} members={members.data ?? []} onClose={() => setEdit(null)} />}
    </Card>
  );
}

function L({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return <label className="block"><span className="label">{label}{hint && <span className="font-normal text-muted"> · {hint}</span>}</span>{children}</label>;
}

function AudiencePick({ value, onChange, members }: { value: string[]; onChange: (v: string[]) => void; members: User[] }) {
  const t = (a: string) => onChange(value.includes(a) ? value.filter((x) => x !== a) : [...value, a]);
  const chip = (a: string, label: string) => <button key={a} type="button" aria-pressed={value.includes(a)} onClick={() => t(a)} className={clsx("chip", value.includes(a) ? "chip-accent" : "opacity-70 hover:opacity-100")}>{label}</button>;
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap gap-1">{chip("all", "Everyone")}{chip("role:admin", "Admins")}{chip("role:editor", "Editors")}{chip("role:viewer", "Viewers")}</div>
      <div className="flex flex-wrap gap-1">{(["receive", "produce", "test", "release", "ship"] as Duty[]).map((d) => chip(`duty:${d}`, DUTY[d].name))}</div>
      <select className="input h-8 w-auto text-[12.5px]" value="" aria-label="Add a person" onChange={(e) => e.target.value && t(`user:${e.target.value}`)}>
        <option value="">Add a person…</option>{members.map((u) => <option key={u.id} value={u.id}>{u.name}{value.includes(`user:${u.id}`) ? " ✓" : ""}</option>)}
      </select>
    </div>
  );
}

function ChannelPick({ value, onChange, channels }: { value: string[]; onChange: (v: string[]) => void; channels: Channel[] }) {
  if (!channels.length) return <p className="text-[12.5px] text-muted">No channels yet: add one under Channels. In-app and browser notifications always go out.</p>;
  return (
    <div className="flex flex-wrap gap-1">
      {channels.map((c) => { const on = value.includes(c.id); const I = CH_ICON[c.type] ?? Send; return (
        <button key={c.id} type="button" aria-pressed={on} onClick={() => onChange(on ? value.filter((x) => x !== c.id) : [...value, c.id])}
          className={clsx("chip", on ? "chip-accent" : "opacity-70 hover:opacity-100")}><I size={11} />{c.name}{!c.verified ? " (unverified)" : ""}</button>); })}
    </div>
  );
}

function RuleEditor({ rule, channels, members, onClose }: { rule: Rule; channels: Channel[]; members: User[]; onClose: () => void }) {
  const qc = useQueryClient();
  const me = useMe().data!;
  const cat = useCatalog().data;
  const [r, setR] = useState<Rule>(rule);
  const p = r.params;
  const setP = (x: Partial<Params>) => setR({ ...r, params: { ...p, ...x } });
  const src = cat?.sources.find((s) => s.key === p.query?.source);
  const save = useMutation({
    mutationFn: () => r.id ? api.send("PUT", `/alerts/rules/${r.id}`, r) : api.send("POST", "/alerts/rules", r),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["alert-rules"] }); onClose(); },
  });
  const preview = useMutation({ mutationFn: () => api.send<{ hits: { key: string; value: number; title: string }[]; total: number }>("POST", "/alerts/rules/preview", r) });
  const kinds = (Object.keys(KIND) as Kind[]).filter((k) => k !== "seal_backlog" || me.operator);
  const fieldErr = (f: string) => save.error instanceof ApiError && Array.isArray(save.error.problem.errors)
    ? (save.error.problem.errors as { field: string; message: string }[]).filter((x) => x.field.startsWith(f)).map((x) => x.message).join(" ") : "";
  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title={r.id ? `Edit “${rule.name}”` : "New alert rule"}>
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
        <L label="Name"><input className="input" required maxLength={80} value={r.name} onChange={(e) => setR({ ...r, name: e.target.value })} placeholder="e.g. Melt temperature too high" /></L>
        <L label="When">
          <select className="input" value={r.kind} onChange={(e) => {
            const k = e.target.value as Kind;
            const d: Record<Kind, Params> = { metric: { query: { source: "records", measure: "mass_kg", agg: "sum" }, windowMinutes: 60, op: ">", value: 0 },
              finding: { minSeverity: "blocker" }, receipt_overdue: { hours: 24 }, rejected: { hours: 48 }, telemetry_gap: { minutes: 30 }, seal_backlog: { minutes: 30 }, plan_limit: { percent: 80 } };
            setR({ ...r, kind: k, params: d[k] });
          }}>{kinds.map((k) => <option key={k} value={k}>{KIND[k].name}</option>)}</select>
          <span className="mt-1 block text-[12px] text-muted">{KIND[r.kind].explain}</span>
        </L>
        {r.kind === "metric" && p.query && (
          <div className="grid grid-cols-2 gap-3 rounded-xl bg-surface-2 p-3">
            <L label="Data from"><select className="input" value={p.query.source} onChange={(e) => { const s = cat?.sources.find((x) => x.key === e.target.value); setP({ query: { source: e.target.value, measure: s?.fields.find((f) => f.kind === "measure" && f.unit !== "count")?.key ?? "", agg: "avg" } }); }}>
              {cat?.sources.map((s) => <option key={s.key} value={s.key}>{s.label}</option>)}</select></L>
            <L label="Measure"><select className="input" value={p.query.measure} onChange={(e) => setP({ query: { ...p.query!, measure: e.target.value } })}>
              {src?.fields.filter((f) => f.kind === "measure").map((f) => <option key={f.key} value={f.key}>{f.label}</option>)}</select></L>
            <L label="Calculate"><select className="input" value={p.query.agg} onChange={(e) => setP({ query: { ...p.query!, agg: e.target.value as Query["agg"] } })}>
              {AGGS.map((a) => <option key={a.value} value={a.value}>{a.label}</option>)}</select></L>
            <L label="Separately for each" hint="optional"><select className="input" value={p.query.groupBy ?? ""} onChange={(e) => setP({ query: { ...p.query!, groupBy: e.target.value || undefined } })}>
              <option value="">Nothing (one value)</option>{src?.fields.filter((f) => f.kind === "dimension").map((f) => <option key={f.key} value={f.key}>{f.label}</option>)}</select></L>
            <L label="Over the last"><select className="input" value={p.windowMinutes} onChange={(e) => setP({ windowMinutes: Number(e.target.value) })}>
              {[[15, "15 minutes"], [60, "hour"], [240, "4 hours"], [1440, "24 hours"], [10080, "7 days"]].map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select></L>
            <div className="grid grid-cols-[80px_1fr] gap-2">
              <L label="Is"><select className="input" value={p.op} onChange={(e) => setP({ op: e.target.value })}>{[">", ">=", "<", "<="].map((o) => <option key={o}>{o}</option>)}</select></L>
              <L label="Value"><input className="input num" type="number" step="any" value={p.value ?? 0} onChange={(e) => setP({ value: Number(e.target.value) })} /></L>
            </div>
          </div>
        )}
        {r.kind === "finding" && <L label="Severity of the finding"><select className="input" value={p.minSeverity} onChange={(e) => setP({ minSeverity: e.target.value })}><option value="blocker">Blocking only</option><option value="warning">Warnings and blocking</option></select></L>}
        {(r.kind === "receipt_overdue" || r.kind === "rejected") && <L label={r.kind === "rejected" ? "Rejections in the last (hours)" : "Not received after (hours)"}><input className="input num" type="number" min={1} max={720} value={p.hours ?? 24} onChange={(e) => setP({ hours: Number(e.target.value) })} /></L>}
        {(r.kind === "telemetry_gap" || r.kind === "seal_backlog") && <L label="After (minutes)"><input className="input num" type="number" min={5} max={10080} value={p.minutes ?? 30} onChange={(e) => setP({ minutes: Number(e.target.value) })} /></L>}
        {r.kind === "plan_limit" && <L label="At (% of the plan)"><input className="input num" type="number" min={50} max={100} value={p.percent ?? 80} onChange={(e) => setP({ percent: Number(e.target.value) })} /></L>}
        {fieldErr("params") && <p className="-mt-2 text-[12px] text-critical">{fieldErr("params")}</p>}
        <div className="flex flex-wrap items-center gap-2">
          <button type="button" className="btn btn-sm" disabled={preview.isPending} onClick={() => preview.mutate()}>Preview now</button>
          {preview.data && <span className="text-[12.5px] text-text-2">{preview.data.total ? `Would fire for ${preview.data.total}: ${preview.data.hits.slice(0, 3).map((h) => h.title).join("; ")}${preview.data.total > 3 ? "…" : ""}` : "Nothing would fire right now."}</span>}
          {preview.error ? <ErrorBox error={preview.error} /> : null}
        </div>
        <div className="grid grid-cols-2 gap-3">
          <L label="Severity"><select className="input" value={r.severity} onChange={(e) => setR({ ...r, severity: e.target.value as Severity })}><option value="info">Info</option><option value="warning">Warning</option><option value="critical">Critical</option></select></L>
          <L label="Fire after" hint="the condition holds this long"><select className="input" value={r.forMinutes} onChange={(e) => setR({ ...r, forMinutes: Number(e.target.value) })}>
            {[[0, "at once"], [5, "5 minutes"], [15, "15 minutes"], [60, "1 hour"], [240, "4 hours"]].map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select></L>
          <L label="Remind every"><select className="input" value={r.repeatMinutes} onChange={(e) => setR({ ...r, repeatMinutes: Number(e.target.value) })}>
            {[[0, "never"], [60, "hour"], [240, "4 hours"], [1440, "day"]].map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select></L>
          <label className="flex items-end gap-2 pb-2 text-[13px]"><input type="checkbox" checked={r.notifyResolved} onChange={(e) => setR({ ...r, notifyResolved: e.target.checked })} /> Send the all-clear</label>
        </div>
        <div><span className="label">Tell</span><AudiencePick value={r.audience} onChange={(audience) => setR({ ...r, audience })} members={members} />
          {fieldErr("audience") && <p className="mt-1 text-[12px] text-critical">{fieldErr("audience")}</p>}</div>
        <div><span className="label">Also send to</span><ChannelPick value={r.channelIds} onChange={(channelIds) => setR({ ...r, channelIds })} channels={channels} /></div>
        <div>
          <span className="label">If nobody acknowledges <span className="font-normal text-muted">· escalation, up to 4 steps</span></span>
          {r.escalation.map((st, i) => (
            <div key={i} className="mb-2 rounded-xl border border-line p-3">
              <div className="mb-2 flex items-center gap-2 text-[13px]">After <input className="input h-8 w-20 num" type="number" min={1} value={st.afterMinutes}
                onChange={(e) => setR({ ...r, escalation: r.escalation.map((x, j) => (j === i ? { ...x, afterMinutes: Number(e.target.value) } : x)) })} /> minutes, also
                <button type="button" className="ml-auto text-muted hover:text-critical" aria-label="Remove step" onClick={() => setR({ ...r, escalation: r.escalation.filter((_, j) => j !== i) })}><Trash2 size={13} /></button></div>
              <AudiencePick value={st.audience} members={members} onChange={(audience) => setR({ ...r, escalation: r.escalation.map((x, j) => (j === i ? { ...x, audience } : x)) })} />
              <div className="mt-2"><ChannelPick value={st.channelIds} channels={channels} onChange={(channelIds) => setR({ ...r, escalation: r.escalation.map((x, j) => (j === i ? { ...x, channelIds } : x)) })} /></div>
            </div>
          ))}
          {r.escalation.length < 4 && <button type="button" className="btn btn-sm" onClick={() => setR({ ...r, escalation: [...r.escalation, { afterMinutes: 30 * (r.escalation.length + 1), channelIds: [], audience: ["role:admin"] }] })}><Siren size={13} /> Add an escalation step</button>}
        </div>
        {save.error && !fieldErr("") ? <ErrorBox error={save.error} /> : save.error ? <p className="text-[12px] text-critical">{fieldErr("")}</p> : null}
        <div className="flex justify-end gap-2"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={save.isPending}>Save rule</button></div>
      </form>
    </Modal>
  );
}

function ChannelsTab() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["alert-channels"], queryFn: () => api.get<{ channels: Channel[]; smtp: boolean; sms: boolean }>("/alerts/channels") });
  const [adding, setAdding] = useState(false);
  const [verifying, setVerifying] = useState<Channel | null>(null);
  const [secret, setSecret] = useState<string | null>(null);
  const del = useMutation({ mutationFn: (id: string) => api.send("DELETE", `/alerts/channels/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ["alert-channels"] }) });
  const test = useMutation({ mutationFn: (id: string) => api.send("POST", `/alerts/channels/${id}/test`), onSettled: () => qc.invalidateQueries({ queryKey: ["alert-channels"] }) });
  return (
    <Card pad={false} title="Channels" sub="Where alerts go outside ROADPASS. Secrets are kept in the vault, never in the database."
      action={<button className="btn btn-sm btn-primary" onClick={() => setAdding(true)}><Plus size={13} /> Add channel</button>}>
      {(del.error || test.error) ? <div className="p-4"><ErrorBox error={del.error ?? test.error} /></div> : null}
      {test.isSuccess && <div className="px-5 pt-3 text-[12.5px] text-[var(--good-ink)]">Test message sent.</div>}
      <div className="overflow-x-auto">
        <table className="table">
          <thead><tr><th>Channel</th><th>Destination</th><th>Status</th><th /></tr></thead>
          <tbody>
            {(q.data?.channels ?? []).map((c) => { const I = CH_ICON[c.type] ?? Send; return (
              <tr key={c.id}>
                <td><div className="flex items-center gap-2 font-medium"><I size={14} className="text-muted" />{c.name}</div><div className="text-[12px] text-muted">{CH_NAME[c.type]}</div></td>
                <td className="mono text-[12.5px] text-text-2">{c.config.to ?? c.config.url ?? c.config.host ?? c.config.chatId ?? "—"}</td>
                <td>{!c.verified ? <button className="chip chip-warning" onClick={() => setVerifying(c)}>Verify</button>
                  : c.lastStatus === "failed" ? <span className="chip chip-critical" title={c.lastError}><CircleAlert size={11} />failed {c.lastSentAt ? ago(c.lastSentAt) : ""}</span>
                  : c.lastStatus === "ok" ? <span className="chip chip-good">delivered {c.lastSentAt ? ago(c.lastSentAt) : ""}</span> : <span className="chip">ready</span>}</td>
                <td className="whitespace-nowrap text-right">
                  <button className="btn btn-sm" disabled={!c.verified || test.isPending} onClick={() => test.mutate(c.id)}>Send a test</button>
                  <button className="btn btn-sm btn-ghost" aria-label={`Delete ${c.name}`} onClick={() => { if (confirm(`Delete the channel “${c.name}”? Rules stop sending to it.`)) del.mutate(c.id); }}><Trash2 size={13} /></button>
                </td>
              </tr>); })}
            {!q.isLoading && !q.data?.channels.length && <tr><td colSpan={4} className="text-center text-muted">No channels yet. In-app and browser notifications work without one.</td></tr>}
          </tbody>
        </table>
      </div>
      {adding && <AddChannel smtp={!!q.data?.smtp} sms={!!q.data?.sms} onClose={() => setAdding(false)} onDone={(c, s) => { setAdding(false); void qc.invalidateQueries({ queryKey: ["alert-channels"] }); if (s) setSecret(s); if (!c.verified) setVerifying(c); }} />}
      {verifying && <VerifyChannel c={verifying} onClose={() => { setVerifying(null); void qc.invalidateQueries({ queryKey: ["alert-channels"] }); }} />}
      <Modal open={!!secret} onOpenChange={(o) => !o && setSecret(null)} title="Webhook signing secret" sub="Copy it now: it is shown once. Your receiver checks X-Roadpass-Signature (t=…,v1=HMAC-SHA256 of “t.body”) with it.">
        <div className="flex items-center gap-2 rounded-xl bg-surface-2 px-3 py-2.5"><span className="mono flex-1 break-all text-[12.5px]">{secret}</span>{secret && <Copy_ text={secret} label="Copy" />}</div>
      </Modal>
    </Card>
  );
}

function AddChannel({ smtp, sms, onClose, onDone }: { smtp: boolean; sms: boolean; onClose: () => void; onDone: (c: Channel, secret?: string) => void }) {
  const [type, setType] = useState("email");
  const name = "";
  const [cfg, setCfg] = useState<Record<string, string>>({});
  const [sec, setSec] = useState<Record<string, string>>({});
  const [own, setOwn] = useState(false);
  const m = useMutation({
    mutationFn: () => {
      const { _name, ...config } = cfg;
      return api.send<{ channel: Channel; secret?: { signingSecret: string } }>("POST", "/alerts/channels", { type, name: _name || name || CH_NAME[type], config, secret: sec });
    },
    onSuccess: (r) => onDone(r.channel, r.secret?.signingSecret),
  });
  const f = (k: string, label: string, ph = "", secret = false) => (
    <L label={label}><input className="input" type={secret ? "password" : "text"} placeholder={ph} autoComplete="off" value={(secret ? sec : cfg)[k] ?? ""}
      onChange={(e) => (secret ? setSec({ ...sec, [k]: e.target.value }) : setCfg({ ...cfg, [k]: e.target.value }))} /></L>
  );
  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title="Add a channel">
      <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <div className="grid grid-cols-3 gap-1.5">
          {Object.keys(CH_NAME).map((t) => { const I = CH_ICON[t]; return (
            <button key={t} type="button" aria-pressed={type === t} onClick={() => { setType(t); setCfg({}); setSec({}); setOwn(false); }}
              className={clsx("flex items-center gap-1.5 rounded-xl border px-2.5 py-2 text-[12.5px] font-medium", type === t ? "border-accent bg-accent-soft" : "border-line hover:bg-surface-2")}><I size={14} />{CH_NAME[t]}</button>); })}
        </div>
        {f("_name", "Name", `e.g. ${CH_NAME[type]} for the night shift`)}
        {type === "email" && <>{f("to", "Address", "alerts@company.com")}
          {(!smtp || own) && <div className="grid grid-cols-2 gap-2 rounded-xl bg-surface-2 p-3">{f("smtpHost", "Your SMTP server", "smtp.company.com")}{f("smtpPort", "Port", "587")}{f("smtpUser", "User")}{f("smtpPassword", "Password", "", true)}{f("from", "Sender", "ROADPASS <alerts@company.com>")}</div>}
          {smtp && <label className="flex items-center gap-2 text-[12.5px]"><input type="checkbox" checked={own} onChange={(e) => setOwn(e.target.checked)} /> Use our own mail server</label>}</>}
        {type === "webhook" && f("url", "URL", "https://erp.company.com/hooks/roadpass")}
        {(type === "slack" || type === "teams") && f("url", "Incoming webhook URL", type === "slack" ? "https://hooks.slack.com/services/…" : "https://….webhook.office.com/…", true)}
        {type === "sms" && <>{f("to", "Phone number", "+31612345678")}
          {(!sms || own) && <div className="grid grid-cols-2 gap-2 rounded-xl bg-surface-2 p-3">{f("accountSid", "Twilio account SID", "", true)}{f("authToken", "Twilio auth token", "", true)}{f("from", "Sender number", "+3197…")}</div>}
          {sms && <label className="flex items-center gap-2 text-[12.5px]"><input type="checkbox" checked={own} onChange={(e) => setOwn(e.target.checked)} /> Use our own Twilio account</label>}</>}
        {type === "telegram" && <>{f("chatId", "Chat id", "-1001234567890")}{f("botToken", "Bot token", "", true)}</>}
        {m.error ? <ErrorBox error={m.error} /> : null}
        <div className="flex justify-end gap-2"><button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={m.isPending}>Add channel</button></div>
      </form>
    </Modal>
  );
}

function VerifyChannel({ c, onClose }: { c: Channel; onClose: () => void }) {
  const [code, setCode] = useState("");
  const send = useMutation({ mutationFn: () => api.send("POST", `/alerts/channels/${c.id}/verify`) });
  const confirm_ = useMutation({ mutationFn: () => api.send("POST", `/alerts/channels/${c.id}/verify/confirm`, { code }), onSuccess: onClose });
  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title={`Verify ${c.name}`} sub={`We send a six-digit code to ${c.config.to}. Enter it to prove the ${c.type === "sms" ? "number" : "address"} is yours.`}>
      <div className="flex flex-col gap-3">
        <button className="btn w-fit" disabled={send.isPending} onClick={() => send.mutate()}>{send.isSuccess ? "Send again" : "Send the code"}</button>
        {send.error ? <ErrorBox error={send.error} /> : null}
        <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); confirm_.mutate(); }}>
          <input className="input num w-36 tracking-[0.3em]" inputMode="numeric" maxLength={6} placeholder="000000" value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))} aria-label="Code" />
          <button className="btn btn-primary" disabled={code.length !== 6 || confirm_.isPending}>Verify</button>
        </form>
        {confirm_.error ? <ErrorBox error={confirm_.error} /> : null}
      </div>
    </Modal>
  );
}

function HistoryTab() {
  const [days, setDays] = useState(30);
  const h = useQuery({ queryKey: ["alert-history"], queryFn: () => api.get<Event[]>("/alerts/history") });
  const st = useQuery({ queryKey: ["alert-stats", days], queryFn: () => api.get<{ fired: number; open: number; ackedPct: number; medianAckSeconds: number; flood: boolean; noisiest?: { ruleId: string; n: number }[] }>(`/alerts/stats?days=${days}`) });
  const rules = useQuery({ queryKey: ["alert-rules"], queryFn: () => api.get<{ rules: Rule[] }>("/alerts/rules") });
  const ruleName = useMemo(() => new Map((rules.data?.rules ?? []).map((r) => [r.id, r.name])), [rules.data]);
  const s = st.data;
  return (
    <div className="flex flex-col gap-4">
      {s?.flood && <div className="rounded-2xl bg-[var(--critical-soft)] px-4 py-3 text-[13px] text-critical">More than ten alerts fired in the last ten minutes. Check for a noisy rule or an outage.</div>}
      <div className="flex items-center justify-end"><select className="input h-8 w-auto" value={days} onChange={(e) => setDays(Number(e.target.value))} aria-label="Period"><option value={7}>Last 7 days</option><option value={30}>Last 30 days</option><option value={90}>Last 90 days</option></select></div>
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <Stat help="How many times an alert started firing in the period." label="Fired" loading={st.isLoading} value={num(s?.fired ?? 0)} hint={`${s?.open ?? 0} open now`} />
        <Stat help="Share of fired alerts someone acknowledged: they saw it and took charge." label="Acknowledged" loading={st.isLoading} value={`${num(s?.ackedPct ?? 0, 0)} %`} hint="of firings" />
        <Stat help="Half of the acknowledged alerts were taken in hand faster than this." label="Median time to acknowledge" loading={st.isLoading} value={s?.medianAckSeconds ? `${num(s.medianAckSeconds / 60, 0)} min` : "—"} />
        <Stat help="The rule that fired most often. A noisy rule may need a higher threshold, a hold time or a maintenance window." label="Noisiest rule" loading={st.isLoading} value={s?.noisiest?.[0] ? num(s.noisiest[0].n) : "—"} hint={s?.noisiest?.[0] ? ruleName.get(s.noisiest[0].ruleId) : "no firings"} />
      </div>
      <Card pad={false} title="History" sub="Every firing, reminder, escalation and all-clear, with where it was delivered.">
        <div className="max-h-[620px] overflow-auto scroll-thin">
          <table className="table text-[12.5px]">
            <thead><tr><th>When</th><th>What</th><th>Event</th><th>Delivered to</th></tr></thead>
            <tbody>
              {(h.data ?? []).map((e) => (
                <tr key={e.id}>
                  <td className="whitespace-nowrap text-text-2">{dateTime(e.at)}</td>
                  <td><div className="font-medium">{e.title}</div><div className="text-muted">{ruleName.get(e.ruleId) ?? e.ruleId}</div></td>
                  <td><span className={clsx("chip", e.state === "resolved" ? "chip-good" : SEV[e.severity]?.chip)}>{e.state}</span></td>
                  <td><div className="flex flex-wrap gap-1">{e.deliveries.map((d, i) => (
                    <span key={i} className={clsx("chip", d.ok ? "" : "chip-critical")} title={d.error}>{d.type === "in-app" ? `${d.to ?? 0} in-app` : d.type === "push" ? `${d.to ?? 0} browsers` : d.channel}{d.ok ? "" : " ✕"}</span>))}</div></td>
                </tr>
              ))}
              {!h.isLoading && !h.data?.length && <tr><td colSpan={4} className="text-center text-muted">Nothing has fired yet.</td></tr>}
            </tbody>
          </table>
        </div>
      </Card>
    </div>
  );
}

function MaintenanceTab({ admin }: { admin: boolean }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["alert-maint"], queryFn: () => api.get<Maintenance[]>("/alerts/maintenance") });
  const local = (d: Date) => new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  const [w, setW] = useState({ startsAt: local(new Date()), endsAt: local(new Date(Date.now() + 4 * 3600_000)), note: "" });
  const add = useMutation({ mutationFn: () => api.send("POST", "/alerts/maintenance", { startsAt: new Date(w.startsAt).toISOString(), endsAt: new Date(w.endsAt).toISOString(), note: w.note }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["alert-maint"] }) });
  const del = useMutation({ mutationFn: (id: string) => api.send("DELETE", `/alerts/maintenance/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ["alert-maint"] }) });
  return (
    <Card title="Maintenance windows" sub="During a window no new alert fires (planned stops, migrations). Conditions are checked again when it ends.">
      {admin && (
        <form className="mb-4 flex flex-wrap items-end gap-2" onSubmit={(e) => { e.preventDefault(); add.mutate(); }}>
          <L label="From"><input className="input h-9" type="datetime-local" value={w.startsAt} onChange={(e) => setW({ ...w, startsAt: e.target.value })} /></L>
          <L label="Until"><input className="input h-9" type="datetime-local" value={w.endsAt} onChange={(e) => setW({ ...w, endsAt: e.target.value })} /></L>
          <L label="Why"><input className="input h-9 w-64" maxLength={280} value={w.note} onChange={(e) => setW({ ...w, note: e.target.value })} placeholder="Planned extruder overhaul" /></L>
          <button className="btn btn-primary h-9" disabled={add.isPending}><Plus size={14} /> Add window</button>
        </form>
      )}
      {add.error ? <ErrorBox error={add.error} /> : null}
      <ul className="flex flex-col gap-1.5">
        {(q.data ?? []).map((m) => (
          <li key={m.id} className="flex items-center gap-3 rounded-xl bg-surface-2 px-3 py-2 text-[13px]">
            <AlarmClock size={14} className="text-muted" /><span className="flex-1">{dateTime(m.startsAt)} → {dateTime(m.endsAt)}{m.note ? ` · ${m.note}` : ""}</span>
            {admin && <button className="text-muted hover:text-critical" aria-label="End this window" onClick={() => del.mutate(m.id)}><Trash2 size={13} /></button>}
          </li>
        ))}
        {!q.isLoading && !q.data?.length && <li className="text-[13px] text-muted">No windows planned.</li>}
      </ul>
    </Card>
  );
}
