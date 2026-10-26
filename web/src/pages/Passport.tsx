import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import QRCode from "qrcode";
import {
  QrCode, MapPin, Truck, TestTube2, ShieldCheck, ArrowRight, BadgeCheck, OctagonX,
  CircleAlert, PackageCheck, Blocks, History, PencilLine, Ban, Loader2,
} from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";
import { useDirectory, useMe, useMeta } from "../lib/hooks";
import type { Passport, PNode, TimelineEvent, Finding, Carbon } from "../lib/types";
import { KIND, LAB_TEST_NAMES, REGION_SHORT, STAGES, TYPE, dutyFor, siteLabel } from "../lib/vocab";
import { co2, date, dateTime, mass, num, titleCase } from "../lib/format";
import {
  Card, Empty, ErrorBox, KV, Modal, SealChip, SignedChip, Skel, StatusChip, TypeBadge, Copy_, Hint,
  MoreList, Segmented,
} from "../components/ui";
import { BarList } from "../components/charts/BarList";
import { StreetMap, type Lay, type LatLon } from "../components/charts/StreetMap";
import { Download as DownloadMenu } from "../components/Download";
import { LineageGraph } from "../components/LineageGraph";
import { VerifyPanel } from "../components/VerifyPanel";
import { RecordForm, type Draft } from "../components/RecordForm";

export function usePassport(id: string, publicView = false) {
  return useQuery({
    queryKey: ["passport", id, publicView],
    queryFn: () => api.get<Passport>(publicView ? `/public/passports/${id}` : `/passports/${id}`),
    staleTime: 60_000,
  });
}

export default function PassportPage() {
  const { id = "" } = useParams();
  const q = usePassport(id);
  const dir = useDirectory();
  if (q.isLoading) return <PassportSkeleton />;
  if (q.isError) return <ErrorBox error={q.error} />;
  const p = q.data!;
  return <PassportView p={p} orgName={(o) => p.orgs[o]?.name ?? dir.orgName(o)} siteName={(id) => { const x = dir.site(id); return x ? siteLabel(x) : undefined; }} />;
}

/**
 * Corrections are new versions: the issuing company, with the duty the record
 * needs, re-submits it and the new version names the previous fingerprint.
 * `?correct=1` (from a finding) opens the form directly.
 */
