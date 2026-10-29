import { BrandCard } from "../components/BrandCard";
import { SeatNote } from "../components/SeatNote";
import { RolePicker } from "../components/RolePicker";
import { sectionName, useRoleTitle } from "../lib/roles";
import { useState } from "react";
import { Navigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, CheckCircle2, Clock3, KeyRound, MapPin, Pencil, Plus, RotateCcw, Send, Trash2, UserPlus } from "lucide-react";
import clsx from "clsx";
import { api, ApiError } from "../lib/api";
import { useMe } from "../lib/hooks";
import type { Duty, Invitation, Org, OrgKind, Site, User, SigningKeyView, AuditEntry } from "../lib/types";
import { DUTY, KIND, SITE_KIND, siteKindName } from "../lib/vocab";
import { COUNTRIES, countryName } from "../lib/countries";
import { ago, date, dateTime, num, shortHash } from "../lib/format";
import { Card, Copy_, ErrorBox, KV, Modal } from "../components/ui";
import { SiteModal } from "../components/SiteModal";
import { Sparkline } from "../components/charts/TimeChart";

// The organisation's settings, shown as sections of the Settings page. Everyone
// can read them; admins change them.


/** The old /organisation address now opens the Settings page. */
export default function Organisation() {
  return <Navigate to="/settings?tab=organisation" replace />;
}

export function OrganisationSection() {
  const me = useMe().data!;
  const admin = me.user.role === "admin";
  const own = useQuery({ queryKey: ["own-org"], queryFn: () => api.get<{ org: Org; sites: Site[] }>("/org") });
  const keys = useQuery({ queryKey: ["signing-keys"], queryFn: () => api.get<SigningKeyView[]>("/org/signing-keys") });
  const [editOpen, setEditOpen] = useState(false);
  const o = own.data?.org ?? me.org;
  const K = KIND[o.kind];
  const myKeys = (keys.data ?? []).filter((k) => k.orgId === o.id && !k.userId);

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <Card title="Profile" action={admin && <button className="btn btn-sm" onClick={() => setEditOpen(true)}><Pencil size={13} /> Edit</button>}>
          <KV items={[
            ["Name", o.name],
            ["Status", <span key="s" className={clsx("chip", o.status === "verified" ? "chip-good" : o.status === "pending" ? "chip-warning" : "chip-critical")}>
              {o.status === "verified" ? <CheckCircle2 size={12} /> : <Clock3 size={12} />}{o.status}{o.verifiedAt ? ` · ${date(o.verifiedAt)}` : ""}</span>],
            ["Role in the chain", <span key="k" className="inline-flex items-center gap-1.5"><K.icon size={14} style={{ color: K.color }} />{[o.kind, ...(o.kinds ?? []).filter((k) => k !== o.kind)].map((k) => KIND[k].name).join(" · ")}</span>],
            ["Tax / VAT / registration no.", o.taxId || "—"],
            ["Location", [o.city, o.province, o.region, countryName(o.country)].filter(Boolean).join(", ") || "—"],
            ["Website", o.website || "—"],
            ["Provenance", o.provenance],
            ["Member since", date(o.createdAt)],
          ]} />
        </Card>
        <Card title="Signing key" sub="Every record your company submits is signed with this Ed25519 key. The public half goes on the ledger when your company is verified.">
          {myKeys.map((k) => (
            <div key={k.id} className="flex items-start gap-3 rounded-xl bg-surface-2 p-3.5">
              <KeyRound size={18} className="mt-0.5 text-muted" />
              <div className="min-w-0 flex-1 text-[13px]">
                <div className="mono font-semibold">{k.id}</div>
                <div className="mono mt-0.5 break-all text-[11.5px] text-text-2">{k.publicKey}</div>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  <span className={clsx("chip", k.status === "active" ? "chip-good" : "chip-critical")}>{k.status}</span>
                  <span className={clsx("chip", k.onLedger ? "chip-good" : "chip-warning")}>{k.onLedger ? `on the ledger · tx ${shortHash(k.ledgerTxId, 5)}` : "not on the ledger yet"}</span>
                  <span className="chip">{k.custody === "vault" ? "held in the OpenBao vault" : k.custody === "platform" ? "sealed by the platform" : "held by you"}</span>
                </div>
              </div>
            </div>
          ))}
        </Card>
      </div>

      {admin && <PublicLedgerCard key={`${o.id}-${o.publicLedger}`} org={o} />}

      {admin && <BrandCard key={`${o.id}-${o.hasLogo}`} org={o} />}

      <SitesSection sites={own.data?.sites ?? []} admin={admin} editor={admin || me.user.role === "editor"} />
      {editOpen && <OrgEditModal org={o} onClose={() => setEditOpen(false)} />}
    </div>
  );
}

