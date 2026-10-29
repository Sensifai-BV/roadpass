import { lazy, Suspense, useMemo, useState, type ReactNode } from "react";
import { Navigate, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle, Building2, CheckCircle2, Clock3, CreditCard, FileText, PauseCircle, Plus, Search, UserPlus, Users,
} from "lucide-react";
import clsx from "clsx";
import { api, qs } from "../lib/api";
import { useMe, useMeta } from "../lib/hooks";
import { roleTitle } from "../lib/roles";
import type { AuditEntry, Org, OrgKind, User } from "../lib/types";
import { KIND } from "../lib/vocab";
import { COUNTRIES, countryName } from "../lib/countries";
import { ago, compact, date, dateTime, num } from "../lib/format";
import { BarList } from "../components/charts/BarList";
import { ActivityPicker } from "../components/ActivityPicker";
import { MailTab } from "./PlatformMail";
import { Card, Copy_, Empty, ErrorBox, InfoTip, Modal, PageHeader, Skel, Stat } from "../components/ui";

// The SaaS manager's console: organisations, users, plans and invoices, and the
// platform's own health. Only the platform operator's admins reach it.

interface Plan { id: string; name: string; description: string; priceCents: number; currency: string; seats: number; recordsPerMonth: number; position: number; active: boolean }
interface Subscription { orgId: string; planId: string; status: "trial" | "active" | "past_due" | "cancelled"; trialEndsAt?: string; updatedAt: string }
interface OrgRow extends Org { members: number; subscription?: Subscription; usage: { records: number; seats: number }; records: number }
interface UserRow extends User { orgName: string; orgKind: OrgKind; orgDemo?: boolean; disabledAt?: string }
interface Invoice { id: string; number?: string; orgId: string; period: string; planId: string; currency: string; amountCents: number; status: "draft" | "issued" | "paid" | "void"; issuedAt?: string; paidAt?: string; lines: { text: string; amountCents: number }[] }
interface Overview { orgs: Record<string, number>; members: number; recordsThisMonth: number; mrrCents: number; trials: number; pastDue: number; overLimit: number; withoutPlan: number }

type Tab = "overview" | "organisations" | "users" | "billing" | "messages" | "email" | "system" | "audit" | "privacy";
const TABS: { key: Tab; title: string; sub: string }[] = [
  { key: "overview", title: "Platform overview", sub: "Customers, usage, revenue and the health of this ROADPASS installation." },
  { key: "organisations", title: "Organisations", sub: "Every customer organisation: verify, suspend, change plans and invite their admins." },
  { key: "users", title: "Users", sub: "Every account on the platform, by organisation and role." },
  { key: "billing", title: "Billing", sub: "Plans, subscriptions and invoices." },
  { key: "messages", title: "Messages", sub: "What visitors sent through the contact form on the public site." },
  { key: "email", title: "Email delivery", sub: "The mail server for every system e-mail: invitations, password resets, alerts, reports and invoices." },
  { key: "system", title: "System health", sub: "The services behind the platform, refreshed every 15 seconds." },
  { key: "audit", title: "Audit log", sub: "Every change across the platform, newest first." },
  { key: "privacy", title: "Data protection", sub: "Former members of every organisation, data export and erasure, and how long data are kept." },
];

interface ContactMsg { id: string; name: string; email: string; company?: string; topic: string; message: string; createdAt: string; handledAt?: string }

const TOPIC: Record<string, string> = { demo: "Demo request", pilot: "Pilot", integration: "Integrations", support: "Support", press: "Press", general: "General" };

