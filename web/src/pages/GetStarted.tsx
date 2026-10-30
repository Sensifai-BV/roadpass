import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link, Navigate, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight, BadgeCheck, Bell, Building2, Check, CheckCircle2, Clock3, Code2, Download, FileSpreadsheet, Gauge, KeyRound, Lightbulb,
  MapPin, PenLine, Plus, Send, Trash2, UserPlus, Users,
} from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";
import { useMe, useMeta } from "../lib/hooks";
import type { Duty, Invitation, Org, OrgKind, RecordType, Site, SiteKind, User } from "../lib/types";
import { DUTY, TYPE, siteKindName } from "../lib/vocab";
import { COUNTRIES } from "../lib/countries";
import { sectionName, useJobRoles, useRoleTitle } from "../lib/roles";
import { enablePush, pushState } from "../lib/push";
import { Copy_, ErrorBox, InfoTip, PageHeader, Skel } from "../components/ui";
import { ActivityPicker } from "../components/ActivityPicker";
import { SeatNote } from "../components/SeatNote";
import { RolePicker, type RoleValue } from "../components/RolePicker";
import { LocationPicker, type LatLon } from "../components/LocationPicker";
import { SiteKindSelect, useSiteKinds } from "../components/SiteModal";

// The guide a new organisation's admin goes through. Each step's state comes
// from the server (what is really set up), so it never claims more than is done.

type StepKey = "profile" | "sites" | "team" | "data" | "connect" | "notify" | "limits" | "verified";
interface Step { key: StepKey; done: boolean; optional: boolean; detail: string }
interface Setup { steps: Step[]; done: number; required: number; dismissed: boolean; mail: boolean; kinds: OrgKind[] }

/** Site name and equipment examples that fit what the company does. */
const SITE_HINT: Partial<Record<OrgKind, [string, string]>> = {
  sorting_plant: ["Sorting line North", "NIR film sorters, baler"],
  recycler: ["Washing and extrusion plant", "Friction washer, extruder, pelletiser"],
  additive_producer: ["Compounding plant", "Twin-screw compounder, pelletiser"],
  asphalt_plant: ["Asphalt plant North", "Batch mixer 240 t/h, additive feeder"],
  contractor: ["Depot East", "Pavers, rollers, transfer vehicle"],
  lab: ["Main laboratory", "MFI tester, DSC, wheel-tracking rig"],
};

const STEP: Record<StepKey, { title: string; icon: typeof Building2; why: string }> = {
  profile: { title: "Your company", icon: Building2, why: "Other companies and the public see these details on every passport you take part in. Your activities decide what you can record and which roles your team can have." },
  sites: { title: "Sites", icon: MapPin, why: "Every record names the site it comes from, so passports can show where material was made and maps can draw the chain." },
  team: { title: "Team and roles", icon: Users, why: "Each person signs what they enter with their own key. Giving each job its own role keeps intake, production, quality and dispatch separate and accountable." },
  data: { title: "Bring data in", icon: FileSpreadsheet, why: "Records are what passports are made of. Each one is checked, signed with your company's key and your own, then sealed on the ledger." },
  connect: { title: "Connect your systems", icon: Code2, why: "If your ERP, weighbridge or plant software already has the data, send it automatically through the API instead of typing it twice." },
  notify: { title: "Notifications", icon: Bell, why: "Alerts tell the right people when something needs them: a load to review, a check that failed, a limit exceeded." },
  limits: { title: "Limits", icon: Gauge, why: "Limits from your permit or mix design are checked on every value; exceedances appear in the monthly compliance report." },
  verified: { title: "Verification", icon: BadgeCheck, why: "The platform operator checks your company's identity. Until then your records are checked and signed, but not sealed on the ledger." },
};

