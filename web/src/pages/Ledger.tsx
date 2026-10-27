import { HELP } from "../lib/help";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Blocks, ChevronDown, ChevronRight, CircleCheck, CircleX, Layers, KeyRound, WifiOff } from "lucide-react";
import clsx from "clsx";
import { api, qs } from "../lib/api";
import { useDirectory, useMe } from "../lib/hooks";
import type { LedgerSummary, OrgBlocks, Seal, SealDetail, Block } from "../lib/types";
import { KIND, TYPE } from "../lib/vocab";
import { ago, dateTime, num, pct, shortHash } from "../lib/format";
import { Card, Drawer, Empty, ErrorBox, Hash, KV, PageHeader, Skel, Stat, Segmented } from "../components/ui";
import { VerifyTool } from "./Verify";

export default function Ledger() {
  const me = useMe().data!;
  const [tab, setTab] = useState<"orgs" | "seals" | "blocks" | "keys" | "verify">("orgs");
  const [seal, setSeal] = useState<string | null>(null);
  const [block, setBlock] = useState<number | null>(null);
  const sum = useQuery({ queryKey: ["ledger-summary"], queryFn: () => api.get<LedgerSummary>("/ledger/summary"), refetchInterval: 15_000 });
  const s = sum.data;
  const mine = s?.mine ?? s?.platform;
  // The organisation's own last seal (the platform's for oversight), not another company's.
  const last = mine?.lastSeal;

  return (
    <div>
      <PageHeader title="Ledger" sub={me.oversight
        ? `Ledger channel “${s?.channel ?? "roadpass"}”: every seal, block and registered signing key.`
        : "Your records on the ledger: the seals that hold them, the blocks behind those seals, and your company's signing keys."} />
      {s && !s.online && (
        <div className="mb-4 flex items-start gap-3 rounded-2xl bg-surface-3 px-4 py-3 text-[13px]">
          <WifiOff size={17} className="mt-0.5 shrink-0 text-muted" />
          <div><b>Ledger offline.</b> Records keep arriving and being checked; they are sealed in order as soon as the network is back.{s.error ? <span className="text-muted"> ({s.error})</span> : null}</div>
        </div>
      )}
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <Stat help="Number of blocks on the ledger channel. Each seal adds a transaction; blocks only grow." label="Block height" icon={<Blocks size={14} />} color="var(--accent)" value={s?.chain ? num(s.chain.height) : "—"} loading={sum.isLoading}
          hint={s?.chain ? `head ${shortHash(s.chain.currentBlockHash, 6)}` : "not connected"} />
        <Stat help={HELP.sealedShare} label={me.oversight ? "Records sealed" : "Your records sealed"} icon={<CircleCheck size={14} />} color="var(--s6)" loading={sum.isLoading}
          value={mine && mine.records > 0 ? pct((mine.sealed / mine.records) * 100) : "—"}
          hint={mine ? (mine.records ? `${num(mine.sealed)} of ${num(mine.records)} versions · ${num(mine.currentSealed)} of ${num(mine.current)} current records` : "no records yet") : undefined} />
        <Stat help="Records signed but not yet in a seal. The worker seals new records every few seconds; records of unverified companies wait until verification." label="Waiting to be sealed" icon={<Layers size={14} />} color="var(--s4)" value={mine ? num(mine.pending) : "—"} loading={sum.isLoading}
          hint={mine?.blocked ? `${num(mine.blocked)} blocked by data checks` : "nothing blocked"} />
        <Stat help={HELP.sealed} label={me.oversight ? "Last seal" : "Your last seal"} icon={<KeyRound size={14} />} color="var(--s3)" value={last ? ago(last.sealedAt) : "—"} loading={sum.isLoading}
          hint={last ? `${num(last.leafCount)} record version${last.leafCount === 1 ? "" : "s"}${me.oversight ? " · whole platform" : ""}` : undefined} />
      </div>

      <div className="mb-3 mt-6">
        <Segmented value={tab} onChange={setTab} label="View" options={[
          { value: "orgs", label: me.oversight ? "By organisation" : "My blocks" },
          { value: "seals", label: me.oversight ? "Seals" : "My seals" },
          ...(me.oversight ? [{ value: "blocks" as const, label: "Blocks" }] : []),
          { value: "keys", label: "Signing keys" },
          { value: "verify", label: "Verify" },
        ]} />
      </div>
      {tab === "orgs" && <ByOrg oversight={me.oversight} onSeal={setSeal} onBlock={setBlock} />}
      {tab === "seals" && <Seals onOpen={setSeal} />}
      {tab === "blocks" && <Blocks_ onOpen={setBlock} />}
      {tab === "keys" && <Keys />}
      {tab === "verify" && <Card title="Verify anything" sub="A fingerprint or a passport JSON file. The browser computes the fingerprint and replays the Merkle path."><VerifyTool /></Card>}

      <SealDrawer id={seal} onClose={() => setSeal(null)} onBlock={(n) => { setSeal(null); setBlock(n); }} />
      <BlockDrawer n={block} onClose={() => setBlock(null)} />
    </div>
  );
}

