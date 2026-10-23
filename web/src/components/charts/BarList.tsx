import type { ReactNode } from "react";

/**
 * Horizontal bars with the label above and the value at the end: the clearest
 * form for ranked magnitudes (regions, origins, mixes). One hue; the value is
 * printed, so colour never carries the number alone.
 */
export function BarList({ items, color = "var(--s1)", format, max: maxValue, onSelect }: {
  items: { key: string; label: ReactNode; value: number; sub?: ReactNode; color?: string }[];
  color?: string;
  format: (v: number) => string;
  max?: number;
  onSelect?: (key: string) => void;
}) {
  const m = maxValue ?? Math.max(1, ...items.map((i) => i.value));
  return (
    <ul className="flex flex-col gap-2.5">
      {items.map((it) => (
        <li key={it.key}>
          <button
            type="button"
            className="group block w-full text-left"
            onClick={onSelect ? () => onSelect(it.key) : undefined}
            disabled={!onSelect}
          >
            <div className="mb-1 flex items-baseline justify-between gap-3 text-[13px]">
              <span className="min-w-0 truncate text-text group-enabled:group-hover:text-accent-ink">{it.label}</span>
              <span className="num shrink-0 font-semibold">{format(it.value)}</span>
            </div>
            <div className="h-[7px] w-full overflow-hidden rounded-full bg-surface-3">
              <div
                className="h-full rounded-full transition-[width] duration-500"
                style={{ width: `${Math.max(1.5, (it.value / m) * 100)}%`, background: it.color ?? color }}
              />
            </div>
            {it.sub && <div className="mt-1 text-[11.5px] text-muted">{it.sub}</div>}
          </button>
        </li>
      ))}
    </ul>
  );
}
