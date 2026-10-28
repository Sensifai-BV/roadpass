import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { Gauge, AlertTriangle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { api, qs } from "../lib/api";
import { useAnalytics, useDirectory, useRange } from "../lib/hooks";
import type { PlantDosing, LineHealth, Suspect, LineHour } from "../lib/types";
import { dateTime, num, pct } from "../lib/format";
import { Card, Empty, ErrorBox, PageHeader, Skel, Hint } from "../components/ui";
import { TimeChart, type Row } from "../components/charts/TimeChart";

export default function Plants() {
  const dosing = useAnalytics<PlantDosing[]>("dosing");
  const lines = useAnalytics<{ lines: LineHealth[]; suspects: Suspect[] }>("lines");
  const dir = useDirectory();
  const [site, setSite] = useState<string>("");
  const siteList = useMemo(() => [...new Set((lines.data?.lines ?? []).map((l) => l.siteId))], [lines.data]);
  const chosen = site || siteList[0] || "";

  return (
    <div>
      <PageHeader title="Plant data" sub="Control-system logs that back the passports: every asphalt mixer cycle with the additive weighed into it, and extruder line readings at recyclers and additive producers." />

      <Card title="Additive dosing at asphalt plants" sub="Measured dosage from every mixer cycle, against the 0.2–1.5 % design window. Missed doses are cycles where the doser delivered nothing." pad={false}>
        {dosing.isLoading ? <div className="p-5"><Skel h={260} /></div> : dosing.isError ? <div className="p-5"><ErrorBox error={dosing.error} /></div> : !dosing.data?.length ? (
          <Empty icon={<Gauge size={20} />} title="No mixer logs">Asphalt plants send mixer cycles through the API (POST /api/v1/telemetry).</Empty>
        ) : (
          <table className="table">
            <thead><tr><th>Plant</th><th className="text-right">Asphalt</th><th className="text-right">Lots</th><th className="text-right">Cycles</th><th className="w-[34%]">Measured dosage</th><th className="text-right">Missed doses</th></tr></thead>
            <tbody>
              {dosing.data.map((d) => {
                const s = dir.site(d.siteId);
                const x = Math.min(100, (d.measuredPct / 1.5) * 100);
                const missedRate = d.cycles ? (d.missedDoses / d.cycles) * 100 : 0;
                return (
                  <tr key={d.siteId}>
                    <td><div className="font-medium">{s?.city ?? d.siteId}</div><div className="text-[11.5px] text-muted">{dir.orgName(d.orgId)}</div></td>
                    <td className="num text-right">{num(d.asphaltTonnes)} t</td>
                    <td className="num text-right">{num(d.lots)}</td>
                    <td className="num text-right">{num(d.cycles)}</td>
                    <td>
                      <div className="flex items-center gap-3">
                        <div className="relative h-2 flex-1 rounded-full bg-surface-3">
                          <div className="absolute inset-y-0 rounded-full bg-[var(--good-soft)]" style={{ left: `${(0.2 / 1.5) * 100}%`, right: 0 }} />
                          <div className="absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-[var(--surface)] bg-[var(--s4)]" style={{ left: `${x}%` }} />
                        </div>
                        <span className="num w-14 text-right font-semibold">{num(d.measuredPct, 2)} %</span>
                      </div>
                    </td>
                    <td className="num text-right">
                      {d.missedDoses > 0 && missedRate > 0.3 ? <span className="inline-flex items-center gap-1 text-warning"><AlertTriangle size={13} />{num(d.missedDoses)}</span> : num(d.missedDoses)}
                      <div className="text-[11px] text-muted">{pct(missedRate, 2)}</div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </Card>

      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
        <Card title="Extruder line" sub="Hourly melt temperature and throughput. Readings that fail a physical plausibility test are left out of the averages and listed on the right."
          action={
            <select className="input w-auto" value={chosen} onChange={(e) => setSite(e.target.value)} aria-label="Site">
              {siteList.map((id) => <option key={id} value={id}>{dir.site(id)?.name ?? id}</option>)}
            </select>
          }>
          {chosen ? <ExtruderChart site={chosen} /> : <Skel h={260} />}
        </Card>
        <Card title="Readings to check" sub="Ranked: likely scaling errors first (×10 temperature, ÷100 pressure). These come from the line, not from a person." pad={false}>
          <div className="max-h-[360px] overflow-auto scroll-thin">
            <table className="table">
              <thead><tr><th>When</th><th>Line</th><th>Reading</th><th>Likely cause</th></tr></thead>
              <tbody>
                {(lines.data?.suspects ?? []).slice(0, 60).map((s, i) => (
                  <tr key={i}>
                    <td className="whitespace-nowrap text-[12px] text-text-2">{dateTime(s.ts)}</td>
                    <td className="text-[12px]">{dir.site(s.siteId)?.city} {s.line}{s.lotId && <div><Link to={`/passports/${s.lotId}`} className="mono text-[11px] text-accent-ink">{s.lotId}</Link></div>}</td>
                    <td className="num text-[12px]">{num(s.meltC, 0)} °C · {num(s.pressureBar, 1)} bar</td>
                    <td className="text-[12px]">{s.reason}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      </div>

      <Card className="mt-4" title="Line health" sub="Run hours, stops and suspect readings per line, this period." pad={false}>
        <table className="table">
          <thead><tr><th>Site</th><th>Line</th><th className="text-right">Run hours</th><th className="text-right">Output</th><th className="text-right">Stopped samples</th><th className="text-right">Suspect readings</th></tr></thead>
          <tbody>
            {(lines.data?.lines ?? []).map((l) => (
              <tr key={l.siteId + l.line} className="clickable" onClick={() => setSite(l.siteId)}>
                <td><div className="font-medium">{dir.site(l.siteId)?.name ?? l.siteId}</div><div className="text-[11.5px] text-muted">{dir.orgName(l.orgId)}</div></td>
                <td>{l.line}</td>
                <td className="num text-right">{num(l.hours)}</td>
                <td className="num text-right">{num(l.outputTonnes)} t</td>
                <td className="num text-right">{pct((l.stopped / Math.max(1, l.samples)) * 100)}</td>
                <td className="num text-right">
                  <Hint text="Values outside the physical range of a PE extruder (melt 50–400 °C, pressure ≥ 10 bar)."><span>{num(l.suspect)}</span></Hint>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </div>
  );
}

function ExtruderChart({ site }: { site: string }) {
  const r = useRange();
  const q = useQuery({
    queryKey: ["extruder", site, r.to],
    queryFn: () => api.get<LineHour[]>(`/analytics/extruder${qs({ site, to: r.to, days: 14 })}`),
  });
  const rows: Row[] = useMemo(() => {
    const by = new Map<string, Row>();
    for (const h of q.data ?? []) {
      const k = h.t;
      const row = by.get(k) ?? ({ t: new Date(h.t), melt: 0, thr: 0, n: 0 } as Row);
      row.melt = ((row.melt as number) * (row.n as number) + h.meltC) / ((row.n as number) + 1);
      row.thr = (row.thr as number) + h.throughputKgH;
      row.n = (row.n as number) + 1;
      by.set(k, row);
    }
    return [...by.values()].sort((a, b) => +a.t - +b.t);
  }, [q.data]);
  if (q.isLoading) return <Skel h={260} />;
  if (q.isError) return <ErrorBox error={q.error} />; // a failed query is not an empty plant
  if (!rows.length) return <Empty title="No readings in the last 14 days of the period" />;
  return (
    <div className="flex flex-col gap-5">
      <div>
        <div className="mb-1 text-[12px] font-medium text-text-2">Melt temperature</div>
        <TimeChart kind="line" height={150} rows={rows} legend={false} series={[{ key: "melt", label: "Melt temperature", color: "var(--s2)" }]} yFormat={(v) => `${num(v)} °C`} />
      </div>
      <div>
        <div className="mb-1 text-[12px] font-medium text-text-2">Throughput</div>
        <TimeChart kind="area" height={150} rows={rows} legend={false} series={[{ key: "thr", label: "Throughput", color: "var(--s1)" }]} yFormat={(v) => `${num(v)} kg/h`} />
      </div>
    </div>
  );
}
