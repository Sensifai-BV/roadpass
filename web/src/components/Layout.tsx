import { useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import * as DM from "@radix-ui/react-dropdown-menu";
import * as Dialog from "@radix-ui/react-dialog";
import * as Tip from "@radix-ui/react-tooltip";
import clsx from "clsx";
import {
  LayoutGrid, Workflow, ScrollText, Map as MapIcon, ShieldCheck, Leaf, Gauge, Blocks, UploadCloud, Building2,
  UserCog, BookOpen, Sun, Moon, Monitor, LogOut, Menu, X, ChevronDown, Inbox, Activity, PanelLeftClose, PanelLeftOpen, Settings2, Siren, Landmark, Rocket, UserX, Users, CreditCard, Server, History, CalendarDays,
  Mail, MessageSquare,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { Work as WorkT } from "../lib/types";
import { api } from "../lib/api";
import { RangeContext, useLive, useMe, useRangeState, useTheme, type RangeKey, type Theme } from "../lib/hooks";
import { KIND } from "../lib/vocab";
import { NAV_HELP } from "../lib/help";
import { timeZone } from "../lib/format";
import { Segmented, Hint } from "./ui";
import { SearchBox, type SearchBoxHandle } from "./SearchBox";
import { NotificationBell } from "./NotificationBell";
import { canSee, type Area } from "../lib/access";

const NAV: { to: string; label: string; icon: typeof LayoutGrid; section?: string; operator?: boolean; duties?: boolean; oversight?: boolean; area?: Area }[] = [
  { to: "/", label: "Overview", icon: LayoutGrid },
  { to: "/work", label: "My work", icon: Inbox, duties: true },
  { to: "/status", label: "Chain status", icon: Activity },
  { to: "/calendar", label: "Calendar", icon: CalendarDays },
  { to: "/chain", label: "Supply chain", icon: Workflow },
  { to: "/passports", label: "Passports", icon: ScrollText },
  { to: "/roads", label: "Roads", icon: MapIcon },
  { to: "/quality", label: "Data checks", icon: ShieldCheck, section: "Assurance" },
  { to: "/alerts", label: "Alerts", icon: Siren, area: "alerts" },
  { to: "/compliance", label: "Compliance", icon: Landmark, area: "compliance" },
  { to: "/carbon", label: "Carbon & impact", icon: Leaf },
  { to: "/plants", label: "Plant data", icon: Gauge },
  { to: "/ledger", label: "Ledger", icon: Blocks, area: "ledger" },
  { to: "/data", label: "Data in", icon: UploadCloud, section: "Workspace", area: "data" },
  { to: "/settings", label: "Settings", icon: Settings2 },
  { to: "/platform", label: "Platform console", icon: UserCog, operator: true },
  { to: "/glossary", label: "Glossary", icon: BookOpen },
];

/** The platform manager runs the service, not a step of the chain. */
const PLATFORM_NAV: typeof NAV = [
  { to: "/", label: "Platform overview", icon: LayoutGrid },
  { to: "/platform/organisations", label: "Organisations", icon: Building2 },
  { to: "/platform/users", label: "Users", icon: Users },
  { to: "/platform/billing", label: "Billing", icon: CreditCard },
  { to: "/platform/messages", label: "Messages", icon: MessageSquare },
  { to: "/platform/email", label: "Email delivery", icon: Mail },
  { to: "/ledger", label: "Ledger", icon: Blocks, section: "Operations" },
  { to: "/alerts", label: "Alerts", icon: Siren },
  { to: "/platform/system", label: "System health", icon: Server },
  { to: "/platform/audit", label: "Audit log", icon: History },
  { to: "/platform/privacy", label: "Data protection", icon: UserX },
  { to: "/settings", label: "Settings", icon: Settings2, section: "Account" },
];

function Logo({ compact }: { compact?: boolean }) {
  return (
    <div className={clsx("flex items-center gap-2.5", compact ? "justify-center" : "px-2")}>
      <svg width="28" height="28" viewBox="0 0 64 64" aria-hidden>
        <rect width="64" height="64" rx="15" fill="var(--text)" />
        <path d="M18 46 L28 18 h8 L46 46" fill="none" stroke="var(--bg)" strokeWidth="5" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M32 24v5m0 6v5" stroke="var(--brand)" strokeWidth="4" strokeLinecap="round" />
      </svg>
      <div className={clsx("leading-tight", compact && "sr-only")}>
        <div className="text-[15px] font-bold tracking-tight">ROADPASS</div>
        <div className="text-[10.5px] font-medium text-muted">Recycled plastic in roads</div>
      </div>
    </div>
  );
}

/** Main navigation: full labels, or a compact icon rail with tooltips. */
function Sidebar({ operator, compact = false, onNavigate }: { operator: boolean; compact?: boolean; onNavigate?: () => void }) {
  const me = useMe().data;
  const work = useQuery({ queryKey: ["work"], queryFn: () => api.get<WorkT>("/work"), enabled: !!me?.duties.length, refetchInterval: 60_000 });
  const pending = work.data ? Object.values(work.data.queues).reduce((s, q) => s + q.total, 0) : 0;
  const show = (n: (typeof NAV)[number]) => (!n.operator || operator) && (!n.duties || !!me?.duties.length) && (!n.oversight || !!me?.oversight)
    && (!n.area || canSee(n.area, me));
  const setup = useQuery({ queryKey: ["org-setup"], queryFn: () => api.get<{ done: number; required: number; dismissed: boolean }>("/org/setup"),
    enabled: me?.user.role === "admin" && !operator, staleTime: 60_000 });
  // Open messages from the public contact form, for the platform manager's nav.
  const inbox = useQuery({ queryKey: ["platform-contact"], queryFn: () => api.get<{ handledAt?: string }[]>("/platform/contact"),
    enabled: operator && me?.user.role === "admin", refetchInterval: 60_000 });
  const openMessages = inbox.data?.filter((m) => !m.handledAt).length ?? 0;
  const setupOpen = !!setup.data && !setup.data.dismissed && setup.data.done < setup.data.required;
  const items = operator && me?.user.role === "admin" ? PLATFORM_NAV
    : [...(setupOpen ? [{ to: "/get-started", label: "Get started", icon: Rocket }] : []), ...NAV.filter(show)];
  return (
    <nav className={clsx("flex h-full flex-col gap-0.5 pb-4 pt-5", compact ? "items-center px-2" : "px-3")} aria-label="Main">
      <Logo compact={compact} />
      <div className="h-5" />
      {items.map((n) => {
        const link = (
          <NavLink
            to={n.to}
            end={n.to === "/"}
            onClick={onNavigate}
            aria-label={compact ? n.label : undefined}
            className={({ isActive }) =>
              clsx(
                "relative flex h-[34px] items-center rounded-[9px] text-[13.5px] font-medium transition-colors",
                compact ? "w-10 justify-center" : "gap-2.5 px-2.5",
                isActive ? "bg-surface-3 text-text" : "text-text-2 hover:bg-[var(--hover)]",
              )
            }
          >
            {({ isActive }) => (
              <>
                <n.icon size={compact ? 18 : 17} strokeWidth={isActive ? 2.3 : 2} className={isActive ? "text-accent" : ""} aria-hidden />
                {!compact && n.label}
                {n.to === "/get-started" && setup.data && !compact && <span className="num ml-auto text-[11px] font-semibold text-muted">{setup.data.done}/{setup.data.required}</span>}
                {n.to === "/platform/messages" && openMessages > 0 && (compact
                  ? <span className="absolute right-1 top-1 size-2 rounded-full bg-accent ring-2 ring-[var(--sidebar)]" aria-label={`${openMessages} open`} />
                  : <span className="num ml-auto rounded-full bg-accent px-1.5 text-[11px] font-semibold leading-[18px] text-[var(--on-accent)]">{openMessages > 99 ? "99+" : openMessages}</span>)}
                {n.duties && pending > 0 && (compact
                  ? <span className="absolute right-1 top-1 size-2 rounded-full bg-accent ring-2 ring-[var(--sidebar)]" aria-label={`${pending} waiting`} />
                  : <span className="num ml-auto rounded-full bg-accent px-1.5 text-[11px] font-semibold leading-[18px] text-[var(--on-accent)]">{pending > 99 ? "99+" : pending}</span>)}
              </>
            )}
          </NavLink>
        );
        return (
          <div key={n.to} className={compact ? "flex w-full flex-col items-center" : undefined}>
            {n.section && (compact ? <div className="mx-auto my-3 h-px w-6 bg-line" aria-hidden /> : <div className="eyebrow mb-1.5 mt-5 px-2.5">{n.section}</div>)}
            <Tip.Root delayDuration={compact ? 80 : 600}>
              {/* A wrapper, not asChild: NavLink's className is a function a Slot cannot merge. */}
              <Tip.Trigger asChild><div className={compact ? "flex w-full justify-center" : undefined}>{link}</div></Tip.Trigger>
              <Tip.Portal>
                <Tip.Content side="right" sideOffset={10} className="tooltip z-50 max-w-[260px] leading-snug">
                  {compact && <div className="font-semibold">{n.label}{n.duties && pending > 0 ? ` · ${pending} waiting` : ""}</div>}
                  {NAV_HELP[n.to] && <div className={compact ? "mt-0.5 opacity-80" : ""}>{n.to === "/" && items === PLATFORM_NAV ? "Customers, usage, revenue and the platform's health." : NAV_HELP[n.to]}</div>}
                </Tip.Content>
              </Tip.Portal>
            </Tip.Root>
          </div>
        );
      })}
      {!compact && <div className="mt-auto px-2.5 pt-6 text-[11px] leading-snug text-muted">© Sensifai BV</div>}
    </nav>
  );
}

// ---------------------------------------------------------------- responsive state

function useMedia(query: string): boolean {
  return useSyncExternalStore(
    (cb) => {
      const m = window.matchMedia(query);
      m.addEventListener("change", cb);
      return () => m.removeEventListener("change", cb);
    },
    () => window.matchMedia(query).matches,
    () => false,
  );
}

/** Whether the desktop sidebar is collapsed to a rail; remembered per browser. */
function useCollapsed(): [boolean, (v: boolean) => void] {
  const [v, setV] = useState(() => {
    try {
      return localStorage.getItem("rp:sidebar") === "rail";
    } catch {
      return false;
    }
  });
  const set = (x: boolean) => {
    setV(x);
    try {
      localStorage.setItem("rp:sidebar", x ? "rail" : "full");
    } catch {
      /* private window */
    }
  };
  return [v, set];
}

const RANGES: { value: RangeKey; label: string }[] = [{ value: "7d", label: "7D" }, { value: "30d", label: "30D" }, { value: "3m", label: "3M" }, { value: "12m", label: "1Y" }, { value: "all", label: "All" }];

function ThemeMenu() {
  const [theme, setTheme] = useTheme();
  const Icon = theme === "dark" ? Moon : theme === "light" ? Sun : Monitor;
  return (
    <DM.Root>
      <DM.Trigger className="btn btn-ghost btn-icon" aria-label="Appearance"><Icon size={16} /></DM.Trigger>
      <DM.Portal>
        <DM.Content align="end" sideOffset={6} className="z-50 min-w-[160px] rounded-xl bg-[var(--bg-elevated)] p-1 shadow-[var(--shadow-pop)]">
          {(["system", "light", "dark"] as Theme[]).map((t) => (
            <DM.Item key={t} onSelect={() => setTheme(t)} className={clsx("flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-1.5 text-[13px] outline-none data-[highlighted]:bg-surface-3", theme === t && "font-semibold")}>
              {t === "system" ? <Monitor size={14} /> : t === "light" ? <Sun size={14} /> : <Moon size={14} />}
              {t === "system" ? "Match system" : t === "light" ? "Light" : "Dark"}
            </DM.Item>
          ))}
        </DM.Content>
      </DM.Portal>
    </DM.Root>
  );
}

function UserMenu() {
  const me = useMe().data;
  const qc = useQueryClient();
  const nav = useNavigate();
  if (!me) return null;
  const K = KIND[me.org.kind];
  return (
    <DM.Root>
      <DM.Trigger className="flex h-9 items-center gap-2 rounded-full py-1 pl-1 pr-2.5 hover:bg-surface-3" aria-label="Account">
        <span className="grid size-7 place-items-center rounded-full text-[12px] font-bold text-white" style={{ background: K.color }}>
          {me.user.name.split(" ").map((p) => p[0]).slice(0, 2).join("")}
        </span>
        <span className="hidden text-left leading-tight xl:block">
          <span className="block max-w-[180px] truncate text-[12.5px] font-semibold">{me.org.name}</span>
          <span className="block text-[11px] text-muted">{K.name} · {me.user.role}</span>
        </span>
        <ChevronDown size={14} className="text-muted" />
      </DM.Trigger>
      <DM.Portal>
        <DM.Content align="end" sideOffset={6} className="z-50 w-[260px] rounded-2xl bg-[var(--bg-elevated)] p-1.5 shadow-[var(--shadow-pop)]">
          <div className="px-3 py-2">
            <div className="text-[13px] font-semibold">{me.user.name}</div>
            <div className="text-[12px] text-muted">{me.user.email}</div>
          </div>
          <DM.Separator className="my-1 h-px bg-line" />
          <DM.Item onSelect={() => nav("/settings")} className="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-[13px] outline-none data-[highlighted]:bg-surface-3">
            <Settings2 size={14} /> Settings
          </DM.Item>
          <DM.Item onSelect={() => nav("/settings?tab=organisation")} className="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-[13px] outline-none data-[highlighted]:bg-surface-3">
            <Building2 size={14} /> Organisation
          </DM.Item>
          <DM.Item
            onSelect={async () => {
              await api.send("POST", "/auth/logout");
              qc.clear();
              nav("/login");
            }}
            className="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-[13px] text-critical outline-none data-[highlighted]:bg-surface-3"
          >
            <LogOut size={14} /> Sign out
          </DM.Item>
        </DM.Content>
      </DM.Portal>
    </DM.Root>
  );
}

/**
 * Date and time in the user's time zone (Settings → Preferences). Live updates
 * run underneath; a dot appears only when they are interrupted.
 */
function Clock({ state }: { state: "live" | "connecting" | "offline" }) {
  const nav = useNavigate();
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    let t: ReturnType<typeof setTimeout>;
    const tick = () => {
      setNow(new Date());
      t = setTimeout(tick, 60_000 - (Date.now() % 60_000) + 50); // on the minute
    };
    t = setTimeout(tick, 60_000 - (Date.now() % 60_000) + 50);
    return () => clearTimeout(t);
  }, []);
  const tz = timeZone();
  const day = new Intl.DateTimeFormat("en-GB", { weekday: "short", day: "numeric", month: "short", timeZone: tz }).format(now);
  const time = new Intl.DateTimeFormat("en-GB", { hour: "2-digit", minute: "2-digit", timeZone: tz }).format(now);
  const abbr = new Intl.DateTimeFormat("en-GB", { timeZoneName: "short", timeZone: tz }).formatToParts(now).find((p) => p.type === "timeZoneName")?.value ?? "";
  const down = state === "offline";
  return (
    <Hint text={<>{tz.replace(/_/g, " ")}{down ? " · live updates interrupted, reconnecting…" : ""}<br /><span className="text-muted">Change in Settings → Preferences</span></>}>
      <button type="button" onClick={() => nav("/settings?tab=preferences")}
        className="hidden h-8 items-center gap-2 rounded-full px-2.5 text-[12.5px] font-medium text-text-2 hover:bg-surface-3 md:inline-flex">
        {down && <span className="size-[7px] rounded-full bg-[var(--warning)]" aria-label="Live updates interrupted" />}
        <span>{day}</span>
        <span className="num font-semibold text-text">{time}</span>
        <span className="text-[11px] text-muted">{abbr}</span>
      </button>
    </Hint>
  );
}

