import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as DM from "@radix-ui/react-dropdown-menu";
import { ArrowRight, Check, ChevronDown, Eye, LayoutGrid, MoreHorizontal, Pencil, Plus, RotateCcw, Save, Trash2, Users, X } from "lucide-react";
import clsx from "clsx";
import { api, ApiError } from "../lib/api";
import { useMe } from "../lib/hooks";
import { KIND } from "../lib/vocab";
import { DUTY } from "../lib/vocab";
import { num } from "../lib/format";
import type { Duty, JobRole, User } from "../lib/types";
import { matchRole, useJobRoles } from "../lib/roles";
import { defaultOverview, defaultWidget, describeWidget, useCatalog, withIds, WIDGETS, type Widget, type WidgetType } from "../lib/dashboards";
import { freeSpot } from "../lib/grid";
import { ErrorBox, InfoTip, Modal, PageHeader, Skel } from "../components/ui";
import { Grid } from "../components/dash/Grid";
import { WidgetBody } from "../components/dash/WidgetView";
import { WidgetEditor } from "../components/dash/WidgetEditor";
import { Download } from "../components/Download";
import { SetupBanner, type Setup } from "./GetStarted";
import { Link, Navigate, useSearchParams } from "react-router-dom";

const sessionStorageGet = (k: string) => { try { return sessionStorage.getItem(k); } catch { return "1"; } };
const sessionStorageSet = (k: string, v: string) => { try { sessionStorage.setItem(k, v); } catch { /* private mode */ } };

interface Layout { source: "personal" | "team" | "default"; widgets: Widget[] | null; version: number; template?: { id: string; name: string } }

function greeting() {
  const h = new Date().getHours();
  return h < 12 ? "Good morning" : h < 19 ? "Good afternoon" : "Good evening";
}

/**
 * The Overview is each member's own dashboard: they move, resize, rename and
 * restyle widgets, add new ones, and the layout is saved for them. Until they
 * change it they see their team's layout (published by an admin) or the default.
 */