function MessagesTab() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["platform-contact"], queryFn: () => api.get<ContactMsg[]>("/platform/contact") });
  const [view, setView] = useState<"open" | "handled" | "all">("open");
  const [text, setText] = useState("");
  const mark = useMutation({
    mutationFn: (m: ContactMsg) => api.send("POST", `/platform/contact/${m.id}/handled`, { handled: !m.handledAt }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["platform-contact"] }),
  });
  const all = q.data ?? [];
  const list = useMemo(() => {
    const needle = text.trim().toLowerCase();
    return all.filter((m) => (view === "all" || (view === "open") === !m.handledAt)
      && (!needle || `${m.name} ${m.email} ${m.company ?? ""} ${m.message}`.toLowerCase().includes(needle)));
  }, [all, view, text]);
  if (q.isLoading) return <Skel h={240} />;
  if (q.isError) return <ErrorBox error={q.error} />;
  const open = all.filter((m) => !m.handledAt).length;
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-3 gap-4 max-sm:grid-cols-1">
        <Stat label="Open" value={String(open)} hint="waiting for a reply" />
        <Stat label="Handled" value={String(all.length - open)} hint="answered or closed" />
        <Stat label="Received" value={String(all.length)} hint="through the public contact form" />
      </div>
      <Card title="Contact form" sub="Messages sent from the public contact page. Reply by e-mail, then mark the message handled."
        action={<div className="flex flex-wrap items-center gap-2">
          <div className="relative"><Search size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted" />
            <input className="input !h-8 w-52 !pl-8" placeholder="Search messages" aria-label="Search messages" value={text} onChange={(e) => setText(e.target.value)} /></div>
          <div className="seg" role="group" aria-label="Show">
            {(["open", "handled", "all"] as const).map((v) => <button key={v} type="button" aria-pressed={view === v} onClick={() => setView(v)}>{v === "open" ? "Open" : v === "handled" ? "Handled" : "All"}</button>)}
          </div></div>}>
        {list.length === 0 ? <Empty title={all.length === 0 ? "No messages yet" : "Nothing matches"}>{all.length === 0 ? "Messages from the contact page appear here." : "Try another filter."}</Empty> : (
          <ul className="divide-y divide-[var(--line)]" data-testid="contact-list">
            {list.map((m) => (
              <li key={m.id} className="py-4" data-testid="contact-message">
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <b>{m.name}</b>
                  <a className="text-accent-ink hover:underline" href={`mailto:${m.email}?subject=${encodeURIComponent("Re: your message to ROADPASS")}`}>{m.email}</a>
                  {m.company && <span className="text-text-2">{m.company}</span>}
                  <span className="chip chip-accent">{TOPIC[m.topic] ?? m.topic}</span>
                  <span className="text-[12px] text-muted">{dateTime(m.createdAt)}</span>
                  {m.handledAt && <span className="chip chip-good">Handled {ago(m.handledAt)}</span>}
                  <button type="button" className="btn btn-sm ml-auto" disabled={mark.isPending} onClick={() => mark.mutate(m)}>{m.handledAt ? "Reopen" : "Mark handled"}</button>
                </div>
                <p className={clsx("mt-2 max-w-3xl whitespace-pre-wrap text-[13.5px] leading-relaxed", m.handledAt && "text-muted")}>{m.message}</p>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

const DataProtection = lazy(() => import("./Compliance").then((m) => ({ default: m.DataProtection })));

export const money = (cents: number, currency = "EUR") =>
  new Intl.NumberFormat("en-GB", { style: "currency", currency, maximumFractionDigits: cents % 100 ? 2 : 0 }).format(cents / 100);
const limit = (n: number) => (n < 0 ? "unlimited" : num(n));

export default function Platform() {
  const me = useMe().data!;
  const nav = useNavigate();
  const [params] = useSearchParams();
  const { tab: path } = useParams();
  const t = TABS.find((x) => x.key === (path ?? params.get("tab"))) ?? TABS[0];
  if (!me.operator || me.user.role !== "admin") return <Navigate to="/" replace />;
  return (
    <div>
      <PageHeader eyebrow="Platform" title={t.title} sub={t.sub} />
      {params.get("welcome") && (
        <div className="mb-4 flex items-start gap-3 rounded-2xl bg-accent-soft px-4 py-3 text-[13px]">
          <CheckCircle2 size={18} className="mt-0.5 shrink-0 text-accent-ink" />
          <div><b>The platform is ready.</b> Create the first organisation in the chain and invite its admin; they invite their own team.</div>
        </div>
      )}
      {t.key === "overview" && <OverviewTab go={(x) => nav(`/platform/${x}`)} />}
      {t.key === "organisations" && <OrganisationsTab />}
      {t.key === "users" && <UsersTab />}
      {t.key === "billing" && <BillingTab />}
      {t.key === "messages" && <MessagesTab />}
      {t.key === "email" && <MailTab />}
      {t.key === "system" && <SystemTab />}
      {t.key === "audit" && <AuditTab />}
      {t.key === "privacy" && <Suspense fallback={<Skel h={300} />}><DataProtection platform /></Suspense>}
    </div>
  );
}

function usePlans() {
  return useQuery({ queryKey: ["plans"], queryFn: () => api.get<Plan[]>("/platform/plans") });
}
function useOrgRows() {
  return useQuery({ queryKey: ["platform-orgs"], queryFn: () => api.get<OrgRow[]>("/platform/orgs") });
}

function OverviewTab({ go }: { go: (t: Tab) => void }) {
  const ov = useQuery({ queryKey: ["platform-overview"], queryFn: () => api.get<Overview>("/platform/overview") });
  const orgs = useOrgRows();
  const plans = usePlans();
  const o = ov.data;
  const planOf = new Map((plans.data ?? []).map((p) => [p.id, p]));
  const attention = (orgs.data ?? []).flatMap((x) => {
    const out: { org: OrgRow; why: string; tone: "warning" | "critical" }[] = [];
    if (x.kind === "operator") return out;
    if (x.status === "pending") out.push({ org: x, why: "Waiting for verification: its records are not sealed yet", tone: "warning" });
    if (x.subscription?.status === "past_due") out.push({ org: x, why: "Payment past due", tone: "critical" });
    const p = x.subscription && planOf.get(x.subscription.planId);
    if (p && p.seats >= 0 && x.usage.seats > p.seats) out.push({ org: x, why: `${x.usage.seats} members on a ${p.seats}-seat plan`, tone: "warning" });
    if (p && p.recordsPerMonth >= 0 && x.usage.records > p.recordsPerMonth) out.push({ org: x, why: `${num(x.usage.records)} records this month, plan includes ${num(p.recordsPerMonth)}`, tone: "warning" });
    if (!x.subscription) out.push({ org: x, why: "No plan assigned", tone: "warning" });
    return out;
  });
  const health = useHealth();
  const customers = (orgs.data ?? []).filter((x) => x.kind !== "operator");
  const kinds = new Map<OrgKind, number>();
  for (const x of customers) kinds.set(x.kind, (kinds.get(x.kind) ?? 0) + 1);
  const byKind = [...kinds.entries()].sort((a, b) => b[1] - a[1]).map(([k, n]) => ({ key: k, label: KIND[k]?.plural ?? k, value: n, color: KIND[k]?.color }));
  const busiest = customers.filter((x) => x.usage.records > 0).sort((a, b) => b.usage.records - a.usage.records).slice(0, 6).map((x) => {
    const p = x.subscription && planOf.get(x.subscription.planId);
    return { key: x.id, label: x.name, value: x.usage.records, sub: p ? (p.recordsPerMonth < 0 ? `${p.name} · unlimited` : `${p.name} · ${num(p.recordsPerMonth)} included`) : "no plan" };
  });
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <Stat help="Customer organisations by status: verified ones have their records sealed; pending ones wait for you to verify them." label="Organisations" icon={<Building2 size={14} />} loading={ov.isLoading} value={num(Object.values(o?.orgs ?? {}).reduce((a, b) => a + b, 0))}
          hint={o ? `${o.orgs.verified ?? 0} verified · ${o.orgs.pending ?? 0} pending · ${o.orgs.suspended ?? 0} suspended` : undefined} />
        <Stat help="Active accounts across all organisations; removed members are not counted." label="Members" icon={<Users size={14} />} loading={ov.isLoading} value={num(o?.members ?? 0)} hint="active accounts" />
        <Stat help="Operational records submitted this month, which is what plans bill. Simulated demo data is not counted." label="Records this month" icon={<FileText size={14} />} loading={ov.isLoading} value={compact(o?.recordsThisMonth ?? 0)} hint="operational only; simulated data is not billed" />
        <Stat help="Sum of the monthly prices of active and past-due subscriptions. Trials are not counted." label="Monthly recurring revenue" icon={<CreditCard size={14} />} loading={ov.isLoading} value={money(o?.mrrCents ?? 0)}
          hint={o ? `${o.trials} on trial · ${o.pastDue} past due` : undefined} />
      </div>
      <div className="grid gap-4 xl:grid-cols-3">
        <Card title="Customers by type" sub="Organisations on the platform, by their step in the chain.">
          {orgs.isLoading ? <Skel h={160} /> : <BarList items={byKind} format={(v) => num(v)} />}
        </Card>
        <Card title="Busiest this month" sub="Operational records submitted, against each plan's allowance.">
          {orgs.isLoading ? <Skel h={160} /> : <BarList items={busiest} format={(v) => num(v)} color="var(--s2)" />}
        </Card>
        <Card title="Services" action={<button className="btn btn-sm" onClick={() => go("system")}>Details</button>}><Services health={health} /></Card>
      </div>
      <Card title="Needs attention" sub="Pending verifications, payments and plan limits." pad={false}
        action={<button className="btn btn-sm" onClick={() => go("organisations")}>All organisations</button>}>
        {orgs.isLoading ? <div className="p-4"><Skel h={80} /></div> : attention.length === 0 ? <Empty title="All clear">Nothing waits for you.</Empty> : (
          <ul className="divide-y divide-line">
            {attention.slice(0, 12).map((a, i) => (
              <li key={i} className="flex items-center gap-3 px-5 py-3 text-[13px]">
                <AlertTriangle size={15} className={a.tone === "critical" ? "text-critical" : "text-warning"} aria-hidden />
                <span className="font-medium">{a.org.name}</span><span className="text-text-2">{a.why}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

function StatusChip({ s }: { s: Org["status"] }) {
  return <span className={clsx("chip", s === "verified" ? "chip-good" : s === "pending" ? "chip-warning" : "chip-critical")}>
    {s === "verified" ? <CheckCircle2 size={12} /> : s === "pending" ? <Clock3 size={12} /> : <PauseCircle size={12} />}{s}</span>;
}

function OrganisationsTab() {
  const qc = useQueryClient();
  const orgs = useOrgRows();
  const plans = usePlans();
  const [creating, setCreating] = useState(false);
  const [filter, setFilter] = useState("");
  const [link, setLink] = useState<{ org: string; link: string; emailed?: boolean } | null>(null);
  const [inviteFor, setInviteFor] = useState<OrgRow | null>(null);
  const refresh = () => { void qc.invalidateQueries({ queryKey: ["platform-orgs"] }); void qc.invalidateQueries({ queryKey: ["platform-overview"] }); void qc.invalidateQueries({ queryKey: ["orgs"] }); };
  const status = useMutation({
    mutationFn: ({ id, s, reason }: { id: string; s: string; reason?: string }) => api.send("POST", `/platform/orgs/${id}/status`, { status: s, reason: reason ?? "" }),
    onSuccess: refresh,
  });
  const sub = useMutation({
    mutationFn: ({ id, planId, s }: { id: string; planId: string; s: string }) => api.send("PUT", `/platform/orgs/${id}/subscription`, { planId, status: s }),
    onSuccess: refresh,
  });
  const f = filter.toLowerCase();
  const rows = (orgs.data ?? []).filter((o) => o.kind !== "operator" && (!f || o.name.toLowerCase().includes(f) || activities(o).some((k) => KIND[k].name.toLowerCase().includes(f))));
  const planOf = new Map((plans.data ?? []).map((p) => [p.id, p]));
  return (
    <Card pad={false} title="Organisations" sub="Create a company, verify it before its records are sealed, set its plan, and invite its first admin."
      action={<div className="flex items-center gap-2">
        <div className="relative"><Search size={13} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted" />
          <input className="input h-8 w-48 pl-7" placeholder="Name or activity" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter organisations" /></div>
        <button className="btn btn-sm btn-primary" onClick={() => setCreating(true)}><Plus size={13} /> New organisation</button>
      </div>}>
      {orgs.error ? <div className="p-4"><ErrorBox error={orgs.error} /></div> : null}
      {status.error || sub.error ? <div className="p-4"><ErrorBox error={status.error ?? sub.error} /></div> : null}
      <div className="overflow-x-auto">
        <table className="table">
          <thead><tr><th>Organisation</th><th>Status</th><th>Plan</th><th className="text-right">Members</th><th className="text-right">Records this month</th><th className="text-right">All records</th><th /></tr></thead>
          <tbody>
            {orgs.isLoading && <tr><td colSpan={7}><Skel h={120} /></td></tr>}
            {rows.map((o) => {
              const p = o.subscription && planOf.get(o.subscription.planId);
              const overSeats = p && p.seats >= 0 && o.members > p.seats;
              const overRecords = p && p.recordsPerMonth >= 0 && o.usage.records > p.recordsPerMonth;
              return (
                <tr key={o.id}>
                  <td><div className="font-medium">{o.name}</div>
                    <div className="mt-1 flex flex-wrap gap-1" aria-label={`Activities of ${o.name}`}>{activities(o).map((k, i) => {
                      const K = KIND[k];
                      return <span key={k} className={clsx("chip inline-flex items-center gap-1", i === 0 && "font-medium")} title={i === 0 ? "Main activity" : "Also does"}><K.icon size={12} style={{ color: K.color }} aria-hidden />{K.name}</span>;
                    })}</div>
                    <div className="mt-1 text-[12px] text-muted">{[o.city, countryName(o.country)].filter(Boolean).join(", ")}</div></td>
                  <td><StatusChip s={o.status} />{o.provenance === "Simulated" && <span className="chip chip-warning ml-1" title="Test organisation: its records are labelled Simulated">Test</span>}</td>
                  <td>
                    <select className="input h-8 w-auto min-w-[150px] text-[12.5px]" disabled={o.demo} aria-label={`Plan of ${o.name}`} value={o.subscription ? `${o.subscription.planId}|${o.subscription.status}` : ""}
                      onChange={(e) => { const [planId, s] = e.target.value.split("|"); if (planId) sub.mutate({ id: o.id, planId, s }); }}>
                      <option value="">No plan</option>
                      {(plans.data ?? []).flatMap((pl) => (["trial", "active", "past_due", "cancelled"] as const).map((s) => (
                        <option key={`${pl.id}|${s}`} value={`${pl.id}|${s}`}>{pl.name} · {s.replace("_", " ")}</option>
                      )))}
                    </select>
                  </td>
                  <td className={clsx("num text-right", overSeats && "font-semibold text-warning")}>{num(o.members)}{p && p.seats >= 0 ? <span className="text-muted"> / {p.seats}</span> : null}</td>
                  <td className={clsx("num text-right", overRecords && "font-semibold text-warning")}>{num(o.usage.records)}{p && p.recordsPerMonth >= 0 ? <span className="text-muted"> / {compact(p.recordsPerMonth)}</span> : null}</td>
                  <td className="num text-right text-text-2">{compact(o.records)}</td>
                  <td className="whitespace-nowrap text-right">
                    {o.demo && <span className="chip" title="Created and reset by the server; it cannot be changed, suspended or deleted here.">Managed automatically</span>}
                    {!o.demo && <button className="btn btn-sm btn-ghost" onClick={() => setInviteFor(o)} aria-label={`Invite an admin to ${o.name}`}><UserPlus size={13} /></button>}
                    {!o.demo && o.status !== "verified" && <button className="btn btn-sm" onClick={() => status.mutate({ id: o.id, s: "verified" })}>{o.status === "suspended" ? "Reactivate" : "Verify"}</button>}
                    {!o.demo && o.status === "verified" && <button className="btn btn-sm" onClick={() => { if (confirm(`Suspend ${o.name}? Its keys are revoked on the ledger; past signatures stay valid.`)) status.mutate({ id: o.id, s: "suspended", reason: "Suspended by the platform manager" }); }}>Suspend</button>}
                  </td>
                </tr>
              );
            })}
            {!orgs.isLoading && rows.length === 0 && <tr><td colSpan={7} className="text-center text-muted">No organisations yet.</td></tr>}
          </tbody>
        </table>
      </div>
      {creating && <CreateOrg plans={plans.data ?? []} onClose={() => setCreating(false)} onDone={(org, l, emailed) => { setCreating(false); setLink({ org, link: l, emailed }); refresh(); }} />}
      {inviteFor && <InviteAdmin org={inviteFor} onClose={() => setInviteFor(null)} onDone={(l, emailed) => { setLink({ org: inviteFor.name, link: l, emailed }); setInviteFor(null); }} />}
      <Modal open={!!link} onOpenChange={(o) => !o && setLink(null)} title={link?.emailed ? "Invitation sent" : "Invitation link"} sub={link?.emailed
        ? `The new admin of ${link?.org ?? ""} has it by e-mail. You can also send them this link; it works once and expires in 7 days.`
        : `Send this to the new admin of ${link?.org ?? ""}. It works once and expires in 7 days. When they sign in, a guide walks them through sites, team and data.`}>
        <div className="flex items-center gap-2 rounded-xl bg-surface-2 px-3 py-2.5">
          <span className="mono flex-1 break-all text-[12.5px]">{link?.link}</span>{link && <Copy_ text={link.link} label="Copy link" />}
        </div>
        <div className="mt-4 flex justify-end"><button className="btn btn-primary" onClick={() => setLink(null)}>Done</button></div>
      </Modal>
    </Card>
  );
}

/** Every activity of an organisation, the main one first. */
function activities(o: Org): OrgKind[] {
  return [o.kind, ...(o.kinds ?? []).filter((k) => k !== o.kind)];
}

/** A labelled control: the <label> wraps it, so the label names the input. */
function Field({ label, children, err }: { label: string; children: ReactNode; err?: string }) {
  return <label className="block"><span className="label">{label}</span>{children}{err && <p className="mt-1 text-[12px] text-critical">{err}</p>}</label>;
}

function CreateOrg({ plans, onClose, onDone }: { plans: Plan[]; onClose: () => void; onDone: (org: string, link: string, emailed: boolean) => void }) {
  const [o, setO] = useState({ name: "", kind: "" as OrgKind | "", kinds: [] as OrgKind[], taxId: "", country: "", region: "", city: "", website: "", planId: "business", trial: true, verified: false, test: false, adminEmail: "" });
  const m = useMutation({
    mutationFn: () => api.send<{ link: string; emailed: boolean }>("POST", "/platform/orgs", o),
    onSuccess: (r) => onDone(o.name, r.link, r.emailed),
  });
  const set = (k: keyof typeof o, v: unknown) => setO({ ...o, [k]: v });
  return (
    <Modal open width={680} onOpenChange={(x) => !x && onClose()} title="New organisation" sub="The company gets its own workspace and signing key. Its first admin receives an invitation and is guided through setting up sites, team and data.">
      <form className="grid grid-cols-2 gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <div className="col-span-2"><Field label="Company name"><input className="input" required value={o.name} onChange={(e) => set("name", e.target.value)} /></Field></div>
        <div className="col-span-2" role="group" aria-label="Role in the supply chain">
          <span className="label">Role in the supply chain</span>
          <ActivityPicker value={{ kind: o.kind, kinds: o.kinds }} onChange={(v) => setO({ ...o, kind: v.kind, kinds: v.kinds })} />
        </div>
        <Field label="Country"><select className="input" required value={o.country} onChange={(e) => set("country", e.target.value)}>
          <option value="">Choose…</option>{COUNTRIES.map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}</select></Field>
        <Field label="Region / state"><input className="input" value={o.region} onChange={(e) => set("region", e.target.value)} /></Field>
        <Field label="City"><input className="input" value={o.city} onChange={(e) => set("city", e.target.value)} /></Field>
        <Field label="Tax / VAT / registration no."><input className="input" value={o.taxId} onChange={(e) => set("taxId", e.target.value)} /></Field>
        <Field label="Plan"><select className="input" value={o.planId} onChange={(e) => set("planId", e.target.value)}>
          <option value="">No plan yet</option>{plans.map((p) => <option key={p.id} value={p.id}>{p.name} · {p.priceCents ? `${money(p.priceCents, p.currency)}/month` : "custom"}</option>)}</select></Field>
        <div className="flex flex-col justify-end gap-1.5 text-[13px]">
          <label className="flex items-center gap-2"><input type="checkbox" checked={o.trial} onChange={(e) => set("trial", e.target.checked)} /> 30-day trial <InfoTip text="Free for 30 days, then the plan's price. Usage counts operational records only." /></label>
          <label className="flex items-center gap-2"><input type="checkbox" checked={o.verified} onChange={(e) => set("verified", e.target.checked)} /> Already verified <InfoTip text="Tick only if you have checked the company's identity (know-your-business). Verified companies get their signing key on the ledger and their records sealed. Otherwise verify it later from the list." /></label>
          <label className="flex items-center gap-2"><input type="checkbox" checked={o.test} onChange={(e) => set("test", e.target.checked)} /> Test organisation <InfoTip text="For trials, training and walkthroughs with made-up data. Its records are labelled Simulated, never Operational, on every passport and report. This cannot be undone." /></label>
        </div>
        <div className="col-span-2"><Field label="First admin's e-mail"><input className="input" type="email" required placeholder="name@company.com" value={o.adminEmail} onChange={(e) => set("adminEmail", e.target.value)} /></Field></div>
        {m.error ? <div className="col-span-2"><ErrorBox error={m.error} /></div> : null}
        <div className="col-span-2 flex justify-end gap-2"><button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={m.isPending || !o.kind}>Create and invite</button></div>
      </form>
    </Modal>
  );
}

function InviteAdmin({ org, onClose, onDone }: { org: OrgRow; onClose: () => void; onDone: (link: string, emailed: boolean) => void }) {
  const [email, setEmail] = useState("");
  const m = useMutation({ mutationFn: () => api.send<{ link: string; emailed: boolean }>("POST", `/platform/orgs/${org.id}/invitations`, { email, role: "admin" }), onSuccess: (r) => onDone(r.link, r.emailed) });
  return (
    <Modal open onOpenChange={(x) => !x && onClose()} title={`Invite an admin to ${org.name}`}>
      <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <input className="input" type="email" required placeholder="name@company.com" value={email} onChange={(e) => setEmail(e.target.value)} aria-label="E-mail" />
        <button className="btn btn-primary" disabled={m.isPending}><UserPlus size={14} /> Invite</button>
      </form>
      {m.error ? <div className="mt-3"><ErrorBox error={m.error} /></div> : null}
    </Modal>
  );
}

function UsersTab() {
  const meta = useMeta();
  const qc = useQueryClient();
  const me = useMe().data!;
  const [q, setQ] = useState("");
  const [disabled, setDisabled] = useState(false);
  const users = useQuery({ queryKey: ["platform-users", q, disabled], queryFn: ({ signal }) => api.get<UserRow[]>(`/platform/users${qs({ q, disabled: disabled ? 1 : undefined })}`, signal) });
  const act = useMutation({
    mutationFn: ({ id, off }: { id: string; off: boolean }) => api.send("POST", `/platform/users/${id}/status`, { disabled: off }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["platform-users"] }),
  });
  return (
    <Card pad={false} title="Users" sub="Everyone with an account, in every organisation. Disabling an account also revokes the person's signing key."
      action={<div className="flex items-center gap-3">
        <label className="flex items-center gap-1.5 text-[12.5px] text-text-2"><input type="checkbox" checked={disabled} onChange={(e) => setDisabled(e.target.checked)} /> Include disabled</label>
        <div className="relative"><Search size={13} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted" />
          <input className="input h-8 w-56 pl-7" placeholder="Name or e-mail" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search users" /></div>
      </div>}>
      {act.error ? <div className="p-4"><ErrorBox error={act.error} /></div> : null}
      <div className="max-h-[640px] overflow-auto scroll-thin">
        <table className="table">
          <thead><tr><th>Name</th><th>Organisation</th><th>Role</th><th>Last sign-in</th><th /></tr></thead>
          <tbody>
            {users.isLoading && <tr><td colSpan={5}><Skel h={120} /></td></tr>}
            {(users.data ?? []).map((u) => (
              <tr key={u.id} className={u.disabledAt ? "opacity-55" : undefined}>
                <td><div className="font-medium">{u.name}{u.demo && <span className="chip ml-1.5">demo</span>}</div><div className="text-[12px] text-muted">{u.email}</div></td>
                <td className="text-text-2">{u.orgName}<div className="text-[12px] text-muted">{KIND[u.orgKind]?.name}</div></td>
                <td><span className="chip">{roleTitle(meta.data?.jobRoles?.[u.orgKind] ?? [], u.role, u.duties)}</span></td>
                <td className="text-text-2">{u.lastLoginAt ? ago(u.lastLoginAt) : "never"}</td>
                <td className="text-right">{u.id !== me.user.id && !u.orgDemo && (u.disabledAt
                  ? <button className="btn btn-sm" onClick={() => act.mutate({ id: u.id, off: false })}>Enable</button>
                  : <button className="btn btn-sm" onClick={() => { if (confirm(`Disable ${u.name}? They are signed out and their signing key is revoked.`)) act.mutate({ id: u.id, off: true }); }}>Disable</button>)}</td>
              </tr>
            ))}
            {!users.isLoading && !users.data?.length && <tr><td colSpan={5} className="text-center text-muted">No users match.</td></tr>}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

function thisMonth() {
  const d = new Date();
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
}

function BillingTab() {
  const qc = useQueryClient();
  const plans = usePlans();
  const orgs = useOrgRows();
  const [month, setMonth] = useState(thisMonth());
  const invoices = useQuery({ queryKey: ["invoices", month], queryFn: () => api.get<Invoice[]>(`/platform/invoices${qs({ month })}`) });
  const gen = useMutation({ mutationFn: () => api.send<{ drafted: number }>("POST", "/platform/invoices/generate", { month }), onSuccess: () => qc.invalidateQueries({ queryKey: ["invoices"] }) });
  const setStatus = useMutation({ mutationFn: ({ id, s }: { id: string; s: string }) => api.send("POST", `/platform/invoices/${id}/status`, { status: s }), onSuccess: () => qc.invalidateQueries({ queryKey: ["invoices"] }) });
  const orgName = useMemo(() => new Map((orgs.data ?? []).map((o) => [o.id, o.name])), [orgs.data]);
  const total = (invoices.data ?? []).filter((i) => i.status !== "void").reduce((s, i) => s + i.amountCents, 0);
  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-4 md:grid-cols-3">
        {(plans.data ?? []).map((p) => <PlanCard key={p.id} plan={p} />)}
      </div>
      <Card pad={false} title="Invoices" sub="Drafted from each organisation's plan; usage is shown on the invoice. Payment is recorded by hand (bank transfer) until a payment provider is connected."
        action={<div className="flex items-center gap-2">
          <input type="month" className="input h-8 w-auto" value={month} onChange={(e) => setMonth(e.target.value)} aria-label="Month" />
          <button className="btn btn-sm btn-primary" disabled={gen.isPending} onClick={() => gen.mutate()}>Draft invoices</button>
        </div>}>
        {gen.data ? <div className="px-5 pt-3 text-[12.5px] text-text-2">{gen.data.drafted} invoice(s) drafted.</div> : null}
        {gen.error || setStatus.error ? <div className="p-4"><ErrorBox error={gen.error ?? setStatus.error} /></div> : null}
        <div className="overflow-x-auto">
          <table className="table">
            <thead><tr><th>Organisation</th><th>Number</th><th>Plan</th><th className="text-right">Amount</th><th>Status</th><th /></tr></thead>
            <tbody>
              {(invoices.data ?? []).map((i) => (
                <tr key={i.id}>
                  <td className="font-medium">{orgName.get(i.orgId) ?? i.orgId}</td>
                  <td className="mono text-[12.5px]">{i.number || "—"}</td>
                  <td className="text-text-2">{i.planId}</td>
                  <td className="num text-right">{money(i.amountCents, i.currency)}</td>
                  <td><span className={clsx("chip", i.status === "paid" ? "chip-good" : i.status === "issued" ? "chip-warning" : i.status === "void" ? "" : "chip-accent")}>{i.status}</span>
                    {i.paidAt ? <span className="ml-1.5 text-[12px] text-muted">{date(i.paidAt)}</span> : null}</td>
                  <td className="whitespace-nowrap text-right">
                    {i.status === "draft" && <button className="btn btn-sm" onClick={() => setStatus.mutate({ id: i.id, s: "issued" })}>Issue</button>}
                    {i.status === "issued" && <button className="btn btn-sm" onClick={() => setStatus.mutate({ id: i.id, s: "paid" })}>Mark paid</button>}
                    {(i.status === "draft" || i.status === "issued") && <button className="btn btn-sm btn-ghost" onClick={() => setStatus.mutate({ id: i.id, s: "void" })}>Void</button>}
                  </td>
                </tr>
              ))}
              {!invoices.isLoading && !invoices.data?.length && <tr><td colSpan={6} className="text-center text-muted">No invoices for this month. Draft them from the subscriptions.</td></tr>}
            </tbody>
            {!!invoices.data?.length && <tfoot><tr><td colSpan={3} className="font-semibold">Total (excluding void)</td><td className="num text-right font-semibold">{money(total)}</td><td colSpan={2} /></tr></tfoot>}
          </table>
        </div>
      </Card>
    </div>
  );
}

function PlanCard({ plan }: { plan: Plan }) {
  const qc = useQueryClient();
  const [edit, setEdit] = useState(false);
  const [p, setP] = useState(plan);
  const m = useMutation({ mutationFn: () => api.send("PUT", `/platform/plans/${plan.id}`, p), onSuccess: () => { setEdit(false); void qc.invalidateQueries({ queryKey: ["plans"] }); } });
  return (
    <Card title={plan.name} sub={plan.description} action={!edit && <button className="btn btn-sm btn-ghost" onClick={() => setEdit(true)}>Edit</button>}>
      {!edit ? (
        <div>
          <div className="text-[26px] font-semibold tracking-tight">{plan.priceCents ? money(plan.priceCents, plan.currency) : "Custom"}<span className="text-[13px] font-normal text-muted">{plan.priceCents ? " / month" : ""}</span></div>
          <div className="mt-2 text-[13px] text-text-2">{limit(plan.seats)} members · {limit(plan.recordsPerMonth)} records / month</div>
        </div>
      ) : (
        <form className="grid grid-cols-2 gap-2" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
          <Field label="Price / month (cents)"><input className="input num" type="number" min={0} value={p.priceCents} onChange={(e) => setP({ ...p, priceCents: Number(e.target.value) })} /></Field>
          <Field label="Currency"><input className="input" maxLength={3} value={p.currency} onChange={(e) => setP({ ...p, currency: e.target.value.toUpperCase() })} /></Field>
          <Field label="Members (-1 = unlimited)"><input className="input num" type="number" min={-1} value={p.seats} onChange={(e) => setP({ ...p, seats: Number(e.target.value) })} /></Field>
          <Field label="Records / month"><input className="input num" type="number" min={-1} value={p.recordsPerMonth} onChange={(e) => setP({ ...p, recordsPerMonth: Number(e.target.value) })} /></Field>
          {m.error ? <div className="col-span-2"><ErrorBox error={m.error} /></div> : null}
          <div className="col-span-2 flex justify-end gap-2"><button type="button" className="btn btn-sm" onClick={() => { setP(plan); setEdit(false); }}>Cancel</button><button className="btn btn-sm btn-primary" disabled={m.isPending}>Save</button></div>
        </form>
      )}
    </Card>
  );
}

function useHealth() {
  return useQuery({ queryKey: ["readyz"], queryFn: async () => {
    const r = await fetch("/readyz", { credentials: "same-origin" });
    return (await r.json()) as Record<string, string | number>;
  }, refetchInterval: 15_000 });
}
const SERVICES: Record<string, string> = { postgres: "PostgreSQL (records)", clickhouse: "ClickHouse (dashboards)", redis: "Redis (cache, live updates)", vault: "OpenBao (signing keys)", ledger: "Blockchain ledger (seals)" };

function Services({ health }: { health: ReturnType<typeof useHealth> }) {
  if (health.isLoading) return <Skel h={160} />;
  return (
    <ul className="flex flex-col gap-2 text-[13px]">
      {Object.entries(SERVICES).map(([k, l]) => {
        const v = health.data?.[k];
        const ok = v === "ok";
        return <li key={k} className="flex items-center justify-between gap-3 rounded-xl bg-surface-2 px-3 py-2.5">
          <span className="flex items-center gap-2"><span className={clsx("size-2 rounded-full", ok ? "bg-[var(--good)]" : v === undefined ? "bg-muted" : "bg-[var(--critical)]")} />{l}</span>
          <span className={clsx("text-[12.5px]", ok ? "text-text-2" : "text-critical")}>{v === undefined ? "not configured" : String(v)}</span></li>;
      })}
    </ul>
  );
}

function SystemTab() {
  const health = useHealth();
  return (
    <Card title="Services">
      <Services health={health} />
      <ul className="mt-3 flex flex-col gap-1.5 text-[12.5px] text-text-2">
        <li className="flex justify-between px-3"><span>New keys are created in</span><span>{health.data?.keyCustody === "vault" ? "the vault" : "the database (development)"}</span></li>
        <li className="flex justify-between px-3"><span>Records waiting for a seal</span><span className="num">{num(Number(health.data?.unsealed ?? 0))}</span></li>
        <li className="flex justify-between px-3"><span>Version</span><span className="mono">{String(health.data?.version ?? "—")}</span></li>
      </ul>
    </Card>
  );
}

function AuditTab() {
  const audit = useQuery({ queryKey: ["audit-platform"], queryFn: () => api.get<AuditEntry[]>("/audit") });
  const [find, setFind] = useState("");
  const rows = (audit.data ?? []).filter((a) => !find || `${a.action} ${a.subject} ${a.actor}`.toLowerCase().includes(find.toLowerCase()));
  return (
    <Card title="Changes" pad={false} action={<label className="relative"><Search size={13} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted" aria-hidden />
      <input className="input h-8 w-52 text-[13px]" style={{ paddingLeft: 28 }} placeholder="Filter actions, subjects, people" value={find} onChange={(e) => setFind(e.target.value)} aria-label="Filter the audit log" /></label>}>
      <div className="max-h-[70vh] overflow-auto scroll-thin">
        <table className="table text-[12.5px]">
          <thead className="sticky top-0 bg-[var(--bg-elevated)]"><tr><th>When</th><th>Action</th><th>Subject</th><th>By</th></tr></thead>
          <tbody>
            {audit.isLoading && <tr><td colSpan={4}><Skel h={120} /></td></tr>}
            {rows.map((a) => (
              <tr key={a.id}><td className="whitespace-nowrap text-text-2">{dateTime(a.at)}</td><td className="font-medium">{a.action}</td>
                <td className="mono text-text-2">{a.subject}</td><td className="mono text-text-2">{a.actor}</td></tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
