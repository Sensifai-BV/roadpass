import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Loader2, Plus, Inbox, Stamp, Ban, TestTube2, Send, PackageCheck, MessageSquareWarning, Truck } from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";
import { useDirectory, useMe, useMeta } from "../lib/hooks";
import type { Duty, Finding, Meta, Passport, RecordType, User, Work as WorkT, WorkItem } from "../lib/types";
import { DUTY, TYPE, siteKindName, siteLabel } from "../lib/vocab";
import { SiteModal } from "../components/SiteModal";
import { ago, dateTime, exactMass } from "../lib/format";
import { Card, Empty, ErrorBox, Modal, PageHeader, StatusChip, TypeBadge } from "../components/ui";
import { RecordForm, type Draft } from "../components/RecordForm";

/** Assignment controls shared by every lane (admins assign; members see theirs). */
interface AssignCtx { admin: boolean; me: string; members: User[]; assign: (recordId: string, duty: Duty, userId: string) => void; mineOnly: boolean }
const Assign = createContext<AssignCtx | null>(null);

// A datetime-local value for now, or for `notBefore` if that is later, rounded up
// to the minute (the input has minute precision; rounding down could date a
// receipt before the shipment it confirms).
/** "a" or "an" before a record type's name ("an additive batch", "an asphalt lot"). */
const article = (name: string) => (/^[aeiou]/i.test(name) ? "an" : "a");

/** "Melt flow index: 1.5 g/10min" from a lab test's data. */
function labLine(d: Record<string, unknown> | undefined, tests: Meta["labTests"] | undefined) {
  const t = tests?.[String(d?.test ?? "")];
  return t ? `${t.name}: ${String(d?.value ?? "")} ${t.unit}` : `${String(d?.test ?? "Test")}: ${String(d?.value ?? "")}`;
}

