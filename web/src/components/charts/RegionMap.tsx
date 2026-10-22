import { useMemo, useState } from "react";
import { geoConicConformal, geoPath } from "d3-geo";
import { feature, mesh } from "topojson-client";
import type { Topology, GeometryCollection } from "topojson-specification";
import type { FeatureCollection, Geometry } from "geojson";
import regionsTopo from "es-atlas/es/autonomous_regions.json";
import { useSize } from "./TimeChart";
import { PointMap, type MapLine } from "./PointMap";

export interface MapPoint {
  id: string;
  lat: number;
  lon: number;
  r: number; // radius px
  color: string;
  label: string;
  sub?: string;
  onClick?: () => void;
}

type Props = { regions: string };
const topo = regionsTopo as unknown as Topology<{ autonomous_regions: GeometryCollection<Props>; border: GeometryCollection }>;
// The chain runs on the peninsula and the Balearic Islands; the Canaries and the
// autonomous cities would only shrink the map (no plants there).
const shown = (n: string) => !["Canarias", "Ceuta", "Melilla"].includes(n);
const allRegions = feature(topo, topo.objects.autonomous_regions) as unknown as FeatureCollection<Geometry, { name: string }>;
const regions: FeatureCollection<Geometry, { name: string }> = { type: "FeatureCollection", features: allRegions.features.filter((f) => shown(f.properties.name)) };
const borders = mesh(topo, topo.objects.autonomous_regions, (a, b) => a !== b && shown((a.properties as unknown as { name: string }).name));

type MapProps = { points: MapPoint[]; lines?: MapLine[]; height?: number; fill?: (region: string) => string | undefined; onRegion?: (r: string) => void };

/**
 * Locations on a street map at their real coordinates, in any country. The
 * regional outline (it only covers the demo dataset's regions) is used solely
 * for a region choropleth without any located points.
 */
export function RegionMap(props: MapProps) {
  const { points, lines, fill, height = 420 } = props;
  const outline = !points.length && !lines?.length && regions.features.some((f) => fill?.(f.properties.name) !== undefined);
  return outline ? <OutlineMap {...props} /> : <PointMap points={points} lines={lines} height={height} />;
}

function OutlineMap({ points, height: maxHeight = 420, fill, onRegion }: MapProps) {
  const [ref, width] = useSize<HTMLDivElement>();
  // The peninsula plus the Balearics is ~1.25:1; never leave a tall empty box on narrow screens.
  const height = width > 0 ? Math.min(maxHeight, Math.round(width / 1.25)) : maxHeight;
  const [hover, setHover] = useState<MapPoint | null>(null);
  const [hoverRegion, setHoverRegion] = useState<string | null>(null);
  const proj = useMemo(() => {
    const p = geoConicConformal().parallels([36, 43]).rotate([3.7, 0]);
    p.fitExtent([[8, 8], [Math.max(10, width - 8), height - 8]], regions);
    return p;
  }, [width, height]);
  const path = geoPath(proj);
  const placed = useMemo(
    // Drawn in the order given: callers put background layers (road sections) first.
    () => points.map((p) => ({ ...p, xy: proj([p.lon, p.lat]) })).filter((p) => p.xy),
    [points, proj],
  );

  return (
    <div ref={ref} className="relative" style={{ height }}>
      {width > 0 && (
        <svg width={width} height={height} role="img" aria-label="Map">
          <g>
            {regions.features.map((f) => {
              const name = f.properties.name;
              return (
                <path key={name} d={path(f) ?? ""} fill={fill?.(name) ?? "var(--surface-2)"}
                  stroke="none" onPointerEnter={() => setHoverRegion(name)} onPointerLeave={() => setHoverRegion(null)}
                  onClick={() => onRegion?.(name)} style={{ cursor: onRegion ? "pointer" : "default", transition: "fill .2s" }}
                  opacity={hoverRegion && hoverRegion !== name && onRegion ? 0.75 : 1} />
              );
            })}
            <path d={path(borders) ?? ""} fill="none" stroke="var(--line-strong)" strokeWidth={0.8} />
          </g>
          <g>
            {placed.map((p) => (
              <circle key={p.id} cx={p.xy![0]} cy={p.xy![1]} r={p.r} fill={p.color} fillOpacity={p.r > 4 ? 0.85 : 0.6}
                stroke="var(--surface)" strokeWidth={p.r > 4 ? 1.5 : 0.5}
                onPointerEnter={() => setHover(p)} onPointerLeave={() => setHover(null)} onClick={p.onClick}
                style={{ cursor: p.onClick ? "pointer" : "default" }} />
            ))}
          </g>
        </svg>
      )}
      {hover && (() => {
        const xy = proj([hover.lon, hover.lat]) ?? [0, 0];
        return (
          <div className="tooltip" style={{ left: Math.min(xy[0] + 12, width - 220), top: Math.max(0, xy[1] - 10) }}>
            <div className="font-semibold">{hover.label}</div>
            {hover.sub && <div className="mt-0.5 text-text-2">{hover.sub}</div>}
          </div>
        );
      })()}
      {!hover && hoverRegion && onRegion && (
        <div className="tooltip" style={{ left: 12, top: 12 }}>{hoverRegion}</div>
      )}
    </div>
  );
}
