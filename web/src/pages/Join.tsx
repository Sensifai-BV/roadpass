import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api } from "../lib/api";
import { ErrorBox } from "../components/ui";
import { AuthFrame } from "./Login";

export default function Join() {
  const { token = "" } = useParams();
  const nav = useNavigate();
  const qc = useQueryClient();
  const info = useQuery({
    queryKey: ["invitation", token],
    queryFn: () => api.get<{ email: string; role: string; title?: string; org: { name: string } }>(`/auth/invitations/${token}`),
    retry: false,
  });
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  return (
    <AuthFrame>
      {info.isError ? (
        <>
          <h2 className="text-[26px] font-bold tracking-tight">Invitation not valid</h2>
          <p className="mt-2 text-text-2">It may have expired or been used already. Ask your organisation's admin for a new link.</p>
        </>
      ) : (
        <>
          <h2 className="text-[26px] font-bold tracking-tight">Join {info.data?.org.name ?? "your team"}</h2>
          <p className="mt-1 text-[14px] text-text-2">
            You were invited as <b>{info.data?.title ?? info.data?.role}</b> with <b>{info.data?.email}</b>.
          </p>
          <form
            className="mt-6 flex flex-col gap-4"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              setErr(null);
              try {
                await api.send("POST", "/auth/invitations/accept", { token, name, password });
                await qc.invalidateQueries({ queryKey: ["me"] });
                nav("/", { replace: true });
              } catch (x) {
                setErr(x);
              } finally {
                setBusy(false);
              }
            }}
          >
            <div>
              <label className="label" htmlFor="n">Your name</label>
              <input id="n" className="input h-10" required value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div>
              <label className="label" htmlFor="p">Choose a password</label>
              <input id="p" type="password" minLength={12} className="input h-10" required autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} aria-describedby="p-hint" />
              <p id="p-hint" className="mt-1 text-[11.5px] text-muted">At least 12 characters. A password manager helps.</p>
            </div>
            {err ? <ErrorBox error={err} /> : null}
            <button className="btn btn-primary h-10 justify-center" disabled={busy || !info.data}>
              {busy ? <Loader2 size={16} className="animate-spin" /> : "Join"}
            </button>
          </form>
        </>
      )}
    </AuthFrame>
  );
}