export default function GetStarted() {
  const me = useMe().data!;
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const q = useQuery({ queryKey: ["org-setup"], queryFn: () => api.get<Setup>("/org/setup") });
  const hide = useMutation({ mutationFn: (dismissed: boolean) => api.send("PUT", "/org/setup", { dismissed }), onSuccess: () => qc.invalidateQueries({ queryKey: ["org-setup"] }) });
  if (me.user.role !== "admin") return <Navigate to="/" replace />;
  if (q.error) return <ErrorBox error={q.error} />;
  if (!q.data) return <Skel h={420} />;
  const steps = q.data.steps;
  const firstOpen = steps.find((s) => !s.done && s.key !== "verified")?.key ?? "profile";
  const cur = (steps.find((s) => s.key === params.get("step"))?.key ?? firstOpen) as StepKey;
  const go = (k: StepKey) => setParams({ step: k }, { replace: true });
  const idx = steps.findIndex((s) => s.key === cur);
  const next = steps.slice(idx + 1).find((s) => s.key !== "verified")?.key;
  const pct = Math.round((q.data.done / Math.max(1, q.data.required)) * 100);

  return (
    <div>
      <PageHeader eyebrow="Get started" title={`Set up ${me.org.name}`}
        sub="A few steps to make ROADPASS work for your company. Each one explains why it matters; you can come back to any of them later."
        actions={<button className="btn btn-ghost" onClick={() => hide.mutate(!q.data!.dismissed)}>{q.data.dismissed ? "Show the guide on the dashboard" : "Hide this guide"}</button>} />
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-[260px_minmax(0,1fr)]">
        <nav aria-label="Setup steps" className="lg:sticky lg:top-4 lg:self-start">
          <div className="card mb-3 flex items-center gap-3 p-4">
            <div className="relative size-12 shrink-0">
              <svg viewBox="0 0 36 36" className="size-12 -rotate-90" aria-hidden><circle cx="18" cy="18" r="15" fill="none" stroke="var(--surface-3)" strokeWidth="4" />
                <circle cx="18" cy="18" r="15" fill="none" stroke="var(--accent)" strokeWidth="4" strokeLinecap="round" strokeDasharray={`${(pct / 100) * 94.2} 94.2`} /></svg>
              <span className="num absolute inset-0 grid place-items-center text-[11px] font-semibold">{pct}%</span>
            </div>
            <div className="text-[12.5px]"><div className="font-semibold">{q.data.done} of {q.data.required} essentials</div><div className="text-muted">Optional steps help, but are not needed to start.</div></div>
          </div>
          <ol className="flex gap-1 overflow-x-auto lg:flex-col">
            {steps.map((s, i) => { const S = STEP[s.key]; return (
              <li key={s.key} className="shrink-0">
                <button type="button" onClick={() => go(s.key)} aria-current={cur === s.key ? "step" : undefined}
                  className={clsx("flex w-full items-center gap-2.5 rounded-xl px-2.5 py-2 text-left text-[13px] transition", cur === s.key ? "bg-surface-3" : "hover:bg-[var(--hover)]")}>
                  <span className={clsx("grid size-6 shrink-0 place-items-center rounded-full text-[11px] font-semibold", s.done ? "bg-[var(--good)] text-white" : "bg-surface-3 text-text-2")}>
                    {s.done ? <Check size={13} strokeWidth={3} /> : i + 1}</span>
                  <span className="min-w-0"><span className="block font-medium">{S.title}</span>
                    <span className="block truncate text-[11.5px] text-muted">{s.optional ? "Optional · " : ""}{s.key === "verified" ? (s.done ? "verified" : "by the platform operator") : s.detail}</span></span>
                </button>
              </li>); })}
          </ol>
        </nav>
        <section className="card min-w-0 p-5 md:p-6" aria-labelledby="step-title">
          <div className="flex items-start gap-3">
            {(() => { const I = STEP[cur].icon; return <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-accent-soft text-accent-ink"><I size={19} /></span>; })()}
            <div className="min-w-0 flex-1">
              <h2 id="step-title" className="text-[18px] font-semibold tracking-tight">{STEP[cur].title}{steps[idx]?.done && <span className="chip chip-good ml-2 align-middle"><Check size={11} />Done</span>}</h2>
              <p className="mt-1 flex items-start gap-1.5 text-[13px] text-text-2"><Lightbulb size={14} className="mt-0.5 shrink-0 text-warning" aria-hidden />{STEP[cur].why}</p>
            </div>
          </div>
          <div className="mt-5">
            {cur === "profile" && <ProfileStep />}
            {cur === "sites" && <SitesStep />}
            {cur === "team" && <TeamStep mail={q.data.mail} />}
            {cur === "data" && <DataStep />}
            {cur === "connect" && <ConnectStep />}
            {cur === "notify" && <NotifyStep />}
            {cur === "limits" && <LimitsStep />}
            {cur === "verified" && <VerifiedStep />}
          </div>
          <div className="mt-6 flex items-center justify-between border-t border-line pt-4">
            <span className="text-[12.5px] text-muted">{steps[idx]?.optional ? "Optional: skip it if it does not apply to you yet." : "You can change all of this later in Settings."}</span>
            {next ? <button className="btn btn-primary" onClick={() => go(next)}>Next: {STEP[next].title}<ArrowRight size={14} /></button>
              : <Link className="btn btn-primary" to="/">Go to your dashboard<ArrowRight size={14} /></Link>}
          </div>
        </section>
      </div>
    </div>
  );
}

function useRefresh() {
  const qc = useQueryClient();
  return () => { for (const k of ["org-setup", "own-org", "me", "sites", "orgs", "members", "invitations"]) void qc.invalidateQueries({ queryKey: [k] }); };
}

function Tip({ children }: { children: ReactNode }) {
  return <div className="flex items-start gap-2 rounded-xl bg-surface-2 px-3 py-2.5 text-[12.5px] text-text-2"><Lightbulb size={14} className="mt-0.5 shrink-0 text-warning" aria-hidden /><div>{children}</div></div>;
}

function Saved({ show }: { show: boolean }) {
  return show ? <span className="inline-flex items-center gap-1 text-[12.5px] font-medium text-[var(--good-ink)]"><CheckCircle2 size={14} />Saved</span> : null;
}

// ---------------------------------------------------------------- profile

function ProfileStep() {
  const me = useMe().data!;
  const refresh = useRefresh();
  const [o, setO] = useState<Org>(me.org);
  const [act, setAct] = useState({ kind: me.org.kind as OrgKind | "", kinds: me.org.kinds ?? [] });
  const save = useMutation({
    mutationFn: async () => { await api.send("PUT", "/org", o); await api.send("PUT", "/org/activities", { kinds: act.kinds }); },
    onSuccess: refresh,
  });
  const f = (k: "name" | "city" | "region" | "taxId" | "website", label: string, help?: string, ph?: string) => (
    <label className="block"><span className="label">{label}{help && <InfoTip text={help} className="ml-1" />}</span>
      <input className="input" placeholder={ph} value={(o[k] as string | undefined) ?? ""} onChange={(e) => setO({ ...o, [k]: e.target.value })} /></label>
  );
  return (
    <form className="flex flex-col gap-5" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
      <div>
        <div className="mb-2 text-[14px] font-semibold">What you do in the chain</div>
        <ActivityPicker value={act} onChange={setAct} lockMain oversight={false} />
      </div>
      <div>
        <div className="mb-2 text-[14px] font-semibold">Company details</div>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="sm:col-span-2">{f("name", "Company name", "As it should appear on passports.")}</div>
          <label className="block"><span className="label">Country</span>
            <select className="input" value={o.country} required onChange={(e) => setO({ ...o, country: e.target.value })}>
              <option value="">Choose…</option>{COUNTRIES.map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}</select></label>
          {f("region", "Region / state / province")}
          {f("city", "City", "Where your head office is. Plants and depots are added as sites in the next step.")}
          {f("taxId", "Tax / VAT / registration no.", "Helps the platform operator verify your company. Not shown on public passports.")}
          <div className="sm:col-span-2">{f("website", "Website", undefined, "https://")}</div>
          <label className="block sm:col-span-2"><span className="label">About the company <span className="font-normal text-muted">· optional</span></span>
            <textarea className="input min-h-[70px] py-2" maxLength={500} value={o.description ?? ""} placeholder="One or two sentences for the public passport page."
              onChange={(e) => setO({ ...o, description: e.target.value })} /></label>
        </div>
      </div>
      {save.error ? <ErrorBox error={save.error} /> : null}
      <div className="flex items-center gap-3"><button className="btn btn-primary" disabled={save.isPending || !o.country || !o.city}>Save</button><Saved show={save.isSuccess} />
        {(!o.country || !o.city) && <span className="text-[12.5px] text-muted">Country and city are needed.</span>}</div>
    </form>
  );
}

// ---------------------------------------------------------------- sites

function SitesStep() {
  const me = useMe().data!;
  const refresh = useRefresh();
  const own = useQuery({ queryKey: ["own-org"], queryFn: () => api.get<{ org: Org; sites: Site[] }>("/org") });
  const sites = own.data?.sites ?? [];
  const def = useSiteKinds(me.org).def;
  const blank = { name: "", kind: def as SiteKind | "", kindOther: "", city: "", province: "", capacityTonnesPerYear: "", equipment: "" };
  const [s, setS] = useState(blank);
  const [at, setAt] = useState<LatLon | null>(null);
  const add = useMutation({
    mutationFn: () => api.send("POST", "/org/sites", { ...s, lat: at!.lat, lon: at!.lon, capacityTonnesPerYear: Number(s.capacityTonnesPerYear) || 0 }),
    onSuccess: () => { setS(blank); setAt(null); refresh(); },
  });
  return (
    <div className="flex flex-col gap-5">
      {sites.length > 0 && (
        <ul className="grid gap-2 sm:grid-cols-2">
          {sites.map((x) => (
            <li key={x.id} className="flex items-start gap-2.5 rounded-xl border border-line p-3 text-[13px]">
              <MapPin size={15} className="mt-0.5 text-accent" />
              <span className="min-w-0"><span className="block font-semibold">{x.name}</span>
                <span className="block text-[12px] text-muted">{[siteKindName(x), [x.city, x.province].filter(Boolean).join(", "), `${x.lat.toFixed(4)}, ${x.lon.toFixed(4)}`].filter(Boolean).join(" · ")}</span></span>
            </li>
          ))}
        </ul>
      )}
      <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); if (at) add.mutate(); }}>
        <div className="text-[14px] font-semibold">{sites.length ? "Add another site" : "Add your first site"}</div>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block sm:col-span-2"><span className="label">Name</span><input className="input" required value={s.name} placeholder={SITE_HINT[me.org.kind]?.[0] ?? "Main site"} onChange={(e) => setS({ ...s, name: e.target.value })} /></label>
          <SiteKindSelect id="setup-site-kind" wide="sm:col-span-2" value={s.kind} other={s.kindOther} org={me.org} onChange={(kind, kindOther) => setS({ ...s, kind, kindOther })} />
          <label className="block"><span className="label">City</span><input className="input" value={s.city} onChange={(e) => setS({ ...s, city: e.target.value })} /></label>
          <label className="block"><span className="label">Region / state / province</span><input className="input" value={s.province} onChange={(e) => setS({ ...s, province: e.target.value })} /></label>
        </div>
        <div><span className="label">Location</span>
          <LocationPicker value={at} onChange={setAt} near={sites.map((x) => ({ lat: x.lat, lon: x.lon }))} /></div>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block"><span className="label">Capacity <span className="font-normal text-muted">· tonnes per year, optional</span></span><input className="input num" inputMode="numeric" value={s.capacityTonnesPerYear} onChange={(e) => setS({ ...s, capacityTonnesPerYear: e.target.value })} /></label>
          <label className="block"><span className="label">Equipment <span className="font-normal text-muted">· optional</span></span><input className="input" value={s.equipment} placeholder={SITE_HINT[me.org.kind]?.[1] ?? ""} onChange={(e) => setS({ ...s, equipment: e.target.value })} /></label>
        </div>
        {add.error ? <ErrorBox error={add.error} /> : null}
        <div className="flex items-center gap-3"><button className="btn btn-primary" disabled={add.isPending || !at || !s.name.trim() || !s.kind || (s.kind === "other" && !s.kindOther.trim())}><Plus size={14} /> Add site</button>
          {!at && <span className="text-[12.5px] text-muted">Place the site on the map first.</span>}</div>
      </form>
      <Tip>Add one site per plant, depot or yard that produces or receives material. A road contractor can add its depots; road sections carry their own coordinates.</Tip>
    </div>
  );
}

