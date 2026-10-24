import { useCallback, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api, ApiError } from "../lib/api";
import { useMeta } from "../lib/hooks";
import { roleTitle } from "../lib/roles";
import type { JobRole, OrgKind } from "../lib/types";
import { KIND } from "../lib/vocab";
import { ErrorBox } from "../components/ui";
import { ChainCanvas } from "../components/ChainCanvas";
import { Challenge } from "../components/Turnstile";

interface DemoUser { userId: string; name: string; role: string; duties: string[]; orgId: string; org: string; kind: OrgKind; city: string }

const memberLabel = (u: DemoUser, roles?: Partial<Record<OrgKind, JobRole[]>>) => roleTitle(roles?.[u.kind] ?? [], u.role, u.duties);

export function AuthFrame({ children, side }: { children: React.ReactNode; side?: React.ReactNode }) {
  // On large screens the brand panel is pinned to the viewport and only the form
  // side scrolls; on small screens the panel is hidden and the page scrolls.
  return (
    <div className="grid min-h-full lg:h-dvh lg:grid-cols-[1.05fr_1fr] lg:overflow-hidden">
      <div className="relative hidden h-full min-h-0 flex-col overflow-hidden bg-[#0b0b0c] px-12 pb-10 pt-10 text-white lg:flex">
        <div className="relative z-10 flex items-center gap-2.5">
          <svg width="30" height="30" viewBox="0 0 64 64" aria-hidden>
            <rect width="64" height="64" rx="15" fill="#fff" />
            <path d="M18 46 L28 18 h8 L46 46" fill="none" stroke="#0b0b0c" strokeWidth="5" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M32 24v5m0 6v5" stroke="#93D500" strokeWidth="4" strokeLinecap="round" />
          </svg>
          <span className="text-[16px] font-bold tracking-tight">ROADPASS</span>
        </div>
        <ChainCanvas className="-mx-12 my-4 min-h-[180px] w-[calc(100%+6rem)] flex-1" />
        <div className="relative z-10 max-w-[540px]">
          <h1 className="text-[clamp(30px,3.1vw,44px)] font-bold leading-[1.05] tracking-[-0.035em]">
            Every road knows
            <br />
            <span className="text-[#93D500]">where its plastic came from.</span>
          </h1>
          <p className="mt-4 text-[15px] leading-relaxed text-white/65">
            Digital passports for recycled plastic in asphalt, from sorting plant to road section. Every handover is
            signed by the company that made it and sealed on a blockchain ledger anyone can check.
          </p>
          {side}
        </div>
      </div>
      <div className="flex min-h-0 flex-col lg:overflow-y-auto">
        <div className="m-auto w-full max-w-[420px] px-5 py-12">{children}</div>
      </div>
    </div>
  );
}

