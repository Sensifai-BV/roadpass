import { useEffect, useRef, useState } from "react";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import { MapPin } from "lucide-react";
import { useMeta } from "../lib/hooks";

export interface LatLon { lat: number; lon: number }

/** Reads "52.3702, 4.8952" or "52.3702 4.8952" (as copied from most map apps). */
export function parseCoords(s: string): LatLon | null {
  const m = s.trim().match(/^(-?\d+(?:\.\d+)?)\s*[,;\s]\s*(-?\d+(?:\.\d+)?)$/);
  if (!m) return null;
  const lat = Number(m[1]), lon = Number(m[2]);
  return Math.abs(lat) <= 90 && Math.abs(lon) <= 180 && !(lat === 0 && lon === 0) ? { lat, lon } : null;
}

/**
 * Picks a location: click the map, drag the pin, or paste coordinates. Starts
 * on `near` (the company's other sites) or the whole world.
 */
export function LocationPicker({ value, onChange, near = [] }: { value: LatLon | null; onChange: (v: LatLon) => void; near?: LatLon[] }) {
  const tiles = useMeta().data?.map;
  const el = useRef<HTMLDivElement>(null);
  const map = useRef<L.Map | null>(null);
  const pin = useRef<L.Marker | null>(null);
  const [text, setText] = useState(value ? `${value.lat}, ${value.lon}` : "");
  const icon = L.divIcon({ className: "", html: '<div class="rp-pin"></div>', iconSize: [22, 22], iconAnchor: [11, 22] });

  useEffect(() => {
    if (!el.current || !tiles?.tiles) return;
    const m = L.map(el.current, { scrollWheelZoom: true, worldCopyJump: true });
    L.tileLayer(tiles.tiles, { maxZoom: 19, attribution: tiles.attribution }).addTo(m);
    if (value) m.setView([value.lat, value.lon], 13);
    else if (near.length) m.fitBounds(L.latLngBounds(near.map((p) => [p.lat, p.lon] as [number, number])).pad(0.6), { maxZoom: 9 });
    else m.setView([30, 10], 2);
    for (const p of near) L.circleMarker([p.lat, p.lon], { radius: 5, color: "#fff", weight: 1.5, fillColor: "#8a8a93", fillOpacity: 1 }).addTo(m);
    m.on("click", (e: L.LeafletMouseEvent) => onChange({ lat: round(e.latlng.lat), lon: round(e.latlng.lng) }));
    map.current = m;
    return () => { m.remove(); map.current = null; pin.current = null; };
  }, [tiles]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const m = map.current;
    if (!m) return;
    if (!value) { // the form was reset: clear the field and the pin with it
      setText("");
      pin.current?.remove();
      pin.current = null;
      return;
    }
    setText(`${value.lat}, ${value.lon}`);
    if (!pin.current) {
      pin.current = L.marker([value.lat, value.lon], { draggable: true, icon }).addTo(m);
      pin.current.on("dragend", () => { const p = pin.current!.getLatLng(); onChange({ lat: round(p.lat), lon: round(p.lng) }); });
    } else pin.current.setLatLng([value.lat, value.lon]);
    // Zoom in from the world view too, not only when the point is off screen.
    if (m.getZoom() < 10 || !m.getBounds().contains([value.lat, value.lon])) m.setView([value.lat, value.lon], Math.max(m.getZoom(), 12));
  }, [value]); // eslint-disable-line react-hooks/exhaustive-deps

  const bad = text.trim() !== "" && !parseCoords(text);
  return (
    <div className="flex flex-col gap-2">
      {tiles?.tiles
        ? <div ref={el} className="rp-street h-[260px] overflow-hidden rounded-xl" role="application" aria-label="Map: click to place the site" />
        : <p className="rounded-xl bg-surface-2 px-3 py-2 text-[12.5px] text-text-2">Street maps are switched off on this platform; paste the coordinates below.</p>}
      <div className="flex items-center gap-2">
        <MapPin size={14} className="shrink-0 text-muted" aria-hidden />
        <input className="input h-9 text-[13px]" value={text} placeholder="Click the map, or paste coordinates: 52.3702, 4.8952" aria-label="Coordinates (latitude, longitude)" aria-invalid={bad}
          onChange={(e) => { setText(e.target.value); const c = parseCoords(e.target.value); if (c) onChange(c); }} />
      </div>
      <p className={bad ? "text-[12px] text-critical" : "text-[12px] text-muted"}>
        {bad ? "Write latitude, then longitude, separated by a comma." : "Tip: in most map apps, right-click a place to copy its coordinates."}
      </p>
    </div>
  );
}

const round = (v: number) => Math.round(v * 1e5) / 1e5; // about one metre
