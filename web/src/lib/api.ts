// A thin, typed client. Writes send X-Requested-With (the server's CSRF check for
// cookie sessions) and always JSON, except CSV uploads.

export interface Problem {
  title: string;
  status: number;
  detail?: string;
  instance?: string;
  errors?: unknown;
}

export class ApiError extends Error {
  status: number;
  problem: Problem;
  constructor(p: Problem) {
    super(p.detail ? `${p.title}: ${p.detail}` : p.title);
    this.status = p.status;
    this.problem = p;
  }
}

async function handle<T>(res: Response): Promise<T> {
  if (res.status === 204) return undefined as T;
  const ct = res.headers.get("content-type") ?? "";
  if (!res.ok) {
    let p: Problem = { title: res.statusText || "Request failed", status: res.status };
    if (ct.includes("json")) {
      try {
        p = { ...p, ...(await res.json()) };
      } catch {
        /* keep the status line */
      }
    }
    // "/" (the public landing page for visitors) and /contact are public too.
    if (res.status === 401 && location.pathname !== "/" && !location.pathname.startsWith("/contact") && !location.pathname.startsWith("/login") && !location.pathname.startsWith("/p/")
      && !location.pathname.startsWith("/setup") && !location.pathname.startsWith("/join") && !location.pathname.startsWith("/verify") && !location.pathname.startsWith("/explorer")) {
      const back = encodeURIComponent(location.pathname + location.search);
      location.assign(`/login?next=${back}`);
    }
    throw new ApiError(p);
  }
  if (ct.includes("json")) return (await res.json()) as T;
  return (await res.text()) as unknown as T;
}

export function qs(params: Record<string, string | number | undefined | null | boolean>): string {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== "" && v !== false) u.set(k, String(v));
  }
  const s = u.toString();
  return s ? `?${s}` : "";
}

export const api = {
  get: <T>(path: string, signal?: AbortSignal) =>
    fetch(`/api/v1${path}`, { credentials: "same-origin", signal }).then((r) => handle<T>(r)),
  send: <T>(method: "POST" | "PUT" | "DELETE" | "PATCH", path: string, body?: unknown, signal?: AbortSignal) =>
    fetch(`/api/v1${path}`, {
      method,
      signal,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-Requested-With": "roadpass" },
      body: body === undefined ? undefined : JSON.stringify(body),
    }).then((r) => handle<T>(r)),
  /** Sends a file's bytes as the request body (logos). */
  putBytes: <T>(path: string, file: Blob) =>
    fetch(`/api/v1${path}`, { method: "PUT", credentials: "same-origin", headers: { "Content-Type": file.type || "application/octet-stream", "X-Requested-With": "roadpass" }, body: file })
      .then((r) => handle<T>(r)),
  upload: <T>(path: string, file: File) => {
    const fd = new FormData();
    fd.set("file", file);
    return fetch(`/api/v1${path}`, {
      method: "POST",
      credentials: "same-origin",
      headers: { "X-Requested-With": "roadpass" },
      body: fd,
    }).then((r) => handle<T>(r));
  },
};
