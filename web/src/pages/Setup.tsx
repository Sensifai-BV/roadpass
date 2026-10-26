import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Loader2, ShieldCheck } from "lucide-react";
import clsx from "clsx";
import { api, ApiError } from "../lib/api";
import { COUNTRIES } from "../lib/countries";
import { ErrorBox } from "../components/ui";
import { AuthFrame } from "./Login";

/**
 * First-run setup: creates the platform organisation and its first manager. It
 * works only with the one-time link the server logs on a fresh installation.
 */
export default function Setup() {
  const [params] = useSearchParams();
  const token = params.get("token") ?? "";
  const nav = useNavigate();
  const qc = useQueryClient();
  const status = useQuery({ queryKey: ["setup"], queryFn: () => api.get<{ needed: boolean }>("/setup"), retry: false });
  const [step, setStep] = useState<1 | 2>(1);
  const [org, setOrg] = useState({ name: "", country: "", city: "", website: "" });
  const [user, setUser] = useState({ name: "", email: "", password: "", confirm: "" });
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const fieldErr = (f: string) =>
    err instanceof ApiError && Array.isArray(err.problem.errors)
      ? (err.problem.errors as { field: string; message: string }[]).find((x) => x.field === f)?.message : undefined;
  const mismatch = user.confirm !== "" && user.password !== user.confirm;

  if (status.data && !status.data.needed) {
    return (
      <AuthFrame>
        <h2 className="text-[26px] font-bold tracking-tight">Already set up</h2>
        <p className="mt-2 text-[14px] text-text-2">This installation has a platform manager. Sign in, or ask them for an invitation.</p>
        <button className="btn btn-primary mt-6 h-10" onClick={() => nav("/login")}>Go to sign in</button>
      </AuthFrame>
    );
  }
  if (!token) {
    return (
      <AuthFrame>
        <h2 className="text-[26px] font-bold tracking-tight">Set up ROADPASS</h2>
        <p className="mt-2 text-[14px] text-text-2">
          Open the setup link from the server's start-up log, or run <code className="mono rounded bg-surface-3 px-1">roadpass setup-link</code> on the server for a new one.
        </p>
      </AuthFrame>
    );
  }

  return (
    <AuthFrame>
      <div className="mb-6 flex items-center gap-2 text-[12px] font-semibold text-muted">
        {[1, 2].map((s) => (
          <span key={s} className={clsx("flex items-center gap-1.5", step >= s && "text-text")}>
            <span className={clsx("grid size-5 place-items-center rounded-full text-[11px]", step > s ? "bg-[var(--good)] text-white" : step === s ? "bg-text text-bg" : "bg-surface-3")}>
              {step > s ? <Check size={11} strokeWidth={3} /> : s}
            </span>
            {s === 1 ? "Platform operator" : "Platform manager"}
            {s === 1 && <span className="mx-1 h-px w-6 bg-line" />}
          </span>
        ))}
      </div>
      <h2 className="text-[26px] font-bold tracking-tight">{step === 1 ? "Who runs this platform?" : "Create the manager account"}</h2>
      <p className="mt-1 text-[14px] text-text-2">
        {step === 1
          ? "The company operating this ROADPASS installation. Its admins manage organisations, users and billing."
          : "You will create the organisations in the chain and invite their first admins. There is no public sign-up."}
      </p>
      <form className="mt-6 flex flex-col gap-4" onSubmit={async (e) => {
        e.preventDefault();
        if (step === 1) { setStep(2); return; }
        if (mismatch) return;
        setBusy(true); setErr(null);
        try {
          await api.send("POST", "/setup", { token, org, user: { name: user.name, email: user.email, password: user.password } });
          await qc.invalidateQueries({ queryKey: ["me"] });
          nav("/platform?welcome=1", { replace: true });
        } catch (x) {
          setErr(x);
          if (x instanceof ApiError && Array.isArray(x.problem.errors) && (x.problem.errors as { field: string }[]).some((f) => f.field.startsWith("org."))) setStep(1);
        } finally { setBusy(false); }
      }}>
        {step === 1 ? (
          <>
            <div><label className="label" htmlFor="s-org">Company name</label>
              <input id="s-org" className="input h-10" required value={org.name} onChange={(e) => setOrg({ ...org, name: e.target.value })} />
              {fieldErr("org.name") && <p className="mt-1 text-[12px] text-critical">{fieldErr("org.name")}</p>}</div>
            <div className="grid grid-cols-2 gap-3">
              <div><label className="label" htmlFor="s-country">Country</label>
                <select id="s-country" className="input h-10" required value={org.country} onChange={(e) => setOrg({ ...org, country: e.target.value })}>
                  <option value="">Choose…</option>
                  {COUNTRIES.map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}
                </select></div>
              <div><label className="label" htmlFor="s-city">City</label>
                <input id="s-city" className="input h-10" value={org.city} onChange={(e) => setOrg({ ...org, city: e.target.value })} /></div>
            </div>
            <div><label className="label" htmlFor="s-web">Website</label>
              <input id="s-web" className="input h-10" placeholder="https://" value={org.website} onChange={(e) => setOrg({ ...org, website: e.target.value })} /></div>
            <button className="btn btn-primary h-10 justify-center" disabled={!org.name || !org.country}>Continue</button>
          </>
        ) : (
          <>
            <div><label className="label" htmlFor="s-name">Your name</label>
              <input id="s-name" className="input h-10" required value={user.name} onChange={(e) => setUser({ ...user, name: e.target.value })} /></div>
            <div><label className="label" htmlFor="s-email">Work e-mail</label>
              <input id="s-email" type="email" className="input h-10" required autoComplete="username" value={user.email} onChange={(e) => setUser({ ...user, email: e.target.value })} />
              {fieldErr("user.email") && <p className="mt-1 text-[12px] text-critical">{fieldErr("user.email")}</p>}</div>
            <div><label className="label" htmlFor="s-pw">Password</label>
              <input id="s-pw" type="password" className="input h-10" required minLength={12} autoComplete="new-password" value={user.password} onChange={(e) => setUser({ ...user, password: e.target.value })} />
              <p className="mt-1 text-[12px] text-muted">{fieldErr("user.password") ?? "At least 12 characters: this account manages the whole platform."}</p></div>
            <div><label className="label" htmlFor="s-pw2">Repeat password</label>
              <input id="s-pw2" type="password" className="input h-10" required autoComplete="new-password" value={user.confirm} onChange={(e) => setUser({ ...user, confirm: e.target.value })} />
              {mismatch && <p className="mt-1 text-[12px] text-critical">The passwords do not match.</p>}</div>
            {err && !fieldErr("user.email") && !fieldErr("user.password") ? <ErrorBox error={err} /> : null}
            <div className="flex gap-2">
              <button type="button" className="btn h-10" onClick={() => setStep(1)}>Back</button>
              <button className="btn btn-primary h-10 flex-1 justify-center" disabled={busy || mismatch}>
                {busy ? <Loader2 size={16} className="animate-spin" /> : <><ShieldCheck size={15} /> Create the platform</>}
              </button>
            </div>
          </>
        )}
      </form>
    </AuthFrame>
  );
}
