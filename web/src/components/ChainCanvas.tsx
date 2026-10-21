import { useEffect, useRef } from "react";

// Dark-surface steps of the validated stage palette (styles.css, dark theme): the
// sign-in panel is always dark, whatever the app theme.
const STAGE = ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181", "#1f9d3a"];
const ACCENT = "#3ecf72";
const NAMES = ["Sort", "Recycle", "Make additive", "Mix asphalt", "Lay", "Own the road"];
const SANS = "Inter Variable, ui-sans-serif, system-ui, sans-serif";
// Vertical position of each stage on the flow band (0 = top, 1 = bottom).
const WAVE = [0.62, 0.36, 0.6, 0.34, 0.58, 0.4];
const PARTICLES = 46;
const PER_SEAL = 7; // arrivals at the road per ledger block

interface Particle { d: number; speed: number; off: number; size: number }
interface Block { born: number; n: number }

/**
 * The supply chain as a living diagram: material flows from sorting plant to
 * road, each stage signs the handover (a ring pulse), and arrivals at the road
 * are sealed into blocks on a ledger strip. Canvas 2D, DPR-aware; a single still
 * frame when the user prefers reduced motion.
 */
export function ChainCanvas({ className }: { className?: string }) {
  const ref = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    let w = 0, h = 0, dpr = 1;
    let pts: { x: number; y: number }[] = []; // sampled path
    let cum: number[] = []; // cumulative length per sample
    let nodeD: number[] = []; // path distance of each stage
    let total = 1;
    let grid: HTMLCanvasElement | null = null;
    const pulses = STAGE.map(() => ({ at: -1e9 }));
    const blocks: Block[] = [];
    let arrivals = 0, sealN = 1, shift = 0, drop = -1e9;
    const rand = mulberry(7);
    const parts: Particle[] = [];

    const layout = () => {
      const r = canvas.getBoundingClientRect();
      w = r.width; h = r.height; dpr = Math.min(window.devicePixelRatio || 1, 2);
      canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr);
      const band = { top: h * 0.08, bottom: h * 0.7 };
      const nodes = WAVE.map((y, i) => ({ x: w * (0.07 + (0.86 * i) / 5), y: band.top + y * (band.bottom - band.top) }));
      // Catmull-Rom through the stages, sampled densely so particles move by arc length.
      pts = []; nodeD = [];
      for (let i = 0; i < nodes.length - 1; i++) {
        const p0 = nodes[Math.max(0, i - 1)], p1 = nodes[i], p2 = nodes[i + 1], p3 = nodes[Math.min(nodes.length - 1, i + 2)];
        for (let s = 0; s < 80; s++) {
          const t = s / 80, t2 = t * t, t3 = t2 * t;
          pts.push({
            x: 0.5 * (2 * p1.x + (-p0.x + p2.x) * t + (2 * p0.x - 5 * p1.x + 4 * p2.x - p3.x) * t2 + (-p0.x + 3 * p1.x - 3 * p2.x + p3.x) * t3),
            y: 0.5 * (2 * p1.y + (-p0.y + p2.y) * t + (2 * p0.y - 5 * p1.y + 4 * p2.y - p3.y) * t2 + (-p0.y + 3 * p1.y - 3 * p2.y + p3.y) * t3),
          });
        }
      }
      pts.push(nodes[nodes.length - 1]);
      cum = [0];
      for (let i = 1; i < pts.length; i++) cum.push(cum[i - 1] + Math.hypot(pts[i].x - pts[i - 1].x, pts[i].y - pts[i - 1].y));
      total = cum[cum.length - 1] || 1;
      nodeD = nodes.map((_, i) => cum[Math.min(i * 80, pts.length - 1)]);
      // Dot grid, drawn once per size.
      grid = document.createElement("canvas");
      grid.width = canvas.width; grid.height = canvas.height;
      const g = grid.getContext("2d")!;
      g.scale(dpr, dpr);
      for (let y = 12; y < h; y += 22) for (let x = 12; x < w; x += 22) {
        const fade = 1 - Math.min(1, Math.hypot(x - w * 0.5, y - h * 0.4) / (Math.max(w, h) * 0.7));
        g.fillStyle = `rgba(255,255,255,${0.07 * fade})`;
        g.fillRect(x, y, 1.2, 1.2);
      }
      if (!parts.length) for (let i = 0; i < PARTICLES; i++) parts.push(spawn(rand() * total));
    };

    const spawn = (d = 0): Particle => ({ d, speed: 38 + rand() * 34, off: (rand() - 0.5) * 14, size: 1.2 + rand() * 1.4 });

    const at = (d: number) => {
      let lo = 0, hi = cum.length - 1;
      while (lo < hi) { const m = (lo + hi) >> 1; if (cum[m] < d) lo = m + 1; else hi = m; }
      const i = Math.max(1, lo), a = pts[i - 1], b = pts[i];
      const seg = cum[i] - cum[i - 1] || 1, f = (d - cum[i - 1]) / seg;
      const dx = b.x - a.x, dy = b.y - a.y, len = Math.hypot(dx, dy) || 1;
      return { x: a.x + dx * f, y: a.y + dy * f, nx: -dy / len, ny: dx / len };
    };
    const stageOf = (d: number) => { let s = 0; while (s < 5 && d >= nodeD[s + 1]) s++; return s; };

    let last = performance.now(), raf = 0;
    const frame = (now: number) => {
      const dt = Math.min(0.05, (now - last) / 1000);
      last = now;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      if (grid && grid.width && grid.height) { ctx.save(); ctx.setTransform(1, 0, 0, 1, 0, 0); ctx.drawImage(grid, 0, 0); ctx.restore(); }

      // The route, coloured stage by stage.
      ctx.lineWidth = 2; ctx.lineCap = "round";
      for (let s = 0; s < 5; s++) {
        const a = at(nodeD[s]), b = at(nodeD[s + 1]);
        const grad = ctx.createLinearGradient(a.x, a.y, b.x, b.y);
        grad.addColorStop(0, rgba(STAGE[s], 0.32)); grad.addColorStop(1, rgba(STAGE[s + 1], 0.32));
        ctx.strokeStyle = grad;
        ctx.beginPath();
        for (let i = s * 80; i <= Math.min((s + 1) * 80, pts.length - 1); i++) i === s * 80 ? ctx.moveTo(pts[i].x, pts[i].y) : ctx.lineTo(pts[i].x, pts[i].y);
        ctx.stroke();
      }

      // Material: each particle drifts along the route; crossing a stage is a signed handover.
      for (const p of parts) {
        const before = p.d;
        if (!still) p.d += p.speed * dt;
        for (let s = 1; s < 6; s++) if (before < nodeD[s] && p.d >= nodeD[s] && now - pulses[s].at > 420) pulses[s].at = now;
        if (p.d >= total) {
          arrivals++;
          if (arrivals % PER_SEAL === 0) { blocks.push({ born: now, n: sealN++ }); drop = now; if (blocks.length > 40) { blocks.shift(); shift -= 30; } }
          Object.assign(p, spawn(0));
          pulses[0].at = now - 300;
        }
        const s = stageOf(p.d);
        const local = s < 5 ? (p.d - nodeD[s]) / (nodeD[s + 1] - nodeD[s]) : 1;
        // Converge to the line at each stage (a handover is one point), spread between.
        const spread = Math.sin(Math.PI * Math.min(1, Math.max(0, local)));
        const q = at(p.d);
        const x = q.x + q.nx * p.off * spread, y = q.y + q.ny * p.off * spread;
        const col = s < 5 && local > 0.5 ? STAGE[s + 1] : STAGE[s];
        ctx.fillStyle = rgba(col, 0.9);
        ctx.beginPath(); ctx.arc(x, y, p.size, 0, Math.PI * 2); ctx.fill();
        ctx.fillStyle = rgba(col, 0.14);
        ctx.beginPath(); ctx.arc(x, y, p.size * 3, 0, Math.PI * 2); ctx.fill();
      }

      // Stages: a solid core and a signature ring that pulses on each handover.
      for (let s = 0; s < 6; s++) {
        const c = at(nodeD[s]);
        const age = (now - pulses[s].at) / 900;
        if (age < 1) {
          ctx.strokeStyle = rgba(STAGE[s], 0.55 * (1 - age)); ctx.lineWidth = 1.5;
          ctx.beginPath(); ctx.arc(c.x, c.y, 9 + 18 * easeOut(age), 0, Math.PI * 2); ctx.stroke();
        }
        ctx.fillStyle = "#0b0b0c";
        ctx.beginPath(); ctx.arc(c.x, c.y, 9, 0, Math.PI * 2); ctx.fill();
        ctx.strokeStyle = rgba(STAGE[s], 0.9); ctx.lineWidth = 2;
        ctx.beginPath(); ctx.arc(c.x, c.y, 9, 0, Math.PI * 2); ctx.stroke();
        ctx.fillStyle = STAGE[s];
        ctx.beginPath(); ctx.arc(c.x, c.y, 3.5, 0, Math.PI * 2); ctx.fill();
        // Name above crests, below troughs, so labels never sit on the route.
        const above = s === 0 ? WAVE[0] > WAVE[1] : WAVE[s] < WAVE[s - 1];
        ctx.font = `600 12px ${SANS}`; ctx.textAlign = "center";
        ctx.fillStyle = "rgba(255,255,255,0.72)";
        ctx.fillText(NAMES[s], c.x, above ? c.y - 22 : c.y + 30);
      }

      // Ledger strip: every few arrivals become a sealed block, chained to the last.
      const end = at(total), stripY = h * 0.86, size = 18, gap = 30;
      const anchorX = end.x;
      ctx.setLineDash([2, 4]); ctx.strokeStyle = "rgba(255,255,255,0.14)"; ctx.lineWidth = 1;
      ctx.beginPath(); ctx.moveTo(end.x, end.y + 12); ctx.lineTo(anchorX, stripY - size / 2 - 6); ctx.stroke();
      ctx.setLineDash([]);
      const da = (now - drop) / 500;
      if (da < 1) {
        ctx.fillStyle = rgba(ACCENT, 1 - da);
        ctx.beginPath(); ctx.arc(anchorX, end.y + 12 + (stripY - end.y - 24) * easeOut(da), 2.5, 0, Math.PI * 2); ctx.fill();
      }
      ctx.font = `600 10.5px ${SANS}`; ctx.textAlign = "left";
      ctx.fillStyle = "rgba(255,255,255,0.4)";
      ctx.fillText("LEDGER  ·  each block seals a batch of signed records", w * 0.07, stripY - size / 2 - 14);
      const target = Math.max(0, blocks.length - 1) * gap;
      shift += (target - shift) * Math.min(1, dt * 6);
      ctx.font = "500 10px ui-monospace, SFMono-Regular, Menlo, monospace";
      ctx.textAlign = "center";
      for (let i = blocks.length - 1; i >= 0; i--) {
        const b = blocks[i];
        const x = anchorX - (shift - i * gap) - size / 2;
        if (x < -size) break;
        const fade = Math.min(1, Math.max(0, x / (w * 0.35))); // dissolve into the left edge
        const fresh = Math.max(0, 1 - (now - b.born) / 1600);
        if (i > 0) {
          ctx.strokeStyle = `rgba(255,255,255,${0.12 * fade})`; ctx.lineWidth = 1;
          ctx.beginPath(); ctx.moveTo(x - gap + size, stripY); ctx.lineTo(x, stripY); ctx.stroke();
        }
        roundRect(ctx, x, stripY - size / 2, size, size, 5);
        ctx.fillStyle = fresh > 0 ? rgba(ACCENT, 0.85 * fresh + 0.06) : `rgba(255,255,255,${0.06 * fade})`;
        ctx.fill();
        ctx.strokeStyle = fresh > 0 ? rgba(ACCENT, 0.9) : `rgba(255,255,255,${0.22 * fade})`;
        ctx.stroke();
        if (i === blocks.length - 1) {
          ctx.fillStyle = `rgba(255,255,255,${0.5 + 0.4 * fresh})`;
          ctx.fillText(`seal ${b.n}`, x + size / 2, stripY + size / 2 + 14);
        }
      }
      if (!still) raf = requestAnimationFrame(frame);
    };

    layout();
    // Warm start: a few sealed blocks already on the strip.
    for (let i = 0; i < 6; i++) blocks.push({ born: -1e9, n: sealN++ });
    shift = (blocks.length - 1) * 30;
    raf = requestAnimationFrame(frame);
    const ro = new ResizeObserver(() => layout());
    ro.observe(canvas);
    return () => { cancelAnimationFrame(raf); ro.disconnect(); };
  }, []);

  return <canvas ref={ref} className={className} aria-hidden />;
}

function rgba(hex: string, a: number) {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${a})`;
}
const easeOut = (t: number) => 1 - (1 - t) * (1 - t);
function roundRect(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
  ctx.beginPath();
  ctx.moveTo(x + r, y); ctx.arcTo(x + w, y, x + w, y + h, r); ctx.arcTo(x + w, y + h, x, y + h, r);
  ctx.arcTo(x, y + h, x, y, r); ctx.arcTo(x, y, x + w, y, r); ctx.closePath();
}
// Small seeded PRNG so the flow looks the same on every load.
function mulberry(seed: number) {
  return () => {
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