function OrgEditModal({ org, onClose }: { org: Org; onClose: () => void }) {
  const qc = useQueryClient();
  const [o, setO] = useState({ ...org });
  const m = useMutation({
    mutationFn: () => api.send("PUT", "/org", o),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["own-org"] }); void qc.invalidateQueries({ queryKey: ["me"] }); onClose(); },
  });
  const f = (k: "name" | "taxId" | "city" | "province" | "region" | "website", label: string) => (
    <div><label className="label" htmlFor={`o-${k}`}>{label}</label>
      <input id={`o-${k}`} className="input" value={(o[k] as string | undefined) ?? ""} onChange={(e) => setO({ ...o, [k]: e.target.value })} /></div>
  );
  return (
    <Modal open onOpenChange={(x) => !x && onClose()} title="Edit organisation" sub="Shown to other companies in the chain and on public passports.">
      <form className="grid grid-cols-2 gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <div className="col-span-2">{f("name", "Company name")}</div>
        {f("taxId", "Tax / VAT / registration no.")}
        <div><label className="label" htmlFor="o-country">Country</label>
          <select id="o-country" className="input" value={o.country} onChange={(e) => setO({ ...o, country: e.target.value })}>
            <option value="">Choose…</option>
            {COUNTRIES.map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}
          </select></div>
        {f("region", "Region / state / province")}{f("city", "City")}
        <div className="col-span-2">{f("website", "Website")}</div>
        {m.error ? <div className="col-span-2"><ErrorBox error={m.error} /></div> : null}
        <div className="col-span-2 flex justify-end gap-2"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={m.isPending}>Save</button></div>
      </form>
    </Modal>
  );
}

interface Activity { actor: string; created: Record<string, number>; corrections: number; receipts: Record<string, number>; openFindings: number; assigned: number; lastAction?: string; daily: number[] }
interface TeamView {
  days: number; members: User[]; activity: Record<string, Activity>; apiKeys: { id: string; name: string }[];
  queues: { duty: Duty; waiting: number; unassigned: number; oldestDays: number }[];
}

