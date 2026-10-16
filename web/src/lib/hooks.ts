import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient, keepPreviousData } from "@tanstack/react-query";
import { api, qs } from "./api";
import { setTimeZone } from "./format";
import type { Me, Meta, Org, Site } from "./types";

// ---------------------------------------------------------------- session & reference data

export function useMe() {
  return useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      const me = await api.get<Me>("/auth/me");
      setTimeZone(me.user?.timezone ?? "");
      return me;
    },
    staleTime: 60_000,
    retry: false,
  });
}

export function useMeta() {
  return useQuery({ queryKey: ["meta"], queryFn: () => api.get<Meta>("/meta"), staleTime: Infinity });
}

export function useOrgs() {
  return useQuery({ queryKey: ["orgs"], queryFn: () => api.get<Org[]>("/orgs"), staleTime: 5 * 60_000 });
}

export function useSites() {
  return useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites"), staleTime: 5 * 60_000 });
}

/** Lookup helpers for organisation and site names. */
export function useDirectory() {
  const orgs = useOrgs().data;
  const sites = useSites().data;
  return useMemo(() => {
    const o = new Map((orgs ?? []).map((x) => [x.id, x]));
    const s = new Map((sites ?? []).map((x) => [x.id, x]));
    return {
      org: (id?: string) => (id ? o.get(id) : undefined),
      orgName: (id?: string) => (id ? o.get(id)?.name ?? id : "—"),
      site: (id?: string) => (id ? s.get(id) : undefined),
      orgs: orgs ?? [],
      sites: sites ?? [],
      ready: !!orgs && !!sites,
    };
  }, [orgs, sites]);
}

// ---------------------------------------------------------------- time range

export type RangeKey = "7d" | "30d" | "3m" | "12m" | "all";
export interface RangeState {
  key: RangeKey;
  from: string;
  to: string;
  set: (k: RangeKey) => void;
}

// The dataset runs January 2024 – September 2026; "now" is today.
const DATA_START = "2024-01-01";

function compute(k: RangeKey): { from: string; to: string } {
  const to = new Date();
  to.setUTCHours(0, 0, 0, 0);
  to.setUTCDate(to.getUTCDate() + 1);
  const from = new Date(to);
  if (k === "all") return { from: DATA_START, to: to.toISOString().slice(0, 10) };
  if (k === "7d" || k === "30d") from.setUTCDate(from.getUTCDate() - (k === "7d" ? 7 : 30));
  else from.setUTCMonth(from.getUTCMonth() - (k === "3m" ? 3 : 12));
  return { from: from.toISOString().slice(0, 10), to: to.toISOString().slice(0, 10) };
}

export const RangeContext = createContext<RangeState | null>(null);

export function useRangeState(): RangeState {
  const [key, setKey] = useState<RangeKey>(() => {
    try {
      const v = localStorage.getItem("rp:range2") as RangeKey | null;
      if (v && ["7d", "30d", "3m", "12m", "all"].includes(v)) return v;
    } catch {
      /* private window */
    }
    return "30d"; // a short window by default: what is happening now, not the whole year
  });
  const set = useCallback((k: RangeKey) => {
    setKey(k);
    try {
      localStorage.setItem("rp:range2", k);
    } catch {
      /* ignore */
    }
  }, []);
  return useMemo(() => ({ key, ...compute(key), set }), [key, set]);
}

export function useRange(): RangeState {
  const r = useContext(RangeContext);
  if (!r) throw new Error("RangeContext missing");
  return r;
}

/** An analytics query over the global window. */
export function useAnalytics<T>(path: string, extra: Record<string, string | number | undefined> = {}, enabled = true) {
  const r = useRange();
  const q = qs({ from: r.from, to: r.to, ...extra });
  return useQuery({
    queryKey: ["analytics", path, q],
    queryFn: ({ signal }) => api.get<T>(`/analytics/${path}${q}`, signal),
    placeholderData: keepPreviousData,
    staleTime: 30_000,
    enabled,
  });
}

// ---------------------------------------------------------------- theme

export type Theme = "system" | "light" | "dark";

export function useTheme(): [Theme, (t: Theme) => void] {
  const [t, setT] = useState<Theme>(() => {
    try {
      const v = localStorage.getItem("rp:theme");
      return v === "light" || v === "dark" ? v : "system";
    } catch {
      return "system";
    }
  });
  const set = useCallback((v: Theme) => {
    setT(v);
    try {
      if (v === "system") localStorage.removeItem("rp:theme");
      else localStorage.setItem("rp:theme", v);
    } catch {
      /* ignore */
    }
    if (v === "system") delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = v;
  }, []);
  return [t, set];
}

// ---------------------------------------------------------------- live updates

/** Subscribes to server-sent events and refreshes what is on screen. */
export function useLive(enabled: boolean): "live" | "connecting" | "offline" {
  const qc = useQueryClient();
  const [state, setState] = useState<"live" | "connecting" | "offline">("connecting");
  useEffect(() => {
    if (!enabled) return;
    let es: EventSource | null = null;
    let timer: number | undefined;
    const refresh = () => {
      window.clearTimeout(timer);
      // Coalesce bursts: one refetch per second at most.
      timer = window.setTimeout(() => {
        void qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "meta" && q.queryKey[0] !== "me" });
      }, 1000);
    };
    const open = () => {
      es = new EventSource("/api/v1/events");
      es.onopen = () => setState("live");
      es.onerror = () => setState(document.hidden ? "connecting" : "offline");
      for (const kind of ["records", "seal", "finding", "org", "plans"]) es.addEventListener(kind, refresh);
      // A notification for this person: refresh the bell, and the alert lists an alert may have changed.
      es.addEventListener("notification", () => {
        for (const k of ["notifications", "alerts-active", "alert-history", "alert-stats"]) void qc.invalidateQueries({ queryKey: [k] });
      });
    };
    const onVis = () => {
      if (document.hidden) {
        es?.close();
        es = null;
        setState("connecting");
      } else if (!es) {
        open();
        refresh();
      }
    };
    open();
    document.addEventListener("visibilitychange", onVis);
    return () => {
      document.removeEventListener("visibilitychange", onVis);
      window.clearTimeout(timer);
      es?.close();
    };
  }, [enabled, qc]);
  return state;
}

/** True while the page renders dark: the viewer's toggle if set, otherwise the OS. Updates live. */
export function useIsDark(): boolean {
  const read = () => {
    const t = document.documentElement.dataset.theme;
    return t === "dark" || (t !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  };
  const [dark, setDark] = useState(read);
  useEffect(() => {
    const on = () => setDark(read());
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    mq.addEventListener("change", on);
    const mo = new MutationObserver(on);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => { mq.removeEventListener("change", on); mo.disconnect(); };
  }, []);
  return dark;
}
