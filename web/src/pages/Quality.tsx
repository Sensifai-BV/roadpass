import { useEffect, useState, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { OctagonX, CircleAlert, CheckCircle2, ShieldCheck, Loader2, PencilLine } from "lucide-react";
import clsx from "clsx";
import { api, qs } from "../lib/api";
import { useAnalytics, useDirectory, useMe, useMeta } from "../lib/hooks";
import type { Finding, TestStats, Series } from "../lib/types";
import { LAB_TEST_NAMES, TYPE } from "../lib/vocab";
import { ago, dateTime, num, pct, titleCase } from "../lib/format";
import { Card, Drawer, Empty, ErrorBox, KV, PageHeader, Skel, Segmented } from "../components/ui";
import { TimeChart, pivot } from "../components/charts/TimeChart";

export default function Quality() {
  const meta = useMeta().data;
  const me = useMe().data!;
  const [params, setParams] = useSearchParams();
  const code = params.get("code") ?? "";
  const record = params.get("record") ?? "";
  const [status, setStatus] = useState<"open" | "resolved" | "accepted" | "">("open");
  const [page, setPage] = useState(0);
  const [sel, setSel] = useState<Finding | null>(null);
  const dir = useDirectory();
  const summary = useQuery({ queryKey: ["findings-summary"], queryFn: () => api.get<Record<string, Record<string, number>>>("/findings/summary") });
  const list = useQuery({
    queryKey: ["findings", code, status, record, page],
    queryFn: () => api.get<{ items: Finding[]; total: number }>(`/findings${qs({ code, status, record, limit: 50, offset: page * 50 })}`),
    placeholderData: (p) => p,
  });
  const lab = useAnalytics<{ stats: TestStats[]; series: Series }>("lab");
  // ?finding=<id> (from My work or a notification) opens that finding.
  const want = params.get("finding");
  useEffect(() => {
    const f = want && list.data?.items.find((x) => String(x.id) === want);
    if (f) { setSel(f); const p = new URLSearchParams(params); p.delete("finding"); setParams(p, { replace: true }); }
  }, [want, list.data]); // eslint-disable-line react-hooks/exhaustive-deps
  const setCode = (c: string) => {
    const p = new URLSearchParams(params);
    if (c && c !== code) p.set("code", c);
    else p.delete("code");
    p.delete("record");
    setParams(p, { replace: true });
    setPage(0);
  };

  return (
    <div>
      <PageHeader title="Data checks" sub="Every record is checked when it arrives. Blockers stop a record from being sealed until it is corrected or independently accepted; warnings ask for a look." />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {(meta?.rules ?? []).map((r) => {
          const c = summary.data?.[r.code] ?? {};
          const open = c.open ?? 0;
          const closed = (c.resolved ?? 0) + (c.accepted ?? 0);
          const active = code === r.code;
          return (
            <button key={r.code} onClick={() => setCode(r.code)} className={clsx("card card-pad rise text-left transition hover:shadow-[var(--shadow-2)]", active && "ring-2 ring-[var(--accent)]")}>
              <div className="flex items-center justify-between">
                <span className="mono text-[12px] font-semibold text-muted">{r.code}</span>
                {r.severity === "blocker"
                  ? <span className="chip chip-critical"><OctagonX size={11} />Blocker</span>
                  : <span className="chip chip-warning"><CircleAlert size={11} />Warning</span>}
              </div>
              <div className="mt-2 text-[14px] font-semibold">{r.name}</div>
              <div className="mt-0.5 line-clamp-2 text-[12px] text-text-2">{r.explain}</div>
              <div className="mt-3 flex items-baseline gap-2">
                <span className="num text-[22px] font-semibold">{num(open)}</span>
                <span className="text-[12px] text-muted">open · {num(closed)} closed</span>
              </div>
            </button>
          );
        })}
      </div>

      <Card className="mt-4" pad={false} title={code ? `${code} findings` : record ? `Findings on ${record}` : "Findings"} sub={list.data ? `${num(list.data.total)} in total` : undefined}
        action={<Segmented value={status} onChange={(v) => { setStatus(v); setPage(0); }} label="Status" options={[{ value: "open", label: "Open" }, { value: "resolved", label: "Resolved" }, { value: "accepted", label: "Accepted" }, { value: "", label: "All" }]} />}>
        {list.isLoading ? <div className="p-4"><Skel h={300} /></div> : list.data?.items.length === 0 ? (
          <Empty icon={<CheckCircle2 size={20} />} title="Nothing here">No findings match. Records that pass every check are sealed automatically.</Empty>
        ) : (
          <table className="table">
            <thead><tr><th>Check</th><th>Record</th><th>What was found</th><th>Organisation</th><th>Raised</th><th>Status</th></tr></thead>
            <tbody>
              {list.data?.items.map((f) => (
                <tr key={f.id} className="clickable" onClick={() => setSel(f)}>
                  <td>
                    <span className="inline-flex items-center gap-1.5 font-semibold">
                      {f.severity === "blocker" ? <OctagonX size={14} className="text-critical" /> : <CircleAlert size={14} className="text-warning" />}
                      {f.code}
                    </span>
                  </td>
                  <td><span className="mono text-[12.5px]">{f.recordId}</span><div className="text-[11.5px] text-muted">{f.recordType ? TYPE[f.recordType]?.name : ""}</div></td>
                  <td className="max-w-[420px]"><div className="line-clamp-2 text-[13px]">{f.message}</div></td>
                  <td className="text-text-2">{dir.orgName(f.orgId)}</td>
                  <td className="whitespace-nowrap text-text-2">{ago(f.createdAt)}</td>
                  <td><span className={clsx("chip", f.status === "open" ? (f.severity === "blocker" ? "chip-critical" : "chip-warning") : "chip-good")}>{titleCase(f.status)}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {list.data && list.data.total > 50 && (
          <div className="flex items-center justify-end gap-2 border-t border-line px-4 py-2.5 text-[12px] text-muted">
            Page {page + 1} of {Math.ceil(list.data.total / 50)}
            <button className="btn btn-sm" disabled={page === 0} onClick={() => setPage((p) => p - 1)}>Previous</button>
            <button className="btn btn-sm" disabled={(page + 1) * 50 >= list.data.total} onClick={() => setPage((p) => p + 1)}>Next</button>
          </div>
        )}
      </Card>

      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
        <Card title="Lab tests passing" sub="Share of tests within specification, per period.">
          {lab.isLoading ? <Skel h={240} /> : <LabChart series={lab.data!.series} />}
        </Card>
        <Card title="Lab results" sub="Distribution of measured values this period." pad={false}>
          <table className="table">
            <thead><tr><th>Test</th><th className="text-right">Tests</th><th className="text-right">Pass</th><th className="text-right">P10 · median · P90</th></tr></thead>
            <tbody>
              {(lab.data?.stats ?? []).map((s) => (
                <tr key={s.test}>
                  <td className="font-medium">{LAB_TEST_NAMES[s.test] ?? s.test}</td>
                  <td className="num text-right">{num(s.count)}</td>
                  <td className="num text-right">{pct(s.passRate * 100)}</td>
                  <td className="num text-right text-text-2">{num(s.p10, 2)} · <b className="text-text">{num(s.p50, 2)}</b> · {num(s.p90, 2)} <span className="text-muted">{meta?.labTests[s.test]?.unit}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      </div>

      <FindingDrawer finding={sel} onClose={() => setSel(null)} canAccept={(me.oversight || me.org.kind === "road_owner") && (me.user.role === "admin" || me.duties.includes("release"))} myOrg={me.org.id} />
    </div>
  );
}

function LabChart({ series }: { series: Series }) {
  const keys = ["itsr", "air_voids", "mfi", "binder_content"];
  const rows = pivot(series.points.filter((p) => keys.includes(p.series)), keys);
  return (
    <TimeChart kind="line" rows={rows} height={240}
      series={[{ key: "itsr", label: "Water sensitivity", color: "var(--s1)" }, { key: "air_voids", label: "Air voids", color: "var(--s2)" },
        { key: "mfi", label: "Melt flow", color: "var(--s3)" }, { key: "binder_content", label: "Binder content", color: "var(--s4)" }]}
      yFormat={(v) => `${num(v)} %`} tipFormat={(v) => pct(v)} />
  );
}

function FindingDrawer({ finding, onClose, canAccept, myOrg }: { finding: Finding | null; onClose: () => void; canAccept: boolean; myOrg: string }) {
  const qc = useQueryClient();
  const [reason, setReason] = useState("");
  const dir = useDirectory();
  const close = useMutation({
    mutationFn: (status: "resolved" | "accepted") => api.send<Finding>("POST", `/findings/${finding!.id}/resolve`, { status, reason }),
    onSuccess: () => {
      setReason("");
      void qc.invalidateQueries({ queryKey: ["findings"] });
      void qc.invalidateQueries({ queryKey: ["findings-summary"] });
      onClose();
    },
  });
  const f = finding;
  return (
    <Drawer open={!!f} onOpenChange={(o) => !o && onClose()} title={f ? `${f.code} · ${f.message}` : ""} sub={f ? `${f.recordId} · ${dir.orgName(f.orgId)}` : undefined}>
      {f && (
        <div className="flex flex-col gap-5">
          <div className={clsx("rounded-2xl px-4 py-3 text-[13px]", f.severity === "blocker" ? "bg-[var(--critical-soft)]" : "bg-[var(--warning-soft)]")}>
            <div className="font-semibold">{f.severity === "blocker" ? "Blocker: the record must be corrected, or accepted by an independent party. Until then it is not sealed, or stays flagged if it already was." : "Warning: the record is sealed, but someone should look."}</div>
            <div className="mt-1 text-text-2">How to fix: {f.fix}</div>
          </div>
          <KV items={[
            ["Record", <Link key="r" to={`/passports/${f.recordId}`} className="mono text-accent-ink hover:underline">{f.recordId}</Link>],
            ["Raised", dateTime(f.createdAt)],
            ["Status", titleCase(f.status)],
            ...(f.closedAt ? [["Closed", `${dateTime(f.closedAt)} by ${f.closedBy}`] as [string, string], ["Reason", f.closeReason ?? ""] as [string, string]] : []),
          ]} />
          {f.detail && Object.keys(f.detail).length > 0 && (
            <KV items={Object.entries(f.detail).map(([k, v]) => [titleCase(k.replace(/_/g, " ")),
              typeof v === "string" && /^[A-Z]{2,4}-\d{2}-\d{6}$/.test(v)
                ? <Link key={k} to={`/passports/${v}`} className="mono text-accent-ink hover:underline">{v}</Link>
                : typeof v === "object" ? JSON.stringify(v) : String(v)] as [string, ReactNode])} />
          )}
          {f.status === "open" && f.orgId === myOrg && f.severity === "blocker" && (
            <Link to={`/passports/${f.recordId}?correct=1`} className="btn btn-primary self-start"><PencilLine size={14} /> Correct {f.recordId}</Link>
          )}
          {f.status === "open" && ((f.severity === "warning" && f.orgId === myOrg) || canAccept) && (
            <div className="flex flex-col gap-3 border-t border-line pt-4">
              <label className="label" htmlFor="reason">Reason (kept in the audit trail)</label>
              <textarea id="reason" className="input" rows={3} value={reason} onChange={(e) => setReason(e.target.value)}
                placeholder={f.severity === "blocker" ? "e.g. Paper delivery note checked on site; quantities match." : "e.g. Re-test passed; certificate attached in the ERP."} />
              {close.error ? <ErrorBox error={close.error} /> : null}
              <div className="flex flex-wrap gap-2">
                {f.severity === "warning" && f.orgId === myOrg && (
                  <button className="btn btn-primary" disabled={reason.length < 5 || close.isPending} onClick={() => close.mutate("resolved")}>
                    {close.isPending ? <Loader2 size={14} className="animate-spin" /> : <CheckCircle2 size={14} />} Mark resolved
                  </button>
                )}
                {canAccept && (
                  <button className="btn btn-accent" disabled={reason.length < 5 || close.isPending} onClick={() => close.mutate("accepted")}>
                    <ShieldCheck size={14} /> Accept with signed reason
                  </button>
                )}
              </div>
              <p className="text-[12px] text-muted">
                {f.severity === "blocker"
                  ? "Blockers are fixed by submitting a corrected version of the record, or accepted by an auditor, road owner or the operator. An acceptance is itself a signed record."
                  : "The record owner can resolve a warning with an explanation."}
              </p>
            </div>
          )}
        </div>
      )}
    </Drawer>
  );
}