// ---------------------------------------------------------------- team

function TeamStep({ mail }: { mail: boolean }) {
  const me = useMe().data!;
  const refresh = useRefresh();
  const roles = useJobRoles(me.org.kind);
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.get<User[]>("/org/members") });
  const invites = useQuery({ queryKey: ["invitations"], queryFn: () => api.get<Invitation[]>("/org/invitations") });
  type Row = { email: string } & RoleValue;
  const first = roles.find((r) => r.access === "editor");
  const blank = (): Row => ({ email: "", role: first?.access ?? "editor", duties: (first?.duties ?? ["produce"]) as Duty[] });
  const [rows, setRows] = useState<Row[]>([blank()]);
  const [sent, setSent] = useState<{ email: string; link: string; emailed: boolean }[]>([]);
  useEffect(() => { if (first && rows.length === 1 && !rows[0].email) setRows([blank()]); }, [first?.key]); // eslint-disable-line react-hooks/exhaustive-deps
  const send = useMutation({
    mutationFn: async () => {
      const out: typeof sent = [];
      for (const r of rows.filter((x) => x.email.trim())) {
        const res = await api.send<{ link: string; emailed: boolean }>("POST", "/org/invitations", { email: r.email.trim(), role: r.role, duties: r.duties, steps: r.role === "editor" ? r.steps ?? [] : [] });
        out.push({ email: r.email.trim(), link: res.link, emailed: res.emailed });
      }
      return out;
    },
    onSuccess: (out) => { setSent(out); setRows([blank()]); refresh(); },
  });
  const named = useRoleTitle(me.org.kind);
  const title = (r: { role: string; duties?: string[] | null; steps?: string[] }) => `${named(r.role, r.duties, r.steps)}${r.steps?.length ? ` · ${r.steps.map(sectionName).join(", ")}` : ""}`;
  return (
    <div className="flex flex-col gap-5">
      <div>
        <div className="mb-2 flex items-center gap-1.5 text-[14px] font-semibold">Roles in your company<InfoTip text="Roles come from what your company does. Each gives access and duties: the records a person may sign. You can combine duties freely with “Custom”." /></div>
        <div className="grid gap-2 sm:grid-cols-2">
          {roles.map((r) => (
            <button key={r.key} type="button" onClick={() => setRows((xs) => { const last = xs[xs.length - 1]; const v = { role: r.access, duties: (r.duties ?? []) as Duty[] }; return last.email ? [...xs, { email: "", ...v }] : [...xs.slice(0, -1), { ...last, ...v }]; })}
              className="group rounded-xl border border-line p-3 text-left transition hover:border-accent hover:bg-accent-soft">
              <div className="flex items-center justify-between gap-2 text-[13px] font-semibold">{r.name}<span className="text-[11.5px] font-normal text-muted opacity-0 transition group-hover:opacity-100">Invite as this →</span></div>
              <div className="mt-0.5 text-[12px] text-text-2">{r.explain}</div>
              {r.access === "editor" && <div className="mt-1.5 flex flex-wrap gap-1">{(r.duties ?? []).map((d) => { const D = DUTY[d as Duty]; return <span key={d} className="chip px-1.5 py-0 text-[10.5px]"><D.icon size={10} />{D.short}</span>; })}</div>}
            </button>
          ))}
        </div>
      </div>
      <form className="flex flex-col gap-2.5" onSubmit={(e) => { e.preventDefault(); send.mutate(); }}>
        <div className="text-[14px] font-semibold">Invite people</div>
        {rows.map((r, i) => (
          <div key={i} className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-start">
            <input className="input" type="email" placeholder="name@company.com" aria-label={`E-mail ${i + 1}`} value={r.email} onChange={(e) => setRows(rows.map((x, j) => (j === i ? { ...x, email: e.target.value } : x)))} />
            <RolePicker kind={me.org.kind} compact label={`Role ${i + 1}`} value={r} onChange={(v) => setRows(rows.map((x, j) => (j === i ? { ...x, ...v } : x)))} />
            <button type="button" className="btn btn-ghost btn-icon" aria-label="Remove this row" disabled={rows.length === 1} onClick={() => setRows(rows.filter((_, j) => j !== i))}><Trash2 size={14} /></button>
          </div>
        ))}
        <div className="flex flex-wrap items-center gap-2">
          <button type="button" className="btn btn-sm" onClick={() => setRows([...rows, blank()])}><Plus size={13} /> Another person</button>
          <button className="btn btn-primary" disabled={send.isPending || !rows.some((r) => r.email.includes("@"))}><UserPlus size={14} /> Send invitations</button>
        </div>
        <SeatNote adding={rows.filter((r) => r.email.includes("@")).length} />
        {send.error ? <ErrorBox error={send.error} /> : null}
      </form>
      {sent.length > 0 && (
        <div className="rounded-xl border border-[var(--good)]/40 bg-[var(--good-soft)] p-3 text-[13px]">
          <div className="font-semibold">{sent.length} invitation{sent.length > 1 ? "s" : ""} created</div>
          <ul className="mt-2 flex flex-col gap-1.5">{sent.map((x) => (
            <li key={x.email} className="flex flex-wrap items-center gap-2"><span className="font-medium">{x.email}</span>
              <span className="text-text-2">{x.emailed ? "· e-mailed" : "· send them this link:"}</span>{!x.emailed && <><span className="mono max-w-[260px] truncate text-[11.5px]">{x.link}</span><Copy_ text={x.link} label="Copy link" /></>}</li>
          ))}</ul>
        </div>
      )}
      {!mail && <Tip>This platform does not send e-mail yet, so share each invitation link yourself (chat, e-mail). Links work once and expire after 7 days.</Tip>}
      <div className="grid gap-3 sm:grid-cols-2">
        <div><div className="mb-1.5 text-[12.5px] font-semibold text-text-2">Members ({members.data?.length ?? 0})</div>
          <ul className="flex flex-col gap-1">{(members.data ?? []).map((u) => <li key={u.id} className="flex justify-between gap-2 rounded-lg bg-surface-2 px-2.5 py-1.5 text-[12.5px]"><span className="truncate">{u.name}</span><span className="text-muted">{title(u)}</span></li>)}</ul></div>
        <div><div className="mb-1.5 text-[12.5px] font-semibold text-text-2">Waiting to join ({invites.data?.length ?? 0})</div>
          <ul className="flex flex-col gap-1">{(invites.data ?? []).map((v) => <li key={v.id} className="flex justify-between gap-2 rounded-lg bg-surface-2 px-2.5 py-1.5 text-[12.5px]"><span className="truncate"><Send size={11} className="mr-1 inline text-muted" />{v.email}</span><span className="text-muted">{title(v)}</span></li>)}
            {!invites.data?.length && <li className="text-[12.5px] text-muted">Nobody yet.</li>}</ul></div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------- data

function DataStep() {
  const me = useMe().data!;
  const meta = useMeta().data;
  const kinds = [me.org.kind, ...(me.org.kinds ?? [])];
  const types = (meta?.types ?? []).filter((t) => t.type !== "acceptance" && t.createdBy.some((k) => kinds.includes(k)));
  const ways = [
    { icon: PenLine, title: "Type it in", text: "A form per record, with help on every field. Best for a few records a day." },
    { icon: FileSpreadsheet, title: "Upload a spreadsheet", text: "Download a template, fill it in Excel or export it from your system, upload. Checked row by row before import." },
    { icon: Code2, title: "Connect a system", text: "REST/JSON or NGSI-LD, for ERPs, weighbridges and data platforms such as FIWARE brokers." },
  ];
  return (
    <div className="flex flex-col gap-5">
      <div className="grid gap-2 sm:grid-cols-3">
        {ways.map((w) => <div key={w.title} className="rounded-xl bg-surface-2 p-3"><w.icon size={16} className="text-accent" /><div className="mt-1.5 text-[13px] font-semibold">{w.title}</div><div className="mt-0.5 text-[12px] text-text-2">{w.text}</div></div>)}
      </div>
      <div>
        <div className="mb-2 flex items-center gap-1.5 text-[14px] font-semibold">What you will record<InfoTip text="These follow from your activities. Each record type has its own form, spreadsheet template and NGSI-LD entity type." /></div>
        <ul className="divide-y divide-line rounded-xl border border-line">
          {types.map((t) => { const T = TYPE[t.type as RecordType]; return (
            <li key={t.type} className="flex flex-wrap items-center gap-3 px-3.5 py-3">
              <span className="grid size-8 shrink-0 place-items-center rounded-lg" style={{ background: `color-mix(in srgb, ${T.color} 15%, transparent)`, color: T.color }}><T.icon size={15} /></span>
              <span className="min-w-0 flex-1"><span className="block text-[13px] font-semibold">{t.name}</span><span className="block text-[12px] text-text-2">{t.summary}</span></span>
              <span className="flex flex-wrap gap-1.5">
                <Link className="btn btn-sm" to={`/work?new=${t.type}`}><PenLine size={13} /> Form</Link>
                <a className="btn btn-sm" href={`/api/v1/templates/${t.type}.csv`} download><Download size={13} /> Template</a>
                <Link className="btn btn-sm" to={`/data?tab=csv&type=${t.type}`}><FileSpreadsheet size={13} /> Upload</Link>
              </span>
            </li>); })}
          {!types.length && <li className="px-3.5 py-3 text-[13px] text-muted">Your activities are read-only: nothing to record.</li>}
        </ul>
      </div>
      <Tip>Start with one real record by hand to see the checks and the passport it creates; then upload the rest. Data are labelled <b>Operational</b>, never mixed with the simulated demo data.</Tip>
    </div>
  );
}

// ---------------------------------------------------------------- connect

function ConnectStep() {
  const me = useMe().data!;
  const refresh = useRefresh();
  const [name, setName] = useState("ERP integration");
  const [duties, setDuties] = useState<Duty[]>(["produce", "ship"]);
  const [secret, setSecret] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: () => api.send<{ secret: string }>("POST", "/org/api-keys", { name, scopes: ["records:read", "records:write"], duties }),
    onSuccess: (r) => { setSecret(r.secret); refresh(); },
  });
  const base = location.origin;
  return (
    <div className="flex flex-col gap-4">
      {!secret ? (
        <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); create.mutate(); }}>
          <label className="block"><span className="label">Key name<InfoTip className="ml-1" text="Name it after the system that will use it, so the audit log shows where records came from." /></span><input className="input" required maxLength={80} value={name} onChange={(e) => setName(e.target.value)} /></label>
          <div><span className="label">May sign as<InfoTip className="ml-1" text="Like a person's duties: the record types this system may submit. Give it only what it needs." /></span>
            <div className="flex flex-wrap gap-1">{(["receive", "produce", "test", "release", "ship"] as Duty[]).map((d) => { const on = duties.includes(d); const D = DUTY[d]; return (
              <button key={d} type="button" aria-pressed={on} onClick={() => setDuties(on ? duties.filter((x) => x !== d) : [...duties, d])} className={clsx("chip", on ? "chip-accent" : "opacity-70")}><D.icon size={11} />{D.name}</button>); })}</div></div>
          {create.error ? <ErrorBox error={create.error} /> : null}
          <div><button className="btn btn-primary" disabled={create.isPending || !duties.length}><KeyRound size={14} /> Create API key</button></div>
        </form>
      ) : (
        <div className="flex flex-col gap-3">
          <div className="rounded-xl border border-[var(--warning)]/40 bg-[var(--warning-soft)] p-3 text-[13px]">
            <div className="font-semibold">Copy the key now: it is shown only once.</div>
            <div className="mt-2 flex items-center gap-2"><span className="mono flex-1 break-all text-[12px]">{secret}</span><Copy_ text={secret} label="Copy key" /></div>
          </div>
          <div className="text-[13px] font-semibold">Send a record (JSON)</div>
          <pre className="mono overflow-x-auto rounded-xl bg-surface-2 p-3 text-[11.5px] leading-relaxed">{`curl -X POST ${base}/api/v1/records \\
  -H "Authorization: Bearer ${secret.slice(0, 12)}…" -H "Content-Type: application/json" \\
  -H "Idempotency-Key: $(uuidgen)" \\
  -d '{"records":[{"type":"…","ref":"YOUR-REF-001","occurredAt":"2026-10-01T08:00:00Z","massKg":24000,"data":{…}}]}'`}</pre>
          <div className="text-[13px] font-semibold">Or as NGSI-LD entities</div>
          <pre className="mono overflow-x-auto rounded-xl bg-surface-2 p-3 text-[11.5px] leading-relaxed">{`curl -X POST ${base}/ngsi-ld/v1/entityOperations/upsert \\
  -H "Authorization: Bearer …" -H "Content-Type: application/json" \\
  -H 'Link: <${base}/ngsi-ld/context.jsonld>; rel="http://www.w3.org/ns/json-ld#context"' -d @entities.json`}</pre>
        </div>
      )}
      <Tip>Field names, units and examples for every record type are under <Link className="font-semibold underline" to="/data?tab=fields">Data in → Field reference</Link>, and the full OpenAPI description at <a className="font-semibold underline" href="/api/openapi.json" target="_blank" rel="noreferrer">/api/openapi.json</a>. {me.org.status !== "verified" && "Records you send now are checked and signed; they are sealed once your company is verified."}</Tip>
    </div>
  );
}

