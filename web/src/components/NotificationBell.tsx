import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as Pop from "@radix-ui/react-popover";
import { Bell, BellRing, CheckCheck, Settings2 } from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";
import { ago } from "../lib/format";
import { enablePush, pushState } from "../lib/push";
import { useMe } from "../lib/hooks";
import { canSee } from "../lib/access";

export interface Notification {
  id: number; kind: string; severity: "info" | "warning" | "critical"; title: string; body: string; link?: string; createdAt: string; readAt?: string;
}

const DOT: Record<string, string> = { critical: "var(--critical)", warning: "var(--warning)", info: "var(--accent)" };

/** The bell: unread count (live), the latest notifications, and push opt-in. */
export function NotificationBell() {
  const me = useMe().data;
  const qc = useQueryClient();
  const nav = useNavigate();
  const [open, setOpen] = useState(false);
  const q = useQuery({ queryKey: ["notifications"], queryFn: () => api.get<{ items: Notification[]; unread: number }>("/notifications?limit=30"), refetchInterval: 120_000 });
  const read = useMutation({ mutationFn: (id: number) => api.send("POST", "/notifications/read", { id }), onSuccess: () => qc.invalidateQueries({ queryKey: ["notifications"] }) });
  const [push, setPush] = useState<Awaited<ReturnType<typeof pushState>> | null>(null);
  const [pushErr, setPushErr] = useState<string | null>(null);
  useEffect(() => { if (open) void pushState().then(setPush); }, [open]);
  const unread = q.data?.unread ?? 0;

  return (
    <Pop.Root open={open} onOpenChange={setOpen}>
      <Pop.Trigger className="btn btn-ghost btn-icon relative" aria-label={unread ? `Notifications, ${unread} unread` : "Notifications"}>
        {unread ? <BellRing size={17} /> : <Bell size={17} />}
        {unread > 0 && (
          <span className="num absolute -right-0.5 -top-0.5 grid h-[17px] min-w-[17px] place-items-center rounded-full bg-[var(--critical)] px-1 text-[10px] font-bold text-white ring-2 ring-[var(--bg)]">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </Pop.Trigger>
      <Pop.Portal>
        <Pop.Content align="end" sideOffset={8} className="z-50 w-[380px] max-w-[calc(100vw-24px)] overflow-hidden rounded-2xl bg-[var(--bg-elevated)] shadow-[var(--shadow-pop)]" style={{ animation: "rise .14s ease-out" }}>
          <div className="flex items-center justify-between border-b border-line px-4 py-3">
            <span className="text-[14px] font-semibold">Notifications</span>
            <div className="flex items-center gap-1">
              {unread > 0 && <button className="btn btn-ghost btn-sm" onClick={() => read.mutate(0)}><CheckCheck size={14} /> Mark all read</button>}
              <button className="btn btn-ghost btn-icon" aria-label="Notification settings" onClick={() => { setOpen(false); nav("/settings?tab=notifications"); }}><Settings2 size={15} /></button>
            </div>
          </div>
          {push === "off" && (
            <div className="flex items-center gap-3 border-b border-line bg-accent-soft px-4 py-2.5 text-[12.5px]">
              <span className="flex-1">Get alerts on this computer even when ROADPASS is closed.</span>
              <button className="btn btn-sm btn-primary" onClick={async () => {
                setPushErr(null);
                try { await enablePush(); setPush("on"); } catch (e) { setPushErr((e as Error).message); }
              }}>Turn on</button>
            </div>
          )}
          {pushErr && <div className="border-b border-line px-4 py-2 text-[12px] text-critical">{pushErr}</div>}
          <ul className="max-h-[420px] overflow-y-auto scroll-thin">
            {(q.data?.items ?? []).map((n) => (
              <li key={n.id}>
                <button type="button" onClick={() => { if (!n.readAt) read.mutate(n.id); setOpen(false); if (n.link) nav(n.link); }}
                  className={clsx("flex w-full gap-3 px-4 py-3 text-left transition hover:bg-surface-2", !n.readAt && "bg-[color-mix(in_srgb,var(--accent)_5%,transparent)]")}>
                  <span className="mt-1.5 size-2 shrink-0 rounded-full" style={{ background: n.readAt ? "transparent" : DOT[n.severity] ?? DOT.info, boxShadow: n.readAt ? "inset 0 0 0 1px var(--line-strong)" : undefined }} aria-hidden />
                  <span className="min-w-0 flex-1">
                    <span className={clsx("block text-[13px] leading-snug", !n.readAt && "font-semibold")}>{n.title}</span>
                    {n.body && <span className="mt-0.5 block text-[12.5px] leading-snug text-text-2 line-clamp-2">{n.body}</span>}
                    <span className="mt-1 block text-[11.5px] text-muted">{ago(n.createdAt)}</span>
                  </span>
                </button>
              </li>
            ))}
            {!q.isLoading && !q.data?.items.length && <li className="px-4 py-10 text-center text-[13px] text-muted">You are all caught up.</li>}
          </ul>
          {canSee("alerts", me) && <div className="border-t border-line px-4 py-2.5 text-center">
            <button className="text-[12.5px] font-semibold text-accent-ink hover:underline" onClick={() => { setOpen(false); nav("/alerts"); }}>Open alerts</button>
          </div>}
        </Pop.Content>
      </Pop.Portal>
    </Pop.Root>
  );
}
