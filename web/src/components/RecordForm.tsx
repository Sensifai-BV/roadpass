import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Loader2, Plus, X } from "lucide-react";
import { api, ApiError } from "../lib/api";
import { useDirectory, useMe, useMeta } from "../lib/hooks";
import type { Field, Input, RecordType } from "../lib/types";
import { TYPE, siteKindName, siteLabel } from "../lib/vocab";
import { exactMass, titleCase } from "../lib/format";
import { ErrorBox, Modal } from "./ui";

interface OpenSection { id: string; ref: string; contractorId: string; road: string; kmFrom: number; kmTo: number; mix: string }

/** The site kind a production record is made at (as the server checks). */
const PRODUCED_AT: Partial<Record<RecordType, string>> = {
  reject_lot: "sorting_plant", recycled_lot: "recycler", additive_batch: "additive_producer", asphalt_lot: "asphalt_plant",
};
const KIND_NAME: Record<string, string> = { sorting_plant: "sorting", recycler: "recycling", additive_producer: "additive", asphalt_plant: "asphalt plant" };

export interface Draft {
  type: RecordType;
  /** Set to correct an existing record: the save becomes its next version. */
  id?: string;
  ref?: string;
  siteId?: string;
  occurredAt?: string;
  note?: string;
  inputs?: Input[];
  data?: Record<string, unknown>;
  massKg?: number;
  title?: string;
}

