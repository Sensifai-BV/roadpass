import { Fragment, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import * as Dialog from "@radix-ui/react-dialog";
import * as Tip from "@radix-ui/react-tooltip";
import * as Pop from "@radix-ui/react-popover";
import clsx from "clsx";
import {
  ArrowDownRight, ArrowUpRight, Check, CircleCheck, CircleAlert, OctagonX, BadgeCheck, Copy, X, Link2, ShieldCheck,
  ShieldAlert, Clock3, Info,
} from "lucide-react";
import type { Status } from "../lib/types";
import { STATUS, TYPE } from "../lib/vocab";
import type { RecordType } from "../lib/types";
import { shortHash } from "../lib/format";

export function Card({ children, className, title, sub, action, pad = true, help }: {
  children: ReactNode; className?: string; title?: ReactNode; sub?: ReactNode; action?: ReactNode; pad?: boolean; help?: ReactNode;
}) {
  return (
    <section className={clsx("card rise", className)}>
      {(title || action) && (
        <header className="flex flex-wrap items-start justify-between gap-x-3 gap-y-2 px-[22px] pt-[18px]">
          {/* A wide action (a legend, filters) wraps below the title rather than squeezing it. */}
          <div className="min-w-[min(100%,240px)] flex-1">
            {title && <div className="flex items-center gap-1.5"><h2 className="card-title">{title}</h2>{help && <InfoTip text={help} />}</div>}
            {sub && <p className="card-sub mt-0.5">{sub}</p>}
          </div>
          {action && <div className="flex shrink-0 items-center gap-2">{action}</div>}
        </header>
      )}
      <div className={clsx(pad && "card-pad", title && pad && "pt-3")}>{children}</div>
    </section>
  );
}

export function PageHeader({ title, sub, actions, eyebrow, help }: { title: ReactNode; sub?: ReactNode; actions?: ReactNode; eyebrow?: ReactNode; help?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0">
        {eyebrow && <div className="eyebrow mb-1.5">{eyebrow}</div>}
        <div className="flex items-center gap-2"><h1 className="h1">{title}</h1>{help && <InfoTip text={help} size={16} />}</div>
        {sub && <p className="mt-1.5 max-w-3xl text-[14px] text-text-2">{sub}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

export function Delta({ value, invert = false }: { value: number | null; invert?: boolean }) {
  if (value === null || !Number.isFinite(value)) return null;
  const up = value >= 0;
  const good = invert ? !up : up;
  const Icon = up ? ArrowUpRight : ArrowDownRight;
  return (
    <span className={clsx("pill", Math.abs(value) < 0.5 ? "pill-flat" : good ? "pill-up" : "pill-down")}>
      <Icon size={11} strokeWidth={2.6} aria-hidden />
      {Math.abs(value) >= 100 ? Math.round(Math.abs(value)) : Math.abs(value).toFixed(Math.abs(value) < 10 ? 1 : 0)}%
      <span className="sr-only">{up ? "up" : "down"} on the previous period</span>
    </span>
  );
}

/** A metric card: label left, delta pill right, value, then one line of context. */
export function Stat({ label, value, unit, delta, hint, icon, color, loading, children, help }: {
  label: string; value: ReactNode; unit?: string; delta?: number | null; hint?: ReactNode; icon?: ReactNode;
  color?: string; loading?: boolean; children?: ReactNode; help?: ReactNode;
}) {
  return (
    <div className="card card-pad rise flex min-h-[118px] flex-col">
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2 text-[12.5px] font-medium text-text-2">
          {icon && <span className="shrink-0" style={{ color: color ?? "var(--text-2)" }}>{icon}</span>}
          <span className="truncate">{label}</span>
          {help && <InfoTip text={help} />}
        </div>
        {delta !== undefined && <Delta value={delta} />}
      </div>
      {loading ? (
        <div className="skeleton mt-3 h-8 w-28" />
      ) : (
        <div className="mt-2.5 flex items-baseline gap-1.5">
          <span className="hero-num">{value}</span>
          {unit && <span className="text-[14px] font-semibold text-text-2">{unit}</span>}
        </div>
      )}
      {hint && <div className="mt-auto truncate pt-1.5 text-[12px] text-muted">{hint}</div>}
      {children}
    </div>
  );
}

export function StatusChip({ status, withIcon = true }: { status: Status; withIcon?: boolean }) {
  const s = STATUS[status];
  const Icon = status === "ok" ? CircleCheck : status === "flagged" ? CircleAlert : status === "blocked" ? OctagonX : BadgeCheck;
  return (
    <Hint text={s.explain}>
      <span className={clsx("chip", s.chip)}>
        {withIcon && <Icon size={12} strokeWidth={2.5} aria-hidden />}
        {s.label}
      </span>
    </Hint>
  );
}

export function SealChip({ sealed, pending }: { sealed: boolean; pending?: boolean }) {
  if (sealed)
    return (
      <Hint text="Sealed: the record's fingerprint is part of a batch whose Merkle root is written on the ledger. Any later change to the record would no longer match it.">
        <span className="chip chip-good"><ShieldCheck size={12} strokeWidth={2.5} aria-hidden />Sealed</span>
      </Hint>
    );
  return (
    <Hint text={pending ? "Waiting for the next seal: the worker seals new records in batches every few seconds once the company is verified." : "Not sealed: records of companies the platform has not verified yet, or blocked records, are not sealed."}>
      <span className="chip"><Clock3 size={12} strokeWidth={2.5} aria-hidden />{pending ? "Awaiting seal" : "Not sealed"}</span>
    </Hint>
  );
}

export function SignedChip({ valid }: { valid: boolean }) {
  return valid ? (
    <Hint text="Signed with the company's Ed25519 key, and with the personal key of the member who entered it. Your browser checks these signatures itself.">
      <span className="chip chip-good"><Check size={12} strokeWidth={3} aria-hidden />Signed</span>
    </Hint>
  ) : (
    <Hint text="The signature does not match the record's content or key. The record may have been altered after signing.">
      <span className="chip chip-critical"><ShieldAlert size={12} strokeWidth={2.5} aria-hidden />Signature invalid</span>
    </Hint>
  );
}

export function TypeBadge({ type, size = 26 }: { type: RecordType; size?: number }) {
  const t = TYPE[type];
  const Icon = t.icon;
  return (
    <span className="grid shrink-0 place-items-center rounded-[9px]" style={{ width: size, height: size, background: `color-mix(in srgb, ${t.color} 15%, transparent)`, color: t.color }}>
      <Icon size={size * 0.55} strokeWidth={2.2} aria-hidden />
    </span>
  );
}

export function RecordLink({ id, type, className }: { id: string; type?: RecordType; className?: string }) {
  const passport = !type || TYPE[type]?.passport;
  if (!passport) return <span className={clsx("mono", className)}>{id}</span>;
  return (
    <Link to={`/passports/${id}`} className={clsx("mono font-medium text-accent-ink hover:underline", className)}>
      {id}
    </Link>
  );
}

export function Hint({ text, children }: { text: ReactNode; children: ReactNode }) {
  return (
    <Tip.Root delayDuration={250}>
      <Tip.Trigger asChild>{children}</Tip.Trigger>
      <Tip.Portal>
        <Tip.Content side="top" sideOffset={6} className="tooltip z-50 max-w-[300px] leading-snug">
          {text}
        </Tip.Content>
      </Tip.Portal>
    </Tip.Root>
  );
}

export function Copy_({ text, label }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className="inline-flex items-center gap-1 rounded-md px-1 text-muted hover:text-text"
      onClick={() => {
        void navigator.clipboard.writeText(text).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1400);
        });
      }}
      aria-label={label ?? "Copy"}
    >
      {done ? <Check size={13} className="text-good" /> : <Copy size={13} />}
    </button>
  );
}

