import { useEffect, useRef } from "react";
import * as THREE from "three";

const STAGE = ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181", "#1f9d3a"];
const LIME_DARK = "#93D500", LIME_LIGHT = "#4f8f00";
const clamp01 = (x: number) => Math.min(1, Math.max(0, x));
const smooth = (x: number) => { const t = clamp01(x); return t * t * (3 - 2 * t); };

function roundedRect(w: number, h: number, r: number) {
  const s = new THREE.Shape(), x = -w / 2, y = -h / 2;
  s.moveTo(x + r, y); s.lineTo(x + w - r, y); s.quadraticCurveTo(x + w, y, x + w, y + r);
  s.lineTo(x + w, y + h - r); s.quadraticCurveTo(x + w, y + h, x + w - r, y + h);
  s.lineTo(x + r, y + h); s.quadraticCurveTo(x, y + h, x, y + h - r);
  s.lineTo(x, y + r); s.quadraticCurveTo(x, y, x + r, y);
  return s;
}

/**
 * The hero scene: a road winding into the distance over a faint contour grid.
 * Recycled-plastic pellets in the six stage colours stream into the asphalt, and
 * passport pins stand at road sections; a signed pulse travels the road and lights
 * each pin as it passes. The camera drifts forward as the page scrolls.
 */
