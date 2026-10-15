import type { Me } from "./types";

/**
 * Who sees which section. Admins run their company's alerts and ledger keys;
 * compliance is for admins and reporters (read-only roles). Oversight members
 * (auditors, the operator) check seals and compliance across the chain.
 * The server enforces data access; this keeps the navigation to what a role uses.
 */
export type Area = "alerts" | "ledger" | "compliance" | "data";

export function canSee(area: Area, me: Me | undefined): boolean {
  if (!me) return false;
  const admin = me.user.role === "admin";
  const reporter = me.user.role === "viewer" || me.org.kind === "reporter";
  switch (area) {
    case "alerts": return admin;
    case "ledger": return admin || me.oversight;
    case "compliance": return admin || reporter || me.oversight;
    case "data": return me.user.role !== "viewer" && me.org.kind !== "reporter";
  }
}