// Every seal is dedicated to one organisation and each seal transaction gets its
// own block, so the ledger splits cleanly: each organisation owns its blocks.
function ByOrg({ oversight, onSeal, onBlock }: { oversight: boolean; onSeal: (id: string) => void; onBlock: (n: number) => void }) {
  const q = useQuery({ queryKey: ["ledger-orgs"], queryFn: () => api.get<OrgBlocks[]>("/ledger/orgs"), refetchInterval: 15_000 });
  const dir = useDirectory();
  const [open, setOpen] = useState<string | null>(null);
  if (q.isError) return <ErrorBox error={q.error} />;
  if (q.isLoading) return <Skel h={360} />;
  const rows = q.data ?? [];
  if (!rows.length) return <Card><Empty icon={<Blocks size={20} />} title="No blocks yet">Each verified organisation gets its own blocks: its records are sealed in seals of their own, one seal transaction per block.</Empty></Card>;
  const dedicated = rows.filter((r) => r.orgId);
  const shared = rows.find((r) => !r.orgId);
  const expanded = open ?? (!oversight ? dedicated[0]?.orgId ?? null : null);
  return (
    <Card pad={false} title={oversight ? "Blocks by organisation" : "Your blocks"}
      sub={oversight
        ? `${num(dedicated.length)} organisation${dedicated.length === 1 ? "" : "s"} with dedicated blocks. Each block holds one seal transaction of one organisation's records.`
        : "Blocks dedicated to your organisation: each holds one seal transaction over your records only."}>
      <table className="table">
        <thead><tr><th className="w-8" /><th>Organisation</th><th className="text-right">Seals</th><th className="text-right">Records</th><th>Blocks</th><th className="whitespace-nowrap">Last sealed</th></tr></thead>
        <tbody>
          {[...dedicated, ...(shared ? [shared] : [])].map((r) => {
            const key = r.orgId || "shared";
            const isOpen = expanded === key;
            const o = dir.org(r.orgId);
            return (
              <OrgRow key={key} row={r} isOpen={isOpen} onToggle={() => setOpen(isOpen ? "" : key)} onSeal={onSeal} onBlock={onBlock}
                name={r.orgId ? dir.orgName(r.orgId) : "Shared (before dedicated blocks)"}
                kind={o ? KIND[o.kind]?.name : undefined} kindColor={o ? KIND[o.kind]?.color : undefined} />
            );
          })}
        </tbody>
      </table>
    </Card>
  );
}

