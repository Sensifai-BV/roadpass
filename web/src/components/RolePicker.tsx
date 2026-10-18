import { useEffect, useId, useState } from "react";
import type { Duty, OrgKind } from "../lib/types";
import { companyKinds, matchRole, rolesFor, sectionName, useJobRoles } from "../lib/roles";
import { useMe, useMeta } from "../lib/hooks";
import { KIND } from "../lib/vocab";
import clsx from "clsx";
import { DUTY } from "../lib/vocab";

export interface RoleValue { role: string; duties: Duty[]; steps?: OrgKind[] }

/**
 * Picks a named role (Intake officer, Quality manager…). "Custom" lets an admin
 * combine duties freely when a job does not match one of the named roles.
 */
export function RolePicker({ kind, value, onChange, disabled, compact, label = "Role" }: {
  kind: OrgKind; value: RoleValue; onChange: (v: RoleValue) => void; disabled?: boolean; compact?: boolean; label?: string;
}) {
  const all = useJobRoles(kind);
  const me = useMe().data;
  const meta = useMeta().data;
  // A company that covers several sections of the chain assigns each person to
  // theirs; the jobs offered are those of the chosen sections.
  const sections = me && kind === me.org.kind ? companyKinds(me.org) : [kind];
  const multi = sections.length > 1;
  const steps = (value.steps ?? []).filter((k) => sections.includes(k));
  const roles = multi && steps.length && meta?.jobRoles ? rolesFor(meta.jobRoles, steps).filter((r) => r.access === "editor").concat(all.filter((r) => r.access !== "editor")) : all;
  const id = useId();
  const match = matchRole(roles, value.role, value.duties);
  // Custom duty combinations exist only where the company has duty roles at all
  // (an auditor or a reporter only reads).
  const canCustom = roles.some((r) => r.access === "editor");
  const [custom, setCustom] = useState(!match && value.role === "editor" && canCustom);
  const current = custom || !match ? (canCustom ? "custom" : "") : match.key;
  const picked = roles.find((r) => r.key === current);
  // A value this company cannot give (a default from elsewhere) becomes its first non-admin role.
  useEffect(() => {
    if (!roles.length || match || (value.role === "editor" && canCustom)) return;
    const r = roles.find((x) => x.access !== "admin") ?? roles[0];
    onChange({ ...value, role: r.access, duties: (r.duties ?? []) as Duty[] });
  }, [roles.length, match, value.role, canCustom]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <div className="flex flex-col gap-1.5">
      {!compact && <label htmlFor={id} className="label">{label}</label>}
      <select id={id} className={compact ? "input h-8 w-auto" : "input"} value={current} disabled={disabled} aria-label={compact ? label : undefined}
        onChange={(e) => {
          if (e.target.value === "custom") { setCustom(true); onChange({ ...value, role: "editor", duties: value.duties.length ? value.duties : ["produce"] }); return; }
          const r = roles.find((x) => x.key === e.target.value)!;
          setCustom(false);
          // Admins and reporters cover the whole company: no sections.
          onChange({ role: r.access, duties: (r.duties ?? []) as Duty[], steps: r.access === "editor" ? value.steps : [] });
        }}>
        {roles.map((r) => <option key={r.key} value={r.key}>{r.name}</option>)}
        {canCustom && <option value="custom">Custom (choose duties)…</option>}
      </select>
      {!compact && picked && <p className="text-[12px] text-muted">{picked.explain}</p>}
      {!compact && !canCustom && <p className="text-[12px] text-muted">This kind of organisation reads the chain and enters no records, so it has no production, quality or dispatch roles.</p>}
      {current === "custom" && <DutyPicker value={value.duties} disabled={disabled} onChange={(d) => onChange({ ...value, role: "editor", duties: d })} />}
      {multi && value.role === "editor" && (
        <div className="flex flex-wrap items-center gap-1" role="group" aria-label={`${label}: sections of the chain`}>
          <span className={compact ? "sr-only" : "mr-1 text-[12px] text-muted"}>Works in</span>
          {sections.map((k) => { const on = steps.includes(k); const K = KIND[k]; return (
            <button key={k} type="button" disabled={disabled} aria-pressed={on} title={on ? `Remove ${sectionName(k)}` : `Works in ${sectionName(k)}`}
              onClick={() => onChange({ ...value, steps: on ? steps.filter((x) => x !== k) : [...steps, k] })}
              className={clsx("chip transition", on ? "chip-accent" : "opacity-60 hover:opacity-100", disabled && "pointer-events-none")}>
              <K.icon size={11} />{sectionName(k)}</button>); })}
          {!steps.length && !compact && <span className="text-[11.5px] text-muted">none chosen = every section</span>}
        </div>
      )}
    </div>
  );
}

const DUTIES: Duty[] = ["receive", "produce", "test", "release", "ship"];

export function DutyPicker({ value, onChange, disabled }: { value: Duty[]; onChange: (d: Duty[]) => void; disabled?: boolean }) {
  return (
    <div className="flex flex-wrap gap-1">
      {DUTIES.map((d) => {
        const on = value.includes(d);
        const D = DUTY[d];
        return (
          <button key={d} type="button" disabled={disabled} title={D.explain} aria-pressed={on}
            onClick={() => onChange(on ? value.filter((x) => x !== d) : [...value, d])}
            className={clsx("chip transition", on ? "chip-accent" : "opacity-60 hover:opacity-100", disabled && "pointer-events-none")}>
            <D.icon size={11} />{D.short}
          </button>
        );
      })}
    </div>
  );
}

