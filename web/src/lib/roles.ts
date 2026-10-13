import { useMe, useMeta } from "./hooks";
import { DUTY } from "./vocab";
import type { Duty, JobRole, OrgKind } from "./types";

const same = (a: readonly string[], b: readonly string[]) => a.length === b.length && a.every((x) => b.includes(x));

/**
 * The roles an organisation of this kind hands out (from the server's
 * catalogue). For the viewer's own organisation the roles of all its activities
 * are combined; two roles granting the same access and duties are one, named
 * after the main activity (as on the server).
 */
export function useJobRoles(kind?: OrgKind): JobRole[] {
  const meta = useMeta().data;
  const me = useMe().data;
  if (!kind || !meta?.jobRoles) return [];
  return rolesFor(meta.jobRoles, me && kind === me.org.kind ? companyKinds(me.org) : [kind]);
}

/** The sections of the chain a company works in, main one first. */
export function companyKinds(o: { kind: OrgKind; kinds?: OrgKind[] }): OrgKind[] {
  return [o.kind, ...(o.kinds ?? []).filter((k) => k !== o.kind)];
}

/** Roles of several activities combined (see useJobRoles). */
export function rolesFor(catalogue: Partial<Record<OrgKind, JobRole[]>>, kinds: OrgKind[]): JobRole[] {
  const seen = new Set<string>();
  const out: JobRole[] = [];
  for (const k of kinds) for (const r of catalogue[k] ?? []) {
    const id = `${r.access}:${[...(r.duties ?? [])].sort().join(",")}`;
    if (seen.has(id)) continue;
    seen.add(id);
    out.push(out.some((x) => x.key === r.key) ? { ...r, key: `${r.key}_${k}` } : r);
  }
  return out;
}

/** The named role matching an access level and duties, if any. */
export function matchRole(roles: JobRole[], access: string, duties: readonly string[] | null | undefined): JobRole | undefined {
  return roles.find((r) => r.access === access && (access !== "editor" || same(r.duties ?? [], duties ?? [])));
}

/** What a member does, in words: "Intake officer", or the duties they hold when combined freely. */
export function roleTitle(roles: JobRole[], access: string, duties: readonly string[] | null | undefined): string {
  const r = matchRole(roles, access, duties);
  if (r) return r.name;
  if (access === "editor" && duties?.length) return (["receive", "produce", "test", "release", "ship"] as Duty[]).filter((d) => duties.includes(d)).map((d) => DUTY[d].name).join(" & ");
  return access === "admin" ? "Administrator" : "Reporter";
}

/**
 * A function naming roles for one organisation kind. A member assigned to some
 * sections (steps) is named after them: in a company that sorts and mixes, the
 * combined catalogue calls "produce" Sorting operator, but a member who only
 * mixes is a Mixing operator (as the role picker and the server name them).
 */
export function useRoleTitle(kind?: OrgKind) {
  const roles = useJobRoles(kind);
  const catalogue = useMeta().data?.jobRoles;
  return (access: string, duties?: readonly string[] | null, steps?: readonly string[] | null) => {
    if (access === "editor" && steps?.length && catalogue) {
      const r = matchRole(rolesFor(catalogue, steps as OrgKind[]), access, duties);
      if (r) return r.name;
    }
    return roleTitle(roles, access, duties);
  };
}

const SECTION: Partial<Record<OrgKind, string>> = {
  sorting_plant: "Sort", recycler: "Recycle", additive_producer: "Make additive", asphalt_plant: "Mix asphalt",
  contractor: "Lay", road_owner: "Own the road", lab: "Testing (lab)",
};

/** A section of the chain, by the step it covers ("Mix asphalt"). */
export const sectionName = (k: string) => SECTION[k as OrgKind] ?? k;
