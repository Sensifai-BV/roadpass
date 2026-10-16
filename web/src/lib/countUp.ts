import { useEffect, useRef, useState } from "react";

/** Animates a number from 0 (or its last value) to `to`. */
export function useCountUp(to: number, ms = 900): number {
  const [v, setV] = useState(to);
  const from = useRef(0);
  useEffect(() => {
    if (!Number.isFinite(to) || window.matchMedia("(prefers-reduced-motion: reduce)").matches) { setV(to); from.current = to; return; }
    const start = performance.now(), a = from.current;
    let raf = 0;
    const tick = (now: number) => {
      const t = Math.min(1, (now - start) / ms);
      const e = 1 - Math.pow(1 - t, 3);
      setV(a + (to - a) * e);
      if (t < 1) raf = requestAnimationFrame(tick);
      else from.current = to;
    };
    raf = requestAnimationFrame(tick);
    return () => { cancelAnimationFrame(raf); from.current = to; };
  }, [to, ms]);
  return v;
}
