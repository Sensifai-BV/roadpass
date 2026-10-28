import { HELP } from "../lib/help";
import { useMemo } from "react";
import { Download, Leaf, Recycle, Factory, Truck } from "lucide-react";
import { useAnalytics, useMeta, useRange } from "../lib/hooks";
import type { Overview, Series, Breakdown } from "../lib/types";
import { REGION_SHORT } from "../lib/vocab";
import { mass, num, massParts, delta } from "../lib/format";
import { Card, PageHeader, Skel, Stat } from "../components/ui";
import { TimeChart, pivot } from "../components/charts/TimeChart";
import { BarList } from "../components/charts/BarList";

export default function Carbon() {
  const ov = useAnalytics<Overview>("overview");
  const impact = useAnalytics<Series>("impact");
  const br = useAnalytics<{ regions: Breakdown[] }>("breakdown");
  const meta = useMeta().data;
  const range = useRange();
  const o = ov.data;
  const rows = useMemo(() => pivot(impact.data?.points ?? [], ["recycled", "avoided", "chain", "asphalt"]), [impact.data]);
  const disposal = o ? o.impact.avoidedKg - o.impact.avoidedVirginKg : 0;
  const net = o ? o.impact.avoidedKg - o.impact.chainFootprintKg : 0;
  const a = massParts(o?.impact.avoidedKg ?? 0);
  const n = massParts(net);

  return (
    <div>
      <PageHeader title="Carbon & impact" sub="Emissions avoided by turning reject film into asphalt additive, and what the recycling chain itself emits. Every figure is mass-allocated along the passports' lineage."
        actions={
          <div className="flex gap-2">
            {(meta?.scenarios ?? []).map((s) => (
              <a key={s.key} className="btn" href={`/api/v1/exports/greta.csv?scenario=${s.key}&from=${range.from}&to=${range.to}`}>
                <Download size={14} /> GRETA · {s.name}
              </a>
            ))}
          </div>
        } />
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <Stat help={HELP.co2Avoided} label="CO₂e avoided" icon={<Leaf size={14} />} color="var(--s6)" value={a[0]} unit={a[1]} loading={ov.isLoading} delta={o ? delta(o.impact.avoidedKg, o.previous.avoidedKg) : null} hint="disposal + virgin polymer" />
        <Stat help="CO₂e avoided minus the recycling chain's own footprint: the climate gain that remains." label="Net benefit" icon={<Recycle size={14} />} color="var(--s2)" value={n[0]} unit={n[1]} loading={ov.isLoading} hint="after the recycling chain's own emissions" />
        <Stat help={HELP.chainFootprint} label="Recycling chain footprint" icon={<Truck size={14} />} color="var(--s8)" value={mass(o?.impact.chainFootprintKg ?? 0)} loading={ov.isLoading} hint="electricity, gas and haulage" />
        <Stat help="Net CO₂e avoided for each tonne of recycled plastic laid in roads; comparable across periods and projects." label="Per tonne of recycled plastic" icon={<Factory size={14} />} color="var(--s3)" loading={ov.isLoading}
          value={o && o.impact.recycledKg ? num(net / o.impact.recycledKg, 2) : "—"} unit="t CO₂e" hint="net, default scenario" />
      </div>
      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <Card title="Avoided and emitted, per period" sub="Tonnes CO₂e. Both measured on the same scale.">
          {impact.isLoading ? <Skel h={260} /> : (
            <TimeChart kind="line" height={260} rows={rows}
              series={[{ key: "avoided", label: "Avoided", color: "var(--s6)" }, { key: "chain", label: "Recycling chain emissions", color: "var(--s8)" }]}
              yFormat={(v) => `${num(v)} t`} tipFormat={(v) => `${num(v, 1)} t CO₂e`} />
          )}
        </Card>
        <Card title="Where the benefit comes from" sub="This period, default scenario.">
          {ov.isLoading ? <Skel h={200} /> : (
            <BarList format={(v) => `${mass(v)} CO₂e`} items={[
              { key: "virgin", label: "Virgin polymer not produced", value: o!.impact.avoidedVirginKg, color: "var(--s3)", sub: `${mass(o!.impact.recycledKg)} recycled plastic replaces a virgin modifier` },
              { key: "disposal", label: "Landfill and incineration avoided", value: disposal, color: "var(--s6)", sub: `${mass(o!.impact.rejectKg)} reject film diverted` },
              { key: "chain", label: "Emitted by the recycling chain", value: o!.impact.chainFootprintKg, color: "var(--s8)", sub: "washing, compounding, trucks" },
            ]} />
          )}
          <p className="mt-4 text-[12px] text-muted">The asphalt's own manufacture ({mass((o?.impact.footprintKg ?? 0) - (o?.impact.chainFootprintKg ?? 0))} CO₂e this period) happens with or without the additive, so it is not counted against the benefit.</p>
        </Card>
      </div>
      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
        <Card title="Recycled plastic by region" sub="Tonnes laid in finished sections.">
          {br.isLoading ? <Skel h={280} /> : <BarList color="var(--s2)" format={(v) => `${num(v, 1)} t`} items={(br.data?.regions ?? []).map((r) => ({ key: r.key, label: REGION_SHORT[r.key] ?? r.key, value: r.value }))} />}
        </Card>
        <Card title="Scenarios and factors" sub="The GRETA-ready export carries these assumptions with their sources." pad={false}>
          <div className="grid gap-3 p-5 md:grid-cols-3">
            {(meta?.scenarios ?? []).map((s) => (
              <div key={s.key} className="rounded-2xl bg-surface-2 p-3.5">
                <div className="text-[13px] font-semibold">{s.name}</div>
                <div className="mt-1 text-[12px] text-text-2">{s.about}</div>
              </div>
            ))}
          </div>
          <p className="px-5 pb-2 text-[12px] text-muted">Factors follow the country of the organisation that recorded each lot. A country without a national value uses the fallback shown first; its source says so.</p>
          <table className="table">
            <thead><tr><th>Factor</th><th className="text-right">Value</th><th>Source</th></tr></thead>
            <tbody>
              {Object.values(meta?.factors ?? {}).map((f) => {
                const national = Object.values(meta?.regionalFactors?.[f.key] ?? {}).sort((a, b) => a.geo.localeCompare(b.geo));
                return (
                  <tr key={f.key}>
                    <td className="font-medium">{f.name}</td>
                    <td className="num whitespace-nowrap text-right">{f.value} <span className="text-muted">{f.unit}</span></td>
                    <td className="text-[12px] text-text-2">
                      {f.source} ({f.geo}, {f.year})
                      {national.length > 0 && (
                        <details className="mt-1.5">
                          <summary className="cursor-pointer text-accent-ink">{national.length} national {national.length === 1 ? "value" : "values"}: {national.slice(0, 6).map((n) => n.geo).join(", ")}{national.length > 6 ? "…" : ""}</summary>
                          <ul className="mt-1 flex flex-col gap-0.5">
                            {national.map((n) => (
                              <li key={n.geo}><span className="num font-medium text-text">{n.geo} {n.value}</span> {n.unit} · {n.source} ({n.year})</li>
                            ))}
                          </ul>
                        </details>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </Card>
      </div>
    </div>
  );
}
