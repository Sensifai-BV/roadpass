import { useEffect, useRef } from "react";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import { useMeta } from "../../lib/hooks";
import { RegionMap } from "./RegionMap";

export interface StreetPoint { id: string; lat: number; lon: number; label: string; sub?: string }
export type LatLon = [number, number];
/** One load laid on the section, from where the paver started to where it finished. */
export interface Lay { id: string; n: number; from: LatLon; to: LatLon; label: string; sub?: string; estimated: boolean }

const css = (v: string) => getComputedStyle(document.documentElement).getPropertyValue(v).trim() || v;

/** Lays alternate between two shades so neighbouring loads stay apart. */
const layColor = (n: number) => css(n % 2 ? "--s5" : "--s7");

/**
 * Where a road section was laid, on a street map. With start and end points it
 * draws the section as a line and each load as the stretch it covered; lines run
 * to the asphalt plants that delivered its mix (line weight follows the mass
 * delivered). `focus` frames the road alone or the road and the plants.
 * Without a tile server it falls back to the outline map.
 */
export function StreetMap({ section, ends, lays = [], plants, focus = "all", active, onActive, height = 340 }: {
  section: StreetPoint; ends?: { from: LatLon; to: LatLon; fromLabel: string; toLabel: string };
  lays?: Lay[]; plants: (StreetPoint & { massKg: number })[]; focus?: "road" | "all";
  active?: string; onActive?: (id?: string) => void; height?: number;
}) {
  const meta = useMeta().data;
  const tiles = meta?.map?.tiles;
  const el = useRef<HTMLDivElement>(null);
  const map = useRef<L.Map>(null);
  const layLines = useRef(new Map<string, L.Polyline>());
  const onActiveRef = useRef(onActive);
  onActiveRef.current = onActive;

  useEffect(() => {
    if (!tiles || !el.current) return;
    const m = L.map(el.current, { scrollWheelZoom: false, attributionControl: true, zoomControl: true });
    map.current = m;
    L.tileLayer(tiles, { maxZoom: 19, attribution: meta?.map?.attribution ?? "" }).addTo(m);
    const plantColor = css("--s4"), roadColor = css("--s6");
    const maxKg = Math.max(1, ...plants.map((p) => p.massKg));
    const here = L.latLng(section.lat, section.lon);
    for (const p of plants) {
      const at = L.latLng(p.lat, p.lon);
      L.polyline([at, here], { color: plantColor, weight: 1.5 + 3.5 * (p.massKg / maxKg), opacity: 0.75, dashArray: "6 6" }).addTo(m);
      L.circleMarker(at, { radius: 7, color: "#fff", weight: 2, fillColor: plantColor, fillOpacity: 1 })
        .bindTooltip(`<b>${esc(p.label)}</b>${p.sub ? `<br>${esc(p.sub)}` : ""}`).addTo(m);
    }
    const tip = `<b>${esc(section.label)}</b>${section.sub ? `<br>${esc(section.sub)}` : ""}`;
    layLines.current.clear();
    if (ends) {
      // The whole section underneath; the stretches already laid on top.
      L.polyline([ends.from, ends.to], { color: roadColor, weight: 12, opacity: 0.25, lineCap: "butt" }).bindTooltip(tip, { sticky: true }).addTo(m);
      for (const l of lays) {
        const line = L.polyline([l.from, l.to], { color: layColor(l.n), weight: 8, opacity: 0.95, lineCap: "butt", dashArray: l.estimated ? "10 4" : undefined })
          .bindTooltip(`<b>Load ${l.n} · ${esc(l.label)}</b>${l.sub ? `<br>${esc(l.sub)}` : ""}${l.estimated ? "<br><i>Position estimated</i>" : ""}`, { sticky: true })
          .on("mouseover", () => onActiveRef.current?.(l.id))
          .on("mouseout", () => onActiveRef.current?.(undefined))
          .addTo(m);
        layLines.current.set(l.id, line);
      }
      L.circleMarker(ends.from, { radius: 6, color: "#fff", weight: 2, fillColor: roadColor, fillOpacity: 1 })
        .bindTooltip(esc(ends.fromLabel), { permanent: true, direction: "left", offset: [-8, 0] }).addTo(m);
      L.circleMarker(ends.to, { radius: 6, color: "#fff", weight: 2, fillColor: roadColor, fillOpacity: 1 })
        .bindTooltip(esc(ends.toLabel), { permanent: true, direction: "right", offset: [8, 0] }).addTo(m);
    } else {
      L.circleMarker(here, { radius: 10, color: "#fff", weight: 3, fillColor: roadColor, fillOpacity: 1 })
        .bindTooltip(tip, { permanent: true, direction: "top", offset: [0, -10] }).addTo(m);
    }
    const road = ends ? [L.latLng(ends.from), L.latLng(ends.to), ...lays.flatMap((l) => [L.latLng(l.from), L.latLng(l.to)])] : [here];
    const all = focus === "all" ? [...road, ...plants.map((p) => L.latLng(p.lat, p.lon))] : road;
    if (all.length > 1) m.fitBounds(L.latLngBounds(all), { padding: [48, 48], maxZoom: 17 });
    else m.setView(here, 14);
    // Let the page scroll; zoom with the buttons, or the wheel once the map is clicked.
    m.on("click", () => m.scrollWheelZoom.enable());
    m.on("mouseout", () => m.scrollWheelZoom.disable());
    return () => { m.remove(); map.current = null; };
  }, [tiles, meta?.map?.attribution, section, ends, lays, plants, focus]);

  // Highlight the load hovered in the list without rebuilding the map.
  useEffect(() => {
    for (const [id, line] of layLines.current) {
      line.setStyle({ weight: id === active ? 13 : 8, opacity: !active || id === active ? 0.95 : 0.45 });
      if (id === active) line.bringToFront();
    }
  }, [active, lays]);

  if (!meta) return <div style={{ height }} />;
  if (!tiles) {
    return <RegionMap height={height} points={[
      ...plants.map((p) => ({ id: p.id, lat: p.lat, lon: p.lon, r: 6, color: "var(--s4)", label: p.label, sub: p.sub })),
      { id: section.id, lat: section.lat, lon: section.lon, r: 8, color: "var(--s6)", label: section.label, sub: section.sub },
    ]} />;
  }
  return <div ref={el} className="rp-street overflow-hidden rounded-xl" style={{ height }} role="region" aria-label={`Map of ${section.label}`} />;
}

function esc(s: string) {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}
