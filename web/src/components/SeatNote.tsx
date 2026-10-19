import { useQuery } from "@tanstack/react-query";
import { Users } from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";

interface Seats { used: number; members: number; pending: number; limit: number; plan: string }

/**
 * The plan's member allowance next to an invite form. Seats are a soft limit:
 * inviting past it still works, but the operator sees the organisation as over
 * its plan, so the admin is told before, not after.
 */
export function SeatNote({ adding = 0 }: { adding?: number }) {
  const q = useQuery({ queryKey: ["org-setup"], queryFn: () => api.get<{ seats?: Seats | null }>("/org/setup") });
  const s = q.data?.seats;
  if (!s || s.limit < 0) return null;
  const after = s.used + adding;
  const over = after > s.limit;
  return (
    <p className={clsx("flex items-center gap-1.5 text-[12px]", over ? "text-warning" : "text-muted")}>
      <Users size={13} aria-hidden />
      {s.used} of {s.limit} seats on the {s.plan} plan ({s.members} {s.members === 1 ? "member" : "members"}, {s.pending} invited)
      {over && ` · inviting ${adding} more goes ${after - s.limit} over; the platform operator will see this and may ask you to change plan.`}
    </p>
  );
}