// ---------------------------------------------------------------- notifications, limits, verification

function NotifyStep() {
  const [state, setState] = useState("…");
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => { void pushState().then(setState); }, []);
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3 rounded-xl border border-line p-3.5">
        <Bell size={18} className="text-accent" />
        <div className="min-w-0 flex-1 text-[13px]"><div className="font-semibold">Pop-up notifications on this computer</div><div className="text-text-2">Even when ROADPASS is closed. Each person chooses for themselves.</div></div>
        {state === "on" ? <span className="chip chip-good"><Check size={11} />On</span>
          : state === "blocked" ? <span className="text-[12.5px] text-warning">Blocked in the browser's site settings</span>
          : state === "unsupported" ? <span className="text-[12.5px] text-muted">Not supported in this browser</span>
          : <button className="btn btn-sm btn-primary" onClick={async () => { setErr(null); try { await enablePush(); setState("on"); } catch (e) { setErr((e as Error).message); } }}>Turn on</button>}
      </div>
      {err && <p className="text-[12.5px] text-critical">{err}</p>}
      <Tip>Starter alert rules for your kind of company are already on: rejected loads, loads not confirmed in time, failed checks. Adjust them, or send alerts to e-mail, Slack, Teams, SMS or a webhook, under <Link className="font-semibold underline" to="/alerts">Alerts</Link>.</Tip>
    </div>
  );
}