function OrgRow({ row, name, kind, kindColor, isOpen, onToggle, onSeal, onBlock }: {
  row: OrgBlocks; name: string; kind?: string; kindColor?: string; isOpen: boolean; onToggle: () => void; onSeal: (id: string) => void; onBlock: (n: number) => void;
}) {
  const key = row.orgId || "shared";
  const seals = useQuery({ queryKey: ["org-seals", key], queryFn: () => api.get<Seal[]>(`/ledger/orgs/${encodeURIComponent(key)}/seals`), enabled: isOpen });
  const shown = row.blocks.slice(0, 24);
  return (
    <>
      <tr className="clickable" onClick={onToggle} aria-expanded={isOpen}>
        <td className="text-muted">{isOpen ? <ChevronDown size={15} /> : <ChevronRight size={15} />}</td>
        <td>
          <div className="font-medium">{name}</div>
          {kind && <div className="mt-0.5 inline-flex items-center gap-1.5 text-[12px] text-muted"><span className="inline-block size-2 rounded-full" style={{ background: kindColor }} />{kind}</div>}
        </td>
        <td className="num text-right">{num(row.seals)}{row.pendingSeals ? <span className="text-muted"> +{row.pendingSeals} pending</span> : null}</td>
        <td className="num text-right">{num(row.records)}</td>
        <td>
          <div className="flex flex-wrap gap-1">
            {shown.map((n) => (
              <button key={n} className="chip mono hover:bg-surface-3" title={`Open block ${n}`} onClick={(e) => { e.stopPropagation(); onBlock(n); }}>{n}</button>
            ))}
            {row.blocks.length > shown.length && <span className="chip text-muted">+{num(row.blocks.length - shown.length)} more</span>}
            {!row.blocks.length && <span className="text-muted">—</span>}
          </div>
        </td>
        <td className="whitespace-nowrap text-text-2">{row.lastSealedAt ? ago(row.lastSealedAt) : "—"}</td>
      </tr>
      {isOpen && (
        <tr>
          <td />
          <td colSpan={5} className="bg-surface-2">
            <div className="eyebrow mb-2">Seal transactions</div>
            {seals.isLoading ? <Skel h={120} /> : seals.isError ? <ErrorBox error={seals.error} /> : (
              <div className="max-h-[360px] overflow-auto rounded-xl border border-line bg-surface scroll-thin">
                <table className="table text-[12.5px]">
                  <thead><tr><th className="text-right">Block</th><th>Seal</th><th>Transaction</th><th>Merkle root</th><th className="text-right">Records</th><th>Sealed</th><th>Status</th></tr></thead>
                  <tbody>
                    {(seals.data ?? []).map((s) => (
                      <tr key={s.id} className="clickable" onClick={() => onSeal(s.id)}>
                        <td className="num text-right font-semibold">{s.blockNumber ? <button className="text-accent-ink hover:underline" onClick={(e) => { e.stopPropagation(); onBlock(s.blockNumber!); }}>{num(s.blockNumber)}</button> : "—"}</td>
                        <td className="mono">{s.id}</td>
                        <td className="mono text-text-2">{shortHash(s.txId, 6)}</td>
                        <td className="mono text-text-2">{shortHash(s.merkleRoot, 8)}</td>
                        <td className="num text-right">{num(s.leafCount)}</td>
                        <td className="text-text-2">{s.sealedAt ? ago(s.sealedAt) : "—"}</td>
                        <td><span className={clsx("chip", s.status === "sealed" ? "chip-good" : s.status === "failed" ? "chip-critical" : "")}>{s.status}</span></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </td>
        </tr>
      )}
    </>
  );
}

function Seals({ onOpen }: { onOpen: (id: string) => void }) {
  const q = useQuery({ queryKey: ["seals"], queryFn: () => api.get<Seal[]>(`/ledger/seals${qs({ limit: 100 })}`) });
  if (q.isLoading) return <Skel h={360} />;
  if (!q.data?.length) return <Card><Empty icon={<Blocks size={20} />} title="No seals yet">Records are sealed in batches of up to 4,096 once the ledger is reachable and your organisation is verified.</Empty></Card>;
  return (
    <Card pad={false}>
      <table className="table">
        <thead><tr><th>Seal</th><th>Merkle root</th><th className="text-right">Records</th><th className="text-right">Yours</th><th>Transaction</th><th className="text-right">Block</th><th>Sealed</th><th>Status</th></tr></thead>
        <tbody>
          {q.data.map((s) => (
            <tr key={s.id} className="clickable" onClick={() => onOpen(s.id)}>
              <td className="mono text-[12.5px] font-semibold">{s.id}</td>
              <td className="mono text-[12px] text-text-2">{shortHash(s.merkleRoot, 8)}</td>
              <td className="num text-right">{num(s.leafCount)}</td>
              <td className="num text-right">{num(s.mine ?? 0)}</td>
              <td className="mono text-[12px] text-text-2">{shortHash(s.txId, 6)}</td>
              <td className="num text-right">{s.blockNumber ? num(s.blockNumber) : "—"}</td>
              <td className="text-text-2">{s.sealedAt ? ago(s.sealedAt) : "—"}</td>
              <td><span className={clsx("chip", s.status === "sealed" ? "chip-good" : s.status === "failed" ? "chip-critical" : "")}>{s.status}</span></td>
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  );
}

function Blocks_({ onOpen }: { onOpen: (n: number) => void }) {
  const q = useQuery({ queryKey: ["blocks"], queryFn: () => api.get<{ blocks: Block[] }>("/ledger/blocks?limit=25"), refetchInterval: 15_000 });
  if (q.isError) return <ErrorBox error={q.error} />;
  if (q.isLoading) return <Skel h={360} />;
  return (
    <Card pad={false}>
      <table className="table">
        <thead><tr><th className="text-right">Block</th><th>Data hash</th><th>Transactions</th><th>Created</th><th className="text-right">Size</th></tr></thead>
        <tbody>
          {q.data!.blocks.map((b) => (
            <tr key={b.number} className="clickable" onClick={() => onOpen(b.number)}>
              <td className="num text-right font-semibold">{num(b.number)}</td>
              <td className="mono text-[12px] text-text-2">{shortHash(b.dataHash, 10)}</td>
              <td className="text-[12.5px]">{b.txs.map((t) => t.function || t.type).join(", ")}</td>
              <td className="text-text-2">{b.txs[0] ? dateTime(b.txs[0].timestamp) : "—"}</td>
              <td className="num text-right text-text-2">{num(b.sizeBytes / 1024, 1)} KB</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  );
}

function Keys() {
  const q = useQuery({ queryKey: ["signers"], queryFn: () => api.get<{ keyId: string; orgId: string; status: string; onChain?: { publicKey: string; registeredAt: string; txId: string; status: string } }[]>("/ledger/signers") });
  const dir = useDirectory();
  if (q.isLoading) return <Skel h={300} />;
  return (
    <Card pad={false} title="Signing keys on the ledger" sub="Each company signs its records with its own Ed25519 key. The public half is registered on the ledger, so anyone can check a signature without trusting this database.">
      <table className="table">
        <thead><tr><th>Organisation</th><th>Key</th><th>Public key</th><th>On the ledger</th><th>Status</th></tr></thead>
        <tbody>
          {(q.data ?? []).map((k) => (
            <tr key={k.keyId}>
              <td className="font-medium">{dir.orgName(k.orgId)}</td>
              <td className="mono text-[12px]">{k.keyId}</td>
              <td className="mono text-[12px] text-text-2">{k.onChain ? shortHash(k.onChain.publicKey, 10) : "—"}</td>
              <td className="text-[12.5px]">{k.onChain ? <span className="inline-flex items-center gap-1 text-good"><CircleCheck size={13} /> {dateTime(k.onChain.registeredAt)}</span> : <span className="inline-flex items-center gap-1 text-muted"><CircleX size={13} /> not yet</span>}</td>
              <td><span className={clsx("chip", k.status === "active" ? "chip-good" : "chip-critical")}>{k.status}</span></td>
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  );
}

function SealDrawer({ id, onClose, onBlock }: { id: string | null; onClose: () => void; onBlock: (n: number) => void }) {
  const q = useQuery({ queryKey: ["seal", id], queryFn: () => api.get<SealDetail>(`/ledger/seals/${id}`), enabled: !!id });
  const dir = useDirectory();
  const d = q.data;
  return (
    <Drawer open={!!id} onOpenChange={(o) => !o && onClose()} title={`Seal ${id ?? ""}`} sub="One ledger transaction committing to up to 4,096 record fingerprints." width={640}>
      {q.isLoading ? <Skel h={300} /> : q.isError ? <ErrorBox error={q.error} /> : d && (
        <div className="flex flex-col gap-6">
          <KV items={[
            ["Merkle root", <Hash key="r" value={d.seal.merkleRoot} n={14} />],
            ["On the ledger", d.onChain ? (d.onChain.merkleRoot === d.seal.merkleRoot ? <span key="m" className="inline-flex items-center gap-1 text-good"><CircleCheck size={14} />root matches the chaincode's</span> : <span key="m" className="text-critical">root differs</span>) : d.ledgerError ?? "—"],
            ["Organisation", d.seal.orgId ? dir.orgName(d.seal.orgId) : "shared (before dedicated blocks)"],
            ["Records", `${num(d.seal.leafCount)} (yours: ${num(d.mine)})`],
            ["Sequence", `${num(d.seal.firstSeq)} – ${num(d.seal.lastSeq)}`],
            ["Provenance", d.seal.provenance],
            ["Transaction", <Hash key="t" value={d.seal.txId} n={12} />],
            ["Block", d.block ? <button key="b" className="font-semibold text-accent-ink hover:underline" onClick={() => onBlock(d.block!.number)}>{num(d.block.number)}</button> : "—"],
            ["Sealed", dateTime(d.seal.sealedAt)],
          ]} />
          <div>
            <div className="eyebrow mb-2">Records in this seal {d.records.length < d.mine ? `(first ${d.records.length})` : ""}</div>
            <div className="max-h-[360px] overflow-auto rounded-xl border border-line scroll-thin">
              <table className="table text-[12.5px]">
                <thead><tr><th className="text-right">Leaf</th><th>Record</th><th>Organisation</th><th>Fingerprint</th></tr></thead>
                <tbody>
                  {d.records.map((r) => (
                    <tr key={r.id + r.version}>
                      <td className="num text-right text-muted">{r.leafIndex}</td>
                      <td>{TYPE[r.type]?.passport ? <Link to={`/passports/${r.id}`} className="mono text-accent-ink">{r.id}</Link> : <span className="mono">{r.id}</span>}{r.version > 1 && <span className="text-muted"> v{r.version}</span>}</td>
                      <td className="text-text-2">{dir.orgName(r.orgId)}</td>
                      <td className="mono text-text-2">{shortHash(r.contentHash, 6)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </Drawer>
  );
}

function BlockDrawer({ n, onClose }: { n: number | null; onClose: () => void }) {
  const q = useQuery({ queryKey: ["block", n], queryFn: () => api.get<Block>(`/ledger/blocks/${n}`), enabled: n !== null });
  const b = q.data;
  return (
    <Drawer open={n !== null} onOpenChange={(o) => !o && onClose()} title={`Block ${n ?? ""}`} sub="Decoded from the peer (qscc). Other organisations' transactions are shown without their contents." width={640}>
      {q.isLoading ? <Skel h={240} /> : q.isError ? <ErrorBox error={q.error} /> : b && (
        <div className="flex flex-col gap-5">
          <KV items={[["Data hash", <Hash key="d" value={b.dataHash} n={14} />], ["Previous hash", <Hash key="p" value={b.previousHash} n={14} />], ["Size", `${num(b.sizeBytes / 1024, 1)} KB`]]} />
          <ul className="flex flex-col gap-2">
            {b.txs.map((t) => (
              <li key={t.txId} className="rounded-xl bg-surface-2 px-3.5 py-2.5 text-[12.5px]">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-semibold">{t.function || t.type}{t.subject ? <span className="mono font-normal text-text-2"> · {t.subject}</span> : null}</span>
                  {t.valid ? <span className="chip chip-good">valid</span> : <span className="chip chip-critical">{t.validation}</span>}
                </div>
                <div className="mt-1 text-muted">{t.creatorMsp} · {t.chaincode ?? "config"} · {dateTime(t.timestamp)}</div>
                <div className="mono mt-0.5 text-[11px] text-muted">{t.txId}</div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Drawer>
  );
}