function localNow(notBefore?: string) {
  const t = Math.max(Date.now(), notBefore ? Date.parse(notBefore) : 0);
  const d = new Date(Math.ceil(t / 60000) * 60000);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

export default function Work() {
  const me = useMe().data!;
  const meta = useMeta().data;
  const dir = useDirectory();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["work"], queryFn: () => api.get<WorkT>("/work"), refetchInterval: 30_000 });
  const [draft, setDraft] = useState<Draft | null>(null);
  // /work?new=<type> opens the form for that record type (from the get-started guide).
  const [params, setParams] = useSearchParams();
  useEffect(() => {
    const t = params.get("new") as RecordType | null;
    if (t && TYPE[t]) { setDraft({ type: t }); setParams((p) => { const n = new URLSearchParams(p); n.delete("new"); return n; }, { replace: true }); }
  }, [params, setParams]);
  const [receive, setReceive] = useState<WorkItem | null>(null);
  const [selProcess, setSelProcess] = useState<string[]>([]);
  const [selShip, setSelShip] = useState<string[]>([]);
  const [ship, setShip] = useState<WorkItem[] | null>(null);
  const [done, setDone] = useState<string | null>(null);
  // A lane shows when the member holds the duty and the company does that step:
  // a lab admin holds every duty but neither receives, produces nor ships.
  // A member assigned to some sections of the chain works only in those (as the
  // server checks): a mixing operator is not offered reject or additive forms.
  const orgKinds = [me.org.kind, ...(me.org.kinds ?? [])];
  const mySteps = me.user.role === "admin" ? [] : orgKinds.filter((k) => me.user.steps?.includes(k));
  const kinds = mySteps.length ? mySteps : orgKinds;
  const doesStep: Record<Duty, boolean> = {
    receive: kinds.some((k) => !!meta?.consumes[k] || k === "contractor"), // contractors take deliveries at the paver
    produce: kinds.some((k) => !!meta?.produces[k]),
    test: kinds.some((k) => meta?.types.find((t) => t.type === "lab_test")?.createdBy.includes(k)),
    release: kinds.some((k) => k === "recycler" || k === "additive_producer"),
    ship: kinds.some((k) => ["sorting_plant", "recycler", "additive_producer", "asphalt_plant"].includes(k)),
  };
  const has = (d: Duty) => me.duties.includes(d) && doesStep[d];
  // Road owners (for their roads) and oversight accept blockers as the independent party.
  const reviews = (kinds.includes("road_owner") && me.duties.includes("release")) || me.oversight;
  const review = useQuery({ queryKey: ["findings", "review"], enabled: reviews,
    queryFn: () => api.get<{ items: Finding[]; total: number }>("/findings?status=open&limit=50") });
  const toReview = (review.data?.items ?? []).filter((f) => f.severity === "blocker" && f.orgId !== me.org.id);
  const admin = me.user.role === "admin";
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.get<User[]>("/org/members"), enabled: admin });
  const [mineOnly, setMineOnly] = useState(false);
  const assign = useMutation({
    mutationFn: ({ recordId, duty, userId }: { recordId: string; duty: Duty; userId: string }) => api.send("POST", "/work/assign", { recordId, duty, userId }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["work"] }),
  });
  // What the company makes at each of its steps, and from what: one that
  // recycles and makes additive records both, each from its own input.
  const makes = kinds.flatMap((k) => {
    const t = meta?.produces[k] as RecordType | undefined;
    return t ? [{ type: t, input: meta?.consumes[k] as RecordType | undefined }] : [];
  }).filter((m, i, a) => a.findIndex((x) => x.type === m.type) === i);
  const consumes = kinds.some((k) => !!meta?.consumes[k]);
  const lotsOnly = shipKinds(kinds);
  const w = q.data?.queues;

  const refresh = (msg: string) => {
    setDone(msg);
    setSelProcess([]);
    setSelShip([]);
    void qc.invalidateQueries();
    setTimeout(() => setDone(null), 4000);
  };

  const [releasing, setReleasing] = useState<{ item: WorkItem; decision: ReleaseDecision } | null>(null);

  if (me.org.kind === "reporter" || (!me.duties.length && !me.oversight)) {
    return <div><PageHeader title="My work" /><Card><Empty icon={<Inbox size={20} />} title="No duties assigned">Ask your organisation's admin to give you a duty (intake, production, quality or dispatch).</Empty></Card></div>;
  }

  const mine = (items: WorkItem[]) => (mineOnly ? items.filter((i) => i.assignment?.userId === me.user.id) : items);
  const processItems = mine(w?.process.items ?? []);
  const shipItems = mine(w?.ship.items ?? []);
  const assignedToMe = w ? [w.receive, w.process, w.release, w.ship].reduce((n, q) => n + (q.items ?? []).filter((i) => i.assignment?.userId === me.user.id).length, 0) : 0;

  return (
    <div>
      <PageHeader title="My work" sub={`What ${me.org.name} has to do next, for your duties. Every action is signed with your company key and your own key.`}
        actions={
          <div className="flex flex-wrap items-center gap-1.5">
            {assignedToMe > 0 && (
              <label className="mr-2 flex items-center gap-1.5 text-[12.5px] font-medium">
                <input type="checkbox" checked={mineOnly} onChange={(e) => setMineOnly(e.target.checked)} /> Only assigned to me ({assignedToMe})
              </label>
            )}
            {me.duties.filter((d) => doesStep[d]).map((d) => { const D = DUTY[d]; return <span key={d} className="chip"><D.icon size={12} />{D.name}</span>; })}
          </div>
        } />
      {done && <div className="rise mb-4 flex items-center gap-2 rounded-2xl bg-[var(--good-soft)] px-4 py-2.5 text-[13px] font-semibold text-good"><CheckCircle2 size={16} />{done}</div>}
      {q.isError && <ErrorBox error={q.error} />}

      {assign.error ? <ErrorBox error={assign.error} /> : null}
      <Assign.Provider value={{ admin, me: me.user.id, members: members.data ?? [], mineOnly,
        assign: (recordId, duty, userId) => assign.mutate({ recordId, duty, userId }) }}>
      <div className="grid gap-4 xl:grid-cols-2 2xl:grid-cols-4">
        {has("receive") && (
          <Lane icon={<PackageCheck size={15} />} title="To receive" sub={kinds.includes("contractor") && !kinds.some((k) => !!meta?.consumes[k]) ? "Asphalt deliveries for your sections. Check mix and temperature at the paver, then sign." : "Shipments on their way to you. Confirm what the weighbridge read."} count={w?.receive.total} color="var(--s1)">
            {mine(w?.receive.items ?? []).map((it) => (
              <Item key={it.record.id} it={it} duty="receive" sub={`${exactMass(it.record.massKg)} from ${dir.orgName(it.record.orgId)} · sent ${ago(it.record.occurredAt)}`}
                action={<button className="btn btn-sm btn-primary" onClick={() => setReceive(it)}>Receive</button>} />
            ))}
          </Lane>
        )}
        {has("produce") && (
          <Lane icon={<Plus size={15} />} title={makes.length === 1 && doesStep.receive ? `To process into ${TYPE[makes[0].type].plural.toLowerCase()}` : "Production"}
            sub={consumes ? "Material in stock not yet used. Select what went into today's lot." : "Record what you produced."}
            count={w?.process.total} color="var(--s2)"
            footer={makes.length > 0 && (
              <div className="flex flex-col gap-2">
                {makes.map(({ type: produces, input }) => {
                  // Each product takes only its own input type from the selection.
                  const sel = processItems.filter((i) => selProcess.includes(i.record.id) && (!input || i.record.type === input));
                  return (
                    <button key={produces} className="btn btn-primary w-full justify-center" onClick={() => {
                      const inKg = sel.reduce((s, i) => s + i.remainingKg, 0);
                      // An asphalt lot is ~200× its additive: never guess its mass from the input.
                      const asphalt = produces === "asphalt_lot";
                      setDraft({ type: produces, inputs: input ? sel.map((i) => ({ id: i.record.id, massKg: Math.round(i.remainingKg) })) : undefined,
                        massKg: input && sel.length && !asphalt ? inKg * (produces === "recycled_lot" ? 0.65 : 1) : undefined,
                        data: asphalt && sel.length ? { additive_kg: Math.round(inKg) } : undefined,
                        title: `Record ${article(TYPE[produces].name)} ${TYPE[produces].name.toLowerCase()}` });
                    }}>
                      <Plus size={14} /> Record {TYPE[produces].name.toLowerCase()}{input && sel.length ? ` from ${sel.length} input${sel.length > 1 ? "s" : ""}` : ""}
                    </button>
                  );
                })}
              </div>
            )}>
            {processItems.map((it) => (
              <Item key={it.record.id} it={it} duty="produce" selectable checked={selProcess.includes(it.record.id)}
                onCheck={(c) => setSelProcess((s) => (c ? [...s, it.record.id] : s.filter((x) => x !== it.record.id)))}
                sub={`${exactMass(it.remainingKg)} left · received ${ago(it.since)}`} />
            ))}
          </Lane>
        )}
        {reviews && (
          <Lane icon={<MessageSquareWarning size={15} />} title="Findings to review" sub="Blockers on records you take part in. Accept one with a signed reason when it is explained, or leave it to the company to correct." count={toReview.length} color="var(--s6)">
            {toReview.map((f) => (
              <li key={f.id} className="flex items-center gap-3 rounded-xl px-2 py-2 hover:bg-surface-2">
                <div className="min-w-0 flex-1"><div className="truncate text-[13px] font-semibold">{f.code} · {f.message}</div>
                  <div className="text-[12px] text-muted"><span className="mono">{f.recordId}</span> · {dir.orgName(f.orgId)} · {ago(f.createdAt)}</div></div>
                <Link className="btn btn-sm" to={`/quality?record=${f.recordId}&finding=${f.id}`}>Review</Link>
              </li>
            ))}
          </Lane>
        )}
        {has("test") && (!doesStep.release || testsBeyondRelease(kinds, meta?.types.find((t) => t.type === "lab_test")?.createdBy ?? [])) && (
          <Lane icon={<TestTube2 size={15} />} title="Lab tests" sub="Record a measurement on a lot, batch, delivery or road section by its passport ID." count={null} color="var(--s3)"
            footer={<button className="btn btn-primary w-full justify-center" onClick={() => setDraft({ type: "lab_test", title: "Record a lab test" })}><TestTube2 size={14} /> Record lab test</button>}>
            {[]}
          </Lane>
        )}
        {(has("test") || has("release")) && doesStep.release && (
          <Lane icon={<Stamp size={15} />} title="To test and release" sub="Your lots without a quality decision. Nothing ships until quality releases it." count={w?.release.total} color="var(--s3)">
            {mine(w?.release.items ?? []).map((it) => (
              <Item key={it.record.id} it={it} duty="release" sub={`made ${ago(it.record.occurredAt)}`}
                action={
                  <div className="flex gap-1">
                    {has("test") && <button className="btn btn-sm" title="Add lab test" aria-label={`Add lab test on ${it.record.id}`} onClick={() => setDraft({ type: "lab_test", data: { subject_id: it.record.id }, title: `Lab test on ${it.record.id}` })}><TestTube2 size={13} /></button>}
                    {has("release") && <>
                      <button className="btn btn-sm btn-accent" onClick={() => setReleasing({ item: it, decision: "released" })}>Release</button>
                      <button className="btn btn-sm" title="Quarantine" aria-label="Quarantine" onClick={() => setReleasing({ item: it, decision: "quarantined" })}><Ban size={13} /></button>
                    </>}
                  </div>
                } />
            ))}
          </Lane>
        )}
        {has("ship") && (
          <Lane icon={<Send size={15} />} title="Ready to ship" sub={lotsOnly === "asphalt" ? "Asphalt lots with mass left: record each truckload delivered to a road section."
              : lotsOnly === "sorted" ? "Sorted lots with mass left. Select one truckload."
              : lotsOnly === "released" ? "Released lots with mass left. Select one truckload."
              : "Lots with mass left. Select one truckload of sorted or released lots; deliver asphalt by the truckload to a road section."} count={w?.ship.total} color="var(--s4)"
            footer={lotsOnly !== "asphalt" && <button className="btn btn-primary w-full justify-center" disabled={!selShip.length}
              onClick={() => setShip(shipItems.filter((i) => selShip.includes(i.record.id)))}><Send size={14} /> Ship {selShip.length || ""} selected</button>}>
            {shipItems.map((it) => it.record.type === "asphalt_lot" ? (
              // Asphalt leaves by the truckload, each ticket naming the road section it is laid on.
              <Item key={it.record.id} it={it} duty="ship" sub={`${exactMass(it.remainingKg)} to deliver`}
                action={<button className="btn btn-sm btn-accent" onClick={() => {
                  const load = Math.round(Math.min(it.remainingKg, 25000));
                  setDraft({ type: "delivery", inputs: [{ id: it.record.id, massKg: load }], massKg: load, title: `Delivery from ${it.record.id}` });
                }}><Truck size={13} /> Deliver</button>} />
            ) : (
              <Item key={it.record.id} it={it} duty="ship" selectable checked={selShip.includes(it.record.id)}
                onCheck={(c) => setSelShip((s) => (c ? [...s, it.record.id] : s.filter((x) => x !== it.record.id)))}
                sub={`${exactMass(it.remainingKg)} to ship${it.decision ? " · released" : ""}`} />
            ))}
          </Lane>
        )}
      </div>
      </Assign.Provider>
      <p className="mt-4 text-[12px] text-muted">Queues cover the last {q.data?.windowDays ?? 90} days. Older open items are in Passports.</p>

      <RecordForm draft={draft} onClose={() => setDraft(null)} onDone={(ids) => { setDraft(null); refresh(`Saved and signed: ${ids.join(", ")}`); }} />
      <ReleaseModal value={releasing} onClose={() => setReleasing(null)} onDone={(id, d) => { setReleasing(null); refresh(`${id} ${d}.`); }}
        onTest={has("test") ? (id) => { setReleasing(null); setDraft({ type: "lab_test", data: { subject_id: id }, title: `Lab test on ${id}` }); } : undefined} />
      <ReceiveModal item={receive} onClose={() => setReceive(null)} onDone={(id) => { setReceive(null); refresh(`Receipt for ${id} signed.`); }} />
      <ShipModal items={ship} onClose={() => setShip(null)} onDone={(id) => { setShip(null); refresh(`Shipment ${id} recorded.`); }} kind={me.org.kind} />
    </div>
  );
}

