import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, Building2, Download, CheckCircle2, CreditCard, History, KeyRound, Monitor, SlidersHorizontal, Trash2, UserRound, Users } from "lucide-react";
import clsx from "clsx";
import { api, ApiError } from "../lib/api";
import { useMe, useTheme, type Theme } from "../lib/hooks";
import { useRoleTitle } from "../lib/roles";
import { DUTY, KIND } from "../lib/vocab";
import { browserTimeZone, date, dateTime, timeZone } from "../lib/format";
import { Card, ErrorBox, PageHeader, Segmented } from "../components/ui";
import { ActivitySection, OrganisationSection, TeamSection, TeamWork } from "./Organisation";
import { disablePush, enablePush, pushState } from "../lib/push";

type Tab = "profile" | "preferences" | "notifications" | "security" | "organisation" | "team" | "activity" | "billing";

const TABS: { key: Tab; label: string; icon: typeof UserRound; group: "you" | "org"; admin?: boolean }[] = [
  { key: "profile", label: "Profile", icon: UserRound, group: "you" },
  { key: "preferences", label: "Preferences", icon: SlidersHorizontal, group: "you" },
  { key: "notifications", label: "Notifications", icon: Bell, group: "you" },
  { key: "security", label: "Security", icon: KeyRound, group: "you" },
  { key: "organisation", label: "Organisation", icon: Building2, group: "org" },
  { key: "team", label: "Team & roles", icon: Users, group: "org" },
  { key: "activity", label: "Activity", icon: History, group: "org", admin: true },
  { key: "billing", label: "Plan & billing", icon: CreditCard, group: "org", admin: true },
];

export default function Settings() {
  const me = useMe().data!;
  const admin = me.user.role === "admin";
  const [params, setParams] = useSearchParams();
  const tabs = TABS.filter((t) => !t.admin || admin);
  const tab = (tabs.find((t) => t.key === params.get("tab"))?.key ?? "profile") as Tab;
  const go = (t: Tab) => setParams({ tab: t }, { replace: true });

  const group = (g: "you" | "org", title: string) => (
    <div>
      <div className="eyebrow mb-1.5 px-2.5">{title}</div>
      {tabs.filter((t) => t.group === g).map((t) => (
        <button key={t.key} type="button" onClick={() => go(t.key)} aria-current={tab === t.key ? "page" : undefined}
          className={clsx("flex h-[34px] w-full items-center gap-2.5 rounded-[9px] px-2.5 text-left text-[13.5px] font-medium transition-colors",
            tab === t.key ? "bg-surface-3 text-text" : "text-text-2 hover:bg-[var(--hover)]")}>
          <t.icon size={16} className={tab === t.key ? "text-accent" : ""} aria-hidden />{t.label}
        </button>
      ))}
    </div>
  );

  return (
    <div>
      <PageHeader title="Settings" sub={admin ? "Your own account, and your organisation's profile, team and activity." : "Your own account and preferences."} />
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[200px_minmax(0,1fr)]">
        <nav className="flex gap-5 overflow-x-auto lg:flex-col" aria-label="Settings">
          {group("you", "You")}
          {group("org", me.org.name.length > 22 ? "Organisation" : me.org.name)}
        </nav>
        <div className="min-w-0">
          {tab === "profile" && <Profile />}
          {tab === "preferences" && <Preferences />}
          {tab === "notifications" && <Notifications />}
          {tab === "security" && <Security />}
          {tab === "organisation" && <OrganisationSection />}
          {tab === "team" && <div className="flex flex-col gap-4">{admin && <TeamWork />}<TeamSection /></div>}
          {tab === "activity" && <ActivitySection />}
          {tab === "billing" && <Billing />}
        </div>
      </div>
    </div>
  );
}

function Saved({ show }: { show: boolean }) {
  return show ? <span className="inline-flex items-center gap-1 text-[12.5px] font-medium text-[var(--good-ink)]"><CheckCircle2 size={14} /> Saved</span> : null;
}

