import { useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ImageUp, Loader2, Trash2 } from "lucide-react";
import { api } from "../lib/api";
import type { Org } from "../lib/types";
import { Card, ErrorBox } from "./ui";

/**
 * The organisation's letterhead: logo, legal name, address, contact and a footer
 * line, printed on every report it downloads or has e-mailed, and its logo on
 * the passports it issues.
 */
export function BrandCard({ org }: { org: Org }) {
  const qc = useQueryClient();
  const file = useRef<HTMLInputElement>(null);
  const [v, setV] = useState(0); // cache-busts the preview after an upload
  const [f, setF] = useState({ legalName: org.legalName ?? "", address: org.address ?? "", contactEmail: org.contactEmail ?? "", phone: org.phone ?? "", reportFooter: org.reportFooter ?? "" });
  const refresh = () => { void qc.invalidateQueries({ queryKey: ["own-org"] }); void qc.invalidateQueries({ queryKey: ["me"] }); void qc.invalidateQueries({ queryKey: ["orgs"] }); };
  const save = useMutation({ mutationFn: () => api.send("PUT", "/org", { ...org, ...f }), onSuccess: refresh });
  const upload = useMutation({ mutationFn: (b: File) => api.putBytes("/org/logo", b), onSuccess: () => { setV(Date.now()); refresh(); } });
  const remove = useMutation({ mutationFn: () => api.send("DELETE", "/org/logo"), onSuccess: () => { setV(Date.now()); refresh(); } });
  const [tooBig, setTooBig] = useState(false);
  const field = (k: keyof typeof f, label: string, placeholder = "", type = "text") => (
    <label className="block"><span className="label">{label}</span>
      <input className="input" type={type} value={f[k]} placeholder={placeholder} onChange={(e) => setF({ ...f, [k]: e.target.value })} /></label>
  );
  return (
    <Card title="Brand and report details" sub="Printed on the reports you download or schedule, and your logo on the passports you issue.">
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-4">
          <div className="grid h-16 w-40 place-items-center overflow-hidden rounded-xl border border-line bg-white">
            {org.hasLogo ? <img src={`/api/v1/orgs/${org.id}/logo?v=${v}`} alt={`${org.name} logo`} className="max-h-14 max-w-[150px] object-contain" />
              : <span className="text-[11.5px] text-muted">No logo</span>}
          </div>
          <div className="flex flex-col gap-1.5">
            <input ref={file} type="file" accept="image/png,image/jpeg" className="hidden" onChange={(e) => {
              const b = e.target.files?.[0]; e.target.value = "";
              if (!b) return;
              if (b.size > 300 * 1024) { setTooBig(true); return; }
              setTooBig(false); upload.mutate(b);
            }} />
            <div className="flex gap-2">
              <button type="button" className="btn btn-sm" disabled={upload.isPending} onClick={() => file.current?.click()}>
                {upload.isPending ? <Loader2 size={13} className="animate-spin" /> : <ImageUp size={13} />} {org.hasLogo ? "Replace logo" : "Upload logo"}</button>
              {org.hasLogo && <button type="button" className="btn btn-sm" aria-label="Remove the logo" disabled={remove.isPending} onClick={() => remove.mutate()}><Trash2 size={13} /></button>}
            </div>
            <p className={tooBig ? "text-[11.5px] text-critical" : "text-[11.5px] text-muted"}>PNG or JPEG, up to 300 kB; a wide logo on white works best.</p>
          </div>
        </div>
        {upload.error ? <ErrorBox error={upload.error} /> : null}
        <form className="grid gap-3 sm:grid-cols-2" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
          <div className="sm:col-span-2">{field("legalName", "Legal name", org.name)}</div>
          <label className="block sm:col-span-2"><span className="label">Address</span>
            <textarea className="input min-h-[64px] py-2" value={f.address} placeholder="Street and number, postcode, city, country" onChange={(e) => setF({ ...f, address: e.target.value })} /></label>
          {field("contactEmail", "Contact e-mail", "info@company.com", "email")}
          {field("phone", "Phone", "With the country code, e.g. +44 20 7946 0000", "tel")}
          <div className="sm:col-span-2">{field("reportFooter", "Report footer", "e.g. Chamber of Commerce no. · ISO 9001 certified")}</div>
          {save.error ? <div className="sm:col-span-2"><ErrorBox error={save.error} /></div> : null}
          <div className="flex items-center justify-end gap-2 sm:col-span-2">
            {save.isSuccess && <span className="mr-auto text-[12.5px] text-good">Saved. New reports use it.</span>}
            <button className="btn btn-primary" disabled={save.isPending}>Save</button>
          </div>
        </form>
      </div>
    </Card>
  );
}
