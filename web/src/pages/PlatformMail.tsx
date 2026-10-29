import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Loader2, Mail, RotateCcw, Send } from "lucide-react";
import { api, ApiError } from "../lib/api";
import { useMe } from "../lib/hooks";
import { Card, ErrorBox, Skel } from "../components/ui";

interface MailSettings {
  host: string; port: string; tls: "starttls" | "tls" | "none"; username: string; from: string; replyTo?: string;
  passwordSet: boolean; source: "platform" | "environment" | ""; envHost?: string;
}

/**
 * The platform's outgoing mail server. Every system e-mail goes through it:
 * invitations, password resets, alerts and digests, scheduled reports and invoices.
 */
export function MailTab() {
  const me = useMe().data!;
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["platform-mail"], queryFn: () => api.get<MailSettings>("/platform/mail") });
  const [f, setF] = useState<MailSettings | null>(null);
  const [password, setPassword] = useState("");
  const [to, setTo] = useState(me.user.email);
  useEffect(() => { if (q.data && !f) setF(q.data); }, [q.data, f]);
  const save = useMutation({
    mutationFn: () => api.send<MailSettings>("PUT", "/platform/mail", { host: f!.host, port: f!.port, tls: f!.tls, username: f!.username, from: f!.from, replyTo: f!.replyTo ?? "", password }),
    onSuccess: (d) => { setF(d); setPassword(""); qc.setQueryData(["platform-mail"], d); },
  });
  const reset = useMutation({
    mutationFn: () => api.send<MailSettings>("DELETE", "/platform/mail"),
    onSuccess: (d) => { setF(d); qc.setQueryData(["platform-mail"], d); },
  });
  const test = useMutation({ mutationFn: () => api.send<{ sentTo: string }>("POST", "/platform/mail/test", { to }) });
  if (q.error) return <ErrorBox error={q.error} />;
  if (!f || !q.data) return <Skel h={360} />;
  const set = (k: keyof MailSettings, v: string) => setF({ ...f, [k]: v });
  const err = (name: string) => {
    const e = save.error;
    if (!(e instanceof ApiError) || !Array.isArray(e.problem.errors)) return undefined;
    return (e.problem.errors as { field: string; message: string }[]).find((x) => x.field === name)?.message;
  };
  const src = q.data.source;
  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_340px]">
      <Card title="Outgoing mail server" sub="Invitations, password resets, alerts, scheduled reports and invoices are sent through it.">
        <div className="mb-4 flex items-center gap-2 rounded-xl bg-surface-2 px-3 py-2 text-[12.5px]">
          <Mail size={14} className="text-muted" />
          {src === "platform" ? <span>In use: the settings below.</span>
            : src === "environment" ? <span>In use: the server's environment default ({q.data.envHost}). Saving here replaces it.</span>
              : <span className="text-warning">No mail server: invitations show a link to copy, and nothing is e-mailed.</span>}
        </div>
        <form className="grid gap-3 sm:grid-cols-2" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
          <label className="block sm:col-span-2"><span className="label">Host</span>
            <input className="input" required value={f.host} placeholder="smtp.example.com" onChange={(e) => set("host", e.target.value)} />
            {err("host") && <p className="mt-1 text-[12px] text-critical">{err("host")}</p>}</label>
          <label className="block"><span className="label">Security</span>
            <select className="input" value={f.tls} onChange={(e) => set("tls", e.target.value)}>
              <option value="starttls">STARTTLS (port 587)</option><option value="tls">TLS (port 465)</option><option value="none">None (local relay only)</option>
            </select></label>
          <label className="block"><span className="label">Port <span className="font-normal text-muted">· optional</span></span>
            <input className="input num" inputMode="numeric" value={f.port} placeholder={f.tls === "tls" ? "465" : f.tls === "none" ? "25" : "587"} onChange={(e) => set("port", e.target.value)} />
            {err("port") && <p className="mt-1 text-[12px] text-critical">{err("port")}</p>}</label>
          <label className="block"><span className="label">Username <span className="font-normal text-muted">· optional</span></span>
            <input className="input" autoComplete="off" value={f.username} onChange={(e) => set("username", e.target.value)} /></label>
          <label className="block"><span className="label">Password</span>
            <input className="input" type="password" autoComplete="new-password" value={password} placeholder={q.data.passwordSet ? "Saved · leave empty to keep" : ""} onChange={(e) => setPassword(e.target.value)} />
            <p className="mt-1 text-[11.5px] text-muted">Kept in the secrets store, never shown again.</p></label>
          <label className="block"><span className="label">Sender</span>
            <input className="input" required value={f.from} placeholder="ROADPASS <no-reply@example.com>" onChange={(e) => set("from", e.target.value)} />
            {err("from") && <p className="mt-1 text-[12px] text-critical">{err("from")}</p>}</label>
          <label className="block"><span className="label">Reply-to <span className="font-normal text-muted">· optional</span></span>
            <input className="input" value={f.replyTo ?? ""} placeholder="support@example.com" onChange={(e) => set("replyTo", e.target.value)} />
            {err("replyTo") && <p className="mt-1 text-[12px] text-critical">{err("replyTo")}</p>}</label>
          {save.error && !(save.error instanceof ApiError && Array.isArray(save.error.problem.errors)) ? <div className="sm:col-span-2"><ErrorBox error={save.error} /></div> : null}
          <div className="flex flex-wrap items-center justify-end gap-2 sm:col-span-2">
            {save.isSuccess && <span className="mr-auto flex items-center gap-1 text-[12.5px] text-good"><CheckCircle2 size={14} /> Saved</span>}
            {src === "platform" && <button type="button" className="btn" disabled={reset.isPending} onClick={() => reset.mutate()}><RotateCcw size={14} /> Use the environment default</button>}
            <button className="btn btn-primary" disabled={save.isPending}>{save.isPending ? <Loader2 size={14} className="animate-spin" /> : null} Save</button>
          </div>
        </form>
      </Card>
      <Card title="Send a test" sub="Uses the settings in force now.">
        <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); test.mutate(); }}>
          <input className="input" type="email" required value={to} onChange={(e) => setTo(e.target.value)} aria-label="Send the test to" />
          <button className="btn btn-primary justify-center" disabled={test.isPending || !src}>{test.isPending ? <Loader2 size={14} className="animate-spin" /> : <Send size={14} />} Send test e-mail</button>
          {test.data && <p className="flex items-center gap-1 text-[12.5px] text-good"><CheckCircle2 size={14} /> Sent to {test.data.sentTo}.</p>}
          {test.error ? <ErrorBox error={test.error} /> : null}
        </form>
      </Card>
    </div>
  );
}
