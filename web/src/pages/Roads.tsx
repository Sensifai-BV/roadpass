import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAnalytics, useDirectory } from "../lib/hooks";
import type { Breakdown, SectionPoint, SiteVolume } from "../lib/types";
import { REGION_SHORT } from "../lib/vocab";
import { date, mass, num, titleCase } from "../lib/format";
import { Card, PageHeader, Skel, Legend } from "../components/ui";
import { RegionMap, type MapPoint } from "../components/charts/RegionMap";
import { BarList } from "../components/charts/BarList";

// Sequential ramp for the regional choropleth (one hue, light → dark).
const SEQ = ["var(--seq-1)", "var(--seq-2)", "var(--seq-3)", "var(--seq-4)", "var(--seq-5)", "var(--seq-6)"];

export default function Roads() {
  const map = useAnalytics<{ sites: SiteVolume[]; sections: SectionPoint[] }>("map");
  const br = useAnalytics<{ regions: Breakdown[]; mixes: Breakdown[] }>("breakdown");
  const dir = useDirectory();
  const nav = useNavigate();
  const [region, setRegion] = useState<string | null>(null);

  const byRegion = useMemo(() => new Map((br.data?.regions ?? []).map((r) => [r.key, r])), [br.data]);
  const maxR = Math.max(1, ...(br.data?.regions ?? []).map((r) => r.value));
  const fill = (name: string) => {
    const r = byRegion.get(name);
    if (!r) return undefined;
    const i = Math.min(SEQ.length - 1, Math.floor((r.value / maxR) * SEQ.length));
    return name === region ? "var(--accent)" : SEQ[i];
  };
  const sections = useMemo(() => (map.data?.sections ?? []).filter((s) => !region || s.region === region), [map.data, region]);
  const points: MapPoint[] = sections.map((s) => ({
    id: s.id, lat: s.lat, lon: s.lon, r: 2 + Math.min(5, Math.sqrt(s.recycledKg) / 8), color: "var(--s6)",
    label: `${s.road} · ${s.id}`, sub: `${num(s.lengthKm, 1)} km ${s.layer} layer · ${mass(s.recycledKg)} recycled plastic · ${date(s.at)}`,
    onClick: () => nav(`/passports/${s.id}`),
  }));
  const totals = sections.reduce((a, s) => ({ km: a.km + s.lengthKm, rec: a.rec + s.recycledKg, co2: a.co2 + s.avoidedKg }), { km: 0, rec: 0, co2: 0 });

  return (
    <div>
      <PageHeader title="Roads" sub="Finished road sections with recycled plastic in their asphalt. Regions shaded by recycled plastic laid; select a region to focus." />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <Card pad>
          {map.isLoading || br.isLoading ? <Skel h={520} /> : <RegionMap points={points} height={520} fill={fill} onRegion={(r) => setRegion((x) => (x === r ? null : r))} />}
          <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2 text-[11.5px] text-muted">
              Less
              <span className="flex">{SEQ.map((c) => <span key={c} className="h-2.5 w-6 first:rounded-l last:rounded-r" style={{ background: c }} />)}</span>
              More recycled plastic
            </div>
            <Legend items={[{ label: "Road section", color: "var(--s6)" }]} />
          </div>
        </Card>
        <div className="flex flex-col gap-4">
          <Card title={region ?? "All regions"} sub={region ? <button className="text-accent-ink hover:underline" onClick={() => setRegion(null)}>Show all regions</button> : "This period"}>
            <div className="grid grid-cols-3 gap-3">
              <div><div className="num text-[22px] font-semibold">{num(sections.length)}</div><div className="text-[12px] text-muted">{sections.length === 1 ? "section" : "sections"}</div></div>
              <div><div className="num text-[22px] font-semibold">{num(totals.km, totals.km < 10 ? 1 : 0)}</div><div className="text-[12px] text-muted">km</div></div>
              <div><div className="num text-[22px] font-semibold">{mass(totals.rec)}</div><div className="text-[12px] text-muted">recycled plastic</div></div>
            </div>
          </Card>
          <Card title="By region" sub="Recycled plastic laid.">
            {br.isLoading ? <Skel h={280} /> : (
              <BarList color="var(--s6)" format={(v) => mass(v * 1000)} onSelect={(k) => setRegion((x) => (x === k ? null : k))}
                items={(br.data?.regions ?? []).map((r) => ({ key: r.key, label: REGION_SHORT[r.key] ?? r.key, value: r.value, sub: `${num(r.count)} ${r.count === 1 ? "section" : "sections"}`, color: r.key === region ? "var(--accent)" : undefined }))} />
            )}
          </Card>
        </div>
      </div>
      <Card className="mt-4" title="Newest sections" pad={false}>
        <div className="max-h-[420px] overflow-auto scroll-thin">
          <table className="table">
            <thead><tr><th>Section</th><th>Road</th><th>Region</th><th>Layer</th><th className="text-right">Length</th><th className="text-right">Asphalt</th><th className="text-right">Recycled plastic</th><th className="text-right">CO₂e avoided</th><th>Recorded</th></tr></thead>
            <tbody>
              {sections.slice(0, 200).map((s) => (
                <tr key={s.id} className="clickable" onClick={() => nav(`/passports/${s.id}`)}>
                  <td><Link to={`/passports/${s.id}`} className="mono font-semibold text-accent-ink">{s.id}</Link></td>
                  <td className="font-medium">{s.road}</td>
                  <td className="text-text-2">{REGION_SHORT[s.region] ?? s.region}</td>
                  <td className="text-text-2">{titleCase(s.layer)}</td>
                  <td className="num text-right">{num(s.lengthKm, 1)} km</td>
                  <td className="num text-right">{mass(s.massKg)}</td>
                  <td className="num text-right font-semibold">{mass(s.recycledKg)}</td>
                  <td className="num text-right">{mass(s.avoidedKg)}</td>
                  <td className="text-text-2">{date(s.at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      {!dir.ready && null}
    </div>
  );
}
