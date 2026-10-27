import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { useAnalytics, useDirectory, useMe } from "../lib/hooks";
import type { FlowLink, FunnelStep, Series, Overview, Breakdown } from "../lib/types";
import { homeType, KIND, STAGES, TYPE } from "../lib/vocab";
import { mass, num } from "../lib/format";
import { Card, Empty, Legend, PageHeader, Skel } from "../components/ui";
import { ChainSankey } from "../components/charts/Sankey";
import { TimeChart, pivot } from "../components/charts/TimeChart";
import { BarList } from "../components/charts/BarList";

export default function Chain() {
  const flows = useAnalytics<FlowLink[]>("flows");
  const funnel = useAnalytics<FunnelStep[]>("funnel");
  const material = useAnalytics<Series>("material");
  const ov = useAnalytics<Overview>("overview");
  const br = useAnalytics<{ regions: Breakdown[]; mixes: Breakdown[] }>("breakdown");
  const dir = useDirectory();
  const nav = useNavigate();
  const oversight = !!useMe().data?.oversight;
  const rows = useMemo(() => pivot(material.data?.points ?? [], ["asphalt_lot"]), [material.data]);

  const t = new Map((ov.data?.types ?? []).map((x) => [x.type, x.massKg]));

  return (
    <div>
      <PageHeader title="Supply chain" sub="How plastic moves between companies, from sorting plants to the roads they end up in. Widths are tonnes of plastic." />
      <PlasticFunnel steps={funnel.data} loading={funnel.isLoading} />

      <Card className="mt-4" title="Who ships to whom" sub="Reject film to recyclers, then recycled plastic on to additive producers, asphalt plants, contractors and road owners. Widths are tonnes of plastic; thin flows are widened so they stay visible. Click a company to open its passports."
        action={<Legend items={STAGES.map((s) => ({ label: s.who, color: s.color }))} />}>
        {flows.isLoading || !dir.ready ? <Skel h={520} /> : !flows.data?.length ? (
          <Empty title="No shipments in this period">Flows appear once material is shipped and received between companies. Pick a longer period above, or record a shipment from My work.</Empty>
        ) : (
          <ChainSankey links={flows.data ?? []} orgName={dir.orgName} onSelect={(id, col) => nav(`/passports?org=${id}&type=${STAGES[col]?.type ?? homeType(dir.org(id)?.kind)}`)} />
        )}
      </Card>

      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <Card title="Asphalt made with the additive" sub="Tonnes of asphalt per period.">
          {material.isLoading ? <Skel h={240} /> : (
            <TimeChart kind="bars" rows={rows} series={[{ key: "asphalt_lot", label: "Asphalt", color: "var(--s4)" }]}
              yFormat={(v) => `${num(v / 1000, v < 10000 ? 1 : 0)} kt`} tipFormat={(v) => `${num(v)} t`} />
          )}
        </Card>
        <Card title="Asphalt by mix" sub="Mix designations (EN 13108 or national), tonnes in this period.">
          {br.isLoading ? <Skel h={240} /> : (
            <BarList color="var(--s4)" format={(v) => `${num(v)} t`} items={(br.data?.mixes ?? []).map((m) => ({ key: m.key, label: m.key, value: m.value, sub: `${num(m.count)} lots` }))} />
          )}
        </Card>
      </div>

      <div className="mt-4 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        {STAGES.slice(0, 4).map((s) => {
          const v = t.get(s.type) ?? 0;
          return (
            <Card key={s.n} className="h-full">
              <div className="flex items-center gap-2">
                <span className="grid size-8 place-items-center rounded-[10px]" style={{ background: `color-mix(in srgb, ${s.color} 16%, transparent)`, color: s.color }}><s.icon size={16} /></span>
                <div>
                  <div className="text-[13px] font-semibold">{s.who}</div>
                  <div className="text-[11.5px] text-muted">{TYPE[s.type].plural}</div>
                </div>
              </div>
              <div className="num mt-3 text-[24px] font-semibold tracking-tight">{mass(v)}</div>
              <div className="text-[12px] text-muted">{num(dir.orgs.filter((o) => o.kind === s.kind || o.kinds?.includes(s.kind)).length)} {KIND[s.kind].plural.toLowerCase()} {oversight ? "on the platform" : "in your chain"}</div>
            </Card>
          );
        })}
      </div>
    </div>
  );
}