export default function Overview() {
  const me = useMe().data!;
  const admin = me.user.role === "admin";
  const qc = useQueryClient();
  const cat = useCatalog().data;
  const q = useQuery({ queryKey: ["my-layout"], queryFn: () => api.get<Layout>("/me/layout") });
  // Admins look at the Overview each job role starts from (?as=<role key>).
  const jobRoles = useJobRoles(me.org.kind);
  const [params, setParams] = useSearchParams();
  const viewRole = admin && !me.operator ? jobRoles.find((r) => r.key === params.get("as")) : undefined;
  const roleQ = useQuery({ queryKey: ["role-layout", viewRole?.key], queryFn: () => api.get<Layout>(`/me/layout?as=${encodeURIComponent(viewRole!.key)}`), enabled: !!viewRole });
  const shown = viewRole ? roleQ : q;
  const base = useMemo(() => shown.data ? (shown.data.widgets?.length ? shown.data.widgets : withIds(defaultOverview(me.org.kind, me.org.id, me.oversight))) : null, [shown.data]);
  const viewAs = (key: string | null) => {
    const next = new URLSearchParams(params);
    if (key) next.set("as", key); else next.delete("as");
    setParams(next, { replace: true });
    setEditing(false);
  };
  const [draft, setDraft] = useState<Widget[] | null>(null);
  const [editing, setEditing] = useState(false);
  const [editW, setEditW] = useState<Widget | null>(null);
  const [publish, setPublish] = useState(false);
  const [roles, setRoles] = useState(false);
  // Admins can edit the Overview a job role starts from, in place of their own.
  const [roleEdit, setRoleEdit] = useState<{ key: string; name: string } | null>(null);
  useEffect(() => { if (!editing) { setDraft(null); setRoleEdit(null); } }, [editing]);
  const widgets = (editing ? draft : null) ?? base ?? [];

  const save = useMutation({
    mutationFn: (ws: Widget[]) => api.send<Layout>("PUT", "/me/layout", { widgets: ws, version: q.data?.source === "personal" ? q.data.version : 0 }),
    onSuccess: (l) => { qc.setQueryData(["my-layout"], l); setEditing(false); },
  });
  const reset = useMutation({
    mutationFn: () => api.send("DELETE", "/me/layout"),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["my-layout"] }); setEditing(false); },
  });
  const publishRole = useMutation({
    mutationFn: (ws: Widget[]) => api.send("POST", "/me/layout/publish", { name: `${roleEdit!.name} overview`, description: "", audience: [`jobrole:${roleEdit!.key}`], widgets: ws }),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["dashboards"] }); void qc.invalidateQueries({ queryKey: ["my-layout"] }); void qc.invalidateQueries({ queryKey: ["role-layout"] }); setEditing(false); },
  });
  const dirty = editing && draft && (roleEdit ? true : JSON.stringify(draft) !== JSON.stringify(base));
  const conflict = save.error instanceof ApiError && save.error.status === 409;

  const setWidgets = (ws: Widget[]) => setDraft(ws);
  const add = (t: WidgetType) => {
    const w = defaultWidget(t, 0);
    setEditW({ ...w, ...freeSpot(widgets, w.w, w.h, 12) });
  };
  const upsert = (w: Widget) => {
    setDraft(widgets.some((x) => x.id === w.id) ? widgets.map((x) => (x.id === w.id ? w : x)) : [...widgets, w]);
    setEditW(null);
  };
  const scopeLine = me.oversight
    ? "Platform-wide figures for every organisation in the chain."
    : `Figures for ${me.org.name}: what you produced, shipped or received, and the passports you take part in.`;
  const editRole = () => { if (viewRole) { setDraft(widgets); setEditing(true); setRoleEdit({ key: viewRole.key, name: viewRole.name }); } };
  const sourceNote = q.data?.source === "team" ? `Your team's layout${q.data.template ? ` (${q.data.template.name})` : ""}` : q.data?.source === "default" ? "The standard layout" : "Your layout";

  // A brand-new organisation's admin starts in the guide (once per session).
  const setup = useQuery({ queryKey: ["org-setup"], queryFn: () => api.get<Setup>("/org/setup"), enabled: admin && !me.operator });
  const fresh = setup.data && !setup.data.dismissed && setup.data.steps.filter((s) => ["sites", "team", "data"].includes(s.key)).every((s) => !s.done);
  if (fresh && !sessionStorageGet("rp-guided")) { sessionStorageSet("rp-guided", "1"); return <Navigate to="/get-started" replace />; }

  return (
    <div>
      <PageHeader eyebrow={`${KIND[me.org.kind].name} · ${me.org.city || me.org.region}`}
        title={viewRole ? `${viewRole.name} · Overview` : `${greeting()}, ${me.user.name.split(" ")[0]}`}
        sub={viewRole ? viewRole.explain : scopeLine}
        actions={editing ? (
          <div className="flex flex-wrap items-center gap-2">
            <AddWidget onAdd={add} />
            <button className="btn" onClick={() => { setEditing(false); save.reset(); }}><X size={14} /> Cancel</button>
            {roleEdit
              ? <button className="btn btn-primary" disabled={publishRole.isPending || !draft} onClick={() => draft && publishRole.mutate(draft)}><Save size={14} /> Save for {roleEdit.name}</button>
              : <button className="btn btn-primary" disabled={save.isPending || !dirty} onClick={() => draft && save.mutate(draft)}><Save size={14} /> Save</button>}
          </div>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            {admin && !me.operator && jobRoles.length > 1 && <RoleSwitch roles={jobRoles} current={viewRole} onPick={viewAs} />}
            {viewRole ? (
              <button className="btn" onClick={editRole} disabled={!base}><Pencil size={14} /> Edit this role's Overview</button>
            ) : <>
            <Download kind="overview" />
            <button className="btn" onClick={() => { setDraft(widgets); setEditing(true); }} disabled={!base}><Pencil size={14} /> Customise</button>
            <DM.Root>
              <DM.Trigger className="btn btn-icon" aria-label="More layout options"><MoreHorizontal size={16} /></DM.Trigger>
              <DM.Portal><DM.Content align="end" sideOffset={6} className="z-50 min-w-[230px] rounded-xl bg-[var(--bg-elevated)] p-1 shadow-[var(--shadow-pop)]">
                <div className="px-3 py-1.5 text-[11.5px] text-muted">{sourceNote}</div>
                <DM.Item disabled={q.data?.source !== "personal"} onSelect={() => reset.mutate()} className="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-[13px] outline-none data-[disabled]:opacity-40 data-[highlighted]:bg-surface-3"><RotateCcw size={14} /> Reset to {admin ? "the team or standard" : "my team's"} layout</DM.Item>
                {admin && <DM.Item onSelect={() => setRoles(true)} className="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-[13px] outline-none data-[highlighted]:bg-surface-3"><Users size={14} /> Overview for each role…</DM.Item>}
                {admin && <DM.Item onSelect={() => setPublish(true)} className="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-[13px] outline-none data-[highlighted]:bg-surface-3"><Users size={14} /> Share this layout with roles…</DM.Item>}
              </DM.Content></DM.Portal>
            </DM.Root>
            </>}
          </div>
        )} />
      {viewRole ? <RoleStatus role={viewRole} layout={roleQ.data} roles={jobRoles} onEdit={editRole} /> : <SetupBanner />}
      {save.error ? <div className="mb-4">{conflict
        ? <div className="rounded-2xl bg-[var(--warning-soft)] px-4 py-3 text-[13px] text-warning">You saved this layout in another window. <button className="font-semibold underline" onClick={() => { void q.refetch(); setEditing(false); save.reset(); }}>Load that version</button></div>
        : <ErrorBox error={save.error} />}</div> : null}
      {roleEdit && <div className="mb-3 rounded-2xl bg-accent-soft px-4 py-2.5 text-[13px]"><b>Editing the Overview of {roleEdit.name}.</b> Members in this role who have not customised their own start from it. Figures show your own view of the data.</div>}
      {publishRole.error ? <div className="mb-4"><ErrorBox error={publishRole.error} /></div> : null}
      {editing && <p className="mb-3 text-[12.5px] text-muted">Drag a widget by its handle, resize it from the corner, or focus it and use the arrow keys (Shift to resize). Click a title to rename it, change its chart or pick other data.</p>}
      {shown.isLoading ? <Skel h={420} /> : shown.error ? <ErrorBox error={shown.error} /> : widgets.length === 0 ? (
        <div className="card flex flex-col items-center gap-3 py-14 text-center">
          <LayoutGrid size={22} className="text-muted" />
          <p className="text-[13.5px] text-text-2">Your Overview is empty.</p>
          {editing ? <AddWidget onAdd={add} /> : <button className="btn" onClick={() => { setDraft(withIds(defaultOverview(me.org.kind, me.org.id, me.oversight))); setEditing(true); }}>Start from the standard layout</button>}
        </div>
      ) : (
        <Grid widgets={widgets} editing={editing} onChange={setWidgets}
          render={(w, size) => (
            <section className={clsx("card flex h-full flex-col overflow-hidden rounded-2xl", w.type === "kpi" && "justify-between")} aria-label={w.title}>
              <header className="flex items-center gap-2 px-4 pb-1 pt-3.5">
                {editing ? (
                  <button type="button" className="min-w-0 flex-1 truncate text-left text-[13.5px] font-semibold hover:text-accent-ink" onClick={() => setEditW(w)}>{w.title || WIDGETS[w.type].name}</button>
                ) : <div className="flex min-w-0 flex-1 items-center gap-1.5"><h3 className="truncate text-[13.5px] font-semibold">{w.title}</h3><InfoTip text={describeWidget(w, cat)} /></div>}
                {editing && <button type="button" className="text-muted hover:text-text" aria-label={`Configure ${w.title}`} onClick={() => setEditW(w)}><Pencil size={13} /></button>}
                {editing && <button type="button" className="text-muted hover:text-critical" aria-label={`Remove ${w.title}`} onClick={() => setWidgets(widgets.filter((x) => x.id !== w.id))}><Trash2 size={13} /></button>}
              </header>
              <div className={clsx("min-h-0 flex-1 px-4 pb-3.5", (w.type === "activity" || w.type === "data_health") && "overflow-y-auto scroll-thin", w.type === "journey" && "overflow-hidden")}>
                <WidgetBody w={w} height={Math.max(40, size.height - 52)} width={Math.max(200, (w.w / 12) * size.width - 32)} />
              </div>
            </section>
          )} />
      )}
      <WidgetEditor widget={editW} isNew={!!editW && !widgets.some((x) => x.id === editW.id)} onClose={() => setEditW(null)} onSave={upsert} />
      {publish && <PublishModal widgets={widgets} onClose={() => setPublish(false)} />}
      {roles && <RoleLayouts onClose={() => setRoles(false)} onEdit={(r, ws) => {
        setRoles(false); setDraft(ws ? withIds(ws) : withIds(defaultOverview(me.org.kind, me.org.id, me.oversight))); setEditing(true); setRoleEdit(r);
      }} />}
    </div>
  );
}