/** What a company ships, when it is one thing only (null: several kinds of lot). */
function shipKinds(kinds: string[]): "asphalt" | "sorted" | "released" | null {
  const ks = kinds.filter((k) => ["sorting_plant", "recycler", "additive_producer", "asphalt_plant"].includes(k));
  const sorts = ks.includes("sorting_plant"), releases = ks.some((k) => k === "recycler" || k === "additive_producer"), asphalt = ks.includes("asphalt_plant");
  if (Number(sorts) + Number(releases) + Number(asphalt) > 1) return null;
  return asphalt ? "asphalt" : sorts ? "sorted" : "released";
}

/** Whether the company tests more than its own lots before release (asphalt, sections, deliveries). */
function testsBeyondRelease(kinds: string[], testers: string[]): boolean {
  return kinds.some((k) => k !== "recycler" && k !== "additive_producer" && testers.includes(k));
}

function Lane({ icon, title, sub, count, color, children, footer }: {
  icon: ReactNode; title: string; sub: string; count?: number | null; color: string; children: ReactNode; footer?: ReactNode;
}) {
  const empty = !Array.isArray(children) || children.length === 0;
  return (
    <Card className="flex flex-col" pad={false}>
      <div className="flex items-start gap-2.5 px-4 pb-2 pt-4">
        <span className="grid size-7 shrink-0 place-items-center rounded-lg" style={{ background: `color-mix(in srgb, ${color} 15%, transparent)`, color }}>{icon}</span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2"><span className="text-[14px] font-semibold">{title}</span>{count !== null && <span className="chip num">{count ?? "…"}</span>}</div>
          <div className="text-[12px] text-muted">{sub}</div>
        </div>
      </div>
      <div className="scroll-thin max-h-[480px] min-h-[120px] flex-1 overflow-y-auto px-2 pb-2">
        {empty ? <div className="grid h-[110px] place-items-center text-[12.5px] text-muted">Nothing waiting.</div> : <ul className="flex flex-col gap-1">{children}</ul>}
      </div>
      {footer && <div className="border-t border-line p-3">{footer}</div>}
    </Card>
  );
}

