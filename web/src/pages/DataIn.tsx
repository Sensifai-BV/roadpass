import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, FileSpreadsheet, KeyRound, Loader2, Plus, Upload, CheckCircle2, Code2, Trash2 } from "lucide-react";
import clsx from "clsx";
import { api, ApiError } from "../lib/api";
import { useMe, useMeta } from "../lib/hooks";
import type { ApiKey, Duty, Ingest, RecordType } from "../lib/types";
import { DUTY, TYPE, dutyFor } from "../lib/vocab";
import { ago, dateTime, num } from "../lib/format";
import { Card, Copy_, Empty, ErrorBox, Modal, PageHeader, Segmented, TypeBadge } from "../components/ui";

interface Preview { rows: number; valid: boolean; errors: { row: number; ref: string; errors: { field: string; message: string }[] }[]; columns: string[] }

// Spreadsheet times without an offset are read in the uploader's own time zone.
const localZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";

export default function DataIn() {
  const [params, setParams] = useSearchParams();
  const tab = (["csv", "api", "history", "fields"].includes(params.get("tab") ?? "") ? params.get("tab") : "csv") as "csv" | "api" | "history" | "fields";
  const setTab = (t: string) => setParams((p) => { const n = new URLSearchParams(p); n.set("tab", t); return n; }, { replace: true });
  return (
    <div>
      <PageHeader title="Data in" sub="Send records by uploading a spreadsheet, or connect your ERP or plant system to the API (JSON or NGSI-LD). Every record is checked, signed with your company key and sealed." />
      <div className="mb-4">
        <Segmented value={tab} onChange={setTab} label="Channel" options={[
          { value: "csv", label: "Upload CSV" }, { value: "api", label: "API & keys" }, { value: "history", label: "History" }, { value: "fields", label: "Field reference" },
        ]} />
      </div>
      {tab === "csv" && <CsvWizard />}
      {tab === "api" && <ApiAccess />}
      {tab === "history" && <History />}
      {tab === "fields" && <Fields />}
    </div>
  );
}

function CsvWizard() {
  const me = useMe().data!;
  const meta = useMeta().data;
  const qc = useQueryClient();
  const kinds = [me.org.kind, ...(me.org.kinds ?? [])];
  const allowed = (meta?.types ?? []).filter((t) => t.type !== "acceptance" && me.duties.includes(dutyFor(t.type))
    && (me.org.kind === "operator" || t.createdBy.some((k) => kinds.includes(k))));
  const [type, setType] = useState<RecordType | "">((new URLSearchParams(location.search).get("type") as RecordType | null) ?? "");
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [done, setDone] = useState<{ results: { id: string; status: string }[] } | null>(null);
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const t = type || allowed[0]?.type;

  const check = async (f: File) => {
    setFile(f);
    setDone(null);
    setErr(null);
    setBusy(true);
    try {
      setPreview(await api.upload<Preview>(`/imports?type=${t}&dryRun=1&tz=${encodeURIComponent(localZone)}`, f));
    } catch (e) {
      setErr(e);
      setPreview(null);
    } finally {
      setBusy(false);
    }
  };
  const submit = async () => {
    if (!file) return;
    setBusy(true);
    setErr(null);
    try {
      setDone(await api.upload(`/imports?type=${t}&tz=${encodeURIComponent(localZone)}`, file));
      setPreview(null);
      setFile(null);
      void qc.invalidateQueries();
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  if (!allowed.length) return <Card><Empty title="Nothing for you to upload">Your organisation or your duties do not include submitting records. Auditors, reporters and road owners read passports; plants upload evidence.</Empty></Card>;
  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)]">
      <Card title="1 · Choose what you are sending">
        <div className="flex flex-col gap-2">
          {allowed.map((x) => (
            <button key={x.type} onClick={() => { setType(x.type); setPreview(null); setDone(null); }}
              className={clsx("flex items-center gap-3 rounded-xl border px-3 py-2.5 text-left transition", t === x.type ? "border-accent bg-accent-soft" : "border-line hover:bg-surface-2")}>
              <TypeBadge type={x.type} />
              <span className="min-w-0 flex-1">
                <span className="block text-[13px] font-semibold">{x.plural}</span>
                <span className="block truncate text-[12px] text-muted">{x.summary}</span>
              </span>
              <a className="btn btn-sm" href={`/api/v1/templates/${x.type}.csv`} onClick={(e) => e.stopPropagation()}><Download size={13} /> Template</a>
            </button>
          ))}
        </div>
      </Card>
      <div className="flex flex-col gap-4">
        <Card title="2 · Drop your file" sub="Comma or semicolon separated, UTF-8, up to 5,000 rows. Excel exports with decimal commas and day-first dates (dd/mm/yyyy) work.">
          <label
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => { e.preventDefault(); const f = e.dataTransfer.files[0]; if (f) void check(f); }}
            className="flex cursor-pointer flex-col items-center justify-center gap-2 rounded-2xl border-2 border-dashed border-line-strong bg-surface-2 px-6 py-10 text-center hover:bg-surface-3">
            {busy ? <Loader2 size={26} className="animate-spin text-muted" /> : <FileSpreadsheet size={26} className="text-muted" />}
            <div className="text-[14px] font-semibold">{file ? file.name : `Drop a ${TYPE[t as RecordType]?.plural.toLowerCase() ?? ""} CSV here`}</div>
            <div className="text-[12px] text-muted">or click to choose a file · nothing is stored until you confirm</div>
            <input type="file" accept=".csv,text/csv" className="hidden" onChange={(e) => e.target.files?.[0] && void check(e.target.files[0])} />
          </label>
          {err ? <div className="mt-3"><ErrorBox error={err} />{err instanceof ApiError && Array.isArray(err.problem.errors) && <RowErrors errors={err.problem.errors as Preview["errors"]} />}</div> : null}
        </Card>
        {preview && (
          <Card title="3 · Check and import" sub={`${num(preview.rows)} rows read.`}>
            {preview.valid ? (
              <div className="flex items-center justify-between gap-3 rounded-xl bg-[var(--good-soft)] px-4 py-3">
                <span className="inline-flex items-center gap-2 text-[13px] font-semibold text-good"><CheckCircle2 size={16} /> Every row is valid.</span>
                <button className="btn btn-primary" onClick={submit} disabled={busy}>{busy ? <Loader2 size={14} className="animate-spin" /> : <Upload size={14} />} Import {num(preview.rows)} records</button>
              </div>
            ) : <RowErrors errors={preview.errors} />}
          </Card>
        )}
        {done && (
          <Card>
            <div className="flex items-center gap-3">
              <CheckCircle2 size={22} className="text-good" />
              <div>
                <div className="text-[14px] font-semibold">{num(done.results.length)} records stored, signed and checked.</div>
                <div className="text-[12.5px] text-muted">{done.results.filter((r) => r.status !== "ok").length} need a look in Data checks. They will be sealed on the ledger in the next batch.</div>
              </div>
            </div>
          </Card>
        )}
      </div>
    </div>
  );
}

