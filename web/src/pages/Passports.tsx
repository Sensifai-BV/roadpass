import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Search, ScrollText } from "lucide-react";
import clsx from "clsx";
import { api, qs } from "../lib/api";
import { useDirectory, useMe } from "../lib/hooks";
import type { RecordT, RecordType, Status } from "../lib/types";
import { homeType, TYPE } from "../lib/vocab";
import { date, mass, num } from "../lib/format";
import { Download } from "../components/Download";
import { Card, Empty, PageHeader, SealChip, StatusChip, TypeBadge, Skel } from "../components/ui";

const TABS: RecordType[] = ["road_section", "asphalt_lot", "additive_batch", "recycled_lot", "reject_lot", "shipment", "delivery", "lab_test"];

/** Type-specific columns: the two facts a person scans for. */
function detail(r: RecordT): [string, string] {
  const d = r.data as Record<string, string | number>;
  switch (r.type) {
    case "road_section":
      return [`${d.road} km ${num(Number(d.km_from), 1)}–${num(Number(d.km_to), 1)}`, `${d.layer} · ${d.mix}`];
    case "asphalt_lot":
      return [String(d.mix), `${num(Number(d.dosage_pct_mix), 2)} % additive · ${d.process}`];
    case "additive_batch":
      return [String(d.product), `${num(Number(d.recycled_content_pct), 1)} % recycled`];
    case "recycled_lot":
      return [String(d.form), d.mfi_g10min !== undefined ? `MFI ${num(Number(d.mfi_g10min), 2)}` : "—"];
    case "reject_lot":
      return [`${num(Number(d.ldpe_share_pct), 0)} % film`, `avoids ${d.avoided_route}`];
    case "shipment":
      return [`${num(Number(d.distance_km), 0)} km`, String(d.carrier ?? "—")];
    case "delivery":
      return [String(d.ticket), `${d.arrival_temp_c ?? "—"} °C on site`];
    case "lab_test":
      return [String(d.test), `${d.value} · ${d.result}`];
    default:
      return ["", ""];
  }
}

