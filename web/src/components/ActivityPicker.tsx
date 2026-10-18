import clsx from "clsx";
import { Check, Info } from "lucide-react";
import type { OrgKind } from "../lib/types";
import { KIND, TYPE } from "../lib/vocab";
import { ACTIVITY } from "../lib/help";
import { useMeta } from "../lib/hooks";
import { InfoTip } from "./ui";

export const CHAIN: OrgKind[] = ["sorting_plant", "recycler", "additive_producer", "asphalt_plant", "contractor", "road_owner", "lab"];
const OVERSIGHT: OrgKind[] = ["auditor", "reporter"];

export interface Activities { kind: OrgKind | ""; kinds: OrgKind[] }

/**
 * Picks an organisation's role in the supply chain: one main activity and,
 * for companies that do more than one step, the others. Shows what the choice
 * means: what the company records and which roles its team can have.
 */
export function ActivityPicker({ value, onChange, lockMain = false, oversight = true }: {
  value: Activities; onChange: (v: Activities) => void; lockMain?: boolean; oversight?: boolean;
}) {
  const meta = useMeta().data;
  const main = value.kind;
  const extra = value.kinds.filter((k) => k !== main);
  const pick = (k: OrgKind) => {
    if (OVERSIGHT.includes(k)) { if (!lockMain) onChange({ kind: k, kinds: [] }); return; }
    if (!main || (OVERSIGHT.includes(main as OrgKind) && !lockMain)) { onChange({ kind: k, kinds: [] }); return; }
    if (k === main) return;
    onChange({ kind: main, kinds: extra.includes(k) ? extra.filter((x) => x !== k) : [...extra, k] });
  };
  const all = main ? [main as OrgKind, ...extra] : [];
  const records = (meta?.types ?? []).filter((t) => t.type !== "acceptance" && t.createdBy.some((k) => all.includes(k)));
  // The same access and duties are one role, named after the main activity (as on the server).
  const roles = [...new Map(all.flatMap((k) => meta?.jobRoles?.[k] ?? []).reverse().map((r) => [`${r.access}:${[...(r.duties ?? [])].sort().join(",")}`, r])).values()].reverse();
  const card = (k: OrgKind) => {
    const K = KIND[k];
    const isMain = k === main, isExtra = extra.includes(k);
    const disabled = lockMain && (OVERSIGHT.includes(k) || (OVERSIGHT.includes(main as OrgKind) && k !== main));
    return (
      <button type="button" key={k} disabled={disabled} onClick={() => pick(k)} aria-pressed={isMain || isExtra}
        className={clsx("relative flex items-start gap-2.5 rounded-xl border p-2.5 text-left transition disabled:opacity-40",
          isMain ? "border-accent bg-accent-soft" : isExtra ? "border-accent/50 bg-surface-2" : "border-line hover:bg-surface-2")}>
        <span className="mt-0.5 grid size-7 shrink-0 place-items-center rounded-lg" style={{ background: `color-mix(in srgb, ${K.color} 15%, transparent)`, color: K.color }}><K.icon size={14} aria-hidden /></span>
        <span className="min-w-0">
          <span className="flex items-center gap-1.5 text-[13px] font-semibold">{K.name}
            {isMain && <span className="chip chip-accent px-1.5 py-0 text-[10.5px]">Main</span>}
            {isExtra && <span className="chip px-1.5 py-0 text-[10.5px]"><Check size={10} />Also</span>}
          </span>
          <span className="mt-0.5 block text-[11.5px] leading-snug text-text-2">{ACTIVITY[k]}</span>
        </span>
      </button>
    );
  };
  return (
    <div className="flex flex-col gap-2.5">
      <p className="flex items-start gap-1.5 text-[12.5px] text-text-2">
        <Info size={13} className="mt-0.5 shrink-0 text-muted" aria-hidden />
        <span>This decides what the company records, which roles its team can have, and where it appears in the chain. {lockMain
          ? "The main activity was set by the platform operator; add any other step you also do."
          : "Pick the main activity first. If the company also does other steps (an asphalt plant that also lays roads), click those too."}</span>
      </p>
      <div className="grid gap-1.5 sm:grid-cols-2">{CHAIN.map(card)}</div>
      {oversight && (
        <>
          <div className="eyebrow mt-1 flex items-center gap-1">Oversight, read-only <InfoTip text="Oversight bodies see the whole chain but never enter records. They cannot be combined with a chain activity." /></div>
          <div className="grid gap-1.5 sm:grid-cols-2">{OVERSIGHT.map(card)}</div>
        </>
      )}
      {main && (
        <div className="rounded-xl bg-surface-2 p-3 text-[12.5px]">
          <div className="font-semibold">What this means</div>
          <div className="mt-1.5 grid gap-2 sm:grid-cols-2">
            <div><div className="text-muted">They will record</div>
              <div className="mt-0.5 text-text-2">{records.length ? records.map((t) => TYPE[t.type].plural).join(", ") : "Nothing: read-only"}</div></div>
            <div><div className="text-muted">Their team can be</div>
              <div className="mt-0.5 text-text-2">{roles.map((r) => r.name).join(", ")}</div></div>
          </div>
        </div>
      )}
    </div>
  );
}