function Row({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="grid gap-2 border-b border-line py-4 last:border-0 sm:grid-cols-[220px_minmax(0,1fr)] sm:gap-6">
      <div><div className="text-[13.5px] font-semibold">{label}</div>{hint && <div className="mt-0.5 text-[12.5px] text-muted">{hint}</div>}</div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function useSaveMe() {
  const qc = useQueryClient();
  const me = useMe().data!;
  return useMutation({
    mutationFn: (patch: { name?: string; timezone?: string }) =>
      api.send("PATCH", "/auth/me", { name: patch.name ?? me.user.name, timezone: patch.timezone ?? me.user.timezone ?? "" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["me"] }),
  });
}

function Profile() {
  const title = useRoleTitle(useMe().data!.org.kind);
  const me = useMe().data!;
  const [name, setName] = useState(me.user.name);
  const save = useSaveMe();
  const K = KIND[me.org.kind];
  return (
    <Card title="Profile" sub="How your colleagues and the activity log see you.">
      <Row label="Name">
        <form className="flex max-w-md gap-2" onSubmit={(e) => { e.preventDefault(); save.mutate({ name }); }}>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} maxLength={80} required aria-label="Name" />
          <button className="btn btn-primary" disabled={save.isPending || name.trim() === me.user.name}>Save</button>
        </form>
        <div className="mt-1.5"><Saved show={save.isSuccess} /></div>
        {save.error ? <div className="mt-2"><ErrorBox error={save.error} /></div> : null}
      </Row>
      <Row label="E-mail" hint="Your sign-in. Ask an admin to change it."><span className="text-[13.5px]">{me.user.email}</span></Row>
      <Row label="Organisation"><span className="inline-flex items-center gap-1.5 text-[13.5px]"><K.icon size={14} style={{ color: K.color }} />{me.org.name} · {K.name}</span></Row>
      <Row label="Role" hint="Set by your organisation's admins. Your duties decide which records you can sign.">
        <div className="flex flex-wrap gap-1.5">
          <span className="chip font-semibold">{title(me.user.role, me.duties, me.user.steps)}</span>
          {me.duties.map((d) => { const D = DUTY[d]; return <span key={d} className="chip chip-accent"><D.icon size={11} />{D.name}</span>; })}
          {!me.duties.length && <span className="text-[12.5px] text-muted">{me.oversight ? "Reads the whole chain" : "Reads only"}</span>}
        </div>
      </Row>
      <Row label="Your data" hint="Everything ROADPASS holds about you, as a JSON file (GDPR art. 15 and 20).">
        <a className="btn btn-sm" href="/api/v1/auth/me/export" download><Download size={13} /> Download my data</a>
      </Row>
    </Card>
  );
}

function Preferences() {
  const me = useMe().data!;
  const save = useSaveMe();
  const [theme, setTheme] = useTheme();
  const zones = useMemo(() => {
    try {
      return (Intl as unknown as { supportedValuesOf: (k: string) => string[] }).supportedValuesOf("timeZone");
    } catch {
      return [browserTimeZone(), "UTC"];
    }
  }, []);
  const [filter, setFilter] = useState("");
  const current = me.user.timezone || "";
  const shown = zones.filter((z) => z.toLowerCase().replace(/_/g, " ").includes(filter.toLowerCase())).slice(0, 200);
  const now = new Date();
  const fmt = (tz: string) => new Intl.DateTimeFormat("en-GB", { weekday: "short", hour: "2-digit", minute: "2-digit", timeZoneName: "short", timeZone: tz }).format(now);
  return (
    <Card title="Preferences">
      <Row label="Time zone" hint="Dates and times across ROADPASS, and spreadsheet times you upload, use this zone.">
        <div className="flex max-w-lg flex-col gap-2">
          <div className="flex items-center justify-between gap-3 rounded-xl bg-surface-2 px-3 py-2.5 text-[13px]">
            <span><b>{timeZone().replace(/_/g, " ")}</b>{!current && <span className="text-muted"> · from your browser</span>}</span>
            <span className="num text-text-2">{fmt(timeZone())}</span>
          </div>
          <input className="input" placeholder="Search zones, e.g. Berlin, New York, Tokyo" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Search time zones" />
          <div className="max-h-[260px] overflow-y-auto rounded-xl border border-line p-1 scroll-thin" role="listbox" aria-label="Time zones">
            <button type="button" role="option" aria-selected={!current} onClick={() => save.mutate({ timezone: "" })}
              className={clsx("flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-left text-[13px] hover:bg-surface-3", !current && "bg-accent-soft font-semibold")}>
              <span>Use my browser's ({browserTimeZone().replace(/_/g, " ")})</span>
            </button>
            {shown.map((z) => (
              <button key={z} type="button" role="option" aria-selected={current === z} onClick={() => save.mutate({ timezone: z })}
                className={clsx("flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-left text-[13px] hover:bg-surface-3", current === z && "bg-accent-soft font-semibold")}>
                <span>{z.replace(/_/g, " ")}</span><span className="num text-[12px] text-muted">{fmt(z)}</span>
              </button>
            ))}
          </div>
          <div><Saved show={save.isSuccess && !save.isPending} /></div>
          {save.error ? <ErrorBox error={save.error} /> : null}
        </div>
      </Row>
      <Row label="Appearance" hint="Stored in this browser.">
        <Segmented<Theme> label="Theme" value={theme} onChange={setTheme} options={[{ value: "system", label: "System" }, { value: "light", label: "Light" }, { value: "dark", label: "Dark" }]} />
      </Row>
    </Card>
  );
}

interface MyKey { id: string; publicKey: string; status: "active" | "retired" | "revoked"; reason?: string; custody: "vault" | "platform" | "customer"; onLedger: boolean; ledgerTxId?: string; createdAt: string }

/**
 * The member's own Ed25519 key. Everything they sign (records, receipts) carries
 * this signature next to the company's, so each action is attributable to them.
 */
function PersonalKey() {
  const qc = useQueryClient();
  const keys = useQuery({ queryKey: ["my-keys"], queryFn: () => api.get<{ keys: MyKey[]; vault: string }>("/auth/me/keys") });
  const rotate = useMutation({ mutationFn: () => api.send("POST", "/auth/me/keys/rotate"), onSuccess: () => qc.invalidateQueries({ queryKey: ["my-keys"] }) });
  const [confirm, setConfirm] = useState(false);
  const active = keys.data?.keys.find((k) => k.status === "active");
  const past = keys.data?.keys.filter((k) => k.status !== "active") ?? [];
  const custody = (c: string) => c === "vault" ? "Held in the OpenBao vault; it never leaves it" : c === "platform" ? "Sealed by the platform (development mode)" : "Held by your organisation";
  return (
    <Row label="Personal signing key" hint="Every record and receipt you sign carries your own Ed25519 signature next to your company's, so each action is provably yours.">
      {keys.isLoading ? <span className="text-[13px] text-muted">Loading…</span> : keys.error ? <ErrorBox error={keys.error} /> : active ? (
        <div className="flex max-w-xl flex-col gap-3">
          <div className="rounded-xl bg-surface-2 p-3.5 text-[13px]">
            <div className="flex items-center justify-between gap-2"><span className="mono font-semibold">{active.id}</span>
              <span className={clsx("chip", active.onLedger ? "chip-good" : "chip-warning")}>{active.onLedger ? "on the ledger" : "registering on the ledger…"}</span></div>
            <div className="mono mt-1 break-all text-[11.5px] text-text-2">{active.publicKey}</div>
            <div className="mt-2 text-[12px] text-muted">{custody(active.custody)} · issued {dateTime(active.createdAt)}</div>
          </div>
          {confirm ? (
            <div className="flex flex-wrap items-center gap-2 rounded-xl bg-[var(--warning-soft)] px-3 py-2.5 text-[12.5px] text-warning">
              Rotate now? Your past signatures stay valid; new ones use the new key.
              <button className="btn btn-sm btn-primary" disabled={rotate.isPending} onClick={() => rotate.mutate(undefined, { onSettled: () => setConfirm(false) })}>Rotate key</button>
              <button className="btn btn-sm" onClick={() => setConfirm(false)}>Cancel</button>
            </div>
          ) : <div><button className="btn btn-sm" onClick={() => setConfirm(true)}>Rotate key</button></div>}
          {rotate.error ? <ErrorBox error={rotate.error} /> : null}
          {past.length > 0 && (
            <details className="text-[12.5px]"><summary className="cursor-pointer text-text-2">Earlier keys ({past.length})</summary>
              <ul className="mt-2 flex flex-col gap-1">
                {past.map((k) => <li key={k.id} className="flex justify-between gap-2 rounded-lg bg-surface-2 px-3 py-1.5"><span className="mono">{k.id}</span><span className="text-muted">{k.status}{k.reason ? ` · ${k.reason}` : ""}</span></li>)}
              </ul>
            </details>
          )}
        </div>
      ) : <span className="text-[13px] text-muted">No key yet.</span>}
    </Row>
  );
}

function Security() {
  const me = useMe().data!;
  const [pw, setPw] = useState({ current: "", next: "", confirm: "" });
  const m = useMutation({
    mutationFn: () => api.send("POST", "/auth/password", { current: pw.current, new: pw.next }),
    onSuccess: () => setPw({ current: "", next: "", confirm: "" }),
  });
  const fieldErr = (f: string) => m.error instanceof ApiError && Array.isArray(m.error.problem.errors)
    ? (m.error.problem.errors as { field: string; message: string }[]).find((x) => x.field === f)?.message : undefined;
  const mismatch = pw.confirm !== "" && pw.next !== pw.confirm;
  return (
    <Card title="Security">
      <Row label="Password" hint={me.user.demo ? "Demo accounts sign in without a password." : "At least 12 characters. Use a password manager."}>
        {me.user.demo ? <span className="text-[13px] text-muted">Not available for demo accounts.</span> : (
          <form className="flex max-w-sm flex-col gap-3" onSubmit={(e) => { e.preventDefault(); if (!mismatch) m.mutate(); }}>
            <div><label className="label" htmlFor="pw-cur">Current password</label>
              <input id="pw-cur" type="password" className="input" autoComplete="current-password" required value={pw.current} onChange={(e) => setPw({ ...pw, current: e.target.value })} />
              {fieldErr("current") && <p className="mt-1 text-[12px] text-critical">{fieldErr("current")}</p>}</div>
            <div><label className="label" htmlFor="pw-new">New password</label>
              <input id="pw-new" type="password" className="input" autoComplete="new-password" minLength={12} required value={pw.next} onChange={(e) => setPw({ ...pw, next: e.target.value })} />
              {fieldErr("new") && <p className="mt-1 text-[12px] text-critical">{fieldErr("new")}</p>}</div>
            <div><label className="label" htmlFor="pw-conf">Repeat new password</label>
              <input id="pw-conf" type="password" className="input" autoComplete="new-password" required value={pw.confirm} onChange={(e) => setPw({ ...pw, confirm: e.target.value })} />
              {mismatch && <p className="mt-1 text-[12px] text-critical">The passwords do not match.</p>}</div>
            <div className="flex items-center gap-3"><button className="btn btn-primary" disabled={m.isPending || mismatch}>Change password</button><Saved show={m.isSuccess} /></div>
            {m.error && !fieldErr("current") && !fieldErr("new") ? <ErrorBox error={m.error} /> : null}
          </form>
        )}
      </Row>
      <PersonalKey />
    </Card>
  );
}

interface BillingView {
  subscription?: { planId: string; status: string; trialEndsAt?: string };
  usage?: { records: number; seats: number };
  plans: { id: string; name: string; priceCents: number; currency: string; seats: number; recordsPerMonth: number }[];
  invoices: { id: string; number?: string; period: string; amountCents: number; currency: string; status: string; issuedAt?: string; paidAt?: string }[];
}

function Billing() {
  const b = useQuery({ queryKey: ["org-billing"], queryFn: () => api.get<BillingView>("/org/billing") });
  const plan = b.data?.plans.find((p) => p.id === b.data?.subscription?.planId);
  const money = (c: number, cur = "EUR") => new Intl.NumberFormat("en-GB", { style: "currency", currency: cur }).format(c / 100);
  const bar = (used: number, max: number) => max < 0 ? null : (
    <div className="mt-1.5 h-[6px] w-full overflow-hidden rounded-full bg-surface-3">
      <div className="h-full rounded-full" style={{ width: `${Math.min(100, (used / Math.max(1, max)) * 100)}%`, background: used > max ? "var(--warning)" : "var(--accent)" }} />
    </div>
  );
  if (b.error) return <ErrorBox error={b.error} />;
  return (
    <div className="flex flex-col gap-4">
      <Card title="Your plan" sub="Plans and invoices are managed by the platform operator. Ask them to change your plan.">
        {!b.data ? <span className="text-[13px] text-muted">Loading…</span> : !plan ? <span className="text-[13px] text-muted">No plan assigned yet.</span> : (
          <div className="grid gap-5 sm:grid-cols-3">
            <div><div className="text-[12px] text-muted">Plan</div><div className="text-[20px] font-semibold">{plan.name}</div>
              <div className="text-[12.5px] text-text-2">{b.data.subscription?.status.replace("_", " ")}{b.data.subscription?.trialEndsAt ? ` until ${date(b.data.subscription.trialEndsAt)}` : ""}</div></div>
            <div><div className="text-[12px] text-muted">Members</div><div className="num text-[20px] font-semibold">{b.data.usage?.seats ?? 0}<span className="text-[13px] font-normal text-muted"> / {plan.seats < 0 ? "unlimited" : plan.seats}</span></div>{bar(b.data.usage?.seats ?? 0, plan.seats)}</div>
            <div><div className="text-[12px] text-muted">Records this month</div><div className="num text-[20px] font-semibold">{b.data.usage?.records ?? 0}<span className="text-[13px] font-normal text-muted"> / {plan.recordsPerMonth < 0 ? "unlimited" : plan.recordsPerMonth}</span></div>{bar(b.data.usage?.records ?? 0, plan.recordsPerMonth)}</div>
          </div>
        )}
      </Card>
      <Card title="Invoices" pad={false}>
        <table className="table">
          <thead><tr><th>Number</th><th>Month</th><th className="text-right">Amount</th><th>Status</th></tr></thead>
          <tbody>
            {(b.data?.invoices ?? []).map((i) => (
              <tr key={i.id}><td className="mono">{i.number || "—"}</td><td>{new Date(i.period).toLocaleDateString("en-GB", { month: "long", year: "numeric", timeZone: "UTC" })}</td>
                <td className="num text-right">{money(i.amountCents, i.currency)}</td><td><span className={clsx("chip", i.status === "paid" ? "chip-good" : "chip-warning")}>{i.status}</span></td></tr>
            ))}
            {!b.data?.invoices.length && <tr><td colSpan={4} className="text-center text-muted">No invoices yet.</td></tr>}
          </tbody>
        </table>
      </Card>
    </div>
  );
}

interface NPrefs { browser: boolean; email: "off" | "instant" | "daily"; minSeverity: "info" | "warning" | "critical"; quietFrom?: number; quietTo?: number; muted: string[] }
const KINDS = [
  { key: "alert", label: "Alerts" }, { key: "assignment", label: "Work assigned to me" }, { key: "org", label: "Organisation changes" },
  { key: "billing", label: "Billing" }, { key: "system", label: "System" },
];
const hhmm = (m?: number) => (m === undefined ? "" : `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`);
const toMin = (v: string) => { const [h, m] = v.split(":").map(Number); return Number.isFinite(h) ? h * 60 + (m || 0) : undefined; };

function Notifications() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["notif-prefs"], queryFn: () => api.get<{ prefs: NPrefs; emailAvailable: boolean }>("/notifications/prefs") });
  const devices = useQuery({ queryKey: ["push-devices"], queryFn: () => api.get<{ id: string; userAgent: string; createdAt: string; lastOkAt?: string }[]>("/notifications/push") });
  const [p, setP] = useState<NPrefs | null>(null);
  const [push, setPush] = useState<string>("…");
  const [pushErr, setPushErr] = useState<string | null>(null);
  useEffect(() => { if (q.data && !p) setP(q.data.prefs); }, [q.data, p]);
  useEffect(() => { void pushState().then(setPush); }, []);
  const save = useMutation({ mutationFn: (x: NPrefs) => api.send("PUT", "/notifications/prefs", x), onSuccess: () => qc.invalidateQueries({ queryKey: ["notif-prefs"] }) });
  const test = useMutation({ mutationFn: () => api.send<{ inApp: number; pushed: number }>("POST", "/notifications/test") });
  const remove = useMutation({ mutationFn: (id: string) => api.send("POST", "/notifications/push/remove", { id }), onSuccess: () => qc.invalidateQueries({ queryKey: ["push-devices"] }) });
  if (!p) return <Card title="Notifications"><span className="text-[13px] text-muted">Loading…</span></Card>;
  const set = (x: Partial<NPrefs>) => { const n = { ...p, ...x }; setP(n); save.mutate(n); };
  return (
    <Card title="Notifications" sub="Everything also appears under the bell. Choose what reaches you elsewhere.">
      <Row label="On this computer" hint="Pop-up notifications from your browser, even when ROADPASS is closed.">
        <div className="flex flex-wrap items-center gap-2">
          {push === "on" ? <><span className="chip chip-good">On for this browser</span><button className="btn btn-sm" onClick={async () => { await disablePush(); setPush("off"); void qc.invalidateQueries({ queryKey: ["push-devices"] }); }}>Turn off</button></>
            : push === "blocked" ? <span className="text-[13px] text-warning">Blocked in this browser's site settings.</span>
            : push === "unsupported" ? <span className="text-[13px] text-muted">This browser cannot receive push notifications.</span>
            : <button className="btn btn-sm btn-primary" onClick={async () => { setPushErr(null); try { await enablePush(); setPush("on"); void qc.invalidateQueries({ queryKey: ["push-devices"] }); } catch (e) { setPushErr((e as Error).message); } }}>Turn on</button>}
          <label className="ml-2 flex items-center gap-1.5 text-[13px]"><input type="checkbox" checked={p.browser} onChange={(e) => set({ browser: e.target.checked })} /> Send to my browsers</label>
        </div>
        {pushErr && <p className="mt-1.5 text-[12px] text-critical">{pushErr}</p>}
        {!!devices.data?.length && (
          <ul className="mt-3 flex flex-col gap-1">
            {devices.data.map((d) => (
              <li key={d.id} className="flex items-center gap-2 rounded-lg bg-surface-2 px-3 py-1.5 text-[12.5px]">
                <Monitor size={13} className="text-muted" /><span className="min-w-0 flex-1 truncate">{browserName(d.userAgent)}</span>
                <span className="text-muted">{d.lastOkAt ? `last delivered ${dateTime(d.lastOkAt)}` : `added ${date(d.createdAt)}`}</span>
                <button className="text-muted hover:text-critical" aria-label="Remove this browser" onClick={() => remove.mutate(d.id)}><Trash2 size={13} /></button>
              </li>
            ))}
          </ul>
        )}
      </Row>
      <Row label="E-mail" hint={q.data?.emailAvailable ? "To your sign-in address." : "This platform has no mail server yet."}>
        <Segmented<NPrefs["email"]> label="E-mail" value={p.email} onChange={(email) => set({ email })}
          options={[{ value: "off", label: "Off" }, { value: "instant", label: "Each one" }, { value: "daily", label: "Daily summary" }]} />
      </Row>
      <Row label="Only when at least" hint="Below this, notifications stay under the bell.">
        <Segmented<NPrefs["minSeverity"]> label="Minimum severity" value={p.minSeverity} onChange={(minSeverity) => set({ minSeverity })}
          options={[{ value: "info", label: "Info" }, { value: "warning", label: "Warning" }, { value: "critical", label: "Critical" }]} />
      </Row>
      <Row label="Quiet hours" hint="No pop-ups or e-mail in this window, in your time zone. Critical alerts still come through.">
        <div className="flex items-center gap-2">
          <input type="time" className="input h-9 w-auto" aria-label="Quiet from" value={hhmm(p.quietFrom)} onChange={(e) => set({ quietFrom: toMin(e.target.value), quietTo: p.quietTo ?? 7 * 60 })} />
          <span className="text-muted">to</span>
          <input type="time" className="input h-9 w-auto" aria-label="Quiet until" value={hhmm(p.quietTo)} onChange={(e) => set({ quietTo: toMin(e.target.value), quietFrom: p.quietFrom ?? 22 * 60 })} />
          {p.quietFrom !== undefined && <button className="btn btn-sm btn-ghost" onClick={() => set({ quietFrom: undefined, quietTo: undefined })}>Clear</button>}
        </div>
      </Row>
      <Row label="Do not send me" hint="Muted kinds stay under the bell only.">
        <div className="flex flex-wrap gap-1.5">
          {KINDS.map((k) => { const on = p.muted.includes(k.key); return (
            <button key={k.key} type="button" aria-pressed={on} onClick={() => set({ muted: on ? p.muted.filter((x) => x !== k.key) : [...p.muted, k.key] })}
              className={clsx("chip", on ? "chip-critical" : "")}>{on ? "Muted: " : ""}{k.label}</button>); })}
        </div>
      </Row>
      <Row label="Check it works">
        <div className="flex items-center gap-3">
          <button className="btn btn-sm" disabled={test.isPending} onClick={() => test.mutate()}>Send me a test</button>
          {test.data && <span className="text-[12.5px] text-text-2">Sent: under the bell{test.data.pushed ? `, and to ${test.data.pushed} browser${test.data.pushed > 1 ? "s" : ""}` : ""}.</span>}
          {save.isSuccess && <Saved show />}
        </div>
      </Row>
    </Card>
  );
}

function browserName(ua: string): string {
  const b = /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox" : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "Browser";
  const os = /Mac OS X/.test(ua) ? "macOS" : /Windows/.test(ua) ? "Windows" : /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Linux/.test(ua) ? "Linux" : "";
  return os ? `${b} on ${os}` : b;
}
