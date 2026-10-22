/**
 * A semi-circular gauge: `value` is the filled share (0–100). The printed figure
 * is the share as a percentage, or `display` when the gauge shows another unit.
 */
export function Gauge({ value, label, sub, color = "var(--accent)", size = 180, display }: {
  value: number; label: string; sub?: string; color?: string; size?: number; display?: string;
}) {
  const v = Math.max(0, Math.min(100, value));
  const r = size / 2 - 10;
  const cx = size / 2;
  const cy = size / 2;
  const arc = (to: number) => {
    const a = Math.PI * (1 - to / 100);
    return `M ${cx - r} ${cy} A ${r} ${r} 0 0 1 ${cx + r * Math.cos(a)} ${cy - r * Math.sin(a)}`;
  };
  return (
    <div className="flex flex-col items-center">
      <svg width={size} height={size / 2 + 12} role="img" aria-label={`${label}: ${display ?? `${v.toFixed(1)} %`}`}>
        <path d={arc(100)} fill="none" stroke="var(--surface-3)" strokeWidth={12} strokeLinecap="round" />
        {v > 0 && <path d={arc(v)} fill="none" stroke={color} strokeWidth={12} strokeLinecap="round" style={{ transition: "d .6s" }} />}
        <text x={cx} y={cy - 6} textAnchor="middle" style={{ fill: "var(--text)", fontSize: 26, fontWeight: 650, fontVariantNumeric: "tabular-nums" }}>
          {display ?? `${v >= 99.95 ? "100" : v.toFixed(1)}%`}
        </text>
        <text x={cx} y={cy + 10} textAnchor="middle" style={{ fill: "var(--muted)", fontSize: 11 }}>{label}</text>
      </svg>
      {sub && <div className="-mt-1 text-center text-[11.5px] text-muted">{sub}</div>}
    </div>
  );
}
