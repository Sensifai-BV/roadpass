import { useState } from "react";
import { CircleCheck, CircleX, Loader2, ShieldCheck, Fingerprint, PenLine, GitMerge, Blocks, CircleMinus } from "lucide-react";
import type { Trust } from "../lib/types";
import { fingerprint, merkleRoot, receiptMessage, recordMessage, verifyEd25519 } from "../lib/verify";
import { dateTime, shortHash } from "../lib/format";
import { Hash } from "./ui";

type Result = "pass" | "fail" | "skip" | "unsupported";
const RESULT_LABEL: Record<Result, string> = { pass: "Passed", fail: "Failed", skip: "Not checked", unsupported: "Not checked in this browser" };
interface StepState { label: string; detail: string; result?: Result }

/**
 * Re-checks a record in the browser: the fingerprint from the record's own
 * content, the company signature and the receipt with Ed25519, and the Merkle
 * path to the root committed on the ledger. Nothing is taken on trust from the
 * server except where the step says so.
 */
export function VerifyPanel({ trust, orgName }: { trust: Trust; orgName: (id: string) => string }) {
  const [steps, setSteps] = useState<StepState[] | null>(null);
  const [running, setRunning] = useState(false);
  // The public layer carries no record body: verify from the published fingerprint.
  const hasDoc = !!trust.hashDoc?.id;

  const run = async () => {
    setRunning(true);
    const out: StepState[] = [];
    const push = (s: StepState) => {
      out.push(s);
      setSteps([...out]);
    };
    const pause = () => new Promise((r) => setTimeout(r, 380));

    let hash = trust.contentHash;
    if (hasDoc) {
      const got = await fingerprint(trust.hashDoc);
      hash = got;
      push({ label: "Fingerprint recomputed", detail: `SHA-256 of the record's canonical JSON (RFC 8785) is ${shortHash(got)}`, result: got === trust.contentHash ? "pass" : "fail" });
    } else {
      push({ label: "Fingerprint", detail: "The public view does not include the record body; using the published fingerprint.", result: "skip" });
    }
    await pause();
    if (trust.signature) {
      const ok = await verifyEd25519(trust.signature.publicKey, trust.signature.value, recordMessage(hash));
      push({
        label: `Signed by ${orgName(trust.signature.orgId)}`,
        detail: ok === null ? "This browser has no Ed25519; the server's check says " + (trust.signature.valid ? "valid." : "invalid.") : `Ed25519 signature with key ${trust.signature.keyId}${trust.signature.onLedger ? ", registered on the ledger" : ""}.`,
        result: ok === null ? "unsupported" : ok ? "pass" : "fail",
      });
    } else {
      push({ label: "Signature", detail: "This record is not signed.", result: "fail" });
    }
    if (trust.actorSignature) {
      const a = trust.actorSignature;
      const ok = await verifyEd25519(a.publicKey, a.value, recordMessage(hash));
      push({
        label: `Signed personally by ${a.holderName ?? `a member of ${orgName(a.orgId)}`}`,
        detail: ok === null ? "This browser has no Ed25519; the server's check says " + (a.valid ? "valid." : "invalid.")
          : `Their own Ed25519 key ${a.keyId}${a.onLedger ? ", registered on the ledger" : ""}: this person, not only the company, stands behind the record.`,
        result: ok === null ? "unsupported" : ok ? "pass" : "fail",
      });
    }
    await pause();
    if (trust.receipt) {
      const rc = trust.receipt;
      const verdict = rc.decision === "rejected" ? "rejected at intake" : rc.decision === "accepted_with_note" ? "accepted with a note" : "accepted";
      const ok = rc.noteHidden ? null
        : await verifyEd25519(rc.publicKey, rc.value, await receiptMessage(hash, rc.receivedAt!, rc.massKg ?? 0, rc.decision, rc.note ?? ""));
      push({
        label: `Receipt signed by ${orgName(rc.orgId)}`,
        detail: rc.noteHidden
          ? `Received ${dateTime(rc.receivedAt)}, ${verdict}. The receiver's note is private, so this view cannot re-check the receipt; the server's check says ${rc.valid ? "valid" : "invalid"}.`
          : ok === null ? "Ed25519 not available in this browser."
          : `Received ${dateTime(rc.receivedAt)}, ${verdict}${rc.note ? ` (“${rc.note}”)` : ""}; countersigned by the receiver.`,
        result: ok === null ? (rc.noteHidden ? "skip" : "unsupported") : ok ? "pass" : "fail",
      });
      if (trust.receiptActor) {
        const a = trust.receiptActor;
        const pok = a.noteHidden ? null
          : await verifyEd25519(a.publicKey, a.value, await receiptMessage(hash, rc.receivedAt!, rc.massKg ?? 0, rc.decision, rc.note ?? ""));
        push({
          label: `Received personally by ${a.holderName ?? `a member of ${orgName(a.orgId)}`}`,
          detail: a.noteHidden ? "The receiver's note is private here; the server's check says " + (a.valid ? "valid." : "invalid.")
            : pok === null ? "Ed25519 not available in this browser." : `Their own key ${a.keyId} countersigns the intake review.`,
          result: pok === null ? (a.noteHidden ? "skip" : "unsupported") : pok ? "pass" : "fail",
        });
      }
      await pause();
    }
    if (trust.seal && trust.seal.status === "sealed") {
      // A seal of one record has an empty path (omitted by the API): the root is the leaf hash.
      const path = trust.path ?? [];
      const root = await merkleRoot(hash, path);
      push({ label: "Included in the seal", detail: path.length ? `${path.length} hashes lead from this record to the root ${shortHash(root)}.` : `This record is the only one in the seal; its leaf hash is the root ${shortHash(root)}.`, result: root === trust.seal.merkleRoot ? "pass" : "fail" });
      await pause();
      push({
        label: "Seal committed on the ledger",
        detail: `Seal ${trust.seal.id} is in ledger transaction ${shortHash(trust.seal.txId, 8)}${trust.seal.blockNumber ? `, block ${trust.seal.blockNumber}` : ""} (reported by the ledger).`,
        result: trust.seal.txId ? "pass" : "fail",
      });
    } else {
      push({ label: "Seal", detail: trust.seal ? "The seal is waiting for the ledger." : "Not sealed yet: records are sealed in batches once the company is verified and the checks pass.", result: "skip" });
    }
    setRunning(false);
  };

  const icon = (r?: Result) =>
    r === "pass" ? <CircleCheck size={18} className="pop text-good" /> : r === "fail" ? <CircleX size={18} className="pop text-critical" />
      : <CircleMinus size={18} className="pop text-muted" />;
  // A skipped step (no record body, not sealed yet) was not checked: say so instead of "everything checks out".
  const done = steps && !running;
  const failed = done ? steps.filter((s) => s.result === "fail").length : 0;
  const unchecked = done ? steps.filter((s) => s.result === "skip" || s.result === "unsupported").length : 0;

  return (
    <div>
      <div className="grid gap-x-6 gap-y-3 text-[13px] sm:grid-cols-2">
        <Field icon={<Fingerprint size={14} />} label="Fingerprint"><Hash value={trust.contentHash} n={12} /></Field>
        <Field icon={<PenLine size={14} />} label="Signature">
          {trust.signature ? <span>{orgName(trust.signature.orgId)} · <span className="mono text-text-2">{trust.signature.keyId}</span></span> : "—"}
        </Field>
        <Field icon={<GitMerge size={14} />} label="Seal">
          {trust.seal ? (
            <span className="text-text-2">
              <span className="mono">{trust.seal.id}</span> · record {trust.leafIndex + 1} of {trust.seal.leafCount}
              {trust.seal.status !== "sealed" && <span className="text-muted"> · waiting for the ledger</span>}
            </span>
          ) : "Not sealed yet"}
        </Field>
        <Field icon={<Blocks size={14} />} label="Ledger">
          {trust.seal?.txId ? <span className="mono text-text-2" title={trust.seal.txId}>tx {shortHash(trust.seal.txId, 8)}{trust.seal.blockNumber ? ` · block ${trust.seal.blockNumber}` : ""}</span> : "Not on the ledger yet"}
        </Field>
      </div>
      <div className="mt-5 flex items-center gap-3">
        <button className="btn btn-accent" onClick={run} disabled={running}>
          {running ? <Loader2 size={15} className="animate-spin" /> : <ShieldCheck size={15} />} Verify in this browser
        </button>
        {done && (failed > 0
          ? <span className="pop text-[13px] font-semibold text-critical">{failed} check{failed === 1 ? "" : "s"} failed.</span>
          : unchecked > 0
            ? <span className="pop text-[13px] font-semibold text-text-2">No problems found; {unchecked} step{unchecked === 1 ? "" : "s"} could not be checked here.</span>
            : <span className="pop text-[13px] font-semibold text-good">Everything checks out.</span>)}
      </div>
      {steps && (
        <ol className="mt-4 flex flex-col gap-2.5" aria-live="polite">
          {steps.map((s, i) => (
            <li key={i} className="rise flex items-start gap-3 rounded-xl bg-surface-2 px-3.5 py-2.5">
              <span className="mt-px" role="img" aria-label={RESULT_LABEL[s.result ?? "skip"]}>{icon(s.result)}</span>
              <span>
                <span className="block text-[13px] font-semibold">{s.label}</span>
                <span className="block text-[12px] text-text-2">{s.detail}</span>
              </span>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

function Field({ icon, label, children }: { icon: React.ReactNode; label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2.5">
      <span className="mt-0.5 text-muted">{icon}</span>
      <div className="min-w-0">
        <div className="text-[11.5px] font-medium text-muted">{label}</div>
        <div className="min-w-0 break-all">{children}</div>
      </div>
    </div>
  );
}