function Item({ it, sub, action, selectable, checked, onCheck, duty }: {
  it: WorkItem; sub: string; action?: ReactNode; selectable?: boolean; checked?: boolean; onCheck?: (c: boolean) => void; duty: Duty;
}) {
  const r = it.record;
  const ctx = useContext(Assign);
  const a = it.assignment;
  const holders = (ctx?.members ?? []).filter((u) => u.role === "admin" || (u.role === "editor" && (u.duties ?? []).includes(duty === "release" ? "release" : duty)));
  const assignee = a && (a.userId === ctx?.me ? "you" : ctx?.members.find((u) => u.id === a.userId)?.name ?? "a colleague");
  return (
    <li className={clsx("flex items-center gap-2.5 rounded-xl px-2.5 py-2", checked ? "bg-accent-soft" : a?.userId === ctx?.me ? "bg-[color-mix(in_srgb,var(--accent)_6%,transparent)]" : "hover:bg-surface-2")}>
      {selectable && <input type="checkbox" className="size-4 accent-[var(--accent)]" checked={checked} onChange={(e) => onCheck?.(e.target.checked)} aria-label={`Select ${r.id}`} />}
      <TypeBadge type={r.type} size={24} />
      <div className="min-w-0 flex-1">
        <Link to={TYPE[r.type].passport ? `/passports/${r.id}` : `/passports/${r.inputs?.[0]?.id ?? r.id}`} className="mono block truncate text-[12.5px] font-semibold hover:text-accent-ink">{r.id}</Link>
        <div className="truncate text-[11.5px] text-muted">{sub}{assignee ? <> · <span className={clsx(a?.userId === ctx?.me && "font-semibold text-accent-ink")}>assigned to {assignee}</span></> : null}</div>
      </div>
      {r.status !== "ok" && <StatusChip status={r.status} withIcon={false} />}
      {ctx?.admin && (
        <select className="input h-7 w-[104px] px-1.5 text-[11.5px]" value={a?.userId ?? ""} aria-label={`Assign ${r.id}`}
          onChange={(e) => ctx.assign(r.id, duty, e.target.value)}>
          <option value="">Team</option>
          {holders.map((u) => <option key={u.id} value={u.id}>{u.name.split(" ")[0]}</option>)}
        </select>
      )}
      {action}
    </li>
  );
}

