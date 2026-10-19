import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { api } from "../lib/api";
import { useMe, useMeta } from "../lib/hooks";
import type { Org, Site, SiteKind } from "../lib/types";
import { SITE_KIND } from "../lib/vocab";
import { COUNTRIES } from "../lib/countries";
import { ErrorBox, Modal } from "./ui";

/** The site types to offer: the organisation's own activities first (main kind, then its other activities), then the rest. */
export function useSiteKinds(org?: Pick<Org, "kind" | "kinds">) {
  const all = useMeta().data?.siteKinds ?? (Object.keys(SITE_KIND) as SiteKind[]);
  const ownKinds = ([org?.kind, ...(org?.kinds ?? [])] as string[]).filter((k, i, a): k is SiteKind => !!k && a.indexOf(k) === i && all.includes(k as SiteKind));
  return { own: ownKinds, rest: all.filter((k) => !ownKinds.includes(k)), def: ownKinds[0] ?? ("" as SiteKind | "") };
}

/** Site type picker; "Other" asks for free text. Organisation's own activities come first. */
export function SiteKindSelect({ value, other, onChange, org, id = "site-kind", wide = "col-span-2" }: {
  value: SiteKind | ""; other: string; onChange: (kind: SiteKind | "", other: string) => void; org?: Pick<Org, "kind" | "kinds">; id?: string;
  /** Class that makes the picker span the form's columns when "Other" is not chosen. */ wide?: string;
}) {
  const { own, rest } = useSiteKinds(org);
  const opt = (k: SiteKind) => <option key={k} value={k}>{SITE_KIND[k]?.name ?? k}</option>;
  return (
    <>
      <div className={clsx(value !== "other" && wide)}>
        <label className="label" htmlFor={id}>Site type</label>
        <select id={id} className="input" required value={value} onChange={(e) => onChange(e.target.value as SiteKind, other)}>
          <option value="">Choose…</option>
          {/* A legacy type outside the list (e.g. an auditor's office) stays selectable until changed. */}
          {value && !own.includes(value) && !rest.includes(value) && <option value={value}>{value.replace(/_/g, " ")}</option>}
          {own.length > 0 ? <>
            <optgroup label="What your company does">{own.map(opt)}</optgroup>
            <optgroup label="Other site types">{rest.map(opt)}</optgroup>
          </> : rest.map(opt)}
        </select>
      </div>
      {value === "other" && <div><label className="label" htmlFor={`${id}-other`}>What kind of site</label>
        <input id={`${id}-other`} className="input" required placeholder="Transfer station" value={other} onChange={(e) => onChange("other", e.target.value)} /></div>}
    </>
  );
}

/** Add or edit one site of the address book. */
export function SiteModal({ partner, site, onClose, onSaved }: { partner: boolean; site?: Site; onClose: () => void; onSaved?: (s: Site) => void }) {
  const qc = useQueryClient();
  const me = useMe().data;
  const str = (v?: number) => (v ? String(v) : "");
  const [s, setS] = useState({
    name: site?.name ?? "", kind: site?.kind ?? ("" as SiteKind | ""), kindOther: site?.kindOther ?? "", partner: site?.partner ?? "",
    address: site?.address ?? "", city: site?.city ?? "", province: site?.province ?? "", country: site?.country ?? "",
    lat: str(site?.lat), lon: str(site?.lon), capacityTonnesPerYear: str(site?.capacityTonnesPerYear), equipment: site?.equipment ?? "",
  });
  const m = useMutation({
    mutationFn: () => {
      const body = { ...s, partner: partner ? s.partner : "", lat: Number(s.lat) || 0, lon: Number(s.lon) || 0, capacityTonnesPerYear: Number(s.capacityTonnesPerYear) || 0 };
      return site ? api.send<Site>("PUT", `/org/sites/${site.id}`, body) : api.send<Site>("POST", "/org/sites", body);
    },
    onSuccess: (x) => {
      void qc.invalidateQueries({ queryKey: ["own-org"] }); void qc.invalidateQueries({ queryKey: ["sites"] });
      onSaved?.(x); onClose();
    },
  });
  const f = (k: keyof typeof s, label: string, ph = "", req = false) => (
    <div><label className="label" htmlFor={`site-${k}`}>{label}{!req && <span className="font-normal text-muted"> · optional</span>}</label>
      <input id={`site-${k}`} className="input" placeholder={ph} required={req} value={s[k]} onChange={(e) => setS({ ...s, [k]: e.target.value })} /></div>
  );
  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title={`${site ? "Edit" : "Add"} ${partner ? "a partner site" : "a site"}`}
      sub={partner ? "A site of a company you ship to or receive from. Only your company sees it." : "One of your company's own plants, yards or depots."}>
      <form className="grid grid-cols-2 gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        {partner && <div className="col-span-2">{f("partner", "Company", "Polímeros del Norte S.L.", true)}</div>}
        <div className="col-span-2">{f("name", "Site name", partner ? "Main plant" : "North recycling yard", true)}</div>
        {/* Partner sites belong to other companies, so only own sites put this company's activities first. */}
        <SiteKindSelect value={s.kind} other={s.kindOther} org={partner ? undefined : me?.org} onChange={(kind, kindOther) => setS({ ...s, kind, kindOther })} />
        <div className="col-span-2">{f("address", "Address")}</div>
        {f("city", "City")}{f("province", "Region / state / province")}
        <div>
          <label className="label" htmlFor="site-country">Country<span className="font-normal text-muted"> · optional</span></label>
          <select id="site-country" className="input" value={s.country} onChange={(e) => setS({ ...s, country: e.target.value })}>
            <option value="">—</option>
            {COUNTRIES.map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}
          </select>
        </div>
        <div />
        {f("lat", "Latitude", "e.g. 52.37", !partner)}{f("lon", "Longitude", "e.g. 4.90", !partner)}
        {!partner && <>{f("capacityTonnesPerYear", "Capacity (t/yr)")}{f("equipment", "Equipment")}</>}
        {m.error ? <div className="col-span-2"><ErrorBox error={m.error} /></div> : null}
        <div className="col-span-2 flex justify-end gap-2">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={m.isPending}>{site ? "Save" : partner ? "Add partner site" : "Add site"}</button>
        </div>
      </form>
    </Modal>
  );
}

