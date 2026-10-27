import { useEffect, useState } from "react";
import { FileJson, Loader2, ShieldCheck, CircleCheck, CircleX, CircleMinus } from "lucide-react";
import { api } from "../lib/api";
import type { Verification } from "../lib/types";
import { fingerprint, merkleRoot } from "../lib/verify";
import { dateTime, shortHash } from "../lib/format";
import { Card, ErrorBox, KV } from "../components/ui";
import { PublicFrame } from "./PublicPassport";
import { useSeo } from "../lib/seo";

/**
 * Verify anything: a fingerprint, or a record document dropped as a file. The
 * fingerprint is computed here; the server only says where it is sealed, and the
 * Merkle path is replayed here too.
 */
export function VerifyTool() {
  const [input, setInput] = useState(() => new URLSearchParams(location.search).get("hash")?.toLowerCase() ?? "");
  const [res, setRes] = useState<(Verification & { localRoot?: string; localHash?: string }) | null>(null);
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const check = async (hash: string, localHash?: string) => {
    setBusy(true);
    setErr(null);
    try {
      const v = await api.send<Verification>("POST", "/ledger/verify", { hash });
      let localRoot: string | undefined;
      // A seal of one record has no path (the root is its leaf hash).
      if (v.sealStatus === "sealed") localRoot = await merkleRoot(v.hash, v.path ?? []);
      setRes({ ...v, localRoot, localHash });
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  // /verify?hash=… (from the explorer) checks that fingerprint straight away.
  useEffect(() => { if (/^[0-9a-f]{64}$/.test(input)) void check(input); /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, []);

  const onFile = async (f: File) => {
    try {
      const doc = JSON.parse(await f.text());
      // Accept a passport download (trust.hashDoc), a hash document, or a record.
      const hd = doc?.trust?.hashDoc ?? doc?.hashDoc ?? doc;
      const h = await fingerprint(hd);
      setInput(h);
      await check(h, h);
    } catch (e) {
      setErr(e);
    }
  };

  const row = (ok: boolean | undefined, label: string) => (
    <li className="flex items-center gap-2.5 text-[13px]">
      {ok === undefined ? <CircleMinus size={17} className="text-muted" /> : ok ? <CircleCheck size={17} className="pop text-good" /> : <CircleX size={17} className="pop text-critical" />}
      {label}
    </li>
  );

  return (
    <div className="flex flex-col gap-4">
      <form className="flex flex-wrap gap-2" onSubmit={(e) => { e.preventDefault(); void check(input.trim().toLowerCase()); }}>
        <input className="input mono h-10 min-w-[280px] flex-1" placeholder="64-character fingerprint" value={input} onChange={(e) => setInput(e.target.value)} aria-label="Fingerprint" />
        <button className="btn btn-accent h-10" disabled={busy || input.trim().length !== 64}>
          {busy ? <Loader2 size={15} className="animate-spin" /> : <ShieldCheck size={15} />} Verify
        </button>
        <label className="btn h-10 cursor-pointer">
          <FileJson size={15} /> Drop a JSON file
          <input type="file" accept="application/json,.json" className="hidden" onChange={(e) => e.target.files?.[0] && void onFile(e.target.files[0])} />
        </label>
      </form>
      {err ? <ErrorBox error={err} /> : null}
      {res && (
        <div className="rise grid gap-4 md:grid-cols-2">
          <div>
            <ul className="flex flex-col gap-2.5">
              {res.localHash && row(true, `Fingerprint computed in this browser: ${shortHash(res.localHash)}`)}
              {row(res.found, res.found ? "A ROADPASS record has this fingerprint" : "No record has this fingerprint")}
              {res.found && row(res.latest, res.latest ? "It is the latest version" : "A newer version exists (this one was corrected)")}
              {res.found && row(res.sealStatus === "sealed" ? res.localRoot === res.merkleRoot : undefined, res.sealStatus === "sealed" ? "Merkle path replayed in this browser reaches the seal root" : "Not sealed yet")}
              {res.found && res.sealStatus === "sealed" && row(res.ledgerError ? undefined : res.anchored, res.ledgerError ? "Ledger not reachable for a live read" : "The seal root on the ledger matches")}
            </ul>
          </div>
          {res.found && (
            <KV items={[
              ["Record", res.recordId ? <span className="mono">{res.recordId} (v{res.version})</span> : "—"],
              ["Issued by", res.orgName],
              ["Recorded", dateTime(res.occurredAt)],
              ["Seal", res.sealId ? <span className="mono">{res.sealId}</span> : "—"],
              ["Transaction", res.txId ? <span className="mono">{shortHash(res.txId, 10)}</span> : "—"],
              ["Block", res.blockNumber ?? "—"],
            ]} />
          )}
        </div>
      )}
    </div>
  );
}

export default function Verify() {
  useSeo("Verify a record · ROADPASS", "Check a ROADPASS record yourself: paste its fingerprint or file and the signature and Merkle proof are re-computed in your browser.", "/verify");
  return (
    <PublicFrame>
      <h1 className="h1">Verify a record</h1>
      <p className="mt-2 max-w-2xl text-text-2">Paste a fingerprint from a delivery note or passport, or drop the passport's JSON file. Your browser computes the fingerprint and replays the proof itself.</p>
      <Card className="mt-6"><VerifyTool /></Card>
    </PublicFrame>
  );
}