type Decision = "accepted" | "accepted_with_note" | "rejected";
const DECISIONS: { value: Decision; label: string; explain: string; icon: typeof CheckCircle2; tone: string }[] = [
  { value: "accepted", label: "Accept", explain: "Arrived as described.", icon: CheckCircle2, tone: "var(--good)" },
  { value: "accepted_with_note", label: "Accept with a note", explain: "Usable, but something differs (moisture, contamination, mass).", icon: MessageSquareWarning, tone: "var(--warning)" },
  { value: "rejected", label: "Reject", explain: "Not taken into stock; it cannot be used as an input.", icon: Ban, tone: "var(--critical)" },
];

/**
 * The intake review: when it arrived, what the weighbridge said, and the
 * receiver's decision. The decision and note are covered by the receipt
 * signature, so neither side can change them afterwards.
 */
function ReceiveModal({ item, onClose, onDone }: { item: WorkItem | null; onClose: () => void; onDone: (id: string) => void }) {
  const [at, setAt] = useState(localNow());
  const [atDefault, setAtDefault] = useState(at);
  const [kg, setKg] = useState("");
  const [decision, setDecision] = useState<Decision>("accepted");
  const [note, setNote] = useState("");
  useEffect(() => {
    if (item) {
      const now = localNow(item.record.occurredAt);
      setAt(now);
      setAtDefault(now);
      setKg("");
      setDecision("accepted");
      setNote("");
    }
  }, [item]);
  const dir = useDirectory();
  const needsNote = decision !== "accepted";
  // An asphalt delivery is checked at the paver (mix, temperature) rather than on a weighbridge.
  const paver = item?.record.type === "delivery";
  const m = useMutation({
    mutationFn: () => api.send("POST", `/shipments/${item!.record.id}/receipt`, {
      // Unchanged default: the exact moment of signing (never before the shipment), not the rounded-up minute.
      receivedAt: (at === atDefault ? new Date(Math.max(Date.now(), Date.parse(item!.record.occurredAt))) : new Date(at)).toISOString(), massKg: Number(kg.replace(",", ".")) || 0, decision, note: note.trim(),
    }),
    onSuccess: () => onDone(item!.record.id),
  });
  const diff = item && kg ? (Number(kg.replace(",", ".")) - item.record.massKg) / item.record.massKg * 100 : null;
  return (
    <Modal open={!!item} onOpenChange={(o) => !o && onClose()} title={`${paver ? "Take delivery" : "Receive"} ${item?.record.id ?? ""}`}
      sub={item ? `${exactMass(item.record.massKg)} sent by ${dir.orgName(item.record.orgId)} on ${dateTime(item.record.occurredAt)}. Review the load and sign: your receipt closes the handover.` : undefined}>
      <form className="grid grid-cols-2 gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <fieldset className="col-span-2">
          <legend className="label">Your decision</legend>
          <div className="grid gap-2 sm:grid-cols-3" role="radiogroup">
            {DECISIONS.map((d) => (
              <button key={d.value} type="button" role="radio" aria-checked={decision === d.value} onClick={() => setDecision(d.value)}
                className={clsx("flex flex-col items-start gap-1 rounded-xl border p-3 text-left transition",
                  decision === d.value ? "border-transparent ring-2" : "border-line hover:bg-surface-2")}
                style={decision === d.value ? { background: `color-mix(in srgb, ${d.tone} 10%, transparent)`, ["--tw-ring-color" as string]: d.tone } : undefined}>
                <span className="flex items-center gap-1.5 text-[13px] font-semibold"><d.icon size={15} style={{ color: d.tone }} aria-hidden />{d.label}</span>
                <span className="text-[11.5px] leading-snug text-muted">{d.explain}</span>
              </button>
            ))}
          </div>
        </fieldset>
        <div><label className="label" htmlFor="r-at">Arrived</label><input id="r-at" type="datetime-local" className="input" value={at} onChange={(e) => setAt(e.target.value)} /></div>
        <div>
          <label className="label" htmlFor="r-kg">{paver ? "Ticket mass (kg)" : "Weighbridge (kg)"}{decision === "rejected" && <span className="font-normal text-muted"> · optional</span>}</label>
          <input id="r-kg" className="input num" inputMode="decimal" aria-describedby="r-kg-sent" value={kg} onChange={(e) => setKg(e.target.value)} required={decision !== "rejected"} />
          {item && <p id="r-kg-sent" className="mt-1 text-[11.5px] text-muted">Sent: {exactMass(item.record.massKg)}. Enter what your {paver ? "ticket" : "weighbridge"} reads.</p>}
          {diff !== null && Number.isFinite(diff) && Math.abs(diff) >= 2 && (
            <p className="mt-1 text-[11.5px] text-warning">{diff > 0 ? "+" : ""}{diff.toFixed(1)} % against the shipment. Consider a note.</p>
          )}
        </div>
        <div className="col-span-2">
          <label className="label" htmlFor="r-note">Note{needsNote ? "" : <span className="font-normal text-muted"> · optional</span>}</label>
          <textarea id="r-note" className="input min-h-[72px] py-2" maxLength={1000} required={needsNote} value={note} onChange={(e) => setNote(e.target.value)}
            placeholder={decision === "rejected" ? (paver ? "Why is the load rejected? e.g. 118 °C at the paver, wrong mix." : "Why is the load rejected? e.g. contamination above 15 %, wrong material.") : "What did you find? Signed with your receipt."} />
        </div>
        {m.error ? <div className="col-span-2"><ErrorBox error={m.error} /></div> : null}
        <div className="col-span-2 flex justify-end gap-2">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button className={clsx("btn", decision === "rejected" ? "btn-danger" : "btn-primary")} disabled={m.isPending}>
            {m.isPending ? <Loader2 size={14} className="animate-spin" /> : null}{decision === "rejected" ? "Sign rejection" : "Sign receipt"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

type ReleaseDecision = "released" | "quarantined";

/** The quality decision on a lot: shows the lab tests behind it, and the note is the signer's own words. */
function ReleaseModal({ value, onClose, onDone, onTest }: { value: { item: WorkItem; decision: ReleaseDecision } | null; onClose: () => void; onDone: (id: string, d: ReleaseDecision) => void; onTest?: (id: string) => void }) {
  const meta = useMeta().data;
  const [decision, setDecision] = useState<ReleaseDecision>("released");
  const [note, setNote] = useState("");
  const id = value?.item.record.id;
  useEffect(() => {
    if (value) {
      setDecision(value.decision);
      setNote("");
    }
  }, [value]);
  const pp = useQuery({ queryKey: ["passport", id], queryFn: ({ signal }) => api.get<Passport>(`/passports/${id}`, signal), enabled: !!id });
  const tests = (pp.data?.tests ?? []).filter((t) => t.data?.subject_id === id);
  const failed = tests.filter((t) => t.data?.result === "fail");
  const untested = pp.isSuccess && tests.length === 0;
  const blocked = value?.item.record.status === "blocked";
  const needsNote = decision === "quarantined" || untested || failed.length > 0 || blocked;
  const m = useMutation({
    mutationFn: () => api.send("POST", "/records", { records: [{ type: "release", ref: `REL-${id}`, occurredAt: new Date().toISOString(), massKg: 0,
      data: { subject_id: id, decision, note: note.trim() } }] }),
    onSuccess: () => onDone(id!, decision),
  });
  return (
    <Modal open={!!value} onOpenChange={(o) => !o && onClose()} title={`Quality decision on ${id ?? ""}`}
      sub="Signed with your key. Released lots may be shipped; quarantined lots are held until a new decision.">
      <form className="grid gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <div className="grid grid-cols-2 gap-2" role="radiogroup" aria-label="Decision">
          {(["released", "quarantined"] as const).map((d) => (
            <button key={d} type="button" role="radio" aria-checked={decision === d} onClick={() => setDecision(d)}
              className={clsx("rounded-xl border p-3 text-left text-[13px] font-semibold transition", decision === d ? "border-transparent ring-2 ring-[var(--accent)]" : "border-line hover:bg-surface-2")}>
              {d === "released" ? "Release" : "Quarantine"}
            </button>
          ))}
        </div>
        <div className="rounded-xl bg-surface-2 p-3 text-[12.5px]">
          <div className="mb-1 font-semibold">Lab tests on this lot</div>
          {pp.isLoading ? <Loader2 size={14} className="animate-spin" /> : tests.length ? (
            <ul className="space-y-0.5">{tests.map((t) => <li key={t.id} className="flex justify-between gap-2"><span>{labLine(t.data, meta?.labTests)}</span>
              <span className={t.data?.result === "fail" ? "text-critical" : "text-good"}>{t.data?.result === "fail" ? "Fail" : t.data?.result === "pass" ? "Pass" : ""}</span></li>)}</ul>
          ) : <span className="text-warning">No lab test recorded. Record one first, or say in the note why the lot is released without it.
            {onTest && <button type="button" className="btn btn-sm mt-2 flex" onClick={() => id && onTest(id)}><TestTube2 size={13} /> Record lab test</button>}</span>}
          {blocked && decision === "released" && <p className="mt-1 text-critical">This lot has an open blocker in Data checks: fix it with a correction first, or say why it is released anyway.</p>}
          {failed.length > 0 && decision === "released" && <p className="mt-1 text-critical">{failed.length} failed test{failed.length > 1 ? "s" : ""}: releasing needs a reason.</p>}
        </div>
        <div>
          <label className="label" htmlFor="rel-note">Note{needsNote ? "" : <span className="font-normal text-muted"> · optional</span>}</label>
          <textarea id="rel-note" className="input min-h-[72px] py-2" maxLength={1000} required={needsNote} minLength={needsNote ? 5 : undefined} value={note} onChange={(e) => setNote(e.target.value)}
            placeholder={decision === "quarantined" ? "Why is the lot held? e.g. MFI out of range, waiting for re-test." : "Certificate or reason, e.g. within spec, CoA 2026-114."} />
        </div>
        {m.error ? <ErrorBox error={m.error} /> : null}
        <div className="flex justify-end gap-2">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button className={clsx("btn", decision === "quarantined" ? "btn-danger" : "btn-primary")} disabled={m.isPending || pp.isLoading}>
            {m.isPending ? <Loader2 size={14} className="animate-spin" /> : null}{decision === "quarantined" ? "Sign quarantine" : "Sign release"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

/** Shipment from a lot: the receiver is a site of the company's own address
 * book (own sites are received by its own team; partner sites are external). */
function ShipModal({ items, onClose, onDone }: { items: WorkItem[] | null; onClose: () => void; onDone: (id: string) => void; kind: string }) {
  const dir = useDirectory();
  const me = useMe().data!;
  const meta = useMeta().data;
  const [to, setTo] = useState("");
  const [km, setKm] = useState("");
  const [carrier, setCarrier] = useState("");
  const [vehicle, setVehicle] = useState("");
  const [adding, setAdding] = useState(false);
  const total = (items ?? []).reduce((s, i) => s + i.remainingKg, 0);
  const lotType = items?.[0]?.record.type;
  const mine = dir.sites.filter((s) => s.orgId === me.org.id && !s.archivedAt);
  const from = mine.find((s) => s.id === items?.[0]?.record.siteId && !s.partner) ?? mine.find((s) => !s.partner);
  // Any of our other sites (internal transfer); partner sites whose type takes
  // this kind of lot in, plus depots and other sites.
  const takes = (k: string) => k === "warehouse" || k === "other" || meta?.consumes?.[k as keyof Meta["consumes"]] === lotType;
  const receivers = mine.filter((s) => s.id !== from?.id && (!s.partner || takes(s.kind) || s.id === to));
  const own = receivers.filter((s) => !s.partner), partners = receivers.filter((s) => !!s.partner);
  const external = !!dir.site(to)?.partner;
  const m = useMutation({
    mutationFn: () => api.send<{ results: { id: string }[] }>("POST", "/records", { records: [{
      type: "shipment", ref: `ALB-${Date.now().toString(36).toUpperCase()}`, siteId: from?.id,
      occurredAt: new Date().toISOString(), massKg: Math.round(total),
      inputs: items!.map((i) => ({ id: i.record.id, massKg: Math.round(i.remainingKg) })),
      data: { to_site_id: to, distance_km: Number(km), carrier: carrier || undefined, vehicle: vehicle || undefined },
    }] }),
    onSuccess: (r) => onDone(r.results[0].id),
  });
  const opt = (s: (typeof receivers)[number]) => <option key={s.id} value={s.id}>{siteLabel(s)} · {siteKindName(s)}</option>;
  return (
    <Modal open={!!items} onOpenChange={(o) => !o && onClose()} title={`Ship ${exactMass(total)}`}
      sub={`${items?.length ?? 0} lot(s)${from ? ` from ${from.name}` : ""}. ${external ? "A partner site is external: the shipment is recorded without a receiver signature." : "Your receiving team confirms with a signed receipt."}`}>
      <form className="grid grid-cols-2 gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <div className="col-span-2">
          <div className="flex items-end justify-between gap-2">
            <label className="label" htmlFor="s-to">Receiving site</label>
            <button type="button" className="btn btn-sm btn-ghost mb-1" onClick={() => setAdding(true)}><Plus size={13} /> Add partner site</button>
          </div>
          <select id="s-to" className="input" value={to} onChange={(e) => setTo(e.target.value)} required>
            <option value="">Choose…</option>
            {own.length > 0 && <optgroup label="Our sites">{own.map(opt)}</optgroup>}
            {partners.length > 0 && <optgroup label="Partner sites">{partners.map(opt)}</optgroup>}
          </select>
        </div>
        <div><label className="label" htmlFor="s-km">Distance (km)</label><input id="s-km" className="input num" inputMode="decimal" value={km} onChange={(e) => setKm(e.target.value)} required /></div>
        <div><label className="label" htmlFor="s-veh">Vehicle</label><input id="s-veh" className="input" value={vehicle} onChange={(e) => setVehicle(e.target.value)} placeholder="Licence plate" /></div>
        <div className="col-span-2"><label className="label" htmlFor="s-car">Carrier</label><input id="s-car" className="input" value={carrier} onChange={(e) => setCarrier(e.target.value)} /></div>
        {m.error ? <div className="col-span-2"><ErrorBox error={m.error} /></div> : null}
        <div className="col-span-2 flex justify-end gap-2"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn btn-primary" disabled={m.isPending}>{m.isPending ? <Loader2 size={14} className="animate-spin" /> : null}Record shipment</button></div>
      </form>
      {!receivers.length && <p className="mt-2 text-[12px] text-muted">No site in your address book takes {lotType ? TYPE[lotType].plural.toLowerCase() : "this material"} in yet. Add the partner site you ship to.</p>}
      {adding && <SiteModal partner onClose={() => setAdding(false)} onSaved={(s) => setTo(s.id)} />}
    </Modal>
  );
}
