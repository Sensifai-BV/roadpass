import { useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { ArrowLeft, Building2, CheckCircle2, Clock3, Loader2, Mail, MapPin, MessageSquare, Phone, Presentation, ShieldCheck } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "../lib/api";
import { ErrorBox } from "../components/ui";
import { Challenge } from "../components/Turnstile";
import { MarketingFrame } from "../components/Marketing";
import { useSeo } from "../lib/seo";

const TOPICS = [
  { value: "demo", label: "Request a demo" },
  { value: "pilot", label: "Plan a pilot" },
  { value: "integration", label: "Integrations and APIs" },
  { value: "support", label: "Support" },
  { value: "press", label: "Press and partnerships" },
  { value: "general", label: "Something else" },
];

export default function Contact() {
  useSeo("Contact · ROADPASS", "Request a ROADPASS demo, plan a pilot or ask about integrations. Sensifai BV, Kraainem, Belgium.", "/contact");
  const [params] = useSearchParams();
  const first = params.get("topic");
  const [f, setF] = useState({ name: "", email: "", company: "", topic: TOPICS.some((t) => t.value === first) ? first! : "demo", message: "", website: "" });
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [err, setErr] = useState<unknown>(null);
  const [fields, setFields] = useState<Record<string, string>>({});
  const cfg = useQuery({ queryKey: ["contact-config"], queryFn: () => api.get<{ turnstileSiteKey: string }>("/contact/config"), retry: false, staleTime: 300_000 });
  const siteKey = cfg.data?.turnstileSiteKey ?? "";
  const [token, setToken] = useState<string | null>(null);
  const [challenge, setChallenge] = useState(0);
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF((x) => ({ ...x, [k]: e.target.value }));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    setFields({});
    try {
      await api.send("POST", "/contact", { ...f, turnstileToken: token ?? "" });
      setDone(true);
    } catch (x) {
      const list = x instanceof ApiError && Array.isArray(x.problem.errors) ? (x.problem.errors as { field: string; message: string }[]) : [];
      if (list.length) setFields(Object.fromEntries(list.map((i) => [i.field, i.message])));
      else setErr(x);
      // A token works once: ask for a fresh one after any refusal.
      if (siteKey) { setToken(null); setChallenge((n) => n + 1); }
    } finally {
      setBusy(false);
    }
  };

  const fieldErr = (k: string) => fields[k] && <p className="mt-1.5 text-[12px] text-critical" id={`err-${k}`}>{fields[k]}</p>;
  const inv = (k: string) => ({ "aria-invalid": fields[k] ? true : undefined, "aria-describedby": fields[k] ? `err-${k}` : undefined });

  return (
    <MarketingFrame>
      <div className="relative isolate overflow-hidden">
        <div className="mk-glow pointer-events-none absolute inset-x-0 top-0 -z-10 h-[420px] opacity-60" aria-hidden />
        <div className="mx-auto grid max-w-[1180px] gap-12 px-5 pb-16 pt-32 sm:pt-36 lg:grid-cols-[1fr_1.1fr] lg:gap-16">
          <div>
            <Link to="/" className="inline-flex items-center gap-1.5 text-[13px] font-medium text-text-2 hover:text-text"><ArrowLeft size={14} /> Back to home</Link>
            <h1 className="mt-6 text-[clamp(34px,5vw,56px)] font-bold leading-[1.02] tracking-[-0.04em]">Let's talk about your chain.</h1>
            <p className="mk-lead mt-5 max-w-[480px]">Whether you sort, recycle, mix, lay or own roads, tell us what you want to prove and to whom. A person on our team will reply.</p>
            <ul className="mt-10 space-y-6">
              {[
                [Presentation, "See it on your data", "A walkthrough of passports, checks and the ledger, shaped around your role in the chain."],
                [Clock3, "A real reply", "We read every message and answer by e-mail."],
                [ShieldCheck, "Handled with care", "We use your details only to answer you. The form never subscribes you to anything."],
              ].map(([I, t, d]) => {
                const Icon = I as typeof Clock3;
                return (
                  <li key={t as string} className="flex gap-4">
                    <span className="grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-accent-soft text-accent-ink"><Icon size={19} /></span>
                    <div><div className="text-[15px] font-semibold">{t as string}</div><p className="mt-0.5 text-[14px] leading-relaxed text-text-2">{d as string}</p></div>
                  </li>
                );
              })}
            </ul>
          </div>

          <div className="mk-card !transform-none p-6 sm:p-9 lg:self-start">
            {done ? (
              <div className="flex min-h-[420px] flex-col items-center justify-center text-center" role="status">
                <span className="pop grid h-14 w-14 place-items-center rounded-full bg-accent-soft text-accent-ink"><CheckCircle2 size={28} /></span>
                <h2 className="mt-6 text-[24px] font-bold tracking-tight">Message sent</h2>
                <p className="mt-2 max-w-[340px] text-[14.5px] leading-relaxed text-text-2">Thank you. We will reply to <b className="text-text">{f.email}</b> soon.</p>
                <div className="mt-8 flex gap-2">
                  <Link to="/" className="btn btn-primary h-10 px-5">Back to home</Link>
                  <button type="button" className="btn h-10 px-5" onClick={() => { setDone(false); setF((x) => ({ ...x, message: "" })); }}>Send another</button>
                </div>
              </div>
            ) : (
              <form onSubmit={submit} noValidate>
                <div className="mb-6 flex items-center gap-2.5"><MessageSquare size={18} className="text-muted" /><h2 className="text-[19px] font-semibold tracking-tight">Send us a message</h2></div>
                <div className="grid gap-5 sm:grid-cols-2">
                  <div>
                    <label className="label" htmlFor="c-name">Your name</label>
                    <input id="c-name" className="input !h-11" autoComplete="name" required maxLength={120} value={f.name} onChange={set("name")} {...inv("name")} />
                    {fieldErr("name")}
                  </div>
                  <div>
                    <label className="label" htmlFor="c-email">Work e-mail</label>
                    <input id="c-email" type="email" className="input !h-11" autoComplete="email" required maxLength={200} value={f.email} onChange={set("email")} {...inv("email")} />
                    {fieldErr("email")}
                  </div>
                  <div>
                    <label className="label" htmlFor="c-company">Organisation <span className="font-normal text-muted">(optional)</span></label>
                    <input id="c-company" className="input !h-11" autoComplete="organization" maxLength={160} value={f.company} onChange={set("company")} {...inv("company")} />
                    {fieldErr("company")}
                  </div>
                  <div>
                    <label className="label" htmlFor="c-topic">What is it about?</label>
                    <select id="c-topic" className="input !h-11" value={f.topic} onChange={set("topic")} {...inv("topic")}>
                      {TOPICS.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
                    </select>
                    {fieldErr("topic")}
                  </div>
                </div>
                <div className="mt-5">
                  <label className="label" htmlFor="c-message">Message</label>
                  <textarea id="c-message" className="input min-h-[150px] resize-y" required rows={6} maxLength={5000} value={f.message} onChange={set("message")} placeholder="Tell us about your role in the chain and what you want to achieve." {...inv("message")} />
                  {fieldErr("message")}
                </div>
                {/* Honeypot: hidden from people and assistive tech; bots fill it in. */}
                <div className="absolute -left-[9999px] h-0 w-0 overflow-hidden" aria-hidden>
                  <label>Leave this empty<input tabIndex={-1} autoComplete="off" value={f.website} onChange={set("website")} /></label>
                </div>
                {siteKey && <div className="mt-5"><Challenge siteKey={siteKey} onToken={setToken} resetKey={challenge} />{fieldErr("turnstile")}</div>}
                {err != null && <div className="mt-5"><ErrorBox error={err} /></div>}
                <button type="submit" className="btn btn-primary mt-6 h-12 w-full justify-center text-[15px]" disabled={busy || (!!siteKey && !token)}>
                  {busy && <Loader2 size={16} className="animate-spin" />} {busy ? "Sending…" : "Send message"}
                </button>
                <p className="mt-4 text-center text-[12px] text-muted">We only use your details to reply to this message.</p>
              </form>
            )}
          </div>
        </div>

        <section className="mx-auto max-w-[1180px] px-5 pb-24" aria-labelledby="direct-contact">
          <h2 id="direct-contact" className="text-[22px] font-bold tracking-tight">Direct contact</h2>
          <p className="mt-1.5 text-[14px] text-text-2">Prefer to write or call? Reach us directly.</p>
          <div className="mt-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {[
              { icon: Mail, label: "E-mail", main: "info@sensifai.com", href: "mailto:info@sensifai.com" },
              { icon: Phone, label: "Phone", main: "+32 489 91 22 11", href: "tel:+32489912211" },
            ].map(({ icon: Icon, label, main, href }) => (
              <a key={label} href={href} className="mk-card group flex flex-col gap-4 p-6">
                <span className="grid h-11 w-11 place-items-center rounded-xl bg-accent-soft text-accent-ink"><Icon size={20} /></span>
                <div><div className="eyebrow">{label}</div><div className="mt-1 text-[16px] font-semibold tracking-tight text-text group-hover:text-accent-ink">{main}</div></div>
              </a>
            ))}
            {[
              { icon: Building2, label: "Head office", lines: ["Sensifai BV", "10th Floor, Flexi-Space", "Mechelsesteenweg 455", "1950 Kraainem, Belgium"] },
              { icon: MapPin, label: "Branch office", lines: ["Silversquare Bailli", "Av. Louise 231", "1050 Bruxelles, Belgium"] },
            ].map(({ icon: Icon, label, lines }) => (
              <address key={label} className="mk-card flex flex-col gap-4 p-6 not-italic">
                <span className="grid h-11 w-11 place-items-center rounded-xl bg-accent-soft text-accent-ink"><Icon size={20} /></span>
                <div><div className="eyebrow">{label}</div>
                  <div className="mt-1 text-[14.5px] leading-relaxed text-text-2">{lines.map((l, i) => <div key={l} className={i === 0 ? "font-semibold text-text" : undefined}>{l}</div>)}</div></div>
              </address>
            ))}
          </div>
        </section>
      </div>
    </MarketingFrame>
  );
}