function AddWidget({ onAdd }: { onAdd: (t: WidgetType) => void }) {
  const groups: { title: string; types: WidgetType[] }[] = [
    { title: "Ready-made", types: ["journey", "sites_map", "data_health", "activity", "text"] },
    { title: "Charts from your data", types: ["kpi", "timeseries", "bars", "donut", "gauge", "table", "map"] },
  ];
  return (
    <DM.Root>
      <DM.Trigger className="btn btn-accent"><Plus size={14} /> Add widget</DM.Trigger>
      <DM.Portal><DM.Content align="end" sideOffset={6} className="z-50 max-h-[70vh] w-[320px] overflow-y-auto rounded-xl bg-[var(--bg-elevated)] p-1 shadow-[var(--shadow-pop)] scroll-thin">
        {groups.map((g) => (
          <DM.Group key={g.title}>
            <DM.Label className="eyebrow px-3 pb-1 pt-2">{g.title}</DM.Label>
            {g.types.map((t) => (
              <DM.Item key={t} onSelect={() => onAdd(t)} className="flex cursor-pointer flex-col rounded-lg px-3 py-2 outline-none data-[highlighted]:bg-surface-3">
                <span className="text-[13px] font-semibold">{WIDGETS[t].name}</span><span className="text-[12px] text-muted">{WIDGETS[t].explain}</span>
              </DM.Item>
            ))}
          </DM.Group>
        ))}
      </DM.Content></DM.Portal>
    </DM.Root>
  );
}

