import { useCallback, useEffect, useRef, useState } from "react";
import { useIsDark } from "../lib/hooks";
import { ErrorBox } from "./ui";

interface TurnstileApi { render: (el: HTMLElement, o: Record<string, unknown>) => string; reset: (id?: string) => void; remove: (id?: string) => void }
declare global { interface Window { turnstile?: TurnstileApi } }

let turnstileScript: Promise<void> | null = null;
/** Loads Cloudflare's Turnstile script once. */
function loadTurnstile(): Promise<void> {
  if (window.turnstile) return Promise.resolve();
  turnstileScript ??= new Promise<void>((resolve, reject) => {
    const el = document.createElement("script");
    el.src = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
    el.async = true; el.onload = () => resolve(); el.onerror = () => { turnstileScript = null; reject(new Error("The spam check could not be loaded.")); };
    document.head.appendChild(el);
  });
  return turnstileScript;
}

/** The Turnstile challenge: reports a token when solved and null when it expires or fails. */
export function Challenge({ siteKey, onToken, resetKey }: { siteKey: string; onToken: (t: string | null) => void; resetKey: number }) {
  const box = useRef<HTMLDivElement>(null);
  const id = useRef<string | undefined>(undefined);
  const dark = useIsDark();
  const [failed, setFailed] = useState<string | null>(null);
  const cb = useCallback(onToken, [onToken]);
  useEffect(() => {
    let gone = false;
    loadTurnstile().then(() => {
      if (gone || !box.current || !window.turnstile) return;
      id.current = window.turnstile.render(box.current, {
        sitekey: siteKey, theme: dark ? "dark" : "light", language: "auto",
        callback: (t: string) => cb(t), "expired-callback": () => cb(null), "error-callback": () => cb(null),
      });
    }).catch((e: Error) => setFailed(e.message));
    return () => { gone = true; if (id.current) window.turnstile?.remove(id.current); id.current = undefined; };
  }, [siteKey, dark, cb]);
  useEffect(() => { if (resetKey > 0 && id.current) window.turnstile?.reset(id.current); }, [resetKey]);
  return failed ? <ErrorBox error={new Error(failed)} /> : <div ref={box} className="min-h-[65px]" aria-label="Spam check" />;
}