function localNow(at?: string) {
  const d = at ? new Date(at) : new Date();
  d.setSeconds(0, 0);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

/**
 * One form for every record type, built from the server's field catalogue (the
 * same table that validates submissions), so a field added on the server appears
 * here with its label, unit, limits and help.
 */
export function RecordForm({ draft, onClose, onDone }: { draft: Draft | null; onClose: () => void; onDone: (ids: string[]) => void }) {
  const meta = useMeta().data;
  // A lab result's unit follows the chosen test.
  const help = (f: Field) => (f.name === "value" && meta?.labTests[data.test ?? ""] ? `In ${meta.labTests[data.test].unit}` : f.help);
  const me = useMe().data!;
  const dir = useDirectory();
  const type = draft?.type;
  const fields: Field[] = useMemo(() => (type && meta ? (meta.fields[type] ?? []).filter((f) => !f.legacy) : []), [type, meta]);
  // The address book: own sites (records come from these) and partner sites (receivers only).
  const book = dir.sites.filter((s) => s.orgId === me.org.id && !s.archivedAt);
  // Production is made at a site of its step (the server checks it): an asphalt lot
  // is not offered the company's compounding site. Deliveries leave from the
  // asphalt plant, so those sites come first. A site of kind "other", or whose
  // kind was never set, always fits.
  const madeAt = type ? PRODUCED_AT[type] : undefined;
  const preferred = madeAt ?? (type === "delivery" ? "asphalt_plant" : undefined);
  const own = book.filter((s) => !s.partner);
  const sites = (madeAt ? own.filter((s) => !s.kind || s.kind === madeAt || s.kind === "other") : own)
    .sort((a, b) => Number(b.kind === preferred) - Number(a.kind === preferred));
  // The default is the one fitting site; with several, the user chooses.
  const fitting = preferred ? sites.filter((s) => s.kind === preferred) : sites;
  const defaultSite = fitting.length === 1 ? fitting[0].id : sites.length === 1 ? sites[0].id : "";
  const [ref, setRef] = useState("");
  const [site, setSite] = useState("");
  const [at, setAt] = useState(localNow());
  // The time the form opened with: left unchanged, the record is dated the exact
  // moment it is saved (the input only has minute precision, and a truncated
  // minute can date production before the receipt of its inputs).
  const [atDefault, setAtDefault] = useState(at);
  const [massKg, setMassKg] = useState<string>("");
  const [inputs, setInputs] = useState<Input[]>([]);
  const [data, setData] = useState<Record<string, string>>({});
  const [note, setNote] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [lastDraft, setLastDraft] = useState<Draft | null>(null);
  // A delivery names a road section laid by another company, which this one cannot list otherwise.
  const sections = useQuery({ queryKey: ["open-sections"], queryFn: ({ signal }) => api.get<OpenSection[]>("/road-sections/open", signal), enabled: type === "delivery" });

  if (draft !== lastDraft) {
    // a new draft resets the form
    setLastDraft(draft);
    setRef(draft?.ref ?? "");
    setSite(draft?.siteId ?? defaultSite);
    const start = localNow(draft?.occurredAt);
    setAt(start);
    setAtDefault(start);
    setInputs(draft?.inputs ?? []);
    setMassKg(draft?.massKg ? String(Math.round(draft.massKg)) : "");
    setData(Object.fromEntries(Object.entries(draft?.data ?? {}).map(([k, v]) => [k, String(v)])));
    setNote(draft?.note ?? "");
    setErr(null);
  }

  const fieldErr = (name: string) => {
    if (!(err instanceof ApiError) || !Array.isArray(err.problem.errors)) return undefined;
    for (const row of err.problem.errors as { errors: { field: string; message: string }[] }[]) {
      const f = row.errors?.find((x) => x.field === name);
      if (f) return f.message;
    }
    return undefined;
  };
  // Only the tests that fit the tested record: polymer tests on lots and batches, asphalt tests on mix and sections.
  const subjectPrefix = String(data.subject_id ?? "").split("-")[0];
  const testMaterial = ({ PRL: "plastic", RPL: "plastic", ADB: "plastic", ASL: "asphalt", DLV: "asphalt", RSC: "asphalt" } as Record<string, string>)[subjectPrefix];
  const needsMass = type && !["lab_test", "release", "acceptance"].includes(type);
  const needsSite = type && !["lab_test", "release", "acceptance", "road_section"].includes(type);

  const submit = async () => {
    if (!type) return;
    setBusy(true);
    setErr(null);
    try {
      const clean: Record<string, unknown> = {};
      for (const f of fields) {
        const v = f.name === "dosage_pct_mix" && !data[f.name] && autoDosage ? autoDosage : data[f.name];
        if (v === undefined || v === "") continue;
        clean[f.name] = f.kind === "number" ? Number(String(v).replace(",", ".")) : f.kind === "time" ? new Date(v).toISOString() : v;
      }
      const res = await api.send<{ results: { id: string }[] }>("POST", "/records", {
        records: [{
          id: draft?.id, type, ref: ref || `${TYPE[type].name.split(" ")[0].toUpperCase()}-${Date.now().toString(36).toUpperCase()}`,
          siteId: needsSite ? site || undefined : undefined, occurredAt: (at !== atDefault ? new Date(at) : draft?.occurredAt ? new Date(draft.occurredAt) : new Date()).toISOString(),
          massKg: needsMass ? Number(massKg) || 0 : 0, inputs: inputs.length ? inputs : undefined, data: clean,
          note: note.trim() || undefined,
        }],
      });
      onDone(res.results.map((r) => r.id));
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  const inputTotal = inputs.reduce((s, i) => s + i.massKg, 0);
  // An asphalt lot's additive mass follows its inputs until someone types a different figure.
  const changeInputs = (next: Input[]) => {
    const total = next.reduce((s, i) => s + i.massKg, 0);
    if (type === "asphalt_lot" && Number(data.additive_kg) === Math.round(inputTotal)) setData({ ...data, additive_kg: String(Math.round(total)) });
    setInputs(next);
  };
  // Asphalt: the dosage follows from additive and lot mass unless typed in.
  const autoDosage = type === "asphalt_lot" && Number(massKg) > 0 && Number(data.additive_kg) > 0
    ? (Number(data.additive_kg) / Number(massKg) * 100).toFixed(2) : "";

  return (
    <Modal open={!!draft} onOpenChange={(o) => !o && onClose()} title={draft?.title ?? (type ? `New ${TYPE[type].name.toLowerCase()}` : "")}
      sub="Signed with your organisation's key and checked as soon as you save. The note, if any, is part of what is signed.">
      {type && (
        <form className="scroll-thin -mr-2 flex max-h-[64vh] flex-col gap-3 overflow-y-auto pr-2" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label" htmlFor="f-ref">Your reference</label>
              <input id="f-ref" className="input" value={ref} onChange={(e) => setRef(e.target.value)} placeholder="auto" />
            </div>
            <div>
              <label className="label" htmlFor="f-at">When</label>
              <input id="f-at" type="datetime-local" className="input" value={at} onChange={(e) => setAt(e.target.value)} required />
            </div>
            {needsSite && (
              <div>
                <label className="label" htmlFor="f-site">Site</label>
                <select id="f-site" className="input" value={site} onChange={(e) => setSite(e.target.value)} disabled={!sites.length} required={sites.length > 0}>
                  {sites.length ? <>
                    {!site && <option value="" disabled>Choose the site…</option>}
                    {sites.map((s) => <option key={s.id} value={s.id}>{s.name} · {siteKindName(s)}</option>)}
                  </> : <option value="">{own.length && madeAt ? `No ${KIND_NAME[madeAt] ?? "matching"} site yet` : "No sites yet"}</option>}
                </select>
                {!sites.length && <p className="mt-1 text-[11.5px] text-muted">
                  {me.user.role === "admin" ? <><Link className="text-accent underline" to="/get-started?step=sites" onClick={onClose}>Add a site</Link> so the passport shows where this was made.</> : "Ask your admin to add a site so the passport shows where this was made."}</p>}
              </div>
            )}
            {needsMass && (
              <div>
                <label className="label" htmlFor="f-mass">{type === "road_section" ? (data.completed_at ? "As-laid mass (kg)" : "Planned mass (kg)") : "Mass (kg)"}</label>
                <input id="f-mass" className="input num" inputMode="decimal" value={massKg} onChange={(e) => setMassKg(e.target.value)} required />
                {fieldErr("mass_kg") ? <p className="mt-1 text-[12px] text-critical">{fieldErr("mass_kg")}</p>
                  : type === "road_section" ? <p className="mt-1 text-[11.5px] text-muted">{data.completed_at ? "What the deliveries laid on the section." : "Expected tonnage; correct it to the as-laid mass when you add the completion date."}</p>
                  : type === "additive_batch" && inputTotal > 0 && Number(massKg) > 0 && (() => {
                    // The recycled inputs must match the declared recycled content (check C5, both ways).
                    const share = (inputTotal / Number(massKg)) * 100, declared = Number(String(data.recycled_content_pct ?? "").replace(",", "."));
                    const off = declared > 0 && Math.abs(share - declared) > declared * 0.02;
                    return <p className={`mt-1 text-[11.5px] ${off ? "text-warning" : "text-muted"}`}>Recycled inputs are {share.toFixed(1)} % of this batch{off ? `, but ${declared} % is declared: correct the mass, the inputs or the declaration` : ""}.</p>;
                  })()}
              </div>
            )}
          </div>
          {inputs.length > 0 && (
            <div>
              <span className="label">Inputs · {exactMass(inputTotal)}</span>
              <ul className="flex flex-col gap-1.5">
                {inputs.map((i, k) => (
                  <li key={i.id} className="flex items-center gap-2 rounded-lg bg-surface-2 px-2.5 py-1.5">
                    <span className="mono flex-1 text-[12.5px]">{i.id}</span>
                    <input className="input num h-7 w-28" inputMode="decimal" value={i.massKg} aria-label={`Mass from ${i.id}`}
                      onChange={(e) => changeInputs(inputs.map((x, j) => (j === k ? { ...x, massKg: Number(e.target.value) || 0 } : x)))} />
                    <span className="text-[12px] text-muted">kg</span>
                    <button type="button" className="text-muted hover:text-critical" aria-label="Remove input" onClick={() => changeInputs(inputs.filter((_, j) => j !== k))}><X size={14} /></button>
                  </li>
                ))}
              </ul>
            </div>
          )}
          <div className="grid grid-cols-2 gap-3">
            {fields.map((f) => (
              <div key={f.name} className={f.kind === "text" && f.name !== "ticket" ? "col-span-2" : ""}>
                <label className="label" htmlFor={`f-${f.name}`}>{f.label}{f.unit ? ` (${f.unit})` : ""}{f.required ? "" : " · optional"}</label>
                {f.kind === "enum" ? (
                  <select id={`f-${f.name}`} className="input" value={data[f.name] ?? ""} onChange={(e) => setData({ ...data, [f.name]: e.target.value })} required={f.required}>
                    <option value="">Choose…</option>
                    {f.enum?.filter((v) => f.name !== "test" || !testMaterial || meta?.labTests[v]?.material === testMaterial)
                      .map((v) => <option key={v} value={v}>{f.name === "test" && meta?.labTests[v] ? `${meta.labTests[v].name} (${meta.labTests[v].standard})` : titleCase(v)}</option>)}
                  </select>
                ) : f.kind === "org" ? (
                  <select id={`f-${f.name}`} className="input" value={data[f.name] ?? ""} onChange={(e) => setData({ ...data, [f.name]: e.target.value })} required={f.required}>
                    <option value="">Choose…</option>
                    {dir.orgs.filter((o) => f.name === "owner_org_id"
                      ? o.kind === "road_owner" || (o.kinds ?? []).includes("road_owner") // a company may own the roads it builds
                      : o.id !== me.org.id)
                      .map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
                  </select>
                ) : f.kind === "site" ? (
                  <select id={`f-${f.name}`} className="input" value={data[f.name] ?? ""} onChange={(e) => setData({ ...data, [f.name]: e.target.value })} required={f.required}>
                    <option value="">Choose…</option>
                    <optgroup label="Our sites">{book.filter((s) => !s.partner && s.id !== site).map((s) => <option key={s.id} value={s.id}>{siteLabel(s)} · {siteKindName(s)}</option>)}</optgroup>
                    <optgroup label="Partner sites">{book.filter((s) => !!s.partner).map((s) => <option key={s.id} value={s.id}>{siteLabel(s)} · {siteKindName(s)}</option>)}</optgroup>
                  </select>
                ) : f.name === "section_id" ? (
                  <select id={`f-${f.name}`} className="input" value={data[f.name] ?? ""} onChange={(e) => setData({ ...data, [f.name]: e.target.value })} required={f.required}>
                    <option value="">{sections.isLoading ? "Loading…" : sections.data?.length ? "Choose…" : "No open road sections"}</option>
                    {sections.data?.map((x) => <option key={x.id} value={x.id}>{x.road} km {x.kmFrom}–{x.kmTo} · {x.mix} · {dir.orgName(x.contractorId)} ({x.id})</option>)}
                  </select>
                ) : f.kind === "time" ? (
                  <input id={`f-${f.name}`} type="datetime-local" className="input" value={data[f.name] ?? ""} onChange={(e) => setData({ ...data, [f.name]: e.target.value })} />
                ) : (
                  <input id={`f-${f.name}`} className={`input ${f.kind === "number" ? "num" : ""} ${f.kind === "ref" ? "mono" : ""}`}
                    inputMode={f.kind === "number" ? "decimal" : undefined} placeholder={f.name === "dosage_pct_mix" && autoDosage ? `${autoDosage} (from additive and lot mass)` : f.example && `e.g. ${f.example}`}
                    value={data[f.name] ?? ""} onChange={(e) => setData({ ...data, [f.name]: e.target.value })} required={f.required && !(f.name === "dosage_pct_mix" && autoDosage)} />
                )}
                {fieldErr(f.name) ? <p className="mt-1 text-[12px] text-critical">{fieldErr(f.name)}</p> : help(f) ? <p className="mt-1 text-[11.5px] text-muted">{help(f)}</p> : null}
              </div>
            ))}
          </div>
          <div>
            <label className="label" htmlFor="f-note">Note <span className="font-normal text-muted">(optional, signed with the record)</span></label>
            <textarea id="f-note" className="input min-h-[64px] py-2" maxLength={1000} value={note} onChange={(e) => setNote(e.target.value)}
              placeholder="Anything the next company should know, e.g. a deviation from the usual process." />
          </div>
          {err ? <ErrorBox error={err} /> : null}
          <div className="sticky bottom-0 -mx-1 flex justify-end gap-2 bg-[var(--bg-elevated)] px-1 pt-2">
            <button type="button" className="btn" onClick={onClose}>Cancel</button>
            <button className="btn btn-primary" disabled={busy}>{busy ? <Loader2 size={14} className="animate-spin" /> : <Plus size={14} />} Save and sign</button>
          </div>
        </form>
      )}
    </Modal>
  );
}
