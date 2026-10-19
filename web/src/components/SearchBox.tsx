import { forwardRef, useEffect, useId, useImperativeHandle, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Search, ArrowRight, Building2, Loader2, X, CornerDownLeft } from "lucide-react";
import clsx from "clsx";
import { api, qs } from "../lib/api";
import { useDirectory } from "../lib/hooks";
import type { RecordT } from "../lib/types";
import { TYPE, KIND } from "../lib/vocab";
import { date, mass } from "../lib/format";
import { TypeBadge } from "./ui";

const PASSPORT_ID = /^[A-Z]{3}-\d{2}-\d{6}$/;

export interface SearchBoxHandle { focus: () => void }

/**
 * Inline search in the header: type a passport id, your own reference or a
 * company; results drop down under the field. ↑/↓ choose, Enter opens (the first
 * result by default, else the full passport search), Esc clears.
 */
export const SearchBox = forwardRef<SearchBoxHandle, { className?: string }>(function SearchBox({ className }, handle) {
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [sel, setSel] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const box = useRef<HTMLDivElement>(null);
  const listId = useId();
  const nav = useNavigate();
  const dir = useDirectory();
  useImperativeHandle(handle, () => ({ focus: () => input.current?.focus() }), []);

  const term = useDebounced(q.trim(), 160);
  const recs = useQuery({
    queryKey: ["search", term],
    queryFn: ({ signal }) => api.get<{ items: RecordT[] }>(`/records${qs({ q: term, limit: 6, type: "road_section,asphalt_lot,additive_batch,recycled_lot,reject_lot,delivery,shipment,lab_test" })}`, signal),
    enabled: term.length >= 2,
    staleTime: 10_000,
  });
  const orgs = useMemo(
    () => (term.length < 2 ? [] : dir.orgs.filter((o) => `${o.name} ${o.city ?? ""}`.toLowerCase().includes(term.toLowerCase())).slice(0, 4)),
    [term, dir.orgs],
  );
  const items = useMemo(() => [
    ...(recs.data?.items ?? []).map((r) => ({ key: r.id, href: `/passports/${r.id}`, rec: r })),
    ...orgs.map((o) => ({ key: o.id, href: `/passports?org=${o.id}`, org: o })),
  ], [recs.data, orgs]);
  useEffect(() => setSel(0), [term]);

  // Close when focus or a click lands outside the box.
  useEffect(() => {
    if (!open) return;
    const away = (e: PointerEvent) => { if (!box.current?.contains(e.target as Node)) setOpen(false); };
    document.addEventListener("pointerdown", away);
    return () => document.removeEventListener("pointerdown", away);
  }, [open]);

  const go = (href: string) => {
    nav(href);
    setOpen(false);
    setQ("");
    input.current?.blur();
  };
  const submit = () => {
    const t = q.trim();
    if (items[sel] && term === t) return go(items[sel].href);
    if (PASSPORT_ID.test(t.toUpperCase())) return go(`/passports/${t.toUpperCase()}`);
    if (t.length >= 2) go(allResults(t));
  };
  // "All results" opens the tab of the best match, not the one the company happens to start on.
  const allResults = (t: string) => `/passports${qs({ q: t, type: term === t ? recs.data?.items?.[0]?.type : undefined })}`;
  const showList = open && q.trim().length >= 2;
  const busy = recs.isFetching || term !== q.trim();

  return (
    <div ref={box} className={clsx("relative", className)}>
      <div className="flex h-9 items-center gap-2 rounded-full bg-surface-3 px-3 text-[13px] ring-accent/40 transition focus-within:bg-surface focus-within:ring-2">
        <Search size={15} className="shrink-0 text-muted" aria-hidden />
        <input
          ref={input}
          role="combobox"
          aria-expanded={showList}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={showList && items[sel] ? `${listId}-${sel}` : undefined}
          aria-label="Search passports and companies"
          value={q}
          onChange={(e) => { setQ(e.target.value); setOpen(true); }}
          onFocus={() => setOpen(true)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") { e.preventDefault(); setOpen(true); setSel((s) => Math.min(items.length - 1, s + 1)); }
            else if (e.key === "ArrowUp") { e.preventDefault(); setSel((s) => Math.max(0, s - 1)); }
            else if (e.key === "Enter") { e.preventDefault(); submit(); }
            else if (e.key === "Escape") { if (q) setQ(""); else input.current?.blur(); setOpen(false); }
          }}
          placeholder="Find a passport, road or company"
          className="h-full min-w-0 flex-1 bg-transparent placeholder:text-muted"
          style={{ outline: "none" }}
        />
        {q ? (
          busy && q.trim().length >= 2 ? <Loader2 size={14} className="animate-spin text-muted" aria-hidden /> : (
            <button type="button" className="text-muted hover:text-text" aria-label="Clear search" onClick={() => { setQ(""); input.current?.focus(); }}><X size={14} /></button>
          )
        ) : (
          <kbd className="hidden rounded-md bg-surface px-1.5 text-[11px] font-semibold text-muted sm:block">⌘K</kbd>
        )}
      </div>

      {showList && (
        <div className="absolute left-0 right-0 top-[calc(100%+6px)] z-50 min-w-[320px] overflow-hidden rounded-2xl bg-[var(--bg-elevated)] shadow-[var(--shadow-pop)]" style={{ animation: "rise .14s ease-out" }}>
          <ul id={listId} role="listbox" className="max-h-[380px] overflow-y-auto p-1.5">
            {!busy && !items.length && <li className="px-3 py-5 text-center text-[13px] text-muted">Nothing found for “{q.trim()}”.</li>}
            {items.map((it, i) => (
              <li key={it.key} id={`${listId}-${i}`} role="option" aria-selected={i === sel}>
                <button
                  type="button"
                  tabIndex={-1}
                  className={clsx("flex w-full items-center gap-3 rounded-xl px-2.5 py-2 text-left", i === sel && "bg-surface-3")}
                  onMouseEnter={() => setSel(i)}
                  onClick={() => go(it.href)}
                >
                  {"rec" in it && it.rec ? (
                    <>
                      <TypeBadge type={it.rec.type} />
                      <span className="min-w-0 flex-1">
                        <span className="mono block text-[13px] font-semibold">{it.rec.id}{it.rec.ref ? <span className="ml-2 font-sans font-normal text-muted">{it.rec.ref}</span> : null}</span>
                        <span className="block truncate text-[12px] text-muted">
                          {TYPE[it.rec.type].name} · {dir.orgName(it.rec.orgId)} · {date(it.rec.occurredAt)} · {mass(it.rec.massKg)}
                        </span>
                      </span>
                    </>
                  ) : "org" in it && it.org ? (
                    <>
                      <span className="grid size-[26px] place-items-center rounded-[9px] bg-surface-3"><Building2 size={14} /></span>
                      <span className="min-w-0 flex-1">
                        <span className="block text-[13px] font-semibold">{it.org.name}</span>
                        <span className="block text-[12px] text-muted">{KIND[it.org.kind].name}{it.org.city ? ` · ${it.org.city}` : ""}</span>
                      </span>
                    </>
                  ) : null}
                  {i === sel ? <CornerDownLeft size={13} className="text-muted" aria-hidden /> : <ArrowRight size={13} className="text-muted opacity-0" aria-hidden />}
                </button>
              </li>
            ))}
          </ul>
          <div className="flex items-center justify-between border-t border-line px-3 py-2 text-[11.5px] text-muted">
            <span>↑↓ to choose · Enter to open · Esc to close</span>
            <button type="button" className="font-semibold text-accent-ink hover:underline" onClick={() => go(allResults(q.trim()))}>All results</button>
          </div>
        </div>
      )}
    </div>
  );
});

function useDebounced<T>(v: T, ms: number): T {
  const [d, setD] = useState(v);
  useEffect(() => {
    const t = setTimeout(() => setD(v), ms);
    return () => clearTimeout(t);
  }, [v, ms]);
  return d;
}