/** Who does what: each duty's queue, and each member's recent work (admins). */
export function TeamWork() {
  const me = useMe().data!;
  const title = useRoleTitle(me.org.kind);
  const [days, setDays] = useState(30);
  const t = useQuery({ queryKey: ["team", days], queryFn: () => api.get<TeamView>(`/org/team?days=${days}`) });
  if (t.error) return <ErrorBox error={t.error} />;
  const actors = t.data ? [
    ...t.data.members.map((u) => ({ id: u.id, name: u.name, sub: `${title(u.role, u.duties, u.steps)}${u.steps?.length ? ` · ${u.steps.map(sectionName).join(", ")}` : ""}`, a: t.data!.activity[u.id] })),
    ...t.data.apiKeys.filter((k) => t.data!.activity[k.id]).map((k) => ({ id: k.id, name: k.name, sub: "API key", a: t.data!.activity[k.id] })),
  ] : [];
  const total = (r?: Record<string, number>) => Object.values(r ?? {}).reduce((x, y) => x + y, 0);
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 xl:grid-cols-4">
        {(t.data?.queues ?? []).map((q) => {
          const D = DUTY[q.duty];
          return (
            <div key={q.duty} className="rounded-2xl bg-surface p-4 shadow-[var(--shadow-1)]">
              <div className="flex items-center gap-1.5 text-[12.5px] font-semibold text-text-2"><D.icon size={14} aria-hidden />{D.name}</div>
              <div className="num mt-2 text-[24px] font-semibold tracking-tight">{num(q.waiting)}<span className="ml-1 text-[12px] font-normal text-muted">waiting</span></div>
              <div className="mt-0.5 text-[12px] text-muted">{q.unassigned} unassigned{q.oldestDays ? ` · oldest ${q.oldestDays} d` : ""}</div>
            </div>
          );
        })}
      </div>
      <Card title="Work by member" sub="Records signed, intake reviews and open findings, from who actually signed them." pad={false}
        action={<select className="input h-8 w-auto" value={days} onChange={(e) => setDays(Number(e.target.value))} aria-label="Period">
          <option value={7}>Last 7 days</option><option value={30}>Last 30 days</option><option value={90}>Last 90 days</option><option value={365}>Last 12 months</option>
        </select>}>
        <div className="overflow-x-auto">
          <table className="table">
            <thead><tr><th>Member</th><th className="text-right">Records</th><th className="text-right">Intake reviews</th><th className="text-right">Corrections</th>
              <th className="text-right">Open findings</th><th className="text-right">Assigned</th><th className="w-[140px]">Activity</th><th>Last action</th></tr></thead>
            <tbody>
              {actors.map(({ id, name, sub, a }) => (
                <tr key={id}>
                  <td><div className="font-medium">{name}</div><div className="text-[12px] text-muted">{sub}</div></td>
                  <td className="num text-right">{num(total(a?.created))}</td>
                  <td className="num text-right">{num(total(a?.receipts))}{a?.receipts.rejected ? <span className="ml-1 text-[12px] text-critical">· {a.receipts.rejected} rejected</span> : null}</td>
                  <td className="num text-right">{num(a?.corrections ?? 0)}</td>
                  <td className={clsx("num text-right", (a?.openFindings ?? 0) > 0 && "font-semibold text-warning")}>{num(a?.openFindings ?? 0)}</td>
                  <td className="num text-right">{num(a?.assigned ?? 0)}</td>
                  <td>{a ? <Sparkline values={a.daily} height={26} /> : <span className="text-[12px] text-muted">—</span>}</td>
                  <td className="whitespace-nowrap text-text-2">{a?.lastAction ? ago(a.lastAction) : "—"}</td>
                </tr>
              ))}
              {t.isLoading && <tr><td colSpan={8} className="text-center text-muted">Loading…</td></tr>}
            </tbody>
          </table>
        </div>
      </Card>
    </div>
  );
}