export default function Login() {
  const meta = useMeta();
  const [params] = useSearchParams();
  const next = params.get("next") || "/";
  const nav = useNavigate();
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [forgot, setForgot] = useState<"off" | "form" | "sent">("off");
  const [token, setToken] = useState<string | null>(null);
  const [challenge, setChallenge] = useState(0);
  const onToken = useCallback((t: string | null) => setToken(t), []);
  const cfg = useQuery({ queryKey: ["contact-config"], queryFn: () => api.get<{ turnstileSiteKey: string }>("/contact/config"), retry: false, staleTime: 300_000 });
  const siteKey = cfg.data?.turnstileSiteKey ?? "";
  const demo = useQuery({ queryKey: ["demo-users"], queryFn: () => api.get<DemoUser[]>("/auth/demo-users"), retry: false });
  const setup = useQuery({ queryKey: ["setup"], queryFn: () => api.get<{ needed: boolean }>("/setup"), retry: false });

  const groups = useMemo(() => {
    const g = new Map<OrgKind, Map<string, DemoUser[]>>();
    for (const u of demo.data ?? []) {
      const byOrg = g.get(u.kind) ?? new Map<string, DemoUser[]>();
      byOrg.set(u.orgId, [...(byOrg.get(u.orgId) ?? []), u]);
      g.set(u.kind, byOrg);
    }
    const order: OrgKind[] = ["operator", "reporter", "auditor", "road_owner", "contractor", "asphalt_plant", "additive_producer", "recycler", "sorting_plant", "lab"];
    return order.filter((k) => g.has(k)).map((k) => [k, [...g.get(k)!.values()]] as const);
  }, [demo.data]);

  const done = async () => {
    await qc.invalidateQueries({ queryKey: ["me"] });
    nav(next.startsWith("/") ? next : "/", { replace: true });
  };

  return (
    <AuthFrame>
      {setup.data?.needed && (
        <div className="mb-6 rounded-2xl bg-accent-soft px-4 py-3 text-[13px]">
          <b>This installation is not set up yet.</b> Open the setup link from the server's start-up log (or run <code className="mono">roadpass setup-link</code>) to create the platform manager.
        </div>
      )}
      {forgot !== "off" ? (
        <div>
          <h2 className="text-[26px] font-bold tracking-tight">Reset your password</h2>
          {forgot === "sent" ? (
            <p className="mt-3 text-[14px] text-text-2">If an account uses <b>{email}</b>, a reset link is on its way. It works once and expires in an hour. Nothing arrived? Ask your organisation's admin.</p>
          ) : (
            <form className="mt-6 flex flex-col gap-4" onSubmit={async (e) => {
              e.preventDefault(); setErr(null); setBusy("forgot");
              try { await api.send("POST", "/auth/password/forgot", { email }); setForgot("sent"); } catch (x) { setErr(x); } finally { setBusy(null); }
            }}>
              <p className="text-[14px] text-text-2">Enter your work e-mail; we send a one-time link to choose a new password.</p>
              <div><label className="label" htmlFor="femail">Work e-mail</label>
                <input id="femail" className="input h-10" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} /></div>
              {err instanceof ApiError || err instanceof Error ? <ErrorBox error={err} /> : null}
              <button className="btn btn-primary h-10 justify-center" disabled={!!busy}>{busy === "forgot" ? <Loader2 size={16} className="animate-spin" /> : "Send reset link"}</button>
            </form>
          )}
          <button className="mt-5 text-[13px] font-semibold text-accent-ink hover:underline" onClick={() => { setForgot("off"); setErr(null); }}>Back to sign in</button>
        </div>
      ) : <>
      <h2 className="text-[26px] font-bold tracking-tight">Sign in</h2>
      <p className="mt-1 text-[14px] text-text-2">Welcome back. Use your company account.</p>
      <form
        className="mt-7 flex flex-col gap-4"
        onSubmit={async (e) => {
          e.preventDefault();
          setErr(null);
          setBusy("password");
          try {
            await api.send("POST", "/auth/login", { email, password, turnstileToken: token ?? "" });
            await done();
          } catch (x) {
            setErr(x);
            if (siteKey) { setToken(null); setChallenge((n) => n + 1); }
          } finally {
            setBusy(null);
          }
        }}
      >
        <div>
          <label className="label" htmlFor="email">Work e-mail</label>
          <input id="email" className="input h-10" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </div>
        <div>
          <div className="flex items-baseline justify-between"><label className="label" htmlFor="password">Password</label>
            <button type="button" className="text-[12px] font-medium text-accent-ink hover:underline" onClick={() => { setForgot("form"); setErr(null); }}>Forgot password?</button></div>
          <input id="password" className="input h-10" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        {siteKey && <Challenge siteKey={siteKey} onToken={onToken} resetKey={challenge} />}
        {err instanceof ApiError || err instanceof Error ? <ErrorBox error={err} /> : null}
        <button className="btn btn-primary h-10 justify-center" disabled={!!busy || (!!siteKey && !token)}>
          {busy === "password" ? <Loader2 size={16} className="animate-spin" /> : "Sign in"}
        </button>
      </form>
      <p className="mt-5 text-center text-[13px] text-text-2">
        New to ROADPASS? Your organisation's admin or the platform manager sends you an invitation.
      </p>
      </>}

      {demo.data && demo.data.length > 0 && (
        <div className="mt-10">
          <div className="flex items-center gap-3">
            <span className="h-px flex-1 bg-line" />
            <span className="eyebrow">Demo · sign in as</span>
            <span className="h-px flex-1 bg-line" />
          </div>
          <div className="scroll-thin mt-4 max-h-[340px] overflow-y-auto pr-1">
            {groups.map(([kind, orgs]) => {
              const K = KIND[kind];
              return (
                <div key={kind} className="mb-3">
                  <div className="mb-1.5 flex items-center gap-1.5 text-[11.5px] font-semibold text-muted">
                    <K.icon size={12} style={{ color: K.color }} aria-hidden /> {K.plural}
                  </div>
                  <div className="flex flex-col gap-1">
                    {orgs.map((members) => (
                      <div key={members[0].orgId} className="rounded-xl px-3 py-2 hover:bg-surface-2">
                        <div className="truncate text-[13px] font-semibold">{members[0].org}</div>
                        <div className="text-[11.5px] text-muted">{members[0].city}</div>
                        <div className="mt-1.5 flex flex-wrap gap-1">
                          {members.map((u) => (
                            <button
                              key={u.userId}
                              disabled={!!busy}
                              title={`${u.name} · ${memberLabel(u, meta.data?.jobRoles)}`}
                              onClick={async () => {
                                setBusy(u.userId);
                                setErr(null);
                                try {
                                  await api.send("POST", "/auth/demo-login", { userId: u.userId });
                                  await done();
                                } catch (x) {
                                  setErr(x);
                                } finally {
                                  setBusy(null);
                                }
                              }}
                              className="chip cursor-pointer hover:bg-accent-soft hover:text-accent-ink"
                            >
                              {busy === u.userId ? <Loader2 size={11} className="animate-spin" /> : null}
                              {memberLabel(u, meta.data?.jobRoles)}
                            </button>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </AuthFrame>
  );
}
