import { useEffect, useRef } from "react";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import { useMeta } from "../../lib/hooks";
import type { MapPoint } from "./RegionMap";

// Leaflet draws on canvas/SVG attributes, so CSS variables must be resolved first.
const resolve = (c: string) => {
  const m = /^var\((--[\w-]+)\)$/.exec(c.trim());
  return m ? getComputedStyle(document.documentElement).getPropertyValue(m[1]).trim() || c : c;
};

/** A flow between two real locations, drawn as a line weighted by mass. */
export interface MapLine { id: string; from: [number, number]; to: [number, number]; weight: number; color: string; label: string }

/** Points and flows on a street map at their real coordinates, in any country; the view fits them. */
export function PointMap({ points, lines = [], height }: { points: MapPoint[]; lines?: MapLine[]; height: number }) {
  const meta = useMeta().data;
  const tiles = meta?.map?.tiles;
  const el = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!tiles || !el.current) return;
    const map = L.map(el.current, { scrollWheelZoom: false, attributionControl: true, zoomControl: true, worldCopyJump: true });
    L.tileLayer(tiles, { maxZoom: 19, attribution: meta?.map?.attribution ?? "" }).addTo(map);
    for (const l of lines) {
      L.polyline([l.from, l.to], { color: resolve(l.color), weight: l.weight, opacity: 0.7, dashArray: "6 6" })
        .bindTooltip(esc(l.label), { sticky: true }).addTo(map);
    }
    for (const p of points) {
      const m = L.circleMarker([p.lat, p.lon], { radius: p.r, color: "#fff", weight: p.r > 4 ? 1.5 : 0.5, fillColor: resolve(p.color), fillOpacity: p.r > 4 ? 0.85 : 0.6 })
        .bindTooltip(`<b>${esc(p.label)}</b>${p.sub ? `<br>${esc(p.sub)}` : ""}`).addTo(map);
      if (p.onClick) (m.getElement() as SVGElement | undefined)?.style.setProperty("cursor", "pointer");
      if (p.onClick) m.on("click", p.onClick);
    }
    const all = [...points.map((p) => L.latLng(p.lat, p.lon)), ...lines.flatMap((l) => [L.latLng(...l.from), L.latLng(...l.to)])];
    if (all.length) map.fitBounds(L.latLngBounds(all), { padding: [28, 28], maxZoom: 11 });
    else map.setView([30, 10], 2);
    map.on("click", () => map.scrollWheelZoom.enable());
    map.on("mouseout", () => map.scrollWheelZoom.disable());
    return () => { map.remove(); };
  }, [tiles, meta?.map?.attribution, points, lines]);

  if (!meta) return <div style={{ height }} />;
  if (!tiles) {
    return <div className="grid place-items-center rounded-xl bg-surface-2 text-[12.5px] text-muted" style={{ height }}>
      {points.length ? `${points.length} locations · no map tiles configured` : "No locations yet."}</div>;
  }
  return (
    <div className="relative" style={{ height }}>
      <div ref={el} className="rp-street h-full overflow-hidden rounded-xl" role="region" aria-label="Map" />
      {!points.length && <div className="pointer-events-none absolute inset-0 z-[500] grid place-items-center text-[12.5px] text-text-2">No locations in this period.</div>}
    </div>
  );
}

function esc(s: string) {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}