const FUNNEL: { type: FunnelStep["type"]; label: string; sub: string; color: string; lost: string }[] = [
  { type: "reject_lot", label: "Reject film sorted", sub: "film rejected by sorting plants", color: "var(--s1)", lost: "" },
  { type: "recycled_lot", label: "Recycled plastic made", sub: "washed and agglomerated", color: "var(--s2)", lost: "Washing and agglomeration losses; most recycled plastic is sold outside the road chain." },
  { type: "additive_batch", label: "Additive made", sub: "compounded for asphalt", color: "var(--s3)", lost: "Additive producers make what road demand needs." },
  { type: "asphalt_lot", label: "Plastic mixed into asphalt", sub: "inside asphalt that was produced", color: "var(--s4)", lost: "Additive in stock waiting for a mixing run." },
  { type: "road_section", label: "Plastic on the road", sub: "inside sections that were laid", color: "var(--s6)", lost: "Asphalt mixed but not laid yet: waiting for paving." },
];

/**
 * The plastic at each step of the chain, and how much of it moved on. The drop
 * between two steps is plastic still waiting or sold outside the road chain, so
 * the chart answers the question people ask first: how much of what we sort ends
 * up in a road, and where is the rest.
 */
function PlasticFunnel({ steps, loading }: { steps: FunnelStep[] | undefined; loading: boolean }) {
  const by = new Map((steps ?? []).map((x) => [x.type, x]));
  const rows = FUNNEL.map((f) => ({ ...f, t: by.get(f.type)?.tonnes ?? 0, n: by.get(f.type)?.count ?? 0 }));
  const start = rows[0].t;
  const max = Math.max(...rows.map((r) => r.t), 0);
  const road = rows[rows.length - 1].t;
  const asphalt = rows[3].t;
  return (
    <Card title="Where the plastic is" sub="Each bar is the plastic that reached that step in this period. The drop to the next bar is plastic still waiting, or sold outside the road chain.">
      {loading ? <Skel h={260} /> : !start && !max ? <Empty title="No plastic recorded in this period">Pick a longer period above.</Empty> : (
        <>
          <ol className="flex flex-col gap-1">
            {rows.map((r, i) => {
              const prev = i ? rows[i - 1].t : 0;
              const kept = prev > 0 ? (r.t / prev) * 100 : null;
              const width = max > 0 ? Math.max(r.t > 0 ? 1.5 : 0, (r.t / max) * 100) : 0;
              return (
                <li key={r.type}>
                  {i > 0 && (
                    <div className="flex items-center gap-2 py-1 pl-[188px] text-[11.5px] text-muted">
                      <span aria-hidden>↓</span>
                      {kept !== null && kept > 100 ? (
                        // A ratio of two totals from one window, not a cohort: a step can use
                        // stock made before the window, so it is not "more than all moved on".
                        <span><b className="num font-semibold text-text-2">{mass((r.t - prev) * 1000)}</b> more than the step above made in this period: it used stock made before the period</span>
                      ) : (
                        <span><b className="num font-semibold text-text-2">{kept === null ? "—" : `${num(kept, kept < 10 ? 1 : 0)} %`}</b> moved on{prev > r.t ? ` · ${mass((prev - r.t) * 1000)} not yet: ${r.lost.charAt(0).toLowerCase()}${r.lost.slice(1)}` : ""}</span>
                      )}
                    </div>
                  )}
                  <div className="grid grid-cols-[180px_minmax(0,1fr)_92px] items-center gap-2">
                    <div className="min-w-0">
                      <div className="truncate text-[13px] font-semibold">{r.label}</div>
                      <div className="truncate text-[11.5px] text-muted">{num(r.n)} {r.n === 1 ? "record" : "records"} · {r.sub}</div>
                    </div>
                    <div className="h-8 rounded-lg bg-surface-2">
                      <div className="h-full rounded-lg" style={{ width: `${width}%`, background: r.color, opacity: 0.9, transition: "width .5s ease" }} />
                    </div>
                    <div className="num text-right text-[15px] font-semibold">{r.t > 0 ? mass(r.t * 1000) : "—"}</div>
                  </div>
                </li>
              );
            })}
          </ol>
          {start > 0 && (
            <p className="mt-4 rounded-xl bg-surface-2 px-4 py-3 text-[13px] text-text-2">
              <b className="text-text">{num((road / start) * 100, road / start < 0.1 ? 1 : 0)} %</b> of the {mass(start * 1000)} of sorted film is in a road so far
              {asphalt > road ? <>; <b className="text-text">{mass((asphalt - road) * 1000)}</b> of plastic sits in asphalt that has not been laid yet</> : null}.
            </p>
          )}
        </>
      )}
    </Card>
  );
}