function CorrectButton({ p }: { p: Passport }) {
  const me = useMe().data;
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [draft, setDraft] = useState<Draft | null>(null);
  const r = p.record;
  const can = !!me && !!r && r.orgId === me.org.id && (me.user.role === "admin" || me.duties.includes(dutyFor(r.type)));
  const open = () => {
    if (!r) return;
    // Correcting a void version restores the record: start from its content without the void flag.
    const { void: _v, void_reason: _why, ...data } = (r.data ?? {}) as Record<string, unknown>;
    setDraft({
      id: r.id, type: r.type, ref: r.ref, siteId: r.siteId, occurredAt: r.occurredAt, massKg: r.massKg,
      inputs: r.inputs, data, title: `Correct ${r.id} · version ${r.version + 1}`,
    });
  };
  useEffect(() => {
    if (can && params.get("correct") === "1") {
      open();
      params.delete("correct");
      setParams(params, { replace: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [can]);
  if (!can) return null;
  return (
    <>
      <Hint text="Submit a corrected version. The old version is kept; the new one names its fingerprint and is checked and sealed again.">
        <button className="btn" onClick={open}><PencilLine size={15} /> Correct</button>
      </Hint>
      <RecordForm draft={draft} onClose={() => setDraft(null)} onDone={() => { setDraft(null); void qc.invalidateQueries(); }} />
    </>
  );
}

/**
 * A void is a correction that says the record should not exist (a load that was
 * never laid). The new version carries no mass and is signed and sealed like any
 * version; the old ones stay. Records something else builds on are refused.
 */
function VoidButton({ p }: { p: Passport }) {
  const me = useMe().data;
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const r = p.record;
  const can = !!me && !!r && r.orgId === me.org.id && !r.data?.void && r.receipt?.decision !== "accepted" && r.receipt?.decision !== "accepted_with_note"
    && (me.user.role === "admin" || me.duties.includes(dutyFor(r.type)));
  const m = useMutation({
    mutationFn: () => api.send("POST", `/records/${r!.id}/void`, { reason: reason.trim() }),
    onSuccess: () => { setOpen(false); setReason(""); void qc.invalidateQueries(); },
  });
  if (!can) return null;
  const rejected = r!.receipt?.decision === "rejected";
  return (
    <>
      <Hint text="Void a record that should not exist, e.g. a load that was never laid. A new signed version with no mass replaces it; the old versions stay.">
        <button className="btn" onClick={() => setOpen(true)}><Ban size={15} /> Void</button>
      </Hint>
      <Modal open={open} onOpenChange={(o) => { setOpen(o); if (!o) m.reset(); }} title={`Void ${r!.id} · version ${r!.version + 1}`}
        sub={rejected ? "The contractor rejected this load at the paver. Voiding says it was not laid: it stops counting towards the section and the KPIs, and closes the rejection finding."
          : "The record stops counting: no mass, nothing taken from its inputs. The void is signed with your key and sealed; earlier versions stay readable."}>
        <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
          <div>
            <label className="label" htmlFor="void-reason">Reason</label>
            <textarea id="void-reason" className="input min-h-[72px] py-2" maxLength={500} required value={reason} onChange={(e) => setReason(e.target.value)}
              placeholder={rejected ? "e.g. Load rejected at the paver (118 °C); returned to the plant and not laid." : "Why should this record not exist?"} />
          </div>
          {m.error ? <ErrorBox error={m.error} /> : null}
          <div className="flex justify-end gap-2">
            <button type="button" className="btn" onClick={() => setOpen(false)}>Cancel</button>
            <button className="btn btn-danger" disabled={m.isPending || !reason.trim()}>
              {m.isPending ? <Loader2 size={14} className="animate-spin" /> : null}Sign void
            </button>
          </div>
        </form>
      </Modal>
    </>
  );
}

function PassportSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <Skel h={120} />
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">{[0, 1, 2, 3].map((i) => <Skel key={i} h={110} />)}</div>
      <Skel h={180} />
    </div>
  );
}

export function PassportView({ p, orgName, siteName, publicView = false }: { p: Passport; orgName: (id: string) => string; siteName?: (id: string) => string | undefined; publicView?: boolean }) {
  const root = p.root;
  const T = TYPE[root.type];
  const [qr, setQr] = useState(false);
  const data = (p.record?.data ?? root.data ?? {}) as Record<string, unknown>;
  const title = root.type === "road_section" && data.road
    ? `${data.road}, km ${num(Number(data.km_from), 1)}–${num(Number(data.km_to), 1)}`
    : `${T.name} ${root.ref ?? ""}`.trim();
  // Deliveries and shipments have no passport of their own: list only what can be opened.
  const downstream = (p.downstream ?? []).filter((n) => TYPE[n.type].passport);

  return (
    <div className="flex flex-col gap-4">
      {/* Header */}
      <section className="card rise overflow-hidden">
        <div className="h-1.5" style={{ background: `linear-gradient(90deg, ${STAGES.map((s) => s.color).join(",")})` }} />
        <div className="flex flex-wrap items-start gap-5 px-6 py-5">
          <TypeBadge type={root.type} size={48} />
          {p.orgs[root.orgId]?.logo && <img src={`/api/v1/orgs/${root.orgId}/logo`} alt={`${orgName(root.orgId)} logo`} className="h-12 max-w-[140px] rounded-lg bg-white object-contain p-1.5" />}
          <div className="min-w-0 flex-1">
            <div className="eyebrow">{T.name} passport</div>
            <h1 className="h1 mt-1">{title}</h1>
            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[13px] text-text-2">
              <span className="inline-flex items-center gap-1"><span className="mono font-semibold text-text">{root.id}</span><Copy_ text={root.id} /></span>
              <span>{orgName(root.orgId)}</span>
              <span>{date(root.occurredAt)}</span>
              {p.org.region && <span className="inline-flex items-center gap-1"><MapPin size={13} />{p.org.city}, {REGION_SHORT[p.org.region] ?? p.org.region}</span>}
            </div>
            <div className="mt-3 flex flex-wrap gap-1.5">
              {data.void === true && <Hint text={`Voided by the issuer${typeof data.void_reason === "string" ? `: ${data.void_reason}` : ""}. It carries no mass; earlier versions stay readable.`}><span className="chip chip-critical"><Ban size={12} /> Void</span></Hint>}
              <StatusChip status={root.status} />
              <SealChip sealed={root.sealed} pending />
              {p.trust?.signature && <SignedChip valid={p.trust.signature.valid} />}
              {p.access === "chain" && <Hint text="You took part in this passport's chain: you see the whole journey; other companies' commercial details stay private."><span className="chip chip-accent">Chain view</span></Hint>}
            </div>
            <TracedNote p={p} />
          </div>
          {!publicView && (
            <div className="flex gap-2">
              <CorrectButton p={p} />
              <VoidButton p={p} />
              <button className="btn" onClick={() => setQr(true)}><QrCode size={15} /> Share</button>
              <DownloadMenu kind="passport" range={false} params={{ id: root.id }} label="Download" />
            </div>
          )}
        </div>
      </section>

      {/* Journey */}
      <Card title="Journey" sub="Each step that contributed material to this passport, with the share that ended up here.">
        <div className="overflow-x-auto scroll-thin">
        <ol className="relative grid gap-3" style={{ gridTemplateColumns: `repeat(${Math.max(1, p.stages.length)}, minmax(0,1fr))`, minWidth: p.stages.length * 112 }}>
          {p.stages.map((s, i) => {
            const st = STAGES.find((x) => x.type === s.type);
            const color = TYPE[s.type].color;
            return (
              <li key={s.type} className="relative min-w-0">
                {/* The material's path fills in step by step: each dot appears, then the line to the next. */}
                <div className="flex items-center">
                  <span className="jt-pop z-10 grid size-8 shrink-0 place-items-center rounded-full text-white" style={{ background: color, animationDelay: `${i * STEP}s` }}>
                    {st ? <st.icon size={15} strokeWidth={2.4} /> : <PackageCheck size={15} />}
                  </span>
                  {i < p.stages.length - 1 && (
                    <span className="relative mx-1 h-[3px] flex-1 overflow-hidden rounded-full bg-surface-3" aria-hidden>
                      <span className="jt-bar absolute inset-0 rounded-full" style={{ background: `linear-gradient(90deg, ${color}, ${TYPE[p.stages[i + 1].type].color})`, animationDelay: `${i * STEP + 0.2}s`, animationDuration: `${STEP}s` }} />
                    </span>
                  )}
                </div>
                <div className="mt-3 pr-3">
                  <div className="text-[13px] font-semibold">{st?.title ?? s.name}</div>
                  <div className="num text-[18px] font-semibold tracking-tight">{mass(s.massKg)}</div>
                  <div className="text-[11.5px] text-muted">{num(s.records)} {s.records === 1 ? TYPE[s.type].name.toLowerCase() : TYPE[s.type].plural.toLowerCase()}</div>
                  <MoreList className="mt-1.5 block text-[11.5px] text-text-2" items={s.orgs.map((id) => ({ key: id, label: stripLegal(orgName(id)), to: publicView ? undefined : `/passports?org=${id}` }))} />
                  <div className="mt-1 text-[11px] text-muted">{date(s.first)}{s.first !== s.last ? ` – ${date(s.last)}` : ""}</div>
                </div>
              </li>
            );
          })}
        </ol>
        </div>
        {p.truncated && <p className="mt-3 text-[12px] text-muted">Very large lineage: the first 1,500 upstream records are shown.</p>}
      </Card>

      {!publicView && p.nodes && p.links && p.nodes.length > 1 && (
        <Card title="Lineage" sub="Every upstream record, largest contribution first. Hover to trace a path; select to open its passport.">
          <LineageGraph nodes={p.nodes} links={p.links} rootId={root.id} orgName={orgName} />
        </Card>
      )}

      {root.type === "road_section" && Number.isFinite(Number(data.lat)) && data.lat !== undefined && (
        <RoadMapCard p={p} data={data} title={title} orgName={orgName} />
      )}

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <Card title="Where the plastic came from" sub="Reject film from packaging sorting plants, by the mass that ended up here.">
          {p.origins.length ? (
            <BarList color="var(--s1)" format={(v) => mass(v)}
              items={p.origins.slice(0, 8).map((o) => ({
                key: o.orgId, label: stripLegal(orgName(o.orgId)), value: o.kg,
                sub: `${REGION_SHORT[o.region] ?? o.region} · ${o.lots} lot${o.lots === 1 ? "" : "s"} · would have gone to ${titleCase(o.route).toLowerCase()}`,
              }))} />
          ) : <Empty title="No reject plastic traced">No plastic reject lot feeds this record.</Empty>}
        </Card>
        <CarbonCard carbon={p.carbon} scenarios={p.scenarios} type={root.type} publicView={publicView} />
      </div>

      {!publicView && (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
          {p.trust && (
            <Card title="Trust" sub="Fingerprint, signatures and the ledger seal. Check them yourself.">
              <VerifyPanel trust={p.trust} orgName={orgName} />
              {p.trust.versions.length > 1 && <Versions versions={p.trust.versions} />}
            </Card>
          )}
          <Card title="Timeline" sub="Records, handovers, tests and seals, oldest first.">
            <Timeline events={p.timeline ?? []} orgName={orgName} siteName={siteName} />
          </Card>
        </div>
      )}

      {!publicView && (p.tests?.length ?? 0) > 0 && <TestsCard tests={p.tests!} orgName={orgName} />}

      {!publicView && p.findings && p.findings.length > 0 && <FindingsCard findings={p.findings} />}

      {!publicView && downstream.length > 0 && (
        <Card title="Where it went" sub="Passports downstream that used material from this one.">
          <ul className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {downstream.slice(0, 24).map((n) => <NodeLink key={n.id} n={n} orgName={orgName} />)}
          </ul>
          {downstream.length > 24 && <p className="mt-3 text-[12px] text-muted">{num(downstream.length - 24)} more not shown.</p>}
        </Card>
      )}

      {p.access === "full" && p.record && <DataCard p={p} />}

      {p.trust && publicView && (
        <Card title="Trust" sub="The issuer's signature and the ledger seal. Check them in this browser.">
          <VerifyPanel trust={p.trust} orgName={orgName} />
        </Card>
      )}

      <ShareModal open={qr} onOpenChange={setQr} id={root.id} orgId={root.orgId} />
    </div>
  );
}

// Seconds per step of the journey animation on a passport.
const STEP = 0.45;

const stripLegal = (s: string) => s.replace(/ (S\.A\.|S\.L\.)$/, "").replace(/ \(demo authority\)$/, "");

// Records that are (or contain) asphalt: only these have a manufacture-and-haulage footprint of their own.
const ASPHALT_TYPES = new Set(["asphalt_lot", "delivery", "road_section"]);

function CarbonCard({ carbon, scenarios, type, publicView }: { carbon: Carbon; scenarios: Carbon[]; type: string; publicView: boolean }) {
  const meta = useMeta().data;
  const name = (k: string) => meta?.scenarios.find((s) => s.key === k)?.name ?? k;
  const parts = [
    { label: "Disposal avoided", value: carbon.avoidedDisposalKg, color: "var(--s6)" },
    { label: "Virgin polymer avoided", value: carbon.avoidedVirginKg, color: "var(--s3)" },
    { label: "Recycling chain emissions", value: -carbon.chainFootprintKg, color: "var(--s8)" },
  ];
  const max = Math.max(...parts.map((x) => Math.abs(x.value)), 1);
  return (
    <Card title="Carbon" sub={publicView ? "Mass-allocated over the lineage." : "Mass-allocated over the lineage; factors and sources under Carbon & impact."}
      action={publicView ? undefined : <Link to="/carbon" className="btn btn-sm">Method</Link>}>
      <ul className="flex flex-col gap-3">
        {parts.map((x) => (
          <li key={x.label}>
            <div className="mb-1 flex justify-between text-[13px]"><span>{x.label}</span><span className="num font-semibold">{x.value < 0 ? "−" : "+"}{co2(Math.abs(x.value))}</span></div>
            <div className="h-[7px] rounded-full bg-surface-3"><div className="h-full rounded-full" style={{ width: `${(Math.abs(x.value) / max) * 100}%`, background: x.color }} /></div>
          </li>
        ))}
      </ul>
      <div className="mt-4 flex items-center justify-between rounded-xl bg-surface-2 px-3.5 py-2.5 text-[13px]">
        <span className="font-semibold">Net benefit</span>
        <span className={clsx("num font-semibold", carbon.netBenefitKg < 0 ? "text-critical" : "text-good")}>{co2(carbon.netBenefitKg)}</span>
      </div>
      <table className="table mt-4 text-[12.5px]">
        <thead><tr><th>Scenario</th><th className="text-right">Avoided</th><th className="text-right">Net</th></tr></thead>
        <tbody>
          {scenarios.map((s) => (
            <tr key={s.scenario}>
              <td>{name(s.scenario)}</td>
              <td className="num text-right">{co2(s.avoidedKg)}</td>
              <td className="num text-right">{co2(s.netBenefitKg)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {ASPHALT_TYPES.has(type) && <p className="mt-2 text-[11.5px] text-muted">The asphalt itself emitted {co2(carbon.footprintKg - carbon.chainFootprintKg)} (manufacture and haulage), with or without the additive.</p>}
    </Card>
  );
}

const EVENT_ICON: Record<string, ReactNode> = {
  created: <PackageCheck size={14} />, shipped: <Truck size={14} />, received: <BadgeCheck size={14} />,
  tested: <TestTube2 size={14} />, sealed: <Blocks size={14} />, accepted: <ShieldCheck size={14} />, voided: <Ban size={14} />,
};

function Timeline({ events, orgName, siteName }: { events: TimelineEvent[]; orgName: (id: string) => string; siteName?: (id: string) => string | undefined }) {
  const [all, setAll] = useState(false);
  const shown = all ? events : events.slice(-14);
  return (
    <div>
      {events.length > 14 && !all && (
        <button className="btn btn-sm mb-3" onClick={() => setAll(true)}>Show all {num(events.length)} events</button>
      )}
      <ol className="relative ml-3 border-l border-line">
        {shown.map((e, i) => (
          <li key={i} className="relative pb-4 pl-6 last:pb-0">
            <span className={clsx("absolute -left-[11px] top-0 grid size-[22px] place-items-center rounded-full border-2 border-[var(--surface)]",
              e.ok === false ? "bg-[var(--critical-soft)] text-critical" : e.kind === "sealed" ? "bg-accent-soft text-accent-ink" : "bg-surface-3 text-text-2")}>
              {EVENT_ICON[e.kind] ?? <History size={14} />}
            </span>
            <div className="text-[13px] font-medium">{e.title}</div>
            <div className="text-[11.5px] text-muted">
              {dateTime(e.at)}{e.orgId ? ` · ${orgName(e.orgId)}` : ""}{e.kind === "shipped" ? (e.toSiteId && siteName?.(e.toSiteId) ? ` → ${siteName(e.toSiteId)}` : e.detail ? ` → ${orgName(e.detail)}` : "") : ""}
            </div>
            {e.detail && (e.kind === "accepted" || e.kind === "voided") && <div className="mt-1 text-[12px] text-text-2">“{e.detail}”</div>}
          </li>
        ))}
      </ol>
    </div>
  );
}

// IDs of the record types that have a passport (lots, batches, sections); deliveries and shipments do not.
const PASSPORT_ID = /^(PRL|RPL|ADB|ASL|RSC)-\d{2}-\d+$/;

function TestsCard({ tests, orgName }: { tests: PNode[]; orgName: (id: string) => string }) {
  const meta = useMeta().data;
  return (
    <Card title="Lab tests" sub="Quality measurements on this passport and its inputs." pad={false}>
      <div className="max-h-[360px] overflow-auto scroll-thin">
        <table className="table">
          <thead><tr><th>Test</th><th>On</th><th className="text-right">Result</th><th>Verdict</th><th>Laboratory</th><th>Date</th></tr></thead>
          <tbody>
            {tests.slice().sort((a, b) => b.occurredAt.localeCompare(a.occurredAt)).map((t) => {
              const d = t.data as { test?: string; value?: number; result?: string; subject_id?: string; lab?: string } | undefined;
              const info = d?.test ? meta?.labTests[d.test] : undefined;
              return (
                <tr key={t.id}>
                  <td><div className="font-medium">{info?.name ?? LAB_TEST_NAMES[d?.test ?? ""] ?? (d?.test ? titleCase(d.test) : "—")}</div><div className="text-[11.5px] text-muted">{info?.standard}</div></td>
                  <td className="mono text-[12px]">
                    {d?.subject_id && PASSPORT_ID.test(d.subject_id) ? <Link to={`/passports/${d.subject_id}`} className="hover:text-accent-ink hover:underline">{d.subject_id}</Link> : d?.subject_id ?? "—"}
                  </td>
                  <td className="num text-right font-semibold">{typeof d?.value === "number" ? num(d.value, 2) : "—"} <span className="font-normal text-muted">{info?.unit}</span></td>
                  <td>{d?.result === "pass" ? <span className="chip chip-good">Pass</span> : d?.result === "fail" ? <span className="chip chip-critical">Fail</span> : <span className="chip">{d?.result ? titleCase(d.result) : "No verdict"}</span>}</td>
                  <td className="text-text-2">{orgName(t.orgId)}{d?.lab && d.lab !== orgName(t.orgId) ? <div className="text-[11.5px] text-muted">{d.lab}</div> : null}</td>
                  <td className="text-text-2">{date(t.occurredAt)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

function FindingsCard({ findings }: { findings: Finding[] }) {
  return (
    <Card title="Data checks on this record" action={<Link to={`/quality?record=${findings[0].recordId}`} className="btn btn-sm">Open in Data checks</Link>}>
      <ul className="flex flex-col gap-2">
        {findings.map((f) => (
          <li key={f.id} className="flex items-start gap-3 rounded-xl bg-surface-2 px-3.5 py-2.5">
            {f.severity === "blocker" ? <OctagonX size={16} className="mt-0.5 text-critical" /> : <CircleAlert size={16} className="mt-0.5 text-warning" />}
            <div className="min-w-0 flex-1">
              <div className="text-[13px] font-semibold">{f.code} · {f.message}</div>
              <div className="text-[12px] text-text-2">{f.status === "open" ? f.fix : f.closeReason ? `${titleCase(f.status)}: ${f.closeReason}` : titleCase(f.status)}</div>
            </div>
            <span className={clsx("chip", f.status === "open" ? (f.severity === "blocker" ? "chip-critical" : "chip-warning") : "chip-good")}>{titleCase(f.status)}</span>
          </li>
        ))}
      </ul>
    </Card>
  );
}

function Versions({ versions }: { versions: { version: number; contentHash: string; prevHash?: string; occurredAt: string; sealId?: string }[] }) {
  return (
    <div className="mt-6">
      <div className="eyebrow mb-2">Versions</div>
      <ol className="flex flex-col gap-1.5">
        {versions.map((v) => (
          <li key={v.version} className="flex items-center gap-3 text-[12.5px]">
            <span className="chip">v{v.version}</span>
            <span className="mono text-text-2">{v.contentHash.slice(0, 12)}…</span>
            {v.prevHash && <span className="text-muted">follows {v.prevHash.slice(0, 8)}…</span>}
            <span className="ml-auto text-muted">{date(v.occurredAt)}</span>
          </li>
        ))}
      </ol>
      <p className="mt-2 text-[11.5px] text-muted">Corrections never overwrite: each version names the fingerprint of the one before it.</p>
    </div>
  );
}

function NodeLink({ n, orgName }: { n: PNode; orgName: (id: string) => string }) {
  return (
    <li>
      <Link to={`/passports/${n.id}`} className="flex items-center gap-3 rounded-xl bg-surface-2 px-3 py-2.5 hover:bg-surface-3">
        <TypeBadge type={n.type} />
        <span className="min-w-0 flex-1">
          <span className="mono block truncate text-[12.5px] font-semibold">{n.id}</span>
          <span className="block truncate text-[11.5px] text-muted">{orgName(n.orgId)} · {date(n.occurredAt)}</span>
        </span>
        <ArrowRight size={14} className="text-muted" />
      </Link>
    </li>
  );
}

function DataCard({ p }: { p: Passport }) {
  const meta = useMeta().data;
  const dir = useDirectory();
  const r = p.record!;
  const fields = meta?.fields[r.type] ?? [];
  const items: [ReactNode, ReactNode][] = [
    ["Your reference", r.ref],
    ["Site", dir.site(r.siteId)?.name ?? r.siteId ?? "—"],
    ["Recorded", dateTime(r.occurredAt)],
    ["Provenance", r.provenance],
    ...fields.filter((f) => r.data[f.name] !== undefined).map((f): [ReactNode, ReactNode] => {
      const v = r.data[f.name];
      const site = f.kind === "site" ? dir.site(String(v)) : undefined;
      const shown = f.kind === "org" ? dir.orgName(String(v)) : f.kind === "site" ? (site ? `${siteLabel(site)}${site.partner ? " (external)" : ""}` : String(v)) : f.kind === "time" ? dateTime(String(v)) : f.kind === "number" ? `${num(Number(v), 2)}${f.unit ? ` ${f.unit}` : ""}`
        : f.name === "test" && meta?.labTests[String(v)] ? meta.labTests[String(v)].name : f.enum ? titleCase(String(v)) : String(v);
      return [f.label, shown];
    }),
  ];
  if (r.inputs?.length) items.push(["Inputs", <span key="in" className="flex flex-wrap gap-1.5">{r.inputs.map((i) => <Link key={i.id} to={`/passports/${i.id}`} className="chip hover:bg-accent-soft"><span className="mono">{i.id}</span> {mass(i.massKg)}</Link>)}</span>]);
  return (
    <Card title="Record data" sub={`As submitted by ${dir.orgName(r.orgId)}${r.version > 1 ? ` (version ${r.version})` : ""}.`}>
      <KV items={items} />
      <div className="mt-4 text-[11.5px] text-muted">Issuer type: {KIND[p.org.kind]?.name ?? titleCase(p.org.kind)}</div>
    </Card>
  );
}

type ShareState = { shared: boolean; token?: string; url?: string };

/** The public link carries a random token, never the passport id; only the maker can publish it. */
function ShareModal({ open, onOpenChange, id, orgId }: { open: boolean; onOpenChange: (o: boolean) => void; id: string; orgId: string }) {
  const me = useMe().data;
  const qc = useQueryClient();
  const mine = !!me && me.org.id === orgId;
  const canEdit = mine && ["admin", "editor"].includes(me!.user.role);
  const [svg, setSvg] = useState("");
  const share = useQuery({ queryKey: ["share", id], queryFn: () => api.get<ShareState>(`/passports/${id}/share`), enabled: open && mine });
  const set = (r: ShareState) => qc.setQueryData(["share", id], r);
  const create = useMutation({ mutationFn: () => api.send<ShareState>("POST", `/passports/${id}/share`), onSuccess: set });
  const rotate = useMutation({ mutationFn: () => api.send<ShareState>("POST", `/passports/${id}/share/rotate`), onSuccess: set });
  const stop = useMutation({ mutationFn: () => api.send("DELETE", `/passports/${id}/share`), onSuccess: () => set({ shared: false }) });
  const url = share.data?.url ?? "";
  useEffect(() => {
    if (open && url) void QRCode.toString(url, { type: "svg", margin: 1, color: { dark: "#1d1d1f", light: "#ffffff" } }).then(setSvg);
  }, [open, url]);
  const err = create.error ?? rotate.error ?? stop.error ?? share.error;
  return (
    <Modal open={open} onOpenChange={onOpenChange} title="Share this passport" sub="The public passport shows the journey, origins, carbon figures and the ledger seal, never commercial details. The link is a random code, not the passport number.">
      <div className="flex flex-col items-center gap-4">
        {!mine ? <p className="text-[13px] text-muted">Only the company that made this passport can publish a link to it.</p>
          : share.data?.shared && url ? (
            <>
              <div className="size-[200px] rounded-2xl bg-white p-3" dangerouslySetInnerHTML={{ __html: svg }} />
              <div className="flex w-full items-center gap-2 rounded-xl bg-surface-2 px-3 py-2">
                <span className="mono flex-1 truncate text-[12.5px]">{url}</span>
                <Copy_ text={url} label="Copy link" />
              </div>
              <div className="flex flex-wrap justify-center gap-2">
                <a className="btn btn-primary" href={url} target="_blank" rel="noreferrer">Open public page</a>
                {canEdit && <button className="btn" disabled={rotate.isPending} onClick={() => rotate.mutate()} title="The current link stops working">New link</button>}
                {canEdit && <button className="btn" disabled={stop.isPending} onClick={() => stop.mutate()}>Stop sharing</button>}
              </div>
            </>
          ) : (
            <>
              <p className="text-[13px] text-muted">This passport is not shared. Create a public link to show it to a customer or auditor.</p>
              {canEdit ? <button className="btn btn-primary" disabled={create.isPending} onClick={() => create.mutate()}>Create public link</button>
                : <p className="text-[12.5px] text-muted">Ask an editor or administrator to create the link.</p>}
            </>
          )}
        {err ? <ErrorBox error={err} /> : null}
      </div>
    </Modal>
  );
}

/** Where the section was laid, each load's stretch of it, and the asphalt plants whose deliveries paved it. */
function RoadMapCard({ p, data, title, orgName }: { p: Passport; data: Record<string, unknown>; title: string; orgName: (id: string) => string }) {
  const dir = useDirectory();
  const [focus, setFocus] = useState<"road" | "all">("road");
  const [active, setActive] = useState<string>();
  const section = useMemo(() => ({
    id: p.root.id, lat: Number(data.lat), lon: Number(data.lon), label: title,
    sub: [data.carriageway ? `${data.carriageway} carriageway` : "", data.layer ? `${data.layer} layer` : "", data.mix as string].filter(Boolean).join(" · "),
  }), [p.root.id, data, title]);
  const ends = useMemo(() => {
    const from = point(data.lat_from, data.lon_from), to = point(data.lat_to, data.lon_to);
    return from && to ? { from, to, fromLabel: `km ${num(Number(data.km_from), 1)}`, toLabel: `km ${num(Number(data.km_to), 1)}` } : undefined;
  }, [data]);
  const lays = useMemo<Lay[]>(() => {
    const loads = (p.nodes ?? []).filter((n) => n.type === "delivery" && n.data?.void !== true)
      .sort((a, b) => a.occurredAt.localeCompare(b.occurredAt));
    const recorded = loads.flatMap((n) => {
      const from = point(n.data?.lat_from, n.data?.lon_from), to = point(n.data?.lat_to, n.data?.lon_to);
      return from && to ? [{ n, from, to }] : [];
    });
    const lay = (n: PNode, i: number, from: LatLon, to: LatLon, estimated: boolean): Lay => ({
      id: n.id, n: i + 1, from, to, estimated, label: n.ref ?? n.id,
      sub: `${mass(n.massKg)} · ${dateTime(n.occurredAt)} · ≈ ${num(metres(from, to))} m`,
    });
    // Positions count as per-load only when they differ between loads: the same
    // stretch on every ticket (often the whole section) is section-level data.
    const perLoad = recorded.length === 1 || new Set(recorded.map((x) => `${coord(x.from)}|${coord(x.to)}`)).size > 1;
    if (recorded.length && perLoad) return recorded.map((x, i) => lay(x.n, i, x.from, x.to, false));
    // No paver positions recorded: spread the loads along the section in delivery order, by tonnes.
    if (!ends || !loads.length) return [];
    const total = Math.max(p.root.massKg, loads.reduce((t, n) => t + n.massKg, 0));
    let done = 0;
    return loads.map((n, i) => {
      const a = done / total;
      done += n.massKg;
      return lay(n, i, lerp(ends.from, ends.to, a), lerp(ends.from, ends.to, done / total), true);
    });
  }, [p.nodes, p.root.massKg, ends]);
  const plants = useMemo(() => {
    const bySite = new Map<string, { kg: number; loads: number; orgId: string }>();
    for (const n of p.nodes ?? []) {
      if (n.type !== "delivery" || !n.siteId) continue;
      const x = bySite.get(n.siteId) ?? { kg: 0, loads: 0, orgId: n.orgId };
      x.kg += n.massKg * (n.share || 1);
      x.loads++;
      bySite.set(n.siteId, x);
    }
    return [...bySite.entries()].flatMap(([id, x]) => {
      const s = dir.site(id);
      return s ? [{ id, lat: s.lat, lon: s.lon, massKg: x.kg, label: stripLegal(orgName(x.orgId)), sub: `${s.name} · ${x.loads} load${x.loads === 1 ? "" : "s"} · ${mass(x.kg)}` }] : [];
    });
  }, [p.nodes, dir, orgName]);
  const estimated = lays.some((l) => l.estimated);
  const sectionLevel = (p.nodes ?? []).some((n) => n.type === "delivery" && point(n.data?.lat_from, n.data?.lon_from));
  const sub = ends
    ? estimated
      ? `${title}: the section from start to end. Where each of its ${lays.length} load${lays.length === 1 ? "" : "s"} went is estimated from delivery order and tonnes: ${sectionLevel ? "the tickets repeat the same stretch, so no per-load paver positions were recorded" : "no paver positions were recorded"}.`
      : `${title}: the section from start to end, and the stretch each of its ${lays.length} load${lays.length === 1 ? "" : "s"} covered, from the paver positions on the tickets.`
    : `${title}. Record the section's start and end points to see where each load was laid.`;
  return (
    <Card title="Where it was laid" sub={sub}
      action={plants.length > 0 && <Segmented label="Map view" value={focus} onChange={setFocus}
        options={[{ value: "road", label: "Road" }, { value: "all", label: plants.length === 1 ? "Road and plant" : "Road and plants" }]} />}>
      <StreetMap section={section} ends={ends} lays={lays} plants={plants} focus={ends ? focus : "all"} active={active} onActive={setActive} />
      {lays.length > 0 && (
        <div className="mt-4 max-h-[242px] overflow-auto scroll-thin">
          <table className="table text-[12.5px]">
            <thead className="sticky top-0 z-10 bg-surface"><tr><th>Load</th><th>Ticket</th><th>Laid</th><th className="text-right">Tonnes</th><th>From</th><th>To</th><th className="text-right">Length{ends ? " ≈" : ""}</th></tr></thead>
            <tbody>
              {lays.map((l) => {
                const n = p.nodes!.find((x) => x.id === l.id)!;
                return (
                  <tr key={l.id} onMouseEnter={() => setActive(l.id)} onMouseLeave={() => setActive(undefined)} className={clsx(active === l.id && "bg-surface-2")}>
                    <td><span className="inline-flex items-center gap-2"><span className="inline-block h-1.5 w-4 rounded-full" style={{ background: l.n % 2 ? "var(--s5)" : "var(--s7)" }} />{l.n}</span></td>
                    <td className="mono">{l.label}{l.estimated && <span className="ml-1.5 text-muted">est.</span>}</td>
                    <td>{dateTime(n.occurredAt)}</td>
                    <td className="num text-right">{mass(n.massKg)}</td>
                    <td className="mono text-text-2">{coord(l.from)}</td>
                    <td className="mono text-text-2">{coord(l.to)}</td>
                    <td className="num text-right">{num(metres(l.from, l.to))} m</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

function point(lat: unknown, lon: unknown): LatLon | undefined {
  if (lat === undefined || lat === null || lat === "" || lon === undefined || lon === null || lon === "") return undefined;
  const a = Number(lat), b = Number(lon);
  return Number.isFinite(a) && Number.isFinite(b) ? [a, b] : undefined;
}
const lerp = (a: LatLon, b: LatLon, f: number): LatLon => [a[0] + (b[0] - a[0]) * f, a[1] + (b[1] - a[1]) * f];
const coord = (p: LatLon) => `${p[0].toFixed(5)}, ${p[1].toFixed(5)}`;
/** Great-circle distance in metres. */
function metres(a: LatLon, b: LatLon) {
  const r = Math.PI / 180, dLat = (b[0] - a[0]) * r, dLon = (b[1] - a[1]) * r;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a[0] * r) * Math.cos(b[0] * r) * Math.sin(dLon / 2) ** 2;
  return 2 * 6371000 * Math.asin(Math.sqrt(h));
}

/**
 * A road section whose as-laid mass differs from the accepted deliveries laid on
 * it: say both, so a passport never presents an untraced quantity as physically
 * traced (an accepted exception documents the gap; it does not close it).
 */
function TracedNote({ p }: { p: Passport }) {
  const claimed = p.root.massKg, traced = p.tracedKg;
  if (traced === undefined || claimed <= 0 || Math.abs(claimed - traced) <= Math.max(claimed, traced) * 0.02) return null;
  const gap = claimed - traced;
  const why = p.root.status === "accepted" ? "An independent party accepted the difference as an exception (see Data checks below); it is still not traced in the chain."
    : p.root.status === "blocked" ? "Data check C5 blocks this section until it is corrected or independently accepted."
    : "Deliveries may still be missing.";
  return (
    <div className="mt-3 max-w-[720px] rounded-xl border border-[var(--warning)]/40 bg-[var(--warning-soft)] px-3 py-2.5 text-[12.5px]">
      <b>Claimed as laid {mass(claimed)}; traced through accepted deliveries {mass(traced)}.</b>{" "}
      {gap > 0 ? `${mass(gap)} has no material lineage, so it carries no recycled plastic here. ` : `${mass(-gap)} more was delivered than claimed. `}
      {why}
    </div>
  );
}
