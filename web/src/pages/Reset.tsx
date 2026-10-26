import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api } from "../lib/api";
import { ErrorBox } from "../components/ui";
import { AuthFrame } from "./Login";

/** Choose a new password from a one-time reset link. */
export default function Reset() {
  const { token = "" } = useParams();
  const nav = useNavigate();
  const qc = useQueryClient();
  const [pw, setPw] = useState("");
  const [again, setAgain] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const mismatch = again.length > 0 && again !== pw;
  return (
    <AuthFrame>
      <h2 className="text-[26px] font-bold tracking-tight">Choose a new password</h2>
      <p className="mt-1 text-[14px] text-text-2">Your other sessions are signed out when you save.</p>
      <form className="mt-7 flex flex-col gap-4" onSubmit={async (e) => {
        e.preventDefault();
        if (mismatch) return;
        setErr(null); setBusy(true);
        try { await api.send("POST", "/auth/password/reset", { token, password: pw }); await qc.invalidateQueries(); nav("/", { replace: true }); }
        catch (x) { setErr(x); } finally { setBusy(false); }
      }}>
        <div><label className="label" htmlFor="npw">New password</label>
          <input id="npw" className="input h-10" type="password" minLength={12} required autoComplete="new-password" value={pw} onChange={(e) => setPw(e.target.value)} aria-describedby="npw-hint" />
          <p id="npw-hint" className="mt-1 text-[11.5px] text-muted">At least 12 characters.</p></div>
        <div><label className="label" htmlFor="npw2">Repeat it</label>
          <input id="npw2" className="input h-10" type="password" required autoComplete="new-password" value={again} onChange={(e) => setAgain(e.target.value)} aria-invalid={mismatch} />
          {mismatch && <p className="mt-1 text-[12px] text-critical">The two passwords differ.</p>}</div>
        {err ? <ErrorBox error={err} /> : null}
        <button className="btn btn-primary h-10 justify-center" disabled={busy || mismatch}>{busy ? <Loader2 size={16} className="animate-spin" /> : "Save and sign in"}</button>
      </form>
    </AuthFrame>
  );
}