export default function Passports() {
  const [params, setParams] = useSearchParams();
  const me = useMe().data!;
  const type = (params.get("type") as RecordType) || homeType(me.org.kind);
  const status = (params.get("status") as Status | null) ?? "";
  const org = params.get("org") ?? "";
  const [q, setQ] = useState(params.get("q") ?? "");
  const [term, setTerm] = useState(q);
  useEffect(() => {
    const t = setTimeout(() => setTerm(q.trim()), 250);
    return () => clearTimeout(t);
  }, [q]);
  const dir = useDirectory();
  const nav = useNavigate();
  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params);
    if (v) p.set(k, v);
    else p.delete(k);
    setParams(p, { replace: true });
  };

  const query = useInfiniteQuery({
    queryKey: ["records", type, status, org, term],
    queryFn: ({ pageParam, signal }) =>
      api.get<{ items: RecordT[]; next: string }>(`/records${qs({ type, status, org, q: term, cursor: pageParam, limit: 100 })}`, signal),
    initialPageParam: "",
    getNextPageParam: (last) => last.next || undefined,
  });
  const rows = useMemo(() => query.data?.pages.flatMap((p) => p.items) ?? [], [query.data]);

  const scroller = useRef<HTMLDivElement>(null);
  const v = useVirtualizer({ count: rows.length, getScrollElement: () => scroller.current, estimateSize: () => 56, overscan: 12 });
  const items = v.getVirtualItems();
  useEffect(() => {
    const last = items[items.length - 1];
    if (last && last.index >= rows.length - 20 && query.hasNextPage && !query.isFetchingNextPage) void query.fetchNextPage();
  }, [items, rows.length, query]);

  const passport = TYPE[type].passport;
  const cols = "grid-cols-[minmax(0,1.3fr)_minmax(0,1.6fr)_minmax(0,1.2fr)_110px_100px_120px]";

  return (
    <div>
      <PageHeader
        title="Passports"
        sub={me.oversight ? "Every traceable lot and road section in the chain." : "Lots and road sections your organisation created, shipped or received. Open one to see its full journey."}
        actions={<Download kind="passports" range={false} params={{ type, status, org, q }} label="Download list" />}
      />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="seg flex-wrap" role="tablist" aria-label="Record type">
          {TABS.map((t) => (
            <button key={t} type="button" role="tab" aria-pressed={t === type} onClick={() => set("type", t)}>{TYPE[t].plural}</button>
          ))}
        </div>
      </div>
      <Card pad={false}>
        <div className="flex flex-wrap items-center gap-2 border-b border-line px-4 py-3">
          <div className="relative w-full max-w-[320px]">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted" aria-hidden />
            <input className="input pl-8" placeholder="Id or your reference" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search" />
          </div>
          <select className="input w-auto" value={status} onChange={(e) => set("status", e.target.value)} aria-label="Status">
            <option value="">Any status</option>
            <option value="ok">Checked</option>
            <option value="flagged">Warning</option>
            <option value="blocked">Blocked</option>
            <option value="accepted">Accepted</option>
          </select>
          {me.oversight && (
            <select className="input w-auto max-w-[260px]" value={org} onChange={(e) => set("org", e.target.value)} aria-label="Organisation">
              <option value="">Every organisation</option>
              {dir.orgs.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
            </select>
          )}
          <span className="ml-auto text-[12px] text-muted">{query.isFetching ? "Loading…" : `${num(rows.length)}${query.hasNextPage ? "+" : ""} shown`}</span>
        </div>
        <div className={clsx("grid gap-3 border-b border-line px-4 py-2 text-[11.5px] font-semibold text-muted", cols)}>
          <span>Passport</span><span>Detail</span><span>Organisation</span><span className="text-right">Mass</span><span>Status</span><span>Ledger</span>
        </div>
        <div ref={scroller} className="scroll-thin h-[calc(100vh-330px)] min-h-[360px] overflow-y-auto">
          {query.isLoading && <div className="flex flex-col gap-2 p-4">{[...Array(8)].map((_, i) => <Skel key={i} h={40} />)}</div>}
          {!query.isLoading && rows.length === 0 && (
            <Empty icon={<ScrollText size={20} />} title={`No ${TYPE[type].plural.toLowerCase()} yet`}>
              Records you create, ship or receive appear here. Upload them under Data in.
            </Empty>
          )}
          <div style={{ height: v.getTotalSize(), position: "relative" }}>
            {items.map((it) => {
              const r = rows[it.index];
              const [a, b] = detail(r);
              const target = passport ? r.id : r.type === "delivery" ? String((r.data as { section_id?: string }).section_id) : r.inputs?.[0]?.id ?? (r.data as { subject_id?: string }).subject_id;
              return (
                <div
                  key={r.id}
                  role="link"
                  tabIndex={0}
                  onClick={() => target && nav(`/passports/${target}`)}
                  onKeyDown={(e) => e.key === "Enter" && target && nav(`/passports/${target}`)}
                  className={clsx("absolute inset-x-0 grid cursor-pointer items-center gap-3 border-b border-line px-4 hover:bg-surface-2", cols)}
                  style={{ top: it.start, height: it.size }}
                >
                  <span className="flex min-w-0 items-center gap-2.5">
                    <TypeBadge type={r.type} size={24} />
                    <span className="min-w-0">
                      <span className="mono block truncate font-semibold text-text">{r.id}</span>
                      <span className="block truncate text-[11.5px] text-muted">{r.ref} · {date(r.occurredAt)}</span>
                    </span>
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate text-[13px]">{a}</span>
                    <span className="block truncate text-[11.5px] text-muted">{b}</span>
                  </span>
                  <span className="truncate text-[13px] text-text-2">{dir.orgName(r.orgId)}</span>
                  <span className="num text-right text-[13px] font-medium">{r.massKg ? mass(r.massKg) : "—"}</span>
                  <span><StatusChip status={r.status} /></span>
                  <span><SealChip sealed={r.sealed} pending /></span>
                </div>
              );
            })}
          </div>
        </div>
      </Card>
    </div>
  );
}
