import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Link, NavLink, useLocation } from "react-router-dom";
import clsx from "clsx";
import { ArrowRight, Menu, Monitor, Moon, Sun, X } from "lucide-react";
import { useTheme, type Theme } from "../lib/hooks";

/** The ROADPASS mark. `onDark` keeps it white-on-black over the always-dark hero. */
export function Mark({ size = 28, onDark = false }: { size?: number; onDark?: boolean }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden>
      <rect width="64" height="64" rx="15" fill={onDark ? "#fff" : "var(--text)"} />
      <path d="M18 46 L28 18 h8 L46 46" fill="none" stroke={onDark ? "#0b0b0c" : "var(--bg)"} strokeWidth="5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M32 24v5m0 6v5" stroke="#93D500" strokeWidth="4" strokeLinecap="round" />
    </svg>
  );
}

const LINKS = [
  { to: "/#features", label: "Features" },
  { to: "/#how", label: "How it works" },
  { to: "/#tour", label: "Product tour" },
  { to: "/#open", label: "Open standards" },
  { to: "/explorer", label: "Explorer" },
  { to: "/verify", label: "Verify a record" },
  { to: "/contact", label: "Contact" },
];

function ThemeCycle() {
  const [theme, setTheme] = useTheme();
  const next: Theme = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
  const Icon = theme === "dark" ? Moon : theme === "light" ? Sun : Monitor;
  return (
    <button type="button" className="btn btn-ghost btn-icon h-10 w-10 sm:h-8 sm:w-8" aria-label={`Appearance: ${theme}. Switch to ${next}`} title={`Appearance: ${theme}`} onClick={() => setTheme(next)}>
      <Icon size={16} />
    </button>
  );
}

/** Scrolls to the #hash after a navigation (the router does not). */
function useHashScroll() {
  const { hash, pathname } = useLocation();
  useEffect(() => {
    if (!hash) { if (pathname !== "/") window.scrollTo(0, 0); return; }
    const t = setTimeout(() => document.getElementById(hash.slice(1))?.scrollIntoView({ behavior: "smooth", block: "start" }), 60);
    return () => clearTimeout(t);
  }, [hash, pathname]);
}

/** Token overrides for a bar that sits on the always-dark hero, whatever the app theme. */
const ON_DARK = { "--text": "#f5f5f7", "--text-2": "rgba(255,255,255,.72)", "--bg": "#0b0b0c", "--hover": "rgba(255,255,255,.1)" } as CSSProperties;