interface TeamLayout { id: string; name: string; audience: string[]; widgets: Widget[]; updatedAt: string; updatedBy: string }

/** Which Overview each job role starts from; admins edit or remove each one. */
function RoleLayouts({ onClose, onEdit }: { onClose: () => void; onEdit: (r: { key: string; name: string }, widgets: Widget[] | null) => void }) {
  const me = useMe().data!;
  const qc = useQueryClient();
  const roles = useJobRoles(me.org.kind);
  const q = useQuery({ queryKey: ["dashboards"], queryFn: () => api.get<TeamLayout[]>("/dashboards") });
  const del = useMutation({ mutationFn: (id: string) => api.send("DELETE", `/dashboards/${id}`), onSuccess: () => { void qc.invalidateQueries({ queryKey: ["dashboards"] }); void qc.invalidateQueries({ queryKey: ["my-layout"] }); } });
  const forRole = (key: string) => q.data?.find((d) => d.audience.includes(`jobrole:${key}`));
  const general = q.data?.filter((d) => !d.audience.some((a) => a.startsWith("jobrole:"))) ?? [];
  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title="Overview for each role" width={620}
      sub="Each role starts from its own Overview. Members who customise theirs keep their own until they reset it.">
      {q.isLoading ? <Skel h={200} /> : (
        <ul className="flex flex-col divide-y divide-line">
          {roles.map((r) => {
            const d = forRole(r.key);
            return (
              <li key={r.key} className="flex items-center gap-3 py-2.5">
                <div className="min-w-0 flex-1">
                  <div className="text-[13.5px] font-semibold">{r.name}</div>
                  <div className="text-[12px] text-muted">{d ? `Own layout · ${d.widgets.length} widgets · updated ${new Date(d.updatedAt).toLocaleDateString()}` : general.length ? `Uses “${general[0].name}”` : "Uses the standard layout"}</div>
                </div>
                <button className="btn btn-sm" onClick={() => onEdit({ key: r.key, name: r.name }, d?.widgets ?? null)}><Pencil size={13} /> {d ? "Edit" : "Create"}</button>
                {d && <button className="btn btn-sm" aria-label={`Remove the layout of ${r.name}`} disabled={del.isPending} onClick={() => del.mutate(d.id)}><Trash2 size={13} /></button>}
              </li>
            );
          })}
        </ul>
      )}
      {del.error ? <div className="mt-3"><ErrorBox error={del.error} /></div> : null}
    </Modal>
  );
}

