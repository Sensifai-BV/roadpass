import { useState } from "react";
import { Search } from "lucide-react";
import { useMeta } from "../lib/hooks";
import { Card, PageHeader } from "../components/ui";
import { STAGES, TYPE } from "../lib/vocab";

export default function Glossary() {
  const meta = useMeta().data;
  const [q, setQ] = useState("");
  const terms = (meta?.glossary ?? []).filter((t) => !q || `${t.word} ${t.technical} ${t.meaning}`.toLowerCase().includes(q.toLowerCase()));
  return (
    <div>
      <PageHeader title="Glossary" sub="ROADPASS uses everyday business words. Here is what each one means, and the technical term it replaces." />
      <Card title="The journey in six steps" className="mb-4">
        <ol className="grid gap-3 md:grid-cols-3 xl:grid-cols-6">
          {STAGES.map((s) => (
            <li key={s.n} className="rounded-2xl bg-surface-2 p-4">
              <span className="grid size-8 place-items-center rounded-[10px]" style={{ background: `color-mix(in srgb, ${s.color} 16%, transparent)`, color: s.color }}><s.icon size={16} /></span>
              <div className="mt-3 text-[13px] font-semibold">{s.n}. {s.title}</div>
              <div className="text-[12px] text-muted">{s.who} record {TYPE[s.type].plural.toLowerCase()}.</div>
            </li>
          ))}
        </ol>
      </Card>
      <div className="relative mb-3 max-w-[360px]">
        <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
        <input className="input pl-8" placeholder="Search terms" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search terms" />
      </div>
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {terms.map((t) => (
          <Card key={t.word} className="h-full">
            <div className="text-[15px] font-semibold">{t.word}</div>
            <div className="mt-0.5 text-[12px] font-medium text-accent-ink">{t.technical}</div>
            <p className="mt-2 text-[13px] text-text-2">{t.meaning}</p>
          </Card>
        ))}
      </div>
      <Card title="Data checks" className="mt-4" pad={false}>
        <table className="table">
          <thead><tr><th>Code</th><th>Check</th><th>Severity</th><th>What it means</th></tr></thead>
          <tbody>
            {(meta?.rules ?? []).map((r) => (
              <tr key={r.code}><td className="mono font-semibold">{r.code}</td><td className="font-medium">{r.name}</td><td><span className={`chip ${r.severity === "blocker" ? "chip-critical" : "chip-warning"}`}>{r.severity}</span></td><td className="text-text-2">{r.explain}</td></tr>
            ))}
          </tbody>
        </table>
      </Card>
    </div>
  );
}