function LimitsStep() {
  return (
    <div className="flex flex-col gap-4">
      <p className="text-[13px] text-text-2">Add the limits your permit, mix design or standard sets, for example additive dosage between 0.4 and 0.7 %, or mixing temperature below 180 °C. Each one is checked on every value or on the daily or monthly mean.</p>
      <div><Link className="btn btn-primary" to="/compliance?tab=limits"><Gauge size={14} /> Set limits</Link></div>
      <Tip>The regulatory register under Compliance shows which rules apply to your activities in your country and the evidence ROADPASS already has for each.</Tip>
    </div>
  );
}

function VerifiedStep() {
  const me = useMe().data!;
  const v = me.org.status === "verified";
  return (
    <div className="flex items-start gap-3 rounded-xl bg-surface-2 p-4 text-[13px]">
      {v ? <BadgeCheck size={20} className="text-good" /> : <Clock3 size={20} className="text-warning" />}
      <div>{v ? <><b>Your company is verified.</b> Its signing key is on the ledger and your records are sealed.</>
        : <><b>Waiting for the platform operator.</b> They check your company's identity, usually within a working day. Meanwhile you can set everything up and enter data: records are checked and signed now, and sealed once you are verified.</>}</div>
    </div>
  );
}

/** A compact progress card for the dashboard, until setup is done or hidden. */
export function SetupBanner() {
  const me = useMe().data!;
  const q = useQuery({ queryKey: ["org-setup"], queryFn: () => api.get<Setup>("/org/setup"), enabled: me.user.role === "admin" && !me.operator });
  const d = q.data;
  const next = useMemo(() => d?.steps.find((s) => !s.done && !s.optional && s.key !== "verified"), [d]);
  if (!d || d.dismissed || !next) return null;
  const pct = Math.round((d.done / Math.max(1, d.required)) * 100);
  return (
    <div className="card mb-4 flex flex-wrap items-center gap-4 p-4">
      <div className="min-w-0 flex-1">
        <div className="text-[14px] font-semibold">Finish setting up {me.org.name}</div>
        <div className="mt-1 flex items-center gap-2 text-[12.5px] text-text-2">
          <span className="h-[6px] w-32 overflow-hidden rounded-full bg-surface-3"><span className="block h-full rounded-full bg-[var(--accent)]" style={{ width: `${pct}%` }} /></span>
          {d.done} of {d.required} done · next: {STEP[next.key].title.toLowerCase()}
        </div>
      </div>
      <Link className="btn btn-primary" to={`/get-started?step=${next.key}`}>Continue<ArrowRight size={14} /></Link>
    </div>
  );
}

export type { Setup };
