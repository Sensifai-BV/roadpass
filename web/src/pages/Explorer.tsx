import { useState, type ReactNode } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, Blocks, CircleCheck, Layers, Search, ShieldCheck, WifiOff, Building2, Lock } from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";
import type { ExplorerBlock, ExplorerOrg, ExplorerOrgDetail, ExplorerSeal, ExplorerSummary, ExplorerTx, PublicRecord, Seal } from "../lib/types";
import { KIND, TYPE } from "../lib/vocab";
import { countryName } from "../lib/countries";
import { ago, dateTime, num, shortHash } from "../lib/format";
import { ErrorBox, Hash, KV, Skel } from "../components/ui";
import { MarketingFrame } from "../components/Marketing";
import { useSeo } from "../lib/seo";

const poll = { refetchInterval: 15_000 };

function Panel({ title, sub, children, pad = true }: { title?: ReactNode; sub?: ReactNode; children: ReactNode; pad?: boolean }) {
  return (
    <section className="rounded-[20px] bg-surface shadow-[var(--shadow-1)]">
      {title && (
        <header className="px-5 pt-5 sm:px-6">
          <h2 className="text-[17px] font-semibold tracking-tight">{title}</h2>
          {sub && <p className="mt-1 text-[13px] text-text-2">{sub}</p>}
        </header>
      )}
      <div className={clsx(pad && "p-5 sm:p-6", !pad && title && "pt-4")}>{children}</div>
    </section>
  );
}

function Tile({ icon, label, value, hint }: { icon: ReactNode; label: string; value: ReactNode; hint?: ReactNode }) {
  return (
    <div className="rounded-[20px] bg-surface p-5 shadow-[var(--shadow-1)]">
      <div className="flex items-center gap-2 text-[12.5px] font-medium text-text-2"><span className="text-[#93D500]">{icon}</span>{label}</div>
      <div className="mt-2 text-[28px] font-bold tracking-tight num">{value}</div>
      {hint && <div className="mt-0.5 truncate text-[12px] text-muted">{hint}</div>}
    </div>
  );
}

function SearchBar() {
  const nav = useNavigate();
  const [q, setQ] = useState("");
  const [err, setErr] = useState("");
  const go = async (e: React.FormEvent) => {
    e.preventDefault();
    const v = q.trim();
    if (!v) return;
    setErr("");
    try {
      const r = await api.get<{ kind: string; id: string }>(`/public/explorer/search?q=${encodeURIComponent(v)}`);
      nav(`/explorer/${r.kind}/${encodeURIComponent(r.id)}`);
    } catch {
      setErr("Nothing public matches that. Organisations that keep their ledger data private are not searchable.");
    }
  };
  return (
    <form onSubmit={go} className="mt-7 max-w-[720px]">
      <div className="flex items-center gap-2 rounded-full bg-surface p-1.5 pl-4 shadow-[var(--shadow-1)] ring-1 ring-line focus-within:ring-2 focus-within:ring-[#93D500]">
        <Search size={17} className="shrink-0 text-muted" aria-hidden />
        <input aria-label="Search the explorer" className="min-w-0 flex-1 bg-transparent py-2 text-[14.5px] outline-none placeholder:text-muted"
          placeholder="Block number, seal, transaction, Merkle root or record fingerprint" value={q} onChange={(e) => setQ(e.target.value)} />
        <button className="btn btn-primary h-10 px-5" type="submit">Search</button>
      </div>
      {err && <p role="alert" className="mt-2 pl-4 text-[13px] text-critical">{err}</p>}
    </form>
  );
}