export function MarketingNav({ overHero = false }: { overHero?: boolean }) {
  const [open, setOpen] = useState(false);
  const [scrolled, setScrolled] = useState(false);
  const loc = useLocation();
  useEffect(() => setOpen(false), [loc.pathname, loc.hash]);
  useEffect(() => {
    const on = () => setScrolled(window.scrollY > 8);
    on();
    window.addEventListener("scroll", on, { passive: true });
    return () => window.removeEventListener("scroll", on);
  }, []);
  return (
    <header style={overHero && !scrolled && !open ? ON_DARK : undefined} className={clsx("text-text fixed inset-x-0 top-0 z-40 transition-colors", open ? "border-b border-line bg-bg" : scrolled ? "border-b border-line bg-[color-mix(in_srgb,var(--bg)_82%,transparent)] backdrop-blur-xl" : "border-b border-transparent")}>
      <div className="mx-auto flex h-16 max-w-[1180px] items-center gap-3 px-5 sm:gap-6">
        <Link to="/" className="flex items-center gap-2.5" aria-label="ROADPASS home">
          <Mark />
          <span className="text-[16px] font-bold tracking-tight">ROADPASS</span>
        </Link>
        <nav className="ml-4 hidden items-center gap-1 lg:flex" aria-label="Main">
          {LINKS.map((l) => (
            <NavLink key={l.to} to={l.to} end className={({ isActive }) => clsx("whitespace-nowrap rounded-full px-3 py-1.5 text-[13.5px] font-medium text-text-2 transition-colors hover:bg-hover hover:text-text", isActive && (l.to === "/contact" || l.to === "/explorer") && "text-text")}>
              {l.label}
            </NavLink>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-1.5">
          <ThemeCycle />
          <Link to="/login" className="btn btn-ghost hidden sm:inline-flex">Sign in</Link>
          <Link to="/contact" className="btn btn-primary hidden h-9 px-4 sm:inline-flex">Talk to us <ArrowRight size={14} /></Link>
          <button type="button" className="btn btn-ghost btn-icon h-10 w-10 lg:hidden" aria-label={open ? "Close menu" : "Open menu"} aria-expanded={open} onClick={() => setOpen(!open)}>
            {open ? <X size={18} /> : <Menu size={18} />}
          </button>
        </div>
      </div>
      {open && (
        <div className="max-h-[calc(100dvh-4rem)] overflow-y-auto overscroll-contain border-t border-line px-5 pb-[max(1.25rem,env(safe-area-inset-bottom))] pt-3 lg:hidden">
          <nav className="flex flex-col" aria-label="Mobile">
            {LINKS.map((l) => <Link key={l.to} to={l.to} className="rounded-xl px-3 py-3.5 text-[15px] font-medium hover:bg-hover">{l.label}</Link>)}
          </nav>
          <div className="mt-3 flex gap-2">
            <Link to="/login" className="btn h-10 flex-1 justify-center">Sign in</Link>
            <Link to="/contact" className="btn btn-primary h-10 flex-1 justify-center">Talk to us</Link>
          </div>
        </div>
      )}
    </header>
  );
}

export function MarketingFooter() {
  const col = "flex flex-col gap-2.5 text-[13.5px] text-text-2";
  const a = "hover:text-text hover:underline underline-offset-4";
  return (
    <footer className="border-t border-line bg-surface">
      <div className="mx-auto grid max-w-[1180px] grid-cols-2 gap-x-6 gap-y-10 px-5 py-12 md:grid-cols-[1.4fr_1fr_1fr_1fr] md:py-14">
        <div className="col-span-2 md:col-span-1">
          <div className="flex items-center gap-2.5"><Mark /><span className="text-[16px] font-bold tracking-tight">ROADPASS</span></div>
          <p className="mt-4 max-w-[320px] text-[13.5px] leading-relaxed text-text-2">
            Digital material passports for recycled plastic in asphalt. Signed by the companies that make each handover, sealed on a blockchain ledger, checkable by anyone.
          </p>
          <address className="mt-4 text-[12.5px] not-italic leading-relaxed text-muted">
            Mechelsesteenweg 455, 1950 Kraainem, Belgium<br />
            <a className="hover:text-text hover:underline" href="mailto:info@sensifai.com">info@sensifai.com</a> · <a className="hover:text-text hover:underline" href="tel:+32489912211">+32 489 91 22 11</a>
          </address>
        </div>
        <div>
          <div className="eyebrow mb-3.5">Product</div>
          <nav className={col} aria-label="Product">
            <Link className={a} to="/#features">Features</Link>
            <Link className={a} to="/#how">How it works</Link>
            <Link className={a} to="/#for">Who it is for</Link>
            <Link className={a} to="/#open">Open standards and APIs</Link>
          </nav>
        </div>
        <div>
          <div className="eyebrow mb-3.5">Use it</div>
          <nav className={col} aria-label="Use it">
            <Link className={a} to="/login">Sign in</Link>
            <Link className={a} to="/explorer">Explorer</Link>
            <Link className={a} to="/verify">Verify a record</Link>
          </nav>
        </div>
        <div>
          <div className="eyebrow mb-3.5">Company</div>
          <nav className={col} aria-label="Company">
            <Link className={a} to="/contact">Contact us</Link>
            <Link className={a} to="/contact?topic=demo">Request a demo</Link>
            <Link className={a} to="/contact?topic=integration">Integrations</Link>
          </nav>
        </div>
      </div>
      <div className="border-t border-line">
        <div className="mx-auto flex max-w-[1180px] flex-wrap items-center justify-between gap-3 px-5 py-5 text-[12.5px] text-muted">
          <span>© {new Date().getFullYear()} Sensifai BV. All rights reserved.</span>
        </div>
      </div>
    </footer>
  );
}

export function MarketingFrame({ children, hero = false }: { children: ReactNode; hero?: boolean }) {
  useHashScroll();
  return (
    <div className="min-h-full overflow-x-clip bg-bg text-text" style={{ scrollBehavior: "smooth" }}>
      <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:z-50 focus:rounded-lg focus:bg-surface focus:px-3 focus:py-2">Skip to content</a>
      <MarketingNav overHero={hero} />
      <main id="main">{children}</main>
      <MarketingFooter />
    </div>
  );
}

/** Fades a block in once, when it first scrolls into view. */
export function Reveal({ children, className, delay = 0 }: { children: ReactNode; className?: string; delay?: number }) {
  const ref = useRef<HTMLDivElement>(null);
  const [seen, setSeen] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof IntersectionObserver === "undefined") { setSeen(true); return; }
    const io = new IntersectionObserver(([e]) => { if (e.isIntersecting) { setSeen(true); io.disconnect(); } }, { rootMargin: "0px 0px -8% 0px" });
    io.observe(el);
    return () => io.disconnect();
  }, []);
  return <div ref={ref} className={clsx("mk-reveal", seen && "mk-in", className)} style={{ transitionDelay: `${delay}ms` }}>{children}</div>;
}
