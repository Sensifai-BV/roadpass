// Browser push: registers the service worker and subscribes this browser with
// the platform's VAPID key, so alerts arrive while ROADPASS is closed.
import { api } from "./api";

export const pushSupported = () => typeof window !== "undefined" && "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;

function keyBytes(b64url: string): Uint8Array {
  const pad = "=".repeat((4 - (b64url.length % 4)) % 4);
  const raw = atob((b64url + pad).replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}

/** Current state of this browser: unsupported, blocked, off or on. */
export async function pushState(): Promise<"unsupported" | "blocked" | "off" | "on"> {
  if (!pushSupported()) return "unsupported";
  if (Notification.permission === "denied") return "blocked";
  const reg = await navigator.serviceWorker.getRegistration("/");
  const sub = await reg?.pushManager.getSubscription();
  return sub ? "on" : "off";
}

/** Asks for permission and subscribes this browser. */
export async function enablePush(): Promise<void> {
  if (!pushSupported()) throw new Error("This browser cannot receive push notifications.");
  const perm = await Notification.requestPermission();
  if (perm !== "granted") throw new Error("Notifications are blocked for this site. Allow them in the browser's site settings.");
  const reg = await navigator.serviceWorker.register("/sw.js", { scope: "/" });
  await navigator.serviceWorker.ready;
  const { publicKey } = await api.get<{ publicKey: string }>("/notifications/push/key");
  const sub = (await reg.pushManager.getSubscription())
    ?? (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(publicKey) as BufferSource }));
  const { endpoint, keys } = sub.toJSON();
  await api.send("POST", "/notifications/push", { endpoint, keys });
}

/** Unsubscribes this browser. */
export async function disablePush(): Promise<void> {
  const reg = await navigator.serviceWorker.getRegistration("/");
  const sub = await reg?.pushManager.getSubscription();
  if (!sub) return;
  await api.send("POST", "/notifications/push/remove", { endpoint: sub.endpoint });
  await sub.unsubscribe();
}