function Header({ children }: { children?: ReactNode }) {
  return (
    <section className="relative isolate overflow-hidden">
      <div className="mk-glow pointer-events-none absolute inset-x-0 top-0 -z-10 h-[420px]" aria-hidden />
      <div className="mx-auto max-w-[1180px] px-5 pb-8 pt-28 sm:pt-32">
        <span className="inline-flex items-center gap-2 rounded-full bg-[var(--surface-3)] px-3.5 py-1.5 text-[12.5px] font-semibold ring-1 ring-line">
          <span className="h-1.5 w-1.5 rounded-full bg-[#93D500]" /> Public ledger explorer
        </span>
        <h1 className="mt-5 text-[clamp(32px,6vw,56px)] font-bold leading-[1.02] tracking-[-0.04em]">Explore the <span className="text-[#93D500]">blockchain ledger</span></h1>
        <p className="mk-lead mt-4 max-w-[640px]">
          Every block, transaction and Merkle root. Organisations that keep their ledger data private show hashes only; those that chose to publish also show their supply-chain details. Records themselves are never on the ledger, only their fingerprints.
        </p>
        {children}
      </div>
    </section>
  );
}

const status = (s: Seal) => <span className={clsx("chip", s.status === "sealed" ? "chip-good" : "")}>{s.status}</span>;
const typeName = (t: string) => TYPE[t as keyof typeof TYPE]?.name ?? t;

/** Who owns a transaction: the organisation when it is public, a hashes-only marker when it is not. */
function Owner({ tx }: { tx: ExplorerTx }) {
  if (tx.public && tx.orgId) return <Link to={`/explorer/org/${encodeURIComponent(tx.orgId)}`} className="font-medium hover:underline" onClick={(e) => e.stopPropagation()}>{tx.orgName ?? tx.orgId}</Link>;
  return <span className="inline-flex items-center gap-1.5 text-muted"><Lock size={12} aria-hidden />Private organisation</span>;
}