export function Hash({ value, n = 10 }: { value?: string; n?: number }) {
  if (!value) return <span className="text-muted">—</span>;
  return (
    <span className="inline-flex items-center gap-1">
      <span className="mono text-text-2" title={value}>{shortHash(value, n)}</span>
      <Copy_ text={value} label="Copy fingerprint" />
    </span>
  );
}

export function Empty({ icon, title, children }: { icon?: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="grid place-items-center px-6 py-14 text-center">
      {icon && <div className="mb-3 grid size-12 place-items-center rounded-2xl bg-surface-3 text-muted">{icon}</div>}
      <div className="text-[15px] font-semibold">{title}</div>
      {children && <div className="mt-1 max-w-md text-[13px] text-text-2">{children}</div>}
    </div>
  );
}

export function ErrorBox({ error }: { error: unknown }) {
  const msg = error instanceof Error ? error.message : String(error);
  return (
    <div className="flex items-start gap-2 rounded-xl bg-[var(--critical-soft)] px-3.5 py-2.5 text-[13px] text-critical" role="alert">
      <OctagonX size={16} className="mt-0.5 shrink-0" aria-hidden />
      <span>{msg}</span>
    </div>
  );
}

export function Segmented<T extends string>({ value, options, onChange, label }: {
  value: T; options: { value: T; label: string }[]; onChange: (v: T) => void; label: string;
}) {
  return (
    <div className="seg" role="group" aria-label={label}>
      {options.map((o) => (
        <button key={o.value} type="button" aria-pressed={o.value === value} onClick={() => onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Drawer({ open, onOpenChange, title, sub, children, width = 560 }: {
  open: boolean; onOpenChange: (o: boolean) => void; title: ReactNode; sub?: ReactNode; children: ReactNode; width?: number;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/25 backdrop-blur-[2px]" />
        <Dialog.Content
          className="fixed inset-y-2 right-2 z-50 flex flex-col overflow-hidden rounded-[22px] bg-[var(--bg-elevated)] shadow-[var(--shadow-pop)] outline-none"
          style={{ width: `min(${width}px, calc(100vw - 16px))`, animation: "rise .25s cubic-bezier(.2,.7,.2,1)" }}
        >
          <header className="flex items-start justify-between gap-3 border-b border-line px-6 py-4">
            <div className="min-w-0">
              <Dialog.Title className="text-[17px] font-semibold tracking-tight">{title}</Dialog.Title>
              {sub ? <Dialog.Description className="card-sub mt-0.5">{sub}</Dialog.Description> : <Dialog.Description className="sr-only">Details</Dialog.Description>}
            </div>
            <Dialog.Close className="btn btn-ghost btn-icon" aria-label="Close"><X size={16} /></Dialog.Close>
          </header>
          <div className="scroll-thin flex-1 overflow-y-auto px-6 py-5">{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function Modal({ open, onOpenChange, title, sub, children, width = 520 }: {
  open: boolean; onOpenChange: (o: boolean) => void; title: ReactNode; sub?: ReactNode; children: ReactNode; width?: number;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/30 backdrop-blur-[2px]" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 max-h-[calc(100dvh-24px)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-[22px] bg-[var(--bg-elevated)] p-6 shadow-[var(--shadow-pop)] outline-none scroll-thin"
          style={{ width: `min(${width}px, calc(100vw - 24px))` }}>
          <Dialog.Title className="text-[17px] font-semibold tracking-tight">{title}</Dialog.Title>
          {sub ? <Dialog.Description className="card-sub mt-1">{sub}</Dialog.Description> : <Dialog.Description className="sr-only">Dialog</Dialog.Description>}
          <div className="mt-4">{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function KV({ items }: { items: [ReactNode, ReactNode][] }) {
  return (
    <dl className="grid grid-cols-[minmax(110px,max-content)_1fr] gap-x-5 gap-y-2.5 text-[13px]">
      {items.map(([k, v], i) => (
        <div key={i} className="contents">
          <dt className="text-muted">{k}</dt>
          <dd className="min-w-0 break-words">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

export function ExternalHint({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a href={href} className="inline-flex items-center gap-1 text-accent-ink hover:underline" target="_blank" rel="noreferrer">
      <Link2 size={13} /> {children}
    </a>
  );
}

export function Skel({ h = 16, w = "100%", className }: { h?: number; w?: number | string; className?: string }) {
  return <div className={clsx("skeleton", className)} style={{ height: h, width: w }} />;
}

export function Legend({ items }: { items: { label: string; color: string; value?: string }[] }) {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1.5 text-[12px] text-text-2">
      {items.map((it) => (
        <li key={it.label} className="inline-flex items-center gap-1.5">
          <span className="inline-block size-2.5 rounded-[3px]" style={{ background: it.color }} aria-hidden />
          {it.label}
          {it.value && <span className="num font-semibold text-text">{it.value}</span>}
        </li>
      ))}
    </ul>
  );
}

/**
 * A list that names the first few items and folds the rest into "+N more";
 * hovering or clicking that opens the full list, each item linked when it has
 * a page.
 */
export function MoreList({ items, max = 2, className }: { items: { key: string; label: string; to?: string; sub?: string }[]; max?: number; className?: string }) {
  const [open, setOpen] = useState(false);
  const timer = useRef<number>(undefined);
  const hover = (o: boolean) => { window.clearTimeout(timer.current); timer.current = window.setTimeout(() => setOpen(o), o ? 150 : 200); };
  if (!items.length) return <span className={className}>—</span>;
  const shown = items.slice(0, max);
  const rest = items.length - shown.length;
  return (
    <span className={className}>
      {shown.map((it, i) => <Fragment key={it.key}>{i > 0 && ", "}{it.to ? <Link to={it.to} className="hover:text-accent-ink hover:underline">{it.label}</Link> : it.label}</Fragment>)}
      {rest > 0 && (
        <Pop.Root open={open} onOpenChange={setOpen}>
          <Pop.Trigger className="chip ml-1 cursor-pointer align-middle hover:bg-accent-soft hover:text-accent-ink" onPointerEnter={() => hover(true)} onPointerLeave={() => hover(false)}
            aria-label={`Show all ${items.length}`}>+{rest} more</Pop.Trigger>
          <Pop.Portal>
            <Pop.Content side="bottom" align="start" sideOffset={6} onPointerEnter={() => hover(true)} onPointerLeave={() => hover(false)}
              className="z-50 max-h-[320px] w-[280px] overflow-y-auto rounded-xl bg-[var(--bg-elevated)] p-1.5 shadow-[var(--shadow-pop)] scroll-thin">
              <div className="eyebrow px-2 pb-1 pt-1">{items.length} in total</div>
              <ul>{items.map((it) => (
                <li key={it.key}>
                  {it.to ? <Link to={it.to} onClick={() => setOpen(false)} className="block rounded-lg px-2 py-1.5 text-[13px] hover:bg-surface-3">{it.label}{it.sub && <span className="block text-[11.5px] text-muted">{it.sub}</span>}</Link>
                    : <span className="block px-2 py-1.5 text-[13px]">{it.label}{it.sub && <span className="block text-[11.5px] text-muted">{it.sub}</span>}</span>}
                </li>
              ))}</ul>
            </Pop.Content>
          </Pop.Portal>
        </Pop.Root>
      )}
    </span>
  );
}

/** A small "i" that explains the thing next to it, on hover or keyboard focus. */
export function InfoTip({ text, size = 13, className }: { text: ReactNode; size?: number; className?: string }) {
  return (
    <Tip.Root delayDuration={120}>
      <Tip.Trigger asChild>
        <button type="button" aria-label="What is this?" onClick={(e) => e.preventDefault()}
          className={clsx("inline-grid shrink-0 cursor-help place-items-center rounded-full text-muted transition hover:text-accent-ink focus-visible:text-accent-ink", className)}>
          <Info size={size} strokeWidth={2.2} aria-hidden />
        </button>
      </Tip.Trigger>
      <Tip.Portal>
        <Tip.Content side="top" sideOffset={6} collisionPadding={12} className="tooltip z-50 max-w-[320px] text-left font-normal normal-case leading-snug tracking-normal">
          {text}
        </Tip.Content>
      </Tip.Portal>
    </Tip.Root>
  );
}
