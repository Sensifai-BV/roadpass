/** The 3D hero needs WebGL2, no reduced-motion preference, and a screen big enough to be worth the three.js download. */
export function canWebGL() {
  if (typeof window === "undefined") return false;
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return false;
  if (window.matchMedia("(max-width: 767px)").matches) return false;
  try { return !!document.createElement("canvas").getContext("webgl2"); } catch { return false; }
}