export function Shell() {
  const me = useMe();
  const range = useRangeState();
  const live = useLive(!!me.data);
  const loc = useLocation();
  // Platform pages show the platform as it is now; the time range only means something on the ledger and alerts.
  const platformPage = !!me.data?.operator && me.data.user.role === "admin" && (loc.pathname === "/" || loc.pathname.startsWith("/platform") || loc.pathname.startsWith("/settings"));
  const [mobile, setMobile] = useState(false);
  const search = useRef<SearchBoxHandle>(null);
  // ≥1024 px: full sidebar or rail (user's choice); 768–1023: rail; phones: drawer.
  const wide = useMedia("(min-width: 1024px)");
  const tablet = useMedia("(min-width: 768px)");
  const phone = !useMedia("(min-width: 640px)");
  const [collapsed, setCollapsed] = useCollapsed();
  const rail = tablet && (!wide || collapsed);
  // Only the content column scrolls (the sidebar stays put), so a new page starts at its top.
  const scroller = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setMobile(false);
    scroller.current?.scrollTo({ top: 0 });
  }, [loc.pathname]);
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        search.current?.focus();
      }
    };
    window.addEventListener("keydown", on);
    return () => window.removeEventListener("keydown", on);
  }, []);
  const pendingBanner: ReactNode =
    me.data && me.data.org.status !== "verified" ? (
      <div className="mb-5 flex items-start gap-3 rounded-2xl bg-[var(--warning-soft)] px-4 py-3 text-[13px] text-warning">
        <ShieldCheck size={18} className="mt-0.5 shrink-0" aria-hidden />
        <div>
          <b>{me.data.org.status === "pending" ? "Awaiting verification." : "Organisation suspended."}</b>{" "}
          {me.data.org.status === "pending"
            ? "You can add sites, invite your team and upload records now. Your records are signed and checked, and are sealed on the ledger once the platform operator has verified your company."
            : "Your records stay readable, but you cannot submit new ones. Contact the platform operator."}
        </div>
      </div>
    ) : null;

  return (
    <RangeContext.Provider value={range}>
      <div className="flex h-dvh overflow-hidden">
        {tablet && (
          <aside className={clsx("h-full shrink-0 p-2.5 pr-0 transition-[width] duration-200", rail ? "w-[74px]" : "w-[256px]")}>
            <div className="h-full overflow-y-auto overflow-x-hidden rounded-[16px] bg-[var(--sidebar)] shadow-[var(--shadow-1)] scroll-thin">
              <Sidebar operator={!!me.data?.operator} compact={rail} />
            </div>
          </aside>
        )}
        <Dialog.Root open={mobile && !tablet} onOpenChange={setMobile}>
          <Dialog.Portal>
            <Dialog.Overlay className="fixed inset-0 z-40 bg-black/45 backdrop-blur-[2px]" style={{ animation: "fade .18s ease-out" }} />
            <Dialog.Content className="fixed inset-y-0 left-0 z-50 w-[280px] max-w-[86vw] bg-[var(--bg-elevated)] shadow-[var(--shadow-pop)] outline-none" style={{ animation: "slide-in .22s cubic-bezier(.2,.8,.2,1)" }}>
              <Dialog.Title className="sr-only">Navigation</Dialog.Title>
              <Dialog.Description className="sr-only">Main menu</Dialog.Description>
              <Dialog.Close className="btn btn-ghost btn-icon absolute right-3 top-5" aria-label="Close menu"><X size={16} /></Dialog.Close>
              <Sidebar operator={!!me.data?.operator} onNavigate={() => setMobile(false)} />
            </Dialog.Content>
          </Dialog.Portal>
        </Dialog.Root>
        <div ref={scroller} className="flex min-w-0 flex-1 flex-col overflow-y-auto overscroll-contain [scrollbar-gutter:stable]">
          <header className="sticky top-0 z-30 bg-[color-mix(in_srgb,var(--bg)_82%,transparent)] px-4 backdrop-blur-xl md:px-7">
            <div className="flex h-[60px] items-center gap-2 sm:gap-3">
              {!tablet ? (
                <button className="btn btn-ghost btn-icon -ml-2 shrink-0" onClick={() => setMobile(true)} aria-label="Open menu"><Menu size={18} /></button>
              ) : wide ? (
                <Hint text={collapsed ? "Show labels" : "Collapse sidebar"}>
                  <button className="btn btn-ghost btn-icon -ml-2 shrink-0" onClick={() => setCollapsed(!collapsed)} aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"} aria-pressed={collapsed}>
                    {collapsed ? <PanelLeftOpen size={18} /> : <PanelLeftClose size={18} />}
                  </button>
                </Hint>
              ) : null}
              <SearchBox ref={search} className="min-w-0 flex-1 sm:min-w-[220px] sm:max-w-[380px]" />
              <div className="ml-auto flex shrink-0 items-center gap-2.5">
                {!phone && (
                  <>
                    {!platformPage && <Segmented<RangeKey> label="Time range" value={range.key} onChange={range.set} options={RANGES} />}
                    <Clock state={live} />
                    <ThemeMenu />
                  </>
                )}
                <NotificationBell />
                <UserMenu />
              </div>
            </div>
            {/* Phones: the time range and data label get their own row. */}
            {phone && (
              <div className="flex items-center justify-between gap-2 pb-2.5">
                {platformPage ? <span /> : <Segmented<RangeKey> label="Time range" value={range.key} onChange={range.set} options={RANGES} />}
                <div className="flex items-center gap-1"><ThemeMenu /></div>
              </div>
            )}
          </header>
          <main className="mx-auto w-full max-w-[1480px] flex-1 px-4 pb-16 pt-3 md:px-7">
            {me.data?.org.demo && (
              <div role="note" className="mb-4 flex items-start gap-3 rounded-2xl bg-[var(--accent-soft)] px-4 py-3 text-[13px] text-text-2">
                <ShieldCheck size={18} className="mt-0.5 shrink-0 text-accent-ink" aria-hidden />
                <div><b className="text-text">Demo workspace.</b> Route 740 quantities are public MoDOT figures; everything else is simulated. You can enter a limited amount of data;
                  it is all reset every day, and invitations, API keys and outgoing alerts are switched off.</div>
              </div>
            )}
            {pendingBanner}
            <Outlet />
          </main>
        </div>
      </div>
    </RangeContext.Provider>
  );
}