function Home() {
  const sum = useQuery({ queryKey: ["x-sum"], queryFn: () => api.get<ExplorerSummary>("/public/explorer/summary"), ...poll });
  const blocks = useQuery({ queryKey: ["x-blocks"], queryFn: () => api.get<{ blocks: ExplorerBlock[] }>("/public/explorer/blocks?limit=20"), ...poll });
  const orgs = useQuery({ queryKey: ["x-orgs"], queryFn: () => api.get<ExplorerOrg[]>("/public/explorer/orgs"), ...poll });
  const s = sum.data;
  return (
    <>
      <Header><SearchBar /></Header>
      <div className="mx-auto flex max-w-[1180px] flex-col gap-5 px-5 pb-20">
        {s && !s.online && (
          <div className="flex items-start gap-3 rounded-2xl bg-surface-3 px-4 py-3 text-[13px]"><WifiOff size={17} className="mt-0.5 shrink-0 text-muted" />
            <div><b>The ledger is offline.</b> Block details are unavailable until it is back.</div></div>
        )}
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
          <Tile icon={<Blocks size={15} />} label="Ledger height" value={s?.chain ? num(s.chain.height) : "—"} hint={s?.chain ? `channel ${s.channel}` : "not connected"} />
          <Tile icon={<Layers size={15} />} label="Seals" value={s ? num(s.seals) : "—"} hint={s ? `in ${num(s.blocks)} blocks` : undefined} />
          <Tile icon={<ShieldCheck size={15} />} label="Records sealed" value={s ? num(s.records) : "—"} hint="fingerprints, not data" />
          <Tile icon={<Building2 size={15} />} label="Public organisations" value={s ? num(s.orgs) : "—"} hint="share their supply-chain details" />
        </div>

        <Panel title="Latest blocks" sub="Every block of the ledger. Organisations that keep their ledger data private show hashes only; public ones show who sealed what." pad={false}>
          {blocks.isError ? <div className="px-6 pb-6"><ErrorBox error={blocks.error} /></div> : blocks.isLoading ? <div className="p-6"><Skel h={280} /></div> : !blocks.data?.blocks.length ? (
            <p className="px-6 pb-8 pt-4 text-[13.5px] text-text-2">No blocks yet.</p>
          ) : (
            <div className="overflow-x-auto"><table className="table">
              <thead><tr><th className="text-right">Block</th><th>Transaction</th><th>Organisation</th><th>Merkle root</th><th>Data hash</th><th>Time</th></tr></thead>
              <tbody>
                {blocks.data.blocks.map((b) => {
                  const t = b.txs[0];
                  return (
                    <tr key={b.number} className="clickable" onClick={() => location.assign(`/explorer/block/${b.number}`)}>
                      <td className="num text-right font-semibold"><Link to={`/explorer/block/${b.number}`} className="text-accent-ink hover:underline" onClick={(e) => e.stopPropagation()}>{num(b.number)}</Link></td>
                      <td className="text-[12.5px]">{t ? (t.function || t.type) : "—"}{b.txs.length > 1 ? <span className="text-muted"> +{b.txs.length - 1}</span> : null}</td>
                      <td className="text-[13px]">{t ? <Owner tx={t} /> : "—"}</td>
                      <td className="mono text-[12px] text-text-2">{t?.merkleRoot ? shortHash(t.merkleRoot, 8) : "—"}</td>
                      <td className="mono text-[12px] text-text-2">{shortHash(b.dataHash, 8)}</td>
                      <td className="whitespace-nowrap text-text-2">{t ? ago(t.timestamp) : "—"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table></div>
          )}
        </Panel>

        <Panel title="Public organisations" sub="Each organisation decides in its settings whether to share its supply-chain details here. The default is private." pad={false}>
          {orgs.isLoading ? <div className="p-6"><Skel h={140} /></div> : !orgs.data?.length ? <p className="px-6 pb-8 pt-4 text-[13.5px] text-text-2">No organisation has published its ledger data yet.</p> : (
            <div className="overflow-x-auto"><table className="table">
              <thead><tr><th>Organisation</th><th className="text-right">Seals</th><th className="text-right">Records</th><th>Latest blocks</th><th>Last sealed</th></tr></thead>
              <tbody>
                {orgs.data.map((o) => (
                  <tr key={o.id} className="clickable" onClick={() => location.assign(`/explorer/org/${encodeURIComponent(o.id)}`)}>
                    <td><Link to={`/explorer/org/${encodeURIComponent(o.id)}`} className="font-medium hover:underline" onClick={(e) => e.stopPropagation()}>{o.name}</Link>
                      <div className="mt-0.5 flex items-center gap-1.5 text-[12px] text-muted"><span className="inline-block size-2 rounded-full" style={{ background: KIND[o.kind]?.color }} />{KIND[o.kind]?.name}{o.country ? ` · ${countryName(o.country)}` : ""}{o.demo ? " · demo" : ""}</div></td>
                    <td className="num text-right">{num(o.seals)}</td>
                    <td className="num text-right">{num(o.records)}</td>
                    <td><div className="flex flex-wrap gap-1">{o.blocks.slice(0, 6).map((n) => <Link key={n} to={`/explorer/block/${n}`} className="chip mono hover:bg-surface-3" onClick={(e) => e.stopPropagation()}>{n}</Link>)}{!o.blocks.length && <span className="text-muted">—</span>}</div></td>
                    <td className="whitespace-nowrap text-text-2">{o.lastSealedAt ? ago(o.lastSealedAt) : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table></div>
          )}
        </Panel>
      </div>
    </>
  );
}

function Back({ to = "/explorer", label = "Explorer" }: { to?: string; label?: string }) {
  return <Link to={to} className="mb-4 inline-flex items-center gap-1.5 text-[13px] font-medium text-text-2 hover:text-text"><ArrowLeft size={14} />{label}</Link>;
}

function Detail({ title, sub, children }: { title: ReactNode; sub?: ReactNode; children: ReactNode }) {
  return (
    <div className="mx-auto max-w-[1180px] px-5 pb-20 pt-28">
      <Back />
      <h1 className="text-[clamp(26px,4vw,38px)] font-bold tracking-[-0.035em]">{title}</h1>
      {sub && <p className="mk-lead mt-2 max-w-[680px]">{sub}</p>}
      <div className="mt-7 flex flex-col gap-5">{children}</div>
    </div>
  );
}

function NotPublic({ what }: { what: string }) {
  return (
    <Detail title={`${what} not found`} sub="It does not exist, or its organisation keeps its ledger data private.">
      <div><Link to="/explorer" className="btn btn-primary">Back to the explorer</Link></div>
    </Detail>
  );
}

function TxCard({ t }: { t: ExplorerTx }) {
  return (
    <li className="rounded-xl bg-surface-2 px-4 py-3 text-[13px]">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="font-semibold">{t.function || t.type}{t.public && t.sealId ? <> · <Link className="mono font-normal text-accent-ink hover:underline" to={`/explorer/seal/${encodeURIComponent(t.sealId)}`}>{t.sealId}</Link></> : null}</span>
        {t.valid ? <span className="chip chip-good">valid</span> : <span className="chip chip-critical">{t.validation}</span>}
      </div>
      <div className="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12.5px]">
        <Owner tx={t} />
        {t.public && t.records ? <span className="text-text-2">{num(t.records)} records · {t.provenance}</span> : null}
        {t.public && t.keyId ? <span className="mono text-text-2">{t.keyId}</span> : null}
      </div>
      {t.merkleRoot && <div className="mt-1 flex items-center gap-2 text-text-2"><span className="text-muted">Merkle root</span><Hash value={t.merkleRoot} n={16} /></div>}
      <div className="mt-1 text-muted">{t.creatorMsp} · {dateTime(t.timestamp)}</div>
      <div className="mono mt-0.5 break-all text-[11.5px] text-muted">{t.txId}</div>
    </li>
  );
}

function BlockPage({ n }: { n: string }) {
  const q = useQuery({ queryKey: ["x-block", n], queryFn: () => api.get<ExplorerBlock>(`/public/explorer/blocks/${n}`), retry: false });
  if (q.isError) return <NotPublic what="Block" />;
  const b = q.data;
  const k = Number(n);
  return (
    <Detail title={<>Block <span className="num">{num(k)}</span></>} sub="Decoded from the ledger peer. A private organisation's transaction shows only its hashes.">
      {!b ? <Skel h={260} /> : (
        <>
          <Panel title="Header">
            <KV items={[
              ["Data hash", <Hash key="d" value={b.dataHash} n={16} />],
              ["Previous block hash", <Hash key="p" value={b.previousHash} n={16} />],
              ["Size", `${num(b.sizeBytes / 1024, 1)} KB`],
              ["Transactions", num(b.txs.length)],
            ]} />
            <div className="mt-4 flex gap-2">
              {k > 0 && <Link to={`/explorer/block/${k - 1}`} className="btn btn-sm"><ArrowLeft size={13} /> Block {k - 1}</Link>}
              <Link to={`/explorer/block/${k + 1}`} className="btn btn-sm">Block {k + 1} <ArrowRight size={13} /></Link>
            </div>
          </Panel>
          <Panel title="Transactions"><ul className="flex flex-col gap-2">{b.txs.map((t) => <TxCard key={t.txId} t={t} />)}</ul></Panel>
        </>
      )}
    </Detail>
  );
}

function RecordTable({ rows, empty }: { rows: PublicRecord[]; empty?: string }) {
  if (!rows.length) return <p className="px-6 pb-6 pt-3 text-[13.5px] text-text-2">{empty ?? "Nothing yet."}</p>;
  return (
    <div className="max-h-[460px] overflow-auto scroll-thin"><table className="table text-[12.5px]">
      <thead><tr><th className="text-right">Leaf</th><th>Record</th><th>Type</th><th>Reference</th><th>Occurred</th><th className="text-right">Mass (kg)</th><th>Provenance</th><th>Fingerprint</th><th /></tr></thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.id + r.version}>
            <td className="num text-right text-muted">{r.leafIndex}</td>
            <td className="mono">{r.id}{r.version > 1 ? <span className="text-muted"> v{r.version}</span> : null}</td>
            <td>{typeName(r.type)}</td>
            <td className="text-text-2">{r.ref || "—"}</td>
            <td className="whitespace-nowrap text-text-2">{dateTime(r.occurredAt)}</td>
            <td className="num text-right">{r.massKg ? num(r.massKg, 0) : "—"}</td>
            <td>{r.provenance}</td>
            <td><Hash value={r.contentHash} n={10} /></td>
            <td><Link to={`/verify?hash=${r.contentHash}`} className="text-accent-ink hover:underline">verify</Link></td>
          </tr>
        ))}
      </tbody>
    </table></div>
  );
}

function SealPage({ id }: { id: string }) {
  const q = useQuery({ queryKey: ["x-seal", id], queryFn: () => api.get<ExplorerSeal>(`/public/explorer/seals/${encodeURIComponent(id)}`), retry: false });
  if (q.isError) return <NotPublic what="Seal" />;
  const d = q.data;
  return (
    <Detail title={<>Seal <span className="mono text-[0.7em]">{id}</span></>} sub="One ledger transaction committing to a batch of record fingerprints through its Merkle root.">
      {!d ? <Skel h={300} /> : (
        <>
          <Panel title="Seal">
            <KV items={[
              ["Organisation", <Link key="o" to={`/explorer/org/${encodeURIComponent(d.seal.orgId ?? "")}`} className="font-medium hover:underline">{d.orgName}</Link>],
              ["Role in the chain", KIND[d.orgKind]?.name ?? d.orgKind],
              ["Merkle root", <Hash key="r" value={d.seal.merkleRoot} n={16} />],
              ["On the ledger", d.onChain ? (d.onChain.merkleRoot === d.seal.merkleRoot
                ? <span key="m" className="inline-flex items-center gap-1 text-good"><CircleCheck size={14} />the chaincode holds the same root</span>
                : <span key="m" className="text-critical">root differs from the chaincode's</span>) : d.ledgerError ?? "—"],
              ["Records", num(d.seal.leafCount)],
              ["Provenance", d.seal.provenance],
              ["Transaction", <Hash key="t" value={d.seal.txId} n={16} />],
              ["Block", d.seal.blockNumber ? <Link key="b" to={`/explorer/block/${d.seal.blockNumber}`} className="font-semibold text-accent-ink hover:underline">{num(d.seal.blockNumber)}</Link> : "—"],
              ["Sealed", dateTime(d.seal.sealedAt)],
            ]} />
          </Panel>
          <Panel title="Supply-chain records" sub={d.records.length < d.seal.leafCount ? `The first ${num(d.records.length)} of ${num(d.seal.leafCount)}, in Merkle leaf order.` : "In Merkle leaf order. Every row is a signed record whose fingerprint is a leaf of the tree."} pad={false}>
            <RecordTable rows={d.records} />
          </Panel>
        </>
      )}
    </Detail>
  );
}

function OrgPage({ id }: { id: string }) {
  const q = useQuery({ queryKey: ["x-org", id], queryFn: () => api.get<ExplorerOrgDetail>(`/public/explorer/orgs/${encodeURIComponent(id)}`), retry: false });
  if (q.isError) return <NotPublic what="Organisation" />;
  const d = q.data?.detail;
  const o = d?.org;
  return (
    <Detail title={o?.name ?? "Organisation"} sub={o ? `${(d!.kinds.length ? d!.kinds : [o.kind]).map((k) => KIND[k]?.name ?? k).join(" · ")}${o.country ? " · " + countryName(o.country) : ""}. ${num(o.seals)} seals over ${num(o.records)} record fingerprints.` : undefined}>
      {!d || !o ? <Skel h={300} /> : (
        <>
          <div className="grid gap-5 lg:grid-cols-2">
            <Panel title="Profile">
              <KV items={[
                ["Location", [d.city, countryName(o.country)].filter(Boolean).join(", ") || "—"],
                ["Website", d.website || "—"],
                ["About", d.description || "—"],
                ["Last sealed", o.lastSealedAt ? dateTime(o.lastSealedAt) : "—"],
                ["Blocks", <span key="b" className="flex flex-wrap gap-1">{o.blocks.slice(0, 12).map((n) => <Link key={n} to={`/explorer/block/${n}`} className="chip mono hover:bg-surface-3">{n}</Link>)}</span>],
              ]} />
            </Panel>
            <Panel title="Supply chain" sub="Current records by type; sealed ones are anchored on the ledger.">
              {d.types.length ? (
                <table className="table text-[13px]"><thead><tr><th>Record type</th><th className="text-right">Records</th><th className="text-right">Sealed</th><th className="text-right">Mass (t)</th></tr></thead>
                  <tbody>{d.types.map((t) => <tr key={t.type}><td>{typeName(t.type)}</td><td className="num text-right">{num(t.count)}</td><td className="num text-right">{num(t.sealed)}</td><td className="num text-right">{t.massKg ? num(t.massKg / 1000, 1) : "—"}</td></tr>)}</tbody></table>
              ) : <p className="text-[13.5px] text-text-2">No records yet.</p>}
            </Panel>
          </div>
          <Panel title="Sites" sub="The organisation's own plants and yards." pad={false}>
            {d.sites.length ? (
              <div className="overflow-x-auto"><table className="table text-[13px]"><thead><tr><th>Site</th><th>Type</th><th>Place</th></tr></thead>
                <tbody>{d.sites.map((x) => <tr key={x.name}><td className="font-medium">{x.name}</td><td>{x.kind.replace(/_/g, " ")}</td><td className="text-text-2">{[x.city, x.region, x.country && countryName(x.country)].filter(Boolean).join(", ")}</td></tr>)}</tbody></table></div>
            ) : <p className="px-6 pb-6 pt-3 text-[13.5px] text-text-2">No own sites listed.</p>}
          </Panel>
          {d.roadSections.length > 0 && (
            <Panel title="Road sections" sub="Where the recycled material ended up." pad={false}><RecordTable rows={d.roadSections} /></Panel>
          )}
          <Panel title="Seals" pad={false}>
            <div className="overflow-x-auto"><table className="table">
              <thead><tr><th>Seal</th><th>Merkle root</th><th className="text-right">Records</th><th className="text-right">Block</th><th>Sealed</th><th>Status</th></tr></thead>
              <tbody>
                {(q.data!.seals).map((s) => (
                  <tr key={s.id} className="clickable" onClick={() => location.assign(`/explorer/seal/${encodeURIComponent(s.id)}`)}>
                    <td className="mono text-[12.5px] font-semibold"><Link to={`/explorer/seal/${encodeURIComponent(s.id)}`} className="hover:underline" onClick={(e) => e.stopPropagation()}>{s.id}</Link></td>
                    <td className="mono text-[12px] text-text-2">{shortHash(s.merkleRoot, 8)}</td>
                    <td className="num text-right">{num(s.leafCount)}</td>
                    <td className="num text-right">{s.blockNumber ? <Link to={`/explorer/block/${s.blockNumber}`} className="text-accent-ink hover:underline" onClick={(e) => e.stopPropagation()}>{num(s.blockNumber)}</Link> : "—"}</td>
                    <td className="whitespace-nowrap text-text-2">{s.sealedAt ? ago(s.sealedAt) : "—"}</td>
                    <td>{status(s)}</td>
                  </tr>
                ))}
              </tbody>
            </table></div>
          </Panel>
        </>
      )}
    </Detail>
  );
}

export default function Explorer() {
  const [kind = "", ...rest] = (useParams()["*"] ?? "").split("/");
  const id = decodeURIComponent(rest.join("/"));
  useSeo("Explorer · ROADPASS", "Browse the blocks, transactions and Merkle roots of the ROADPASS blockchain ledger for organisations that publish them.", "/explorer");
  return (
    <MarketingFrame>
      {kind === "block" && id ? <BlockPage n={id} /> : kind === "seal" && id ? <SealPage id={id} /> : kind === "org" && id ? <OrgPage id={id} /> : <Home />}
    </MarketingFrame>
  );
}
