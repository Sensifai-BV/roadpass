import { useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { Check } from "lucide-react";
import { useIsDark } from "../lib/hooks";
import * as THREE from "three";

export interface Chapter { shot: string; alt: string; step: string; sub: string; title: string; text: string; points: string[]; caption: string; color: string }

const STAGES = 5; // scene stages: chain, checks, ledger, proof, orbit
// Which scene stage each chapter shows; fractions blend two stages.
const SCENE_AT = [0, 0.5, 1, 2, 3, 4];
const STAGE = ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181", "#1f9d3a"];
const clamp01 = (x: number) => Math.min(1, Math.max(0, x));
const smooth = (x: number) => { const t = clamp01(x); return t * t * (3 - 2 * t); };
/** 1 when p is on chapter c, falling to 0 one chapter away. */
const bump = (p: number, c: number) => smooth(1 - Math.abs(p - c));

/** Where each of the six stage nodes sits in each chapter: the chain, the chain, a feed column, a row above the proof, an orbit. */
function layout(i: number, k: number, t: number): [number, number, number, number] {
  switch (k) {
    case 0: case 1: return [-3.3 + i * 1.32, Math.sin(i * 1.1) * 0.9, Math.cos(i * 1.1) * 1.0, 1];
    case 2: return [-2.6, 1.9 - i * 0.76, 0, 0.8];
    case 3: return [-2.5 + i * 1.0, 2.25, 0, 0.5];
    default: { const a = (i / 6) * Math.PI * 2 + t * 0.35; return [Math.cos(a) * 2.5, Math.sin(a) * 2.0, Math.sin(a) * 1.1, 0.9]; }
  }
}

/**
 * The product tour: chapters scroll on one side while a three.js scene, pinned on
 * the other, changes with them: the chain of organisations, a check ring on every
 * handover, ledger blocks, a Merkle proof, and the systems orbiting the core.
 * Without WebGL, or with reduced motion, the chapters stand alone.
 */
export function FeatureTour({ chapters }: { chapters: Chapter[] }) {
  const n = chapters.length;
  const dark = useIsDark();
  const LIME = dark ? "#93D500" : "#4f8f00";
  const ink = dark ? 0xffffff : 0x4b5563;
  const sec = useRef<HTMLElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const mount = useRef<HTMLDivElement>(null);
  const prog = useRef(0);
  const [active, setActive] = useState(0);
  const plain = useMemo(() => {
    if (typeof window === "undefined") return true;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return true;
    try { return !document.createElement("canvas").getContext("webgl2"); } catch { return true; }
  }, []);

  // One progress value for the whole tour, in chapters: 0 … n-1.
  useEffect(() => {
    const on = () => {
      const el = list.current;
      if (!el) return;
      const r = el.getBoundingClientRect();
      const ch = r.height / n;
      const p = Math.min(n - 1, Math.max(0, (window.innerHeight * 0.5 - r.top) / ch - 0.5));
      prog.current = p;
      setActive((a) => (a === Math.round(p) ? a : Math.round(p)));
    };
    on();
    window.addEventListener("scroll", on, { passive: true });
    window.addEventListener("resize", on);
    return () => { window.removeEventListener("scroll", on); window.removeEventListener("resize", on); };
  }, [n]);

  useEffect(() => {
    if (plain) return;
    const host = mount.current, root = sec.current;
    if (!host || !root) return;

    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: "high-performance" });
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    host.appendChild(renderer.domElement);
    renderer.domElement.style.cssText = "display:block;width:100%;height:100%";
    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(34, 1, 0.1, 60);
    const rig = new THREE.Group();
    scene.add(rig);
    scene.add(new THREE.AmbientLight(0xffffff, 0.9));
    const sun = new THREE.DirectionalLight(0xffffff, 2.4); sun.position.set(3, 5, 6); scene.add(sun);
    const rim = new THREE.PointLight(new THREE.Color(LIME).getHex(), 40, 0, 2); rim.position.set(-4, -3, 4); scene.add(rim);

    // Everything that fades registers its materials here with a base opacity.
    const fades: { m: THREE.Material & { opacity: number }; base: number; group: number }[] = [];
    const track = <T extends THREE.Material>(m: T, base: number, group: number): T => {
      m.transparent = true; fades.push({ m: m as T & { opacity: number }, base, group }); return m;
    };
    const G_CHAIN = 0, G_RING = 1, G_LEDGER = 2, G_MERKLE = 3, G_CORE = 4;

    // --- the six stage nodes, their ring, and the line through them
    const nodes = STAGE.map((c, i) => {
      const g = new THREE.Group();
      const body = new THREE.Mesh(new THREE.SphereGeometry(0.24, 32, 24), track(new THREE.MeshStandardMaterial({ color: c, emissive: c, emissiveIntensity: 0.55, roughness: 0.35, metalness: 0.2 }), 1, G_CHAIN));
      const ringColor = i === 3 ? "#fab219" : LIME;
      const ring = new THREE.Mesh(new THREE.TorusGeometry(0.46, 0.02, 12, 56), track(new THREE.MeshBasicMaterial({ color: ringColor }), 0.95, G_RING));
      g.add(body, ring);
      rig.add(g);
      return { g, ring };
    });
    const lineGeo = new THREE.BufferGeometry().setFromPoints(nodes.map(() => new THREE.Vector3()));
    rig.add(new THREE.Line(lineGeo, track(new THREE.LineBasicMaterial({ color: ink }), 0.28, G_CHAIN)));

    // --- material flowing along the chain
    const COUNT = 140;
    const pGeo = new THREE.BufferGeometry();
    const pPos = new Float32Array(COUNT * 3);
    pGeo.setAttribute("position", new THREE.BufferAttribute(pPos, 3));
    const pOff = Array.from({ length: COUNT }, (_, i) => ({ t: i / COUNT, s: 0.05 + ((i * 7) % 10) / 160, j: ((i * 13) % 17) / 17 - 0.5 }));
    rig.add(new THREE.Points(pGeo, track(new THREE.PointsMaterial({ color: ink, size: 0.055, sizeAttenuation: true, depthWrite: false }), 0.85, G_CHAIN)));

    // --- ledger: blocks stacking up, each one a sealed batch
    const ledger = new THREE.Group(); ledger.position.set(2.5, 0, 0); rig.add(ledger);
    const blocks = Array.from({ length: 7 }, (_, i) => {
      const geo = new THREE.BoxGeometry(1.7, 0.3, 1.1);
      const b = new THREE.Mesh(geo, track(new THREE.MeshStandardMaterial({ color: dark ? "#1b2410" : "#d9e6c0", emissive: LIME, emissiveIntensity: 0.18, roughness: 0.4, metalness: 0.5 }), 0.95, G_LEDGER));
      const e = new THREE.LineSegments(new THREE.EdgesGeometry(geo), track(new THREE.LineBasicMaterial({ color: LIME }), 0.9, G_LEDGER));
      b.add(e); ledger.add(b);
      return { b, i };
    });

    // --- Merkle tree over eight records, one leaf's proof path lit
    const merkle = new THREE.Group(); merkle.position.set(0, -0.5, 0); rig.add(merkle);
    const tree: THREE.Vector3[][] = [];
    for (let lvl = 0, cnt = 8; cnt >= 1; lvl++, cnt /= 2) {
      tree.push(Array.from({ length: cnt }, (_, k) => new THREE.Vector3((k + 0.5) * (8 / cnt) - 4, -1.5 + lvl * 1.15, 0)).map((v) => v.multiplyScalar(0.7)));
    }
    const leaf = 5, path = [leaf, leaf >> 1, leaf >> 2, leaf >> 3];
    const lit = (lvl: number, k: number) => path[lvl] === k;
    tree.forEach((row, lvl) => row.forEach((v, k) => {
      const on = lit(lvl, k);
      const s = new THREE.Mesh(new THREE.SphereGeometry(on ? 0.17 : 0.12, 20, 16), track(new THREE.MeshStandardMaterial({ color: on ? LIME : "#6b7280", emissive: on ? LIME : "#222", emissiveIntensity: on ? 0.9 : 0.3 }), 1, G_MERKLE));
      s.position.copy(v); merkle.add(s);
      if (lvl > 0) for (const c of [k * 2, k * 2 + 1]) {
        const w = tree[lvl - 1][c], hot = on && lit(lvl - 1, c);
        merkle.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints([v, w]), track(new THREE.LineBasicMaterial({ color: hot ? LIME : 0x888888 }), hot ? 1 : 0.4, G_MERKLE)));
      }
    }));

    // --- core: the platform at the centre of what it connects to
    const core = new THREE.Group(); rig.add(core);
    core.add(new THREE.Mesh(new THREE.IcosahedronGeometry(0.8, 1), track(new THREE.MeshStandardMaterial({ color: dark ? "#0f1a05" : "#cfe3a8", emissive: LIME, emissiveIntensity: 0.35, flatShading: true, roughness: 0.4 }), 1, G_CORE)));
    core.add(new THREE.LineSegments(new THREE.EdgesGeometry(new THREE.IcosahedronGeometry(1.15, 1)), track(new THREE.LineBasicMaterial({ color: LIME }), 0.55, G_CORE)));

    const size = () => {
      const w = host.clientWidth, h = host.clientHeight;
      renderer.setSize(w, h, false);
      camera.aspect = w / h;
      // Keep the widest layout (the chain, about 8.5 units across) inside the canvas at any shape.
      camera.position.set(0, 0.2, Math.min(30, Math.max(10.5, 14 / camera.aspect)));
      camera.lookAt(0, 0, 0);
      camera.updateProjectionMatrix();
    };
    size();
    const ro = new ResizeObserver(size); ro.observe(host);
    const ptr = { x: 0, y: 0 };
    const onMove = (e: PointerEvent) => { ptr.x = (e.clientX / window.innerWidth - 0.5) * 2; ptr.y = (e.clientY / window.innerHeight - 0.5) * 2; };
    window.addEventListener("pointermove", onMove, { passive: true });

    let visible = true, raf = 0, p = prog.current;
    const io = new IntersectionObserver(([e]) => { visible = e.isIntersecting; }, { rootMargin: "80px" });
    io.observe(root);
    const clock = new THREE.Clock();
    const pts = nodes.map(() => new THREE.Vector3());
    const pos = new THREE.Vector3();

    const tick = () => {
      raf = requestAnimationFrame(tick);
      if (!visible) return;
      const t = clock.getElapsedTime();
      p += (prog.current - p) * 0.08;
      const ci = Math.min(n - 2, Math.floor(p)), cf = p - ci;
      const sp = SCENE_AT[ci] + (SCENE_AT[ci + 1] - SCENE_AT[ci]) * cf; // scene stage position

      const k = Math.min(STAGES - 2, Math.floor(sp)), u = smooth(sp - k);
      nodes.forEach(({ g, ring }, i) => {
        const a = layout(i, k, t), b = layout(i, Math.min(k + 1, n - 1), t);
        g.position.set(a[0] + (b[0] - a[0]) * u, a[1] + (b[1] - a[1]) * u, a[2] + (b[2] - a[2]) * u);
        g.scale.setScalar(a[3] + (b[3] - a[3]) * u);
        ring.rotation.x = t * 0.9 + i; ring.rotation.y = t * 0.6;
        ring.scale.setScalar(1 + Math.sin(t * 2.4 + i * 0.9) * 0.06);
        pts[i].copy(g.position);
      });
      const arr = lineGeo.attributes.position as THREE.BufferAttribute;
      pts.forEach((v, i) => arr.setXYZ(i, v.x, v.y, v.z));
      arr.needsUpdate = true;
      const curve = new THREE.CatmullRomCurve3(pts, false, "catmullrom", 0.4);
      pOff.forEach((o, i) => {
        o.t = (o.t + o.s * 0.0045 * (1 + sp * 0.1)) % 1;
        curve.getPoint(o.t, pos);
        pPos[i * 3] = pos.x + o.j * 0.18; pPos[i * 3 + 1] = pos.y + Math.sin(t * 2 + i) * 0.06 + o.j * 0.12; pPos[i * 3 + 2] = pos.z + o.j * 0.14;
      });
      pGeo.attributes.position.needsUpdate = true;

      blocks.forEach(({ b, i }) => {
        const w = smooth(bump(sp, 2) * 1.6 - i * 0.12);
        b.position.set(0, -1.15 + i * 0.4 + (1 - w) * 2.2, 0);
        b.rotation.y = Math.sin(t * 0.5 + i * 0.4) * 0.06 + (1 - w) * 0.6;
        b.scale.setScalar(0.4 + w * 0.6);
      });
      core.rotation.y = t * 0.3; core.rotation.x = t * 0.18;
      core.scale.setScalar(0.6 + bump(sp, 4) * 0.4 + Math.sin(t * 1.6) * 0.02);

      const weights = [1 - bump(sp, 3) * 0.55, bump(sp, 1) * 0.9, bump(sp, 2), bump(sp, 3), bump(sp, 4)];
      weights[G_CHAIN] = clamp01(weights[G_CHAIN] * (1 - bump(sp, 4) * 0.15));
      for (const f of fades) f.m.opacity = f.base * weights[f.group];
      ledger.visible = weights[G_LEDGER] > 0.01; merkle.visible = weights[G_MERKLE] > 0.01; core.visible = weights[G_CORE] > 0.01;

      rig.rotation.y += ((Math.sin(sp * 0.9) * 0.25 + ptr.x * 0.12) - rig.rotation.y) * 0.05;
      rig.rotation.x += ((ptr.y * -0.07) - rig.rotation.x) * 0.05;
      renderer.render(scene, camera);
    };
    tick();

    return () => {
      cancelAnimationFrame(raf); io.disconnect(); ro.disconnect(); window.removeEventListener("pointermove", onMove);
      scene.traverse((o) => { const m = o as THREE.Mesh; m.geometry?.dispose?.(); const mt = m.material as THREE.Material | THREE.Material[] | undefined; if (Array.isArray(mt)) mt.forEach((x) => x.dispose()); else mt?.dispose(); });
      renderer.dispose(); renderer.domElement.remove();
    };
  }, [plain, n, dark]);

  const jump = (i: number) => {
    const el = list.current; if (!el) return;
    window.scrollTo({ top: window.scrollY + el.getBoundingClientRect().top + (el.offsetHeight / n) * (i + 0.5) - window.innerHeight * 0.5 + 2, behavior: "smooth" });
  };

  const card = (c: Chapter, i: number) => (
    <article key={c.step} className={clsx("flex items-center py-6 transition-opacity duration-500 sm:py-10 lg:min-h-[88vh]", !plain && i !== active && "lg:opacity-30")} aria-label={c.title}>
      <div className="w-full min-w-0 rounded-3xl bg-surface p-5 ring-1 ring-line sm:p-8 lg:bg-transparent lg:p-0 lg:ring-0">
        <img src={`/tour/${c.shot}-${dark ? "dark" : "light"}.jpg`} alt={c.alt} loading="lazy" decoding="async" width={1440} height={900} className="mb-6 aspect-[1440/900] w-full rounded-xl object-cover object-top ring-1 ring-line lg:hidden" />
        <div className="flex items-center gap-3">
          <span className="num grid h-9 w-9 place-items-center rounded-xl text-[13px] font-bold text-[#0b0b0c]" style={{ background: c.color }}>{c.step}</span>
          <span className="text-[12px] font-semibold uppercase tracking-[0.1em]" style={{ color: c.color }}>{c.sub}</span>
        </div>
        <h3 className="mt-5 text-[clamp(26px,3.1vw,40px)] font-bold leading-[1.06] tracking-[-0.03em]">{c.title}</h3>
        <p className="mt-4 max-w-[480px] text-[15.5px] sm:text-[16px] leading-relaxed text-text-2">{c.text}</p>
        <ul className="mt-6 space-y-3">
          {c.points.map((t) => <li key={t} className="flex gap-3 text-[14.5px] text-text"><Check size={17} className="mt-0.5 shrink-0" style={{ color: c.color }} />{t}</li>)}
        </ul>
      </div>
    </article>
  );

  return (
    <section ref={sec} id="tour" className="mk-section relative overflow-x-clip bg-bg text-text">
      <div className="mk-glow pointer-events-none absolute inset-x-0 top-0 h-[420px] opacity-60" aria-hidden />
      <div className="relative mx-auto max-w-[1180px] px-5 pt-16 sm:pt-32">
        <div className="max-w-[720px]">
          <div className="eyebrow mb-3 !text-accent-ink">Product tour</div>
          <h2 className="mk-h2">From a handover to proof.</h2>
          <p className="mt-4 text-[clamp(15px,1.4vw,18px)] leading-relaxed text-text-2">Scroll to follow one record on its way through ROADPASS.</p>
        </div>
        <div className="mt-6 grid lg:grid-cols-[1fr_1.1fr] lg:gap-10">
          <div className="sticky top-0 z-0 mx-0 hidden h-dvh lg:order-2 lg:block">
            {!plain && <div ref={mount} className="absolute inset-0 opacity-60" aria-hidden />}
            <div className="absolute inset-0 grid place-items-center px-5 lg:px-0" style={{ perspective: "1600px" }}>
              <div className="relative w-full max-w-[640px] lg:w-[122%] lg:max-w-none lg:-translate-x-[6%]" style={{ transform: "rotateY(-9deg) rotateX(3deg)", transformStyle: "preserve-3d" }}>
                <div className="absolute -inset-6 rounded-[36px] opacity-60 blur-3xl transition-colors duration-700" style={{ background: chapters[active]?.color, opacity: 0.18 }} aria-hidden />
                <div className="relative overflow-hidden rounded-2xl bg-surface shadow-[0_40px_90px_-30px_rgba(0,0,0,.6)] ring-1 ring-line-strong">
                  <div className="flex items-center gap-1.5 border-b border-line bg-surface-2 px-3.5 py-2.5">
                    <span className="h-2.5 w-2.5 rounded-full bg-[var(--line-strong)]" /><span className="h-2.5 w-2.5 rounded-full bg-[var(--line-strong)]" /><span className="h-2.5 w-2.5 rounded-full bg-[var(--line-strong)]" />
                    <span className="mx-auto rounded-md bg-surface-3 px-3 py-0.5 text-[11px] text-muted">{chapters[active]?.sub} · ROADPASS</span>
                    <span className="w-10" />
                  </div>
                  <div className="relative aspect-[1440/900] bg-surface-2">
                    {chapters.map((c, i) => (
                      <img key={c.shot} src={`/tour/${c.shot}-${dark ? "dark" : "light"}.jpg`} alt={i === active ? c.alt : ""} aria-hidden={i !== active} loading={i < 2 ? "eager" : "lazy"} decoding="async"
                        className={clsx("absolute inset-0 h-full w-full object-cover object-top transition-all duration-700", i === active ? "scale-100 opacity-100" : i < active ? "-translate-y-3 scale-[1.03] opacity-0" : "translate-y-3 scale-[0.98] opacity-0")} />
                    ))}
                  </div>
                </div>
              </div>
            </div>
            <div className="pointer-events-none absolute inset-x-5 bottom-2 flex flex-col items-start gap-3 lg:inset-x-0 lg:bottom-8">
              <span key={active} className="mk-in rounded-full bg-[var(--surface-3)] px-3.5 py-1.5 text-[12.5px] font-medium text-text-2 ring-1 ring-line backdrop-blur" style={{ animation: "rise .5s both" }}>{chapters[active]?.caption}</span>
              <div className="pointer-events-auto flex items-center gap-2" role="tablist" aria-label="Tour steps">
                {chapters.map((c, i) => (
                  <button key={c.step} type="button" role="tab" aria-selected={i === active} aria-label={c.sub} onClick={() => jump(i)}
                    className={clsx("h-1.5 rounded-full transition-all", i === active ? "w-10" : "w-5 bg-[var(--line-strong)] hover:bg-muted")} style={i === active ? { background: c.color } : undefined} />
                ))}
              </div>
            </div>
          </div>
          <div ref={list} className="relative z-10 lg:order-1">{chapters.map(card)}</div>
        </div>
      </div>
    </section>
  );
}