export function HeroScene({ className, dark = true }: { className?: string; dark?: boolean }) {
  const LIME = dark ? LIME_DARK : LIME_LIGHT;
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const host = ref.current;
    if (!host) return;
    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: "high-performance" });
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    host.appendChild(renderer.domElement);
    renderer.domElement.style.cssText = "display:block;width:100%;height:100%";
    const scene = new THREE.Scene();
    scene.fog = new THREE.FogExp2(dark ? 0x0b0b0c : 0xefeff1, 0.04);
    const camera = new THREE.PerspectiveCamera(42, 1, 0.1, 90);
    const rig = new THREE.Group(); scene.add(rig);
    scene.add(new THREE.AmbientLight(0xffffff, 0.75));
    const sun = new THREE.DirectionalLight(0xffffff, 1.5); sun.position.set(-4, 8, 4); scene.add(sun);

    // --- the road
    const curve = new THREE.CatmullRomCurve3([
      new THREE.Vector3(-6.5, 0.1, 9), new THREE.Vector3(-3, 0.2, 4.5), new THREE.Vector3(0.5, 0.35, 0.5), new THREE.Vector3(3.8, 0.45, -3.5),
      new THREE.Vector3(2.4, 0.5, -8), new THREE.Vector3(-1.5, 0.45, -12.5), new THREE.Vector3(3, 0.35, -18), new THREE.Vector3(6, 0.3, -25),
    ], false, "catmullrom", 0.5);
    const SAMPLES = 260;
    const up = new THREE.Vector3(0, 1, 0);
    const ribbon = (w: number, y: number) => {
      const pos: number[] = [], idx: number[] = [], uv: number[] = [];
      for (let i = 0; i <= SAMPLES; i++) {
        const t = i / SAMPLES, p = curve.getPointAt(t), tg = curve.getTangentAt(t), n = new THREE.Vector3().crossVectors(tg, up).normalize();
        pos.push(p.x - n.x * w / 2, p.y + y, p.z - n.z * w / 2, p.x + n.x * w / 2, p.y + y, p.z + n.z * w / 2);
        uv.push(0, t * 60, 1, t * 60);
        if (i < SAMPLES) { const a = i * 2; idx.push(a, a + 1, a + 2, a + 1, a + 3, a + 2); }
      }
      const g = new THREE.BufferGeometry();
      g.setAttribute("position", new THREE.Float32BufferAttribute(pos, 3)); g.setAttribute("uv", new THREE.Float32BufferAttribute(uv, 2));
      g.setIndex(idx); g.computeVertexNormals();
      return g;
    };
    const glowMesh = new THREE.Mesh(ribbon(3.4, -0.01), new THREE.MeshBasicMaterial({ color: LIME, transparent: true, opacity: dark ? 0.07 : 0.12, depthWrite: false }));
    const asphalt = new THREE.Mesh(ribbon(1.9, 0), new THREE.MeshStandardMaterial({ color: "#1b1d21", roughness: 0.95, metalness: 0.05 }));
    rig.add(glowMesh, asphalt);
    const edge = (off: number) => {
      const pts: THREE.Vector3[] = [];
      for (let i = 0; i <= SAMPLES; i++) { const t = i / SAMPLES, p = curve.getPointAt(t), n = new THREE.Vector3().crossVectors(curve.getTangentAt(t), up).normalize(); pts.push(new THREE.Vector3(p.x + n.x * off, p.y + 0.012, p.z + n.z * off)); }
      return pts;
    };
    for (const off of [-0.88, 0.88]) rig.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(edge(off)), new THREE.LineBasicMaterial({ color: dark ? "#e9edf2" : "#ffffff", transparent: true, opacity: dark ? 0.55 : 0.9 })));
    const dashed = new THREE.Line(new THREE.BufferGeometry().setFromPoints(edge(0)), new THREE.LineDashedMaterial({ color: LIME, dashSize: 0.32, gapSize: 0.28, transparent: true, opacity: 0.95 }));
    dashed.computeLineDistances(); rig.add(dashed);

    // --- contour grid: gentle hills that stay clear of the road
    const guide = Array.from({ length: 90 }, (_, i) => curve.getPointAt(i / 89));
    const T = new THREE.PlaneGeometry(70, 70, 90, 90); T.rotateX(-Math.PI / 2);
    const tp = T.attributes.position as THREE.BufferAttribute;
    for (let i = 0; i < tp.count; i++) {
      const x = tp.getX(i), z = tp.getZ(i) - 8;
      let d = 99; for (const g of guide) d = Math.min(d, Math.hypot(g.x - x, g.z - z - 8 + 8));
      const h = (Math.sin(x * 0.35) * Math.cos(z * 0.3) * 1.1 + Math.sin(x * 0.13 + z * 0.21) * 1.6) * smooth((d - 1.6) / 4.5);
      tp.setY(i, h - 0.06);
    }
    const terrain = new THREE.Mesh(T, new THREE.MeshBasicMaterial({ color: LIME, wireframe: true, transparent: true, opacity: dark ? 0.1 : 0.22 }));
    terrain.position.z = -8; rig.add(terrain);

    // --- recycled plastic: pellets streaming along the road
    const NP = 260;
    const pellets = new THREE.InstancedMesh(new THREE.IcosahedronGeometry(0.045, 0), new THREE.MeshStandardMaterial({ roughness: 0.4, emissiveIntensity: 0.5 }), NP);
    const col = new THREE.Color();
    const pel = Array.from({ length: NP }, (_, i) => {
      pellets.setColorAt(i, col.set(STAGE[i % 6]));
      return { t: 0.1 + Math.random() * 0.9, s: 0.018 + Math.random() * 0.03, j: (Math.random() - 0.5) * 1.5, h: Math.random() * 6.28 };
    });
    (pellets.material as THREE.MeshStandardMaterial).emissive = new THREE.Color("#ffffff");
    (pellets.material as THREE.MeshStandardMaterial).emissiveIntensity = 0.08;
    rig.add(pellets);

    // --- passport pins at road sections
    const cardGeo = new THREE.ExtrudeGeometry(roundedRect(1.0, 0.62, 0.1), { depth: 0.04, bevelEnabled: false }); cardGeo.center();
    const shield = new THREE.Shape();
    shield.moveTo(0, 0.14); shield.lineTo(0.12, 0.09); shield.lineTo(0.11, -0.02); shield.quadraticCurveTo(0.1, -0.11, 0, -0.17);
    shield.quadraticCurveTo(-0.1, -0.11, -0.11, -0.02); shield.lineTo(-0.12, 0.09); shield.closePath();
    const shieldGeo = new THREE.ExtrudeGeometry(shield, { depth: 0.02, bevelEnabled: false });
    const PIN_T = [0.2, 0.42, 0.64, 0.86];
    const pins = PIN_T.map((t, i) => {
      const g = new THREE.Group(); const p = curve.getPointAt(t);
      g.position.set(p.x + 1.15, p.y, p.z);
      const pole = new THREE.Mesh(new THREE.CylinderGeometry(0.012, 0.012, 1.3, 8), new THREE.MeshBasicMaterial({ color: dark ? "#ffffff" : "#4b5563", transparent: true, opacity: 0.45 }));
      pole.position.y = 0.65; g.add(pole);
      const card = new THREE.Group(); card.position.y = 1.5; g.add(card);
      const body = new THREE.Mesh(cardGeo, new THREE.MeshStandardMaterial({ color: "#12151a", metalness: 0.6, roughness: 0.3 }));
      const edgeL = new THREE.LineSegments(new THREE.EdgesGeometry(cardGeo), new THREE.LineBasicMaterial({ color: LIME, transparent: true, opacity: 0.9 }));
      const sh = new THREE.Mesh(shieldGeo, new THREE.MeshStandardMaterial({ color: LIME, emissive: LIME, emissiveIntensity: 0.8 })); sh.position.set(-0.28, 0.02, 0.03);
      card.add(body, edgeL, sh);
      for (let r = 0; r < 3; r++) { const l = new THREE.Mesh(new THREE.BoxGeometry(0.4 - r * 0.08, 0.045, 0.01), new THREE.MeshBasicMaterial({ color: STAGE[(i + r) % 6] })); l.position.set(0.16 + (0.4 - r * 0.08) / 2 - 0.2, 0.12 - r * 0.12, 0.03); card.add(l); }
      const ring = new THREE.Mesh(new THREE.RingGeometry(0.2, 0.24, 48), new THREE.MeshBasicMaterial({ color: LIME, transparent: true, opacity: 0, side: THREE.DoubleSide, depthWrite: false }));
      ring.rotation.x = -Math.PI / 2; ring.position.set(-1.15, 0.03, 0); g.add(ring);
      rig.add(g);
      return { g, card, ring, edgeL, sh, t };
    });
    const pulse = new THREE.Mesh(new THREE.SphereGeometry(0.11, 20, 16), new THREE.MeshBasicMaterial({ color: dark ? "#ffffff" : "#1f2937" }));
    const halo = new THREE.Mesh(new THREE.SphereGeometry(0.22, 20, 16), new THREE.MeshBasicMaterial({ color: LIME, transparent: true, opacity: 0.2, depthWrite: false }));
    pulse.add(halo); rig.add(pulse);

    // --- dust
    const D = 220, dust = new Float32Array(D * 3);
    for (let i = 0; i < D; i++) dust.set([(Math.random() - 0.5) * 40, Math.random() * 9 + 0.5, -Math.random() * 30 + 8], i * 3);
    const dg = new THREE.BufferGeometry(); dg.setAttribute("position", new THREE.BufferAttribute(dust, 3));
    const points = new THREE.Points(dg, new THREE.PointsMaterial({ color: dark ? 0xffffff : 0x6b7280, size: 0.035, transparent: true, opacity: dark ? 0.5 : 0.6, depthWrite: false }));
    scene.add(points);

    let wide = true;
    const size = () => {
      const w = host.clientWidth, h = host.clientHeight;
      renderer.setSize(w, h, false);
      camera.aspect = w / h; wide = camera.aspect > 1.25;
      rig.position.x = wide ? 2.4 : 0.8;
      camera.updateProjectionMatrix();
    };
    size();
    const ro = new ResizeObserver(size); ro.observe(host);
    const ptr = { x: 0, y: 0 };
    const onMove = (e: PointerEvent) => { ptr.x = (e.clientX / window.innerWidth - 0.5) * 2; ptr.y = (e.clientY / window.innerHeight - 0.5) * 2; };
    window.addEventListener("pointermove", onMove, { passive: true });
    let sy = 0;
    const onScroll = () => { sy = clamp01(window.scrollY / window.innerHeight); };
    onScroll(); window.addEventListener("scroll", onScroll, { passive: true });
    let visible = true, raf = 0;
    const io = new IntersectionObserver(([e]) => { visible = e.isIntersecting; }); io.observe(host);
    const clock = new THREE.Clock();
    const m4 = new THREE.Matrix4(), q = new THREE.Quaternion(), sc = new THREE.Vector3(), pt = new THREE.Vector3(), tg = new THREE.Vector3(), nrm = new THREE.Vector3();
    let cx = 0, cy = 0, cz = 0;

    const tick = () => {
      raf = requestAnimationFrame(tick);
      if (!visible) return;
      const t = clock.getElapsedTime();

      const s = (t * 0.075) % 1; // the signed pulse, end to end
      curve.getPointAt(s, pt); pulse.position.copy(pt); pulse.position.y += 0.18;
      halo.scale.setScalar(1 + Math.sin(t * 8) * 0.12);

      pel.forEach((o, i) => {
        o.t += o.s * 0.006 * 60 * 0.016; if (o.t > 1) o.t -= 0.9;
        curve.getPointAt(o.t, pt); curve.getTangentAt(o.t, tg); nrm.crossVectors(tg, up).normalize();
        const drop = 1 - smooth(o.t * 7);
        m4.compose(new THREE.Vector3(pt.x + nrm.x * o.j * 0.55, pt.y + 0.07 + drop * 1.3 + Math.sin(t * 3 + o.h) * 0.015, pt.z + nrm.z * o.j * 0.55), q.setFromAxisAngle(up, t + o.h), sc.setScalar(0.7 + Math.sin(t * 2 + o.h) * 0.2));
        pellets.setMatrixAt(i, m4);
      });
      pellets.instanceMatrix.needsUpdate = true;

      pins.forEach((p, i) => {
        let d = Math.abs(s - p.t); d = Math.min(d, 1 - d);
        const lit = Math.exp(-Math.pow(d * 22, 2));
        p.card.position.y = 1.5 + Math.sin(t * 1.2 + i) * 0.06 + lit * 0.12;
        p.card.rotation.y = Math.sin(t * 0.5 + i) * 0.35 - 0.4;
        (p.edgeL.material as THREE.LineBasicMaterial).opacity = 0.45 + lit * 0.55;
        (p.sh.material as THREE.MeshStandardMaterial).emissiveIntensity = 0.35 + lit * 1.4;
        const rm = p.ring.material as THREE.MeshBasicMaterial; rm.opacity = lit * 0.9; p.ring.scale.setScalar(1 + (1 - lit) * 0.2 + lit * 1.2);
      });

      points.position.y = Math.sin(t * 0.2) * 0.1;
      // camera: low and looking down the road; drifts forward and lower as the page scrolls
      const tx = -1.2 + ptr.x * 0.7, ty = 1.35 - sy * 0.5 - ptr.y * 0.25, tz = 7.6 - sy * 6;
      cx += (tx - cx) * 0.05; cy += (ty - cy) * 0.05; cz += (tz - cz) * 0.05;
      camera.position.set(cx, cy, cz);
      camera.lookAt(wide ? 1.6 : 0.6, 0.7, -8);
      renderer.render(scene, camera);
    };
    tick();

    return () => {
      cancelAnimationFrame(raf); io.disconnect(); ro.disconnect();
      window.removeEventListener("pointermove", onMove); window.removeEventListener("scroll", onScroll);
      scene.traverse((o) => { const m = o as THREE.Mesh; m.geometry?.dispose?.(); const mt = m.material as THREE.Material | THREE.Material[] | undefined; if (Array.isArray(mt)) mt.forEach((x) => x.dispose()); else mt?.dispose(); });
      renderer.dispose(); renderer.domElement.remove();
    };
  }, [dark]);

  return <div ref={ref} className={className} aria-hidden />;
}
