import { Link, useParams } from "react-router-dom";
import { ErrorBox } from "../components/ui";
import { PassportView, usePassport } from "./Passport";

export function PublicFrame({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-full">
      <header className="sticky top-0 z-20 border-b border-line bg-[color-mix(in_srgb,var(--bg)_80%,transparent)] backdrop-blur-xl">
        <div className="mx-auto flex h-14 max-w-[1100px] items-center gap-3 px-5">
          <svg width="26" height="26" viewBox="0 0 64 64" aria-hidden>
            <rect width="64" height="64" rx="15" fill="var(--text)" />
            <path d="M18 46 L28 18 h8 L46 46" fill="none" stroke="var(--bg)" strokeWidth="5" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M32 24v5m0 6v5" stroke="var(--brand)" strokeWidth="4" strokeLinecap="round" />
          </svg>
          <span className="text-[15px] font-bold tracking-tight">ROADPASS</span>
          <span className="text-[12px] text-muted">Digital product passport</span>
          <Link to="/login" className="btn btn-sm ml-auto">Sign in</Link>
        </div>
      </header>
      <main className="mx-auto max-w-[1100px] px-5 py-8">{children}</main>
      <footer className="pb-10 text-center text-[12px] text-muted">Verified with ROADPASS · records sealed on a blockchain ledger · <Link to="/verify" className="hover:underline">Verify a record</Link></footer>
    </div>
  );
}

export default function PublicPassport() {
  const { id = "" } = useParams();
  const q = usePassport(id, true);
  return (
    <PublicFrame>
      {q.isLoading ? <div className="skeleton h-40" /> : q.isError ? <ErrorBox error={q.error} /> : (
        <PassportView p={q.data!} orgName={(o) => q.data!.orgs[o]?.name ?? o} publicView />
      )}
    </PublicFrame>
  );
}