export function TeamSection() {
  const me = useMe().data!;
  const qc = useQueryClient();
  const admin = me.user.role === "admin";
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.get<User[]>("/org/members") });
  const invites = useQuery({ queryKey: ["invitations"], queryFn: () => api.get<Invitation[]>("/org/invitations"), enabled: admin });
  const keys = useQuery({ queryKey: ["signing-keys"], queryFn: () => api.get<SigningKeyView[]>("/org/signing-keys") });
  const keyOf = new Map((keys.data ?? []).filter((k) => k.userId && k.status === "active").map((k) => [k.userId!, k]));
  const title = useRoleTitle(me.org.kind);
  const [invite, setInvite] = useState<{ email: string; role: string; duties: Duty[]; steps?: OrgKind[] }>({ email: "", role: "", duties: [] }); // the picker fills in the first fitting role
  const [link, setLink] = useState<string | null>(null);
  const inv = useMutation({
    mutationFn: () => api.send<{ link: string }>("POST", "/org/invitations", invite),
    onSuccess: (r) => { setLink(r.link); setInvite({ email: "", role: invite.role, duties: invite.duties, steps: invite.steps }); void qc.invalidateQueries({ queryKey: ["invitations"] }); void qc.invalidateQueries({ queryKey: ["org-setup"] }); },
  });
  const [removing, setRemoving] = useState<User | null>(null);
  const remove = useMutation({
    mutationFn: (id: string) => api.send("DELETE", `/org/members/${id}`),
    onSuccess: () => { setRemoving(null); void qc.invalidateQueries({ queryKey: ["members"] }); void qc.invalidateQueries({ queryKey: ["privacy"] }); void qc.invalidateQueries({ queryKey: ["org-setup"] }); },
  });
  const role = useMutation({
    mutationFn: ({ id, r, duties, steps }: { id: string; r?: string; duties?: Duty[]; steps?: OrgKind[] }) => api.send("PUT", `/org/members/${id}`, { role: r, duties, steps: steps ?? [] }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["members"] }),
  });

  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
      <Card title="Members" sub="Each person has a role for their job: intake, production, quality and dispatch can be different people. Administrators manage the team; observers read." pad={false}>
        <div className="overflow-x-auto">
          <table className="table">
            <thead><tr><th>Name</th><th>Role</th><th>Signing key</th><th>Last sign-in</th>{admin && <th><span className="sr-only">Actions</span></th>}</tr></thead>
            <tbody>
              {(members.data ?? []).map((u) => (
                <tr key={u.id}>
                  <td><div className="font-medium">{u.name}{u.id === me.user.id && <span className="ml-1.5 text-muted">(you)</span>}</div><div className="text-[12px] text-muted">{u.email}</div></td>
                  <td className="min-w-[200px]">
                    {admin && u.id !== me.user.id ? (
                      <RolePicker kind={me.org.kind} compact label={`Role of ${u.name}`} value={{ role: u.role, duties: u.duties ?? [], steps: u.steps ?? [] }}
                        onChange={(v) => role.mutate({ id: u.id, r: v.role, duties: v.duties, steps: v.steps })} />
                    ) : <span className="chip">{title(u.role, u.duties, u.steps)}{u.steps?.length ? ` · ${u.steps.map(sectionName).join(", ")}` : ""}</span>}
                  </td>
                  <td>{(() => {
                    const k = keyOf.get(u.id);
                    if (!k) return <span className="text-[12px] text-muted">issued at first signature</span>;
                    return <span className={clsx("chip", k.onLedger ? "chip-good" : "chip-warning")} title={`${k.id} · ${k.custody === "vault" ? "held in the vault" : k.custody}`}>
                      <KeyRound size={11} />{k.onLedger ? "on ledger" : "pending"}</span>;
                  })()}</td>
                  <td className="text-text-2">{u.lastLoginAt ? ago(u.lastLoginAt) : "never"}</td>
                  {admin && <td className="text-right">{u.id !== me.user.id && <button className="btn btn-sm btn-ghost text-critical" aria-label={`Remove ${u.name}`} onClick={() => setRemoving(u)}><Trash2 size={13} /></button>}</td>}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {role.error ? <div className="p-3"><ErrorBox error={role.error} /></div> : null}
      </Card>
      <Modal open={!!removing} onOpenChange={(o) => !o && setRemoving(null)} title={`Remove ${removing?.name ?? "member"}?`}
        sub="They can no longer sign in. Records they signed stay and keep verifying. Their personal data can then be exported or erased under Compliance → Data protection.">
        <div className="flex justify-end gap-2">
          <button className="btn" onClick={() => setRemoving(null)}>Cancel</button>
          <button className="btn btn-primary" disabled={remove.isPending} onClick={() => removing && remove.mutate(removing.id)}>Remove</button>
        </div>
        {remove.error ? <div className="mt-3"><ErrorBox error={remove.error} /></div> : null}
      </Modal>
      {admin && (
        <Card title="Invite a colleague" sub="They get a one-time link and choose their own password.">
          <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); inv.mutate(); }}>
            <input className="input" type="email" placeholder="name@company.com" value={invite.email} onChange={(e) => setInvite({ ...invite, email: e.target.value })} required aria-label="E-mail" />
            <RolePicker kind={me.org.kind} value={{ role: invite.role, duties: invite.duties }} onChange={(v) => setInvite({ ...invite, ...v })} />
            <SeatNote adding={invite.email.includes("@") ? 1 : 0} />
            <div className="flex justify-end">
              <button className="btn btn-primary" disabled={inv.isPending}><UserPlus size={14} /> Invite</button>
            </div>
            {inv.error ? <ErrorBox error={inv.error} /> : null}
          </form>
          {(invites.data ?? []).length > 0 && (
            <ul className="mt-4 flex flex-col gap-1.5 text-[12.5px]">
              {invites.data!.map((i) => (
                <li key={i.id} className="flex items-center justify-between rounded-lg bg-surface-2 px-3 py-2">
                  <span><Send size={12} className="mr-1.5 inline text-muted" />{i.email} · {title(i.role, i.duties, i.steps)}{i.steps?.length ? ` · ${i.steps.map(sectionName).join(", ")}` : ""}</span>
                  <span className="flex items-center gap-2 text-muted">expires {date(i.expiresAt)}
                    <button className="text-critical" aria-label="Revoke invitation" onClick={async () => { await api.send("DELETE", `/org/invitations/${i.id}`); void qc.invalidateQueries({ queryKey: ["invitations"] }); void qc.invalidateQueries({ queryKey: ["org-setup"] }); }}><Trash2 size={13} /></button>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>
      )}
      <Modal open={!!link} onOpenChange={(o) => !o && setLink(null)} title="Invitation link" sub="Send this link to your colleague. It works once and expires in 7 days.">
        <div className="flex items-center gap-2 rounded-xl bg-surface-2 px-3 py-2.5">
          <span className="mono flex-1 break-all text-[12.5px]">{link}</span>
          {link && <Copy_ text={link} label="Copy link" />}
        </div>
      </Modal>
    </div>
  );
}

export function ActivitySection() {
  const audit = useQuery({ queryKey: ["audit"], queryFn: () => api.get<AuditEntry[]>("/audit") });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.get<User[]>("/org/members") });
  const [who, setWho] = useState("");
  const names = new Map((members.data ?? []).map((u) => [u.id, u.name]));
  const rows = (audit.data ?? []).filter((a) => !who || a.actor === who);
  return (
    <Card title="Activity" sub="Who did what in your organisation: sign-ins aside, every change is recorded here." pad={false}
      action={<select className="input h-8 w-auto" value={who} onChange={(e) => setWho(e.target.value)} aria-label="Filter by person">
        <option value="">Everyone</option>
        {(members.data ?? []).map((u) => <option key={u.id} value={u.id}>{u.name}</option>)}
      </select>}>
      {audit.error instanceof ApiError && audit.error.status === 403 ? <div className="p-4 text-[13px] text-muted">Only admins see the activity log.</div> : (
        <div className="max-h-[560px] overflow-auto scroll-thin">
          <table className="table text-[12.5px]">
            <thead><tr><th>When</th><th>Action</th><th>Subject</th><th>By</th></tr></thead>
            <tbody>
              {rows.map((a) => (
                <tr key={a.id}>
                  <td className="whitespace-nowrap text-text-2">{dateTime(a.at)}</td>
                  <td className="font-medium">{a.action}</td>
                  <td className="mono text-text-2">{a.subject}</td>
                  <td className="text-text-2">{names.get(a.actor) ?? <span className="mono">{a.actor}</span>}</td>
                </tr>
              ))}
              {!rows.length && <tr><td colSpan={4} className="text-center text-muted">Nothing recorded yet.</td></tr>}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

/** The organisation's private address book: its own sites, and the partner
 * companies' sites it ships to. Other customers never see it. */
function SitesSection({ sites, admin, editor }: { sites: Site[]; admin: boolean; editor: boolean }) {
  const [edit, setEdit] = useState<{ partner: boolean; site?: Site } | null>(null);
  const [archived, setArchived] = useState(false);
  const qc = useQueryClient();
  const arch = useMutation({
    mutationFn: (x: Site) => api.send("POST", `/org/sites/${x.id}/archive`, { archived: !x.archivedAt }),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["own-org"] }); void qc.invalidateQueries({ queryKey: ["sites"] }); },
  });
  const shown = sites.filter((x) => archived || !x.archivedAt);
  const anyArchived = sites.some((x) => x.archivedAt);
  const table = (rows: Site[], partner: boolean) => {
    const may = partner ? editor : admin;
    return (
      <div className="overflow-x-auto">
        <table className="table">
          <thead><tr><th>Site</th>{partner && <th>Company</th>}<th>Type</th><th>Location</th>{!partner && <th className="text-right">Capacity</th>}<th>Id</th>{may && <th />}</tr></thead>
          <tbody>
            {rows.map((x) => {
              const T = SITE_KIND[x.kind] ?? SITE_KIND.other;
              return (
                <tr key={x.id} className={clsx(x.archivedAt && "opacity-60")}>
                  <td className="font-medium">{x.name}{x.archivedAt && <span className="chip ml-2">archived</span>}</td>
                  {partner && <td className="text-text-2">{x.partner}</td>}
                  <td><span className="inline-flex items-center gap-1.5 text-[12.5px]"><T.icon size={13} style={{ color: T.color }} />{siteKindName(x)}</span></td>
                  <td className="text-text-2"><span className="inline-flex items-center gap-1"><MapPin size={12} />{[x.city, x.province, x.country && x.country !== "" ? countryName(x.country) : ""].filter(Boolean).join(", ") || "—"}</span></td>
                  {!partner && <td className="num text-right">{x.capacityTonnesPerYear ? `${num(x.capacityTonnesPerYear)} t/yr` : "—"}</td>}
                  <td><span className="mono text-[12px]">{x.id}</span><Copy_ text={x.id} /></td>
                  {may && <td className="whitespace-nowrap text-right">
                    <button className="btn btn-sm btn-ghost" aria-label={`Edit ${x.name}`} onClick={() => setEdit({ partner, site: x })}><Pencil size={13} /></button>
                    <button className="btn btn-sm btn-ghost" aria-label={`${x.archivedAt ? "Restore" : "Archive"} ${x.name}`} disabled={arch.isPending}
                      onClick={() => arch.mutate(x)}>{x.archivedAt ? <RotateCcw size={13} /> : <Archive size={13} />}</button>
                  </td>}
                </tr>
              );
            })}
            {rows.length === 0 && <tr><td colSpan={7} className="text-center text-muted">{partner ? "No partner sites yet. Add the sites you ship to." : "No sites yet."}</td></tr>}
          </tbody>
        </table>
      </div>
    );
  };
  const toggle = anyArchived && <label className="flex items-center gap-1.5 text-[12px] text-muted"><input type="checkbox" checked={archived} onChange={(e) => setArchived(e.target.checked)} /> Show archived</label>;
  return (
    <>
      <Card title="Our sites" sub="Your company's plants, yards and depots. Records name the site they come from; shipments between your own sites are received by your team." pad={false}
        action={<div className="flex items-center gap-3">{toggle}{admin && <button className="btn btn-sm" onClick={() => setEdit({ partner: false })}><Plus size={13} /> Add site</button>}</div>}>
        {table(shown.filter((x) => !x.partner), false)}
      </Card>
      <Card title="Partner sites" sub="The companies you work with in the supply chain and their sites. Only your company sees this list; shipments to a partner site are recorded as external (no receiver signature)." pad={false}
        action={editor && <button className="btn btn-sm" onClick={() => setEdit({ partner: true })}><Plus size={13} /> Add partner site</button>}>
        {table(shown.filter((x) => !!x.partner), true)}
      </Card>
      {edit && <SiteModal partner={edit.partner} site={edit.site} onClose={() => setEdit(null)} />}
    </>
  );
}

export { DutyPicker } from "../components/RolePicker";

/** Opt-in to the public /explorer page. Off by default; the demo workspace is always public. */
function PublicLedgerCard({ org }: { org: Org }) {
  const qc = useQueryClient();
  const [on, setOn] = useState(org.publicLedger ?? false); // flips at once; goes back if the save fails
  const set = useMutation({
    mutationFn: (v: boolean) => api.send("PUT", "/org/public-ledger", { public: v }),
    onMutate: (v) => setOn(v),
    onError: (_e, v) => setOn(!v),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["own-org"] }); void qc.invalidateQueries({ queryKey: ["me"] }); },
  });
  return (
    <Card title="Public ledger explorer" sub="Show this organisation's blocks, seal transactions and Merkle roots on the public Explorer page. Only fingerprints are shown, never record contents, names of sites or prices. Off by default.">
      <div className="flex flex-wrap items-center gap-4">
        <label className="flex cursor-pointer items-center gap-3 text-[13.5px] font-medium">
          <input type="checkbox" role="switch" className="size-4 accent-[var(--accent)]" checked={on} disabled={org.demo}
            onChange={(e) => set.mutate(e.target.checked)} />
          Show our ledger data in the public explorer
        </label>
        <span className={clsx("chip", on ? "chip-good" : "")}>{on ? "public" : "private"}</span>
        {on && <a href="/explorer" target="_blank" rel="noreferrer" className="text-[13px] font-medium text-accent-ink hover:underline">Open the explorer</a>}
        {org.demo && <span className="text-[12.5px] text-muted">The demo workspace is always public.</span>}
      </div>
      {set.error ? <div className="mt-3"><ErrorBox error={set.error} /></div> : null}
    </Card>
  );
}
