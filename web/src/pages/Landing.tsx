import { lazy, Suspense, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";
import {
  ArrowRight, Bell, Blocks, Check, ClipboardCheck, Fingerprint, Leaf, Lock, Plug, ScanSearch,
  ShieldCheck, Users, Workflow,
} from "lucide-react";
import { ChainCanvas } from "../components/ChainCanvas";
import { MarketingFrame, Reveal } from "../components/Marketing";
import { canWebGL } from "../lib/webgl";
import { useIsDark } from "../lib/hooks";
import { useSeo } from "../lib/seo";

// three.js is loaded only when the visitor reaches the page, and only as its own chunk.
const HeroScene = lazy(() => import("../components/HeroScene").then((m) => ({ default: m.HeroScene })));
const FeatureTour = lazy(() => import("../components/FeatureTour").then((m) => ({ default: m.FeatureTour })));

const TOUR = [
  { shot: "register", alt: "ROADPASS Data in page: upload a CSV or connect an API", step: "01", sub: "Register", title: "Every handover recorded where it happens", text: "Each organisation records what it made, received or shipped, in the way that suits its team.", points: ["Dashboard forms, CSV upload, REST and NGSI-LD", "Corrections are new versions that link to the old one", "Nothing is ever overwritten"], caption: "Data in: CSV, REST, NGSI-LD", color: "#3987e5" },
  { shot: "work", alt: "ROADPASS My work page with queues to receive, process, test and ship", step: "02", sub: "Work", title: "Everyone sees the work waiting for them", text: "Job roles map to duties, so the intake officer, the operator, the quality manager and the dispatcher each get their own queue.", points: ["Queues to receive, produce, test, release and ship", "Every action signed with your company key and your own", "Assign work to people in your team"], caption: "Queues by job role", color: "#1baf7a" },
  { shot: "assure", alt: "ROADPASS Data checks page listing findings by check", step: "03", sub: "Assure", title: "Mistakes surface at the desk that made them", text: "Rule-based checks run on every record the moment it arrives, so errors do not wait for an audit.", points: ["Mass balance, link integrity and lab limits", "A flagged record is never hidden", "Accepting one takes a signed reason"], caption: "Checks C1–C9 on every record", color: "#d95926" },
  { shot: "seal", alt: "ROADPASS Ledger page showing sealed blocks by organisation", step: "04", sub: "Seal", title: "Signed by you, sealed on a blockchain ledger", text: "The company that made a record signs it with its own key. Batches of records are then anchored on a blockchain ledger.", points: ["Ed25519 signature per organisation", "Merkle batches, one ledger transaction each", "Only hashes go on the ledger, never your data"], caption: "Merkle batch → blockchain ledger", color: "#c98500" },
  { shot: "prove", alt: "ROADPASS road section passport with journey and lineage", step: "05", sub: "Prove", title: "A passport anyone can check", text: "Each lot and road section carries its proof. Fingerprint, signature and Merkle path are re-computed on the visitor's device.", points: ["Journey and lineage from road back to sorting plant", "Public share links you can revoke at any time", "Lab results next to the lots they describe"], caption: "SHA-256 · Ed25519 · RFC 6962", color: "#d55181" },
  { shot: "connect", alt: "ROADPASS Carbon and impact page", step: "06", sub: "Connect", title: "Impact and data, ready to use elsewhere", text: "Recycled content and emissions are calculated from the records. Your verified data leaves over open standards when you want.", points: ["Carbon and recycled content from the chain data", "GRETA-ready life-cycle assessment export", "REST and NGSI-LD APIs, open data export"], caption: "Carbon · GRETA · open APIs", color: "#93D500" },
];

// The public front door: what ROADPASS is, how it works, and where to start. It
// makes claims about the product only, no figures from data; the sample passport
// is labelled as an illustration.

const STAGES = [
  { name: "Sorting plant", does: "Registers the plastic rejects it sorts out", color: "var(--s1)" },
  { name: "Recycler", does: "Turns rejects into recycled LDPE lots", color: "var(--s2)" },
  { name: "Additive producer", does: "Blends the lots into an asphalt additive", color: "var(--s3)" },
  { name: "Asphalt plant", does: "Mixes the additive into asphalt", color: "var(--s4)" },
  { name: "Contractor", does: "Lays the asphalt on a road section", color: "var(--s5)" },
  { name: "Road owner", does: "Owns the finished road and its passport", color: "var(--s6)" },
];


const FEATURES: { icon: typeof Leaf; title: string; text: string }[] = [
  { icon: Fingerprint, title: "Passports for every lot", text: "One traceable passport from rejected plastic to the road section, with the full lineage of lots, shipments and lab tests behind it." },
  { icon: ShieldCheck, title: "Signed and sealed", text: "Records are signed by the company that made them and sealed on a blockchain ledger. Corrections link to what they replace, so history stays intact." },
  { icon: ScanSearch, title: "Check it yourself", text: "Fingerprint, signature and Merkle proof are re-computed in the visitor's browser. Nobody has to take our word for it." },
  { icon: ClipboardCheck, title: "Automatic data checks", text: "Nine rule-based checks run on each record, so errors surface at the desk that made them, not in the audit months later." },
  { icon: Leaf, title: "Carbon and LCA ready", text: "Emission and recycled-content figures are calculated from the chain data, with a GRETA-ready export for life-cycle assessment." },
  { icon: Workflow, title: "Work queues by role", text: "Intake officers, quality managers and dispatchers each see what is waiting for them: receive, produce, test, release, ship." },
  { icon: Bell, title: "Alerts and reports", text: "Rules notify the right people by e-mail or push. Compliance registers and signed reports are generated from the same data." },
  { icon: Plug, title: "Open by design", text: "REST and NGSI-LD APIs, CSV import, and export to other data platforms. Your records are never locked in." },
  { icon: Lock, title: "Private by default", text: "Every organisation works in its own space and decides what is shared with whom. Public passport links can be revoked at any time." },
];

const AUDIENCE = [
  { title: "Plastic sorters and recyclers", text: "Prove the origin and quality of every lot you sell, and get paid for the recycled content you can show.", color: "var(--s1)" },
  { title: "Asphalt producers and contractors", text: "Know what went into every load, record dosing, and hand the road owner a complete file.", color: "var(--s4)" },
  { title: "Road owners and cities", text: "Hold a verifiable record of the recycled plastic in your network, ready for green-procurement reporting.", color: "var(--s6)" },
  { title: "Labs and auditors", text: "Attach test results to the lots they describe, and check a chain end to end without asking for spreadsheets.", color: "var(--s7)" },
];

const CODE: Record<string, { label: string; lines: ReactNode[] }> = {
  passport: {
    label: "Read a passport",
    lines: [
      <><span className="text-muted"># any lot, shipment or road section</span></>,
      <>curl <span className="text-[#93D500]">https://roadpass.sensifai.com/api/v1/passports/{"{id}"}</span> \</>,
      <>  -H <span className="text-[#7cc4ff]">"Authorization: Bearer rpk_…"</span></>,
    ],
  },
  ngsi: {
    label: "NGSI-LD",
    lines: [
      <><span className="text-muted"># the same data for FIWARE-style platforms</span></>,
      <>curl <span className="text-[#93D500]">https://roadpass.sensifai.com/ngsi-ld/v1/types</span> \</>,
      <>  -H <span className="text-[#7cc4ff]">"Authorization: Bearer rpk_…"</span></>,
    ],
  },
  verify: {
    label: "Verify a hash",
    lines: [
      <><span className="text-muted"># public: is this fingerprint sealed on the ledger?</span></>,
      <>curl -X POST <span className="text-[#93D500]">https://roadpass.sensifai.com/api/v1/ledger/verify</span> \</>,
      <>  -H <span className="text-[#7cc4ff]">"Content-Type: application/json"</span> \</>,
      <>  -d <span className="text-[#7cc4ff]">{"'{\"hash\": \"9f2c…e41a\"}'"}</span></>,
    ],
  },
};

function SectionHead({ id, eyebrow, title, lead, center }: { id?: string; eyebrow: string; title: ReactNode; lead?: ReactNode; center?: boolean }) {
  return (
    <Reveal className={clsx("max-w-[720px]", center && "mx-auto text-center")}>
      <div className="eyebrow mb-3 !text-accent-ink">{eyebrow}</div>
      <h2 id={id} className="mk-h2">{title}</h2>
      {lead && <p className="mk-lead mt-4">{lead}</p>}
    </Reveal>
  );
}

function Hero() {
  const gl = canWebGL();
  const dark = useIsDark();
  return (
    <section aria-label="Introduction" className="relative isolate flex min-h-[100svh] flex-col overflow-hidden bg-bg text-text">
      <div className="mk-glow pointer-events-none absolute inset-x-0 top-0 -z-10 h-[520px]" aria-hidden />
      <div className="pointer-events-none absolute inset-0 -z-[4] max-lg:bg-bg/70 lg:inset-y-0 lg:right-auto lg:w-[78%] lg:bg-gradient-to-r lg:from-bg lg:via-bg/85 lg:to-transparent" aria-hidden />
      {gl ? (
        <Suspense fallback={null}><HeroScene dark={dark} className="absolute inset-0 -z-[5]" /></Suspense>
      ) : (
        <ChainCanvas className="absolute inset-x-0 bottom-0 -z-[5] hidden h-[45%] w-full opacity-80 md:block" />
      )}
      <div className="mx-auto flex w-full max-w-[1180px] flex-1 items-center px-5 pb-10 pt-24 sm:pt-32">
        <div className="max-w-[760px]">
          <span className="inline-flex items-center gap-2 rounded-full bg-[var(--surface-3)] px-3.5 py-1.5 text-[12.5px] font-semibold text-text ring-1 ring-line backdrop-blur">
            <span className="h-1.5 w-1.5 rounded-full bg-[#93D500]" /> Digital material passports for roads
          </span>
          <h1 className="mt-6 text-[clamp(34px,9.5vw,80px)] font-bold leading-[0.98] tracking-[-0.045em]">
            Every road knows
            <br />
            <span className="text-[#93D500]">where its plastic came from.</span>
          </h1>
          <p className="mt-6 max-w-[580px] text-[clamp(16px,1.6vw,19px)] leading-relaxed text-text-2">
            ROADPASS gives recycled plastic in asphalt a passport. Each company in the chain signs what it handed over, the platform checks it, and a blockchain ledger seals it, so anyone can verify the claim without asking for a spreadsheet.
          </p>
          <div className="mt-9 flex flex-wrap items-center gap-3 max-sm:[&>.btn]:flex-1 max-sm:[&>.btn]:justify-center">
            <Link to="/contact?topic=demo" className="btn h-12 bg-[#93D500] px-6 text-[15px] text-[#0b0b0c] hover:bg-[#a6e61a]">Request a demo <ArrowRight size={16} /></Link>
            <Link to="/verify" className="btn h-12 bg-[var(--surface-3)] px-6 text-[15px] text-text ring-1 ring-line hover:bg-[var(--surface-3)]">Verify a record</Link>
            <Link to="/login" className="px-3 text-[14px] font-medium text-text-2 hover:text-text">Sign in →</Link>
          </div>
        </div>
      </div>
      <div className="border-t border-line bg-[color-mix(in_srgb,var(--bg)_60%,transparent)] backdrop-blur">
        <ul className="mx-auto grid max-w-[1180px] gap-2.5 px-5 py-4 text-[13px] text-text-2 sm:grid-cols-3 sm:py-5">
          {["Signed by the company that made it", "Sealed on a blockchain ledger", "Checkable by anyone, in the browser"].map((t) => (
            <li key={t} className="flex items-center gap-2.5"><Check size={15} className="shrink-0 text-[#93D500]" />{t}</li>
          ))}
        </ul>
      </div>
    </section>
  );
}

function How() {
  return (
    <section id="how" className="mk-section bg-surface py-16 sm:py-32">
      <div className="mx-auto max-w-[1180px] px-5">
        <SectionHead eyebrow="How it works" title="From rejected plastic to a verified road." lead="Six kinds of organisation hand material down the chain. Every handover is recorded, checked, sealed and provable; the tour below follows one." />
        <Reveal className="mt-14">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            {STAGES.map((s, i) => (
              <div key={s.name} className="relative rounded-2xl bg-surface-2 p-4 ring-1 ring-line">
                <div className="flex items-center gap-2">
                  <span className="grid h-6 w-6 place-items-center rounded-full text-[11px] font-bold text-text" style={{ background: s.color }}>{i + 1}</span>
                  <span className="text-[13.5px] font-semibold leading-tight">{s.name}</span>
                </div>
                <p className="mt-2.5 text-[12.5px] leading-snug text-text-2">{s.does}</p>
                <div className="absolute inset-x-4 bottom-0 h-[3px] rounded-full" style={{ background: s.color }} />
              </div>
            ))}
          </div>
        </Reveal>
      </div>
    </section>
  );
}

function Features() {
  return (
    <section id="features" className="mk-section mx-auto max-w-[1180px] px-5 py-16 sm:py-32">
      <SectionHead eyebrow="Features" title="Everything the chain needs, and nothing it has to take on trust." center />
      <div className="mt-10 grid gap-4 sm:mt-14 sm:grid-cols-2 lg:grid-cols-3">
        {FEATURES.map((f, i) => (
          <Reveal key={f.title} delay={(i % 3) * 70}>
            <div className="mk-card h-full p-5 sm:p-7">
              <span className="grid h-11 w-11 place-items-center rounded-xl bg-accent-soft text-accent-ink"><f.icon size={21} /></span>
              <h3 className="mt-5 text-[17px] font-semibold tracking-tight">{f.title}</h3>
              <p className="mt-2 text-[14px] leading-relaxed text-text-2">{f.text}</p>
            </div>
          </Reveal>
        ))}
      </div>
    </section>
  );
}

/** An illustrative passport; clearly labelled, with placeholder fingerprints. */
function PassportShowcase() {
  const rows = [
    ["Sorted", "Sorting plant", "var(--s1)"],
    ["Recycled", "Recycler", "var(--s2)"],
    ["Blended", "Additive producer", "var(--s3)"],
    ["Mixed", "Asphalt plant", "var(--s4)"],
    ["Laid", "Contractor", "var(--s5)"],
  ];
  return (
    <section className="bg-bg py-16 text-text sm:py-32">
      <div className="mx-auto grid max-w-[1180px] items-center gap-10 px-5 sm:gap-14 grid-cols-[minmax(0,1fr)] lg:grid-cols-2">
        <Reveal>
          <div className="eyebrow mb-3 !text-[#93D500]">The passport</div>
          <h2 className="mk-h2">One page that answers every question about a road.</h2>
          <p className="mt-5 max-w-[520px] text-[17px] leading-relaxed text-text-2">
            Open a passport and follow the material back through every handover. Each step shows who signed it, when, and where it is sealed. The checks run again in your browser.
          </p>
          <ul className="mt-8 space-y-3.5 text-[15px] text-text">
            {["Full lineage from road section back to the sorting plant", "Lab results next to the lots they describe", "A share link you can revoke, for clients and auditors"].map((t) => (
              <li key={t} className="flex gap-3"><Check size={18} className="mt-0.5 shrink-0 text-[#93D500]" />{t}</li>
            ))}
          </ul>
          <div className="mt-9 flex flex-wrap gap-3">
            <Link to="/verify" className="btn btn-primary h-11 px-5 text-[14px]">Try the verifier <ArrowRight size={15} /></Link>
            <Link to="/explorer" className="btn h-11 bg-[var(--surface-3)] px-5 text-[14px] text-text ring-1 ring-line hover:bg-[var(--surface-3)]">Open the explorer <ArrowRight size={15} /></Link>
          </div>
        </Reveal>
        <Reveal delay={100}>
          <div className="mk-float max-w-full rounded-3xl bg-surface-2 p-5 ring-1 ring-line backdrop-blur sm:p-7">
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="text-[11px] font-semibold uppercase tracking-[0.08em] text-muted">Road section passport</div>
                <div className="mt-1 text-[20px] font-semibold tracking-tight">Sample road section</div>
              </div>
              <span className="inline-flex items-center gap-1.5 rounded-full bg-[#93D500]/15 px-2.5 py-1 text-[12px] font-semibold text-[#b6f03a]"><ShieldCheck size={13} /> Verified</span>
            </div>
            <ol className="mt-6 space-y-0">
              {rows.map(([what, who, c], i) => (
                <li key={what} className="relative flex items-center gap-3.5 py-2.5 pl-1">
                  {i < rows.length - 1 && <span className="absolute left-[13px] top-[30px] h-[calc(100%-6px)] w-px bg-[var(--line-strong)]" aria-hidden />}
                  <span className="relative grid h-[22px] w-[22px] shrink-0 place-items-center rounded-full ring-4 ring-[#17171a]" style={{ background: c }}><Check size={12} className="text-text" strokeWidth={3} /></span>
                  <div className="min-w-0 flex-1">
                    <div className="text-[14px] font-medium">{what}</div>
                    <div className="text-[12.5px] text-muted">{who} · signed</div>
                  </div>
                  <span className="mono hidden text-[11.5px] text-muted sm:block">9f2c…{["e41a", "7b03", "c8d9", "12af", "5e60"][i]}</span>
                </li>
              ))}
            </ol>
            <div className="mt-5 flex items-center gap-3 rounded-2xl bg-[color-mix(in_srgb,var(--bg)_60%,transparent)] px-4 py-3 text-[12.5px] text-text-2 ring-1 ring-line">
              <Blocks size={16} className="shrink-0 text-[#93D500]" />
              Sealed on the ledger · Merkle proof re-computed in your browser
            </div>
            <div className="mt-3 text-center text-[11px] text-muted">Illustration. Fingerprints are placeholders.</div>
          </div>
        </Reveal>
      </div>
    </section>
  );
}

function Audience() {
  return (
    <section id="for" className="mk-section mx-auto max-w-[1180px] px-5 py-16 sm:py-32">
      <SectionHead eyebrow="Who it is for" title="One chain, six kinds of organisation, a clear job for each." lead="ROADPASS works for any country, city or government. Each organisation sees its own work and what it is allowed to see of the others." />
      <div className="mt-12 grid gap-4 sm:grid-cols-2">
        {AUDIENCE.map((a, i) => (
          <Reveal key={a.title} delay={(i % 2) * 80}>
            <div className="mk-card relative h-full overflow-hidden p-5 pl-7 sm:p-7 sm:pl-9">
              <span className="absolute inset-y-6 left-0 w-1.5 rounded-r-full" style={{ background: a.color }} />
              <div className="flex items-center gap-2.5"><Users size={17} className="text-muted" /><h3 className="text-[18px] font-semibold tracking-tight">{a.title}</h3></div>
              <p className="mt-2.5 text-[14.5px] leading-relaxed text-text-2">{a.text}</p>
            </div>
          </Reveal>
        ))}
      </div>
    </section>
  );
}

function Open() {
  const [tab, setTab] = useState<keyof typeof CODE>("passport");
  const standards = ["RFC 8785 JSON canonicalisation", "SHA-256", "Ed25519 signatures", "RFC 6962 Merkle trees", "NGSI-LD", "OpenAPI"];
  return (
    <section id="open" className="mk-section bg-surface py-16 sm:py-32">
      <div className="mx-auto grid max-w-[1180px] items-center gap-10 px-5 sm:gap-14 grid-cols-[minmax(0,1fr)] lg:grid-cols-[1fr_1.1fr]">
        <Reveal>
          <div className="eyebrow mb-3 !text-accent-ink">Open standards</div>
          <h2 className="mk-h2">Built on standards, so your data can leave when you want.</h2>
          <p className="mk-lead mt-5">Connect plant systems and lab software through REST or NGSI-LD, import spreadsheets, and export verified records to other data platforms. The proof relies on public standards, not on our software.</p>
          <ul className="mt-8 flex flex-wrap gap-2">
            {standards.map((s) => <li key={s} className="rounded-full bg-surface-3 px-3 py-1.5 text-[12.5px] font-medium text-text-2">{s}</li>)}
          </ul>
        </Reveal>
        <Reveal delay={100}>
          <div className="mk-dark overflow-hidden rounded-2xl bg-bg text-text shadow-[var(--shadow-2)] ring-1 ring-line">
            <div className="flex items-center gap-1.5 border-b border-line px-4 py-3">
              <span className="h-2.5 w-2.5 rounded-full bg-[var(--line-strong)]" /><span className="h-2.5 w-2.5 rounded-full bg-[var(--line-strong)]" /><span className="h-2.5 w-2.5 rounded-full bg-[var(--line-strong)]" />
              <div className="ml-2 flex min-w-0 gap-1 overflow-x-auto sm:ml-4" role="tablist" aria-label="API examples">
                {Object.entries(CODE).map(([k, v]) => (
                  <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)}
                    className={clsx("shrink-0 whitespace-nowrap rounded-md px-2.5 py-1.5 text-[12px] font-medium transition-colors", tab === k ? "bg-[var(--surface-3)] text-text" : "text-muted hover:text-text")}>{v.label}</button>
                ))}
              </div>
            </div>
            <pre className="mono overflow-x-auto p-4 text-[12px] sm:p-5 sm:text-[12.5px] leading-[1.9] text-text">{CODE[tab].lines.map((l, i) => <div key={i}>{l}</div>)}</pre>
          </div>
        </Reveal>
      </div>
    </section>
  );
}