/** Admins hand their layout to a role or duty as its starting Overview. */
function PublishModal({ widgets, onClose }: { widgets: Widget[]; onClose: () => void }) {
  const qc = useQueryClient();
  const [audience, setAudience] = useState<string[]>([]);
  const [name, setName] = useState("Team overview");
  const m = useMutation({
    mutationFn: () => api.send("POST", "/me/layout/publish", { name, description: "", audience, widgets }),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["my-layout"] }); onClose(); },
  });
  const me = useMe().data!;
  const roles = useJobRoles(me.org.kind);
  // A named role reaches exactly its members (not everyone sharing one duty).
  const reach = (r: JobRole) => [`jobrole:${r.key}`];
  const on = (keys: string[]) => keys.every((k) => audience.includes(k));
  const toggle = (keys: string[]) => setAudience(on(keys) ? audience.filter((x) => !keys.includes(x)) : [...new Set([...audience, ...keys])]);
  const opt = (keys: string[], label: string, title?: string) => <button type="button" key={label} title={title} aria-pressed={on(keys)} onClick={() => toggle(keys)} className={clsx("chip", on(keys) ? "chip-accent" : "opacity-70 hover:opacity-100")}>{label}</button>;
  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title="Share this layout with a team">
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <p className="text-[13px] text-text-2">Members in the roles you pick start from this layout. Anyone who already customised their Overview keeps theirs until they reset it.</p>
        <label className="block"><span className="label">Name</span><input className="input" value={name} maxLength={80} onChange={(e) => setName(e.target.value)} /></label>
        <div>
          <span className="label">For</span>
          <div className="flex flex-wrap gap-1">{opt(["all"], "Everyone")}{roles.map((r) => opt(reach(r), r.name, r.explain))}</div>
        </div>
        {m.error ? <ErrorBox error={m.error} /> : null}
        <div className="flex justify-end gap-2"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={!audience.length || m.isPending}>Share</button></div>
      </form>
    </Modal>
  );
}

/** Admins switch the Overview between their own and the one each job role starts from. */
function RoleSwitch({ roles, current, onPick }: { roles: JobRole[]; current?: JobRole; onPick: (key: string | null) => void }) {
  const item = "flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-[13px] outline-none data-[highlighted]:bg-surface-3";
  return (
    <DM.Root>
      <DM.Trigger className={clsx("btn", current && "btn-accent")} aria-label="Whose Overview to show"><Eye size={14} /> {current ? current.name : "My Overview"} <ChevronDown size={13} /></DM.Trigger>
      <DM.Portal><DM.Content align="end" sideOffset={6} className="z-50 max-h-[70vh] w-[280px] overflow-y-auto rounded-xl bg-[var(--bg-elevated)] p-1 shadow-[var(--shadow-pop)] scroll-thin">
        <DM.Item onSelect={() => onPick(null)} className={item}><span className="w-3.5">{!current && <Check size={14} />}</span> My Overview</DM.Item>
        <DM.Separator className="my-1 h-px bg-line" />
        <DM.Label className="eyebrow px-3 pb-1 pt-1.5">View as a role</DM.Label>
        {roles.map((r) => (
          <DM.Item key={r.key} onSelect={() => onPick(r.key)} className={clsx(item, "items-start")} title={r.explain}>
            <span className="mt-0.5 w-3.5 shrink-0">{current?.key === r.key && <Check size={14} />}</span>
            <span className="flex flex-col"><span>{r.name}</span><span className="text-[11.5px] text-muted">{r.access === "admin" ? "Every duty" : r.duties?.length ? r.duties.map((d) => DUTY[d].name).join(" · ") : "Read-only"}</span></span>
          </DM.Item>
        ))}
      </DM.Content></DM.Portal>
    </DM.Root>
  );
}

interface QueueSummary { duty: Duty; waiting: number; unassigned: number; oldestDays: number }

/** The work queues a role sees: testing and releasing share the release queue. */
function roleQueues(role: JobRole, qs: QueueSummary[]): QueueSummary[] {
  if (role.access === "admin") return qs;
  const ds = role.duties ?? [];
  return qs.filter((q) => ds.includes(q.duty) || (q.duty === "release" && ds.includes("test")));
}

