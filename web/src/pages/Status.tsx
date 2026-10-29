import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Truck, Stamp, OctagonX, Ban, CircleAlert, Blocks, CircleCheck, Search } from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";
import { useDirectory, useMe } from "../lib/hooks";
import type { OrgKind, StageStatus } from "../lib/types";
import { KIND, STAGES } from "../lib/vocab";
import { ago, mass, num, pct } from "../lib/format";
import { Download } from "../components/Download";
import { Card, InfoTip, PageHeader, Segmented, Skel } from "../components/ui";
import { HELP } from "../lib/help";

const ORDER: OrgKind[] = ["sorting_plant", "recycler", "additive_producer", "asphalt_plant", "contractor", "road_owner", "lab"];

type Row = StageStatus & { kind: OrgKind; name: string; city: string; attention: number };

// Blocked and quarantined records stop material; awaiting release and
// warnings only slow it down.
const attention = (r: StageStatus) => r.blocked * 3 + r.quarantined * 3 + r.awaitingRelease + r.flagged;

/** The reporter's board: where everything stands, in every company, on one screen. */
export default function Status() {
  const me = useMe().data!;
  const dir = useDirectory();
  const [params, setParams] = useSearchParams();
  const step = (params.get("step") as OrgKind | null) ?? null;
  const show = (params.get("show") as "attention" | "all" | null) ?? "attention";
  const [find, setFind] = useState("");
  const q = useQuery({ queryKey: ["chain-status"], queryFn: () => api.get<StageStatus[]>("/chain/status"), refetchInterval: 60_000 });

  const rows: Row[] = useMemo(() => (q.data ?? []).flatMap((r) => {
    const o = dir.org(r.orgId);
    if (!o || !ORDER.includes(o.kind) || (r.records === 0 && r.inTransit === 0)) return [];
    // A row per activity: a company that sorts and mixes shows a blocked asphalt lot under Mix asphalt.
    const kind = r.activity && ORDER.includes(r.activity) ? r.activity : o.kind;
    return [{ ...r, kind, name: o.name, city: o.city ?? "", attention: attention(r) }];
  }), [q.data, dir]);

  const stages = useMemo(() => ORDER.map((k) => {
    const rs = rows.filter((r) => r.kind === k);
    const s = (f: (r: Row) => number) => rs.reduce((a, r) => a + f(r), 0);
    return {
      kind: k, orgs: rs.length, inTransit: s((r) => r.inTransit), awaiting: s((r) => r.awaitingRelease),
      stopped: s((r) => r.blocked + r.quarantined), flagged: s((r) => r.flagged),
      sealed: (s((r) => r.sealed) / Math.max(1, s((r) => r.records))) * 100, needs: rs.filter((r) => r.attention > 0).length,
    };
  }).filter((s) => s.orgs > 0), [rows]);

  const set = (k: string, v: string | null) => setParams((p) => { const n = new URLSearchParams(p); if (v) n.set(k, v); else n.delete(k); return n; }, { replace: true });
  const list = useMemo(() => rows
    .filter((r) => !step || r.kind === step)
    .filter((r) => show === "all" || r.attention > 0)
    .filter((r) => !find || `${r.name} ${r.city}`.toLowerCase().includes(find.toLowerCase()))
    .sort((a, b) => b.attention - a.attention || ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind) || b.records - a.records), [rows, step, show, find]);

  const sum = (f: (r: StageStatus) => number) => rows.reduce((s, r) => s + f(r), 0);
  const stepName = (k: OrgKind) => STAGES.find((s) => s.kind === k)?.title ?? KIND[k].name;

  return (
    <div>
      <PageHeader title="Chain status" sub={me.oversight ? "Where material stands right now in every company. Pick a step to narrow the list." : `Where material stands right now at ${me.org.name}: what is on its way to you, waiting for release, held or blocked.`} actions={<Download kind="status" range={false} />} />

      {/* The whole chain in one strip, so the list below stays in view. */}
      <div className="card grid grid-cols-2 divide-line sm:grid-cols-5 sm:divide-x">
        {[
          { label: "In transit", help: HELP.inTransit, icon: Truck, color: "var(--s1)", value: num(sum((r) => r.inTransit)), hint: mass(sum((r) => r.inTransitKg)) },
          { label: "To release", help: HELP.awaitingRelease, icon: Stamp, color: "var(--warning)", value: num(sum((r) => r.awaitingRelease)), hint: "last 90 days" },
          { label: "Quarantined", help: HELP.quarantined, icon: Ban, color: "var(--serious)", value: num(sum((r) => r.quarantined)), hint: "held by quality" },
          { label: "Blocked", help: HELP.blocked, icon: OctagonX, color: "var(--critical)", value: num(sum((r) => r.blocked)), hint: `${num(sum((r) => r.flagged))} warnings` },
          { label: "Sealed", help: HELP.sealedShare, icon: Blocks, color: "var(--accent)", value: pct((sum((r) => r.sealed) / Math.max(1, sum((r) => r.records))) * 100), hint: `${num(sum((r) => r.records))} records` },
        ].map((m) => (
          <div key={m.label} className="px-4 py-3">
            <div className="flex items-center gap-1.5 text-[12px] font-medium text-text-2"><m.icon size={13} style={{ color: m.color }} aria-hidden />{m.label}<InfoTip text={m.help} /></div>
            {q.isLoading ? <Skel h={24} w={60} className="mt-1" /> : <div className="num mt-0.5 text-[22px] font-semibold tracking-tight">{m.value}</div>}
            <div className="text-[11.5px] text-muted">{m.hint}</div>
          </div>
        ))}
      </div>

      {/* One tile per step, in chain order: a click narrows the table. */}
      <div className="-mx-1 mt-3 flex snap-x gap-2.5 overflow-x-auto px-1 pb-1 scroll-thin" role="group" aria-label="Steps of the chain">
        {q.isLoading ? ORDER.map((k) => <Skel key={k} h={112} className="min-w-[150px] flex-1" />) : stages.map((s) => {
          const K = KIND[s.kind];
          const on = step === s.kind;
          return (
            <button key={s.kind} type="button" aria-pressed={on} onClick={() => set("step", on ? null : s.kind)}
              className={clsx("card flex min-w-[150px] flex-1 snap-start flex-col p-3 text-left transition hover:bg-surface-2", on && "ring-2 ring-[var(--accent)]")}>
              <div className="flex items-center gap-2">
                <span className="grid size-7 shrink-0 place-items-center rounded-[9px]" style={{ background: `color-mix(in srgb, ${K.color} 16%, transparent)`, color: K.color }}><K.icon size={14} aria-hidden /></span>
                <span className="min-w-0">
                  <span className="block truncate text-[13px] font-semibold">{stepName(s.kind)}</span>
                  <span className="block text-[11.5px] text-muted">{s.orgs} {s.orgs === 1 ? "company" : "companies"}</span>
                </span>
              </div>
              <div className="mt-2.5 flex flex-wrap gap-1">
                {s.stopped > 0 && <span className="chip chip-critical"><OctagonX size={11} />{num(s.stopped)} stopped</span>}
                {s.awaiting > 0 && <span className="chip chip-warning"><Stamp size={11} />{num(s.awaiting)} to release</span>}
                {s.inTransit > 0 && <span className="chip"><Truck size={11} />{num(s.inTransit)} inbound</span>}
                {s.stopped + s.awaiting + s.inTransit === 0 && <span className="chip chip-good"><CircleCheck size={11} />Clear</span>}
              </div>
              <div className="mt-auto pt-2.5">
                <div className="flex justify-between text-[11.5px] text-muted"><span>Sealed</span><span className="num">{pct(s.sealed, 0)}</span></div>
                <div className="mt-1 h-[4px] overflow-hidden rounded-full bg-surface-3"><div className="h-full rounded-full bg-[var(--accent)]" style={{ width: `${s.sealed}%` }} /></div>
              </div>
            </button>
          );
        })}
      </div>

      <Card className="mt-3" pad={false}
        title={step ? <span className="inline-flex items-center gap-2">{stepName(step)} · {KIND[step].plural}<button className="chip" onClick={() => set("step", null)} aria-label="Show every step">All steps ×</button></span> : "Companies"}
        sub={show === "attention" ? `${list.length} need attention, most urgent first` : `${list.length} ${list.length === 1 ? "row" : "rows"}: one per company and step`}
        action={<>
          <label className="relative hidden sm:block">
            <Search size={13} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted" aria-hidden />
            <input className="input h-8 w-44 text-[13px]" style={{ paddingLeft: 28 }} placeholder="Find a company" value={find} onChange={(e) => setFind(e.target.value)} aria-label="Find a company" />
          </label>
          <Segmented label="Which companies" value={show} onChange={(v) => set("show", v === "attention" ? null : v)}
            options={[{ value: "attention", label: "Needs attention" }, { value: "all", label: "All" }]} />
        </>}>
        {q.isLoading ? <div className="p-4"><Skel h={240} /></div> : list.length === 0 ? (
          <div className="flex items-center justify-center gap-2 px-6 py-10 text-[13.5px] text-text-2">
            <CircleCheck size={16} className="text-good" />Nothing waiting{step ? ` at ${stepName(step).toLowerCase()}` : ""}.
            {show === "attention" && <button className="btn btn-sm btn-ghost" onClick={() => set("show", "all")}>Show all companies</button>}
          </div>
        ) : (
          <div className="max-h-[min(560px,60vh)] overflow-auto scroll-thin">
            <table className="table">
              <thead className="sticky top-0 z-10 bg-[var(--bg-elevated)]">
                <tr><th>Company</th><th>Step</th><th>Needs <InfoTip text="Blocked and quarantined records stop material; lots to release and warnings slow it down. Companies are sorted by urgency." /></th><th className="text-right">Inbound <InfoTip text={HELP.inTransit} /></th><th className="text-right">Sealed <InfoTip text={HELP.sealedShare} /></th><th>Last activity</th></tr>
              </thead>
              <tbody>
                {list.map((r) => {
                  const K = KIND[r.kind];
                  return (
                    <tr key={`${r.orgId}|${r.kind}`}>
                      <td><Link to={`/passports?org=${r.orgId}`} className="font-medium hover:text-accent-ink">{r.name}</Link><div className="text-[11.5px] text-muted">{r.city}</div></td>
                      <td><span className="inline-flex items-center gap-1.5 text-[12.5px] text-text-2"><K.icon size={13} style={{ color: K.color }} aria-hidden />{stepName(r.kind)}</span></td>
                      <td>
                        <div className="flex flex-wrap gap-1">
                          {r.blocked > 0 && <span className="chip chip-critical"><OctagonX size={11} />{r.blocked} blocked</span>}
                          {r.quarantined > 0 && <span className="chip chip-critical"><Ban size={11} />{r.quarantined} quarantined</span>}
                          {r.awaitingRelease > 0 && <span className="chip chip-warning"><Stamp size={11} />{r.awaitingRelease} to release</span>}
                          {r.flagged > 0 && <span className="chip chip-warning"><CircleAlert size={11} />{r.flagged} warnings</span>}
                          {r.attention === 0 && <span className="text-[12px] text-muted">—</span>}
                        </div>
                      </td>
                      <td className="num text-right">{r.inTransit ? <span>{num(r.inTransit)} <span className="text-muted">· {mass(r.inTransitKg)}</span></span> : "—"}</td>
                      <td className="num whitespace-nowrap text-right">{pct((r.sealed / Math.max(1, r.records)) * 100, 0)}</td>
                      <td className="whitespace-nowrap text-text-2">{ago(r.lastActivity)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}