function RowErrors({ errors }: { errors: Preview["errors"] }) {
  return (
    <div className="mt-2 max-h-[280px] overflow-auto rounded-xl border border-line scroll-thin">
      <table className="table text-[12.5px]">
        <thead><tr><th>Row</th><th>Reference</th><th>Problem</th></tr></thead>
        <tbody>
          {errors.slice(0, 200).map((e) => (
            <tr key={e.row}>
              <td className="num">{e.row}</td>
              <td className="mono">{e.ref || "—"}</td>
              <td>{e.errors.map((x) => <div key={x.field}><b>{x.field}</b>: {x.message}</div>)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ApiAccess() {
  const me = useMe().data!;
  const qc = useQueryClient();
  const keys = useQuery({ queryKey: ["api-keys"], queryFn: () => api.get<ApiKey[]>("/org/api-keys"), enabled: me.user.role === "admin" });
  const [fresh, setFresh] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<string[]>(["records:read", "records:write"]);
  const [duties, setDuties] = useState<Duty[]>(["receive", "produce", "test", "release", "ship"]);
  const create = useMutation({
    mutationFn: () => api.send<{ secret: string }>("POST", "/org/api-keys", { name, scopes, duties }),
    onSuccess: (r) => { setFresh(r.secret); setName(""); void qc.invalidateQueries({ queryKey: ["api-keys"] }); },
  });
  const revoke = useMutation({ mutationFn: (id: string) => api.send("DELETE", `/org/api-keys/${id}`), onSuccess: () => void qc.invalidateQueries({ queryKey: ["api-keys"] }) });
  const base = location.origin;
  const jsonSnippet = `curl -X POST ${base}/api/v1/records \\
  -H "Authorization: Bearer $ROADPASS_KEY" \\
  -H "Content-Type: application/json" \\
  -H "Idempotency-Key: erp-batch-2026-10-15-01" \\
  -d '{"records":[{"type":"reject_lot","ref":"LOT-4471","siteId":"<your site id>",
       "occurredAt":"2026-10-15T08:30:00Z","massKg":18450,
       "data":{"source":"municipal_packaging","ldpe_share_pct":76,"avoided_route":"landfill"}}]}'`;
  const ngsiSnippet = `curl -X POST ${base}/ngsi-ld/v1/entityOperations/upsert \\
  -H "Authorization: Bearer $ROADPASS_KEY" \\
  -H "Content-Type: application/ld+json" \\
  -d '[{"@context":"${base}/ngsi-ld/v1/context.jsonld",
        "type":"PlasticRejectLot","reference":"LOT-4472",
        "dateObserved":"2026-10-15T09:10:00Z",
        "mass":{"type":"Property","value":17.9,"unitCode":"TNE"},
        "source":"municipal_packaging","ldpeSharePct":74,"avoidedRoute":"landfill"}]'`;
  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
      <div className="flex flex-col gap-4">
        <Card title="JSON API" sub="All or nothing per request (1–1,000 records). Replays with the same Idempotency-Key return the first answer." action={<a className="btn btn-sm" href="/api/openapi.json" target="_blank" rel="noreferrer"><Code2 size={13} /> OpenAPI</a>}>
          <Snippet code={jsonSnippet} />
        </Card>
        <Card title="NGSI-LD (ETSI GS CIM 009)" sub="The same records as NGSI-LD entities, for FIWARE brokers and other data platforms. Normalized or concise; options=keyValues on reads.">
          <Snippet code={ngsiSnippet} />
          <div className="mt-3 flex flex-wrap gap-2 text-[12px]">
            <a className="chip hover:bg-accent-soft" href="/ngsi-ld/v1/context.jsonld" target="_blank" rel="noreferrer">@context</a>
            <span className="chip">GET /ngsi-ld/v1/entities?type=RoadSection</span>
            <span className="chip">PATCH …/entities/{"{id}"}/attrs creates v+1</span>
          </div>
        </Card>
      </div>
      <Card title="API keys" sub="Keys belong to your organisation. The secret is shown once; only its fingerprint is stored." pad={false}>
        {me.user.role !== "admin" ? <Empty icon={<KeyRound size={20} />} title="Ask an admin">Only organisation admins manage API keys.</Empty> : (
          <>
            <form className="flex flex-wrap items-end gap-2 border-b border-line p-4" onSubmit={(e) => { e.preventDefault(); create.mutate(); }}>
              <div className="min-w-[180px] flex-1">
                <label className="label" htmlFor="kname">Name</label>
                <input id="kname" className="input" placeholder="ERP integration" value={name} onChange={(e) => setName(e.target.value)} required />
              </div>
              {["records:read", "records:write"].map((s) => (
                <label key={s} className="flex h-[34px] items-center gap-1.5 text-[12.5px]">
                  <input type="checkbox" checked={scopes.includes(s)} onChange={(e) => setScopes((x) => (e.target.checked ? [...x, s] : x.filter((y) => y !== s)))} /> {s}
                </label>
              ))}
              <button className="btn btn-primary" disabled={create.isPending || !name || !scopes.length}><Plus size={14} /> Create key</button>
              <div className="w-full">
                <span className="label">Duties this key may perform</span>
                <div className="flex flex-wrap gap-1">
                  {(Object.keys(DUTY) as Duty[]).map((d) => (
                    <button key={d} type="button" onClick={() => setDuties((x) => (x.includes(d) ? x.filter((y) => y !== d) : [...x, d]))}
                      className={clsx("chip", duties.includes(d) ? "chip-accent" : "opacity-60")} title={DUTY[d].explain}>{DUTY[d].short}</button>
                  ))}
                </div>
                <p className="mt-1 text-[11.5px] text-muted">e.g. a weighbridge terminal gets only Receive; the ERP gets Produce and Ship.</p>
              </div>
            </form>
            <table className="table">
              <thead><tr><th>Name</th><th>Key</th><th>Scopes</th><th>Duties</th><th>Last used</th><th /></tr></thead>
              <tbody>
                {(keys.data ?? []).map((k) => (
                  <tr key={k.id} className={k.revokedAt ? "opacity-50" : ""}>
                    <td className="font-medium">{k.name}</td>
                    <td className="mono text-[12px] text-text-2">{k.prefix}</td>
                    <td className="text-[12px]">{k.scopes.join(", ")}</td>
                    <td className="text-[12px]">{(k.duties ?? []).map((d) => DUTY[d]?.short).join(", ")}</td>
                    <td className="text-text-2">{k.revokedAt ? "revoked" : k.lastUsedAt ? ago(k.lastUsedAt) : "never"}</td>
                    <td className="text-right">{!k.revokedAt && <button className="btn btn-ghost btn-sm text-critical" onClick={() => revoke.mutate(k.id)}><Trash2 size={13} /> Revoke</button>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </Card>
      <Modal open={!!fresh} onOpenChange={(o) => !o && setFresh(null)} title="Your new API key" sub="Copy it now. It will not be shown again.">
        <div className="flex items-center gap-2 rounded-xl bg-surface-2 px-3 py-2.5">
          <span className="mono flex-1 break-all text-[12.5px]">{fresh}</span>
          {fresh && <Copy_ text={fresh} label="Copy key" />}
        </div>
        <button className="btn btn-primary mt-4" onClick={() => setFresh(null)}>Done</button>
      </Modal>
    </div>
  );
}

function Snippet({ code }: { code: string }) {
  return (
    <div className="relative">
      <pre className="mono overflow-x-auto rounded-xl bg-[#111113] p-4 text-[11.5px] leading-relaxed text-[#e6e6e9] scroll-thin">{code}</pre>
      <div className="absolute right-2 top-2 rounded-md bg-white/10 text-white"><Copy_ text={code} label="Copy snippet" /></div>
    </div>
  );
}

function History() {
  const q = useQuery({ queryKey: ["imports"], queryFn: () => api.get<Ingest[]>("/imports") });
  return (
    <Card pad={false} title="Recent submissions" sub="API, NGSI-LD and CSV, newest first.">
      {!q.data?.length ? <Empty title="Nothing submitted yet" /> : (
        <table className="table">
          <thead><tr><th>When</th><th>Channel</th><th>Type</th><th className="text-right">Rows</th><th className="text-right">Stored</th><th>Status</th></tr></thead>
          <tbody>
            {q.data.map((i) => (
              <tr key={i.id}>
                <td className="text-text-2">{dateTime(i.createdAt)}</td>
                <td><span className="chip">{i.channel}</span></td>
                <td>{i.recordType ? TYPE[i.recordType as RecordType]?.plural : "mixed"}</td>
                <td className="num text-right">{num(i.total)}</td>
                <td className="num text-right">{num(i.accepted)}</td>
                <td><span className={clsx("chip", i.status === "done" ? "chip-good" : i.status === "failed" ? "chip-critical" : "")}>{i.status}</span>
                  {i.errors?.length ? <span className="ml-2 text-[12px] text-muted">{i.errors.length} invalid rows</span> : null}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}

function Fields() {
  const meta = useMeta().data;
  const [t, setT] = useState<RecordType>("asphalt_lot");
  const info = meta?.types.find((x) => x.type === t);
  return (
    <div className="grid gap-4 xl:grid-cols-[260px_minmax(0,1fr)]">
      <Card pad={false}>
        <ul className="p-2">
          {(meta?.types ?? []).map((x) => (
            <li key={x.type}>
              <button onClick={() => setT(x.type)} className={clsx("flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-[13px]", t === x.type ? "bg-surface-3 font-semibold" : "hover:bg-surface-2")}>
                <TypeBadge type={x.type} size={22} /> {x.name}
              </button>
            </li>
          ))}
        </ul>
      </Card>
      <Card title={info?.name} sub={`${info?.summary ?? ""} Technical term: ${info?.technical ?? ""}. NGSI-LD type below.`} pad={false}>
        <table className="table">
          <thead><tr><th>Field</th><th>Meaning</th><th>Type</th><th>Required</th><th>Example</th></tr></thead>
          <tbody>
            {[{ name: "ref", label: "Your reference (unique per type)", kind: "text", required: true, example: "LOT-4471" },
              { name: "occurred_at", label: "When it happened", kind: "time", required: true, example: "2026-10-15 08:30" },
              { name: "mass_kg", label: "Mass", kind: "number", unit: "kg", required: false, example: "18450" },
              ...(meta?.fields[t] ?? [])].map((f) => (
              <tr key={f.name}>
                <td className="mono text-[12.5px] font-semibold">{f.name}</td>
                <td>{f.label}{"help" in f && f.help ? <div className="text-[11.5px] text-muted">{f.help}</div> : null}</td>
                <td className="text-[12px] text-text-2">{f.kind}{"unit" in f && f.unit ? ` · ${f.unit}` : ""}{"enum" in f && f.enum ? <div className="text-[11px] text-muted">{f.enum.join(" | ")}</div> : null}</td>
                <td>{f.required ? <span className="chip chip-accent">required</span> : <span className="text-[12px] text-muted">optional</span>}</td>
                <td className="mono text-[12px] text-text-2">{f.example}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </div>
  );
}