/** The state of one job role: who holds it, what waits for it, and which layout it starts from. */
function RoleStatus({ role, layout, roles, onEdit }: { role: JobRole; layout?: Layout; roles: JobRole[]; onEdit: () => void }) {
  const qc = useQueryClient();
  const team = useQuery({ queryKey: ["team", 30], queryFn: () => api.get<{ members: User[]; queues: QueueSummary[] }>("/org/team?days=30") });
  const dash = useQuery({ queryKey: ["dashboards"], queryFn: () => api.get<TeamLayout[]>("/dashboards") });
  const own = dash.data?.find((d) => d.audience.includes(`jobrole:${role.key}`));
  const del = useMutation({
    mutationFn: (id: string) => api.send("DELETE", `/dashboards/${id}`),
    onSuccess: () => { for (const k of ["dashboards", "my-layout", "role-layout"]) void qc.invalidateQueries({ queryKey: [k] }); },
  });
  const members = (team.data?.members ?? []).filter((m) => !m.disabledAt && matchRole(roles, m.role, m.duties)?.key === role.key);
  const queues = roleQueues(role, team.data?.queues ?? []);
  const layoutNote = layout?.source === "team"
    ? own ? `Its own layout · ${own.widgets.length} ${own.widgets.length === 1 ? "widget" : "widgets"} · updated ${new Date(own.updatedAt).toLocaleDateString()}` : `Uses “${layout.template?.name ?? "a shared layout"}”`
    : "Uses the standard layout";
  return (
    <div className="mb-4 grid gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.4fr)]">
      <div className="rounded-2xl bg-surface p-4 shadow-[var(--shadow-1)]">
        <div className="flex items-center gap-1.5 text-[12.5px] font-semibold text-text-2"><Users size={14} aria-hidden /> Members</div>
        {team.isLoading ? <Skel h={40} /> : team.error ? <ErrorBox error={team.error} /> : <>
          <div className="num mt-2 text-[24px] font-semibold tracking-tight">{members.length}<span className="ml-1 text-[12px] font-normal text-muted">{members.length === 1 ? "person" : "people"}</span></div>
          <div className="mt-0.5 truncate text-[12px] text-muted" title={members.map((m) => m.name).join(", ")}>{members.length ? members.map((m) => m.name).join(", ") : "Nobody holds this role yet."}</div>
        </>}
        <Link to="/settings?tab=team" className="mt-2 inline-flex items-center gap-1 text-[12.5px] font-semibold text-accent-ink hover:underline">Manage members <ArrowRight size={12} /></Link>
      </div>
      <div className="rounded-2xl bg-surface p-4 shadow-[var(--shadow-1)]">
        <div className="flex items-center gap-1.5 text-[12.5px] font-semibold text-text-2"><LayoutGrid size={14} aria-hidden /> Starting layout</div>
        <div className="mt-2 text-[13px]">{dash.isLoading ? "…" : layoutNote}</div>
        <div className="mt-0.5 text-[12px] text-muted">Members who customised their Overview keep their own.</div>
        <div className="mt-2 flex flex-wrap gap-2">
          <button className="btn btn-sm" onClick={onEdit}><Pencil size={13} /> {own ? "Edit" : "Create"}</button>
          {own && <button className="btn btn-sm" disabled={del.isPending} onClick={() => del.mutate(own.id)}><Trash2 size={13} /> Remove its own layout</button>}
        </div>
        {del.error ? <div className="mt-2"><ErrorBox error={del.error} /></div> : null}
      </div>
      <div className="rounded-2xl bg-surface p-4 shadow-[var(--shadow-1)]">
        <div className="flex items-center gap-1.5 text-[12.5px] font-semibold text-text-2"><ArrowRight size={14} aria-hidden /> Work waiting</div>
        {team.isLoading ? <Skel h={40} /> : !queues.length ? <p className="mt-2 text-[12.5px] text-muted">This role reads and reports; no work queue.</p> : (
          <div className="mt-2 grid grid-cols-2 gap-3 sm:grid-cols-4">
            {queues.map((q) => {
              const D = DUTY[q.duty];
              return (
                <div key={q.duty}>
                  <div className="flex items-center gap-1 text-[12px] text-muted"><D.icon size={12} aria-hidden />{D.name}</div>
                  <div className="num text-[20px] font-semibold tracking-tight">{num(q.waiting)}</div>
                  <div className={clsx("text-[11.5px]", q.unassigned ? "text-warning" : "text-muted")}>{q.unassigned} unassigned{q.oldestDays ? ` · ${q.oldestDays} d` : ""}</div>
                </div>
              );
            })}
          </div>
        )}
        {queues.length > 0 && <Link to="/work" className="mt-2 inline-flex items-center gap-1 text-[12.5px] font-semibold text-accent-ink hover:underline">Open work queues <ArrowRight size={12} /></Link>}
      </div>
    </div>
  );
}