function Cta() {
  return (
    <section aria-labelledby="cta" className="mk-dark relative isolate overflow-hidden border-t border-line bg-bg text-text">
      <div className="mk-glow pointer-events-none absolute inset-x-0 top-0 -z-10 h-[420px]" aria-hidden />
      <div className="pointer-events-none absolute inset-x-0 bottom-0 -z-10 h-px bg-gradient-to-r from-transparent via-[#93D500]/50 to-transparent" aria-hidden />
      <Reveal className="mx-auto max-w-[1180px] px-5 py-20 text-center sm:py-32">
        <div className="eyebrow mb-4 !text-[#93D500]">Get started</div>
        <h2 id="cta" className="mx-auto max-w-[820px] text-[clamp(34px,6vw,72px)] font-bold leading-[1] tracking-[-0.04em]">
          Give your roads <span className="text-[#93D500]">a passport.</span>
        </h2>
        <p className="mx-auto mt-6 max-w-[560px] text-[clamp(15px,1.5vw,18px)] leading-relaxed text-text-2">Tell us about your chain. We will show you ROADPASS on your own data and help you plan a first pilot.</p>
        <div className="mt-10 flex flex-wrap justify-center gap-3 max-sm:[&>.btn]:flex-1">
          <Link to="/contact?topic=pilot" className="btn h-12 bg-[#93D500] px-7 text-[15px] text-[#0b0b0c] hover:bg-[#a6e61a]">Talk to us <ArrowRight size={16} /></Link>
          <Link to="/login" className="btn h-12 bg-[var(--surface-3)] px-7 text-[15px] text-text ring-1 ring-line hover:bg-[var(--surface-3)]">Sign in</Link>
        </div>
        <ul className="mt-12 flex flex-wrap justify-center gap-x-8 gap-y-3 text-[13px] text-text-2">
          {["Pilot on your own data", "Open REST and NGSI-LD APIs", "Verifiable by anyone"].map((t) => (
            <li key={t} className="flex items-center gap-2"><Check size={15} className="shrink-0 text-[#93D500]" />{t}</li>
          ))}
        </ul>
      </Reveal>
    </section>
  );
}

export default function Landing() {
  useSeo("ROADPASS · Digital material passports for recycled plastic in roads", "ROADPASS gives recycled plastic in asphalt a passport. Every company in the chain signs its handover, the platform checks it, and a blockchain ledger seals it, so anyone can verify the claim.", "/");
  return (
    <MarketingFrame>
      <Hero />
      <How />
      <Features />
      <Suspense fallback={<div className="h-[100vh] bg-bg" />}><FeatureTour chapters={TOUR} /></Suspense>
      <PassportShowcase />
      <Audience />
      <Open />
      <Cta />
    </MarketingFrame>
  );
}
