// Independent verification in the browser: the page recomputes the fingerprint,
// checks the signature and the Merkle path itself, with WebCrypto, so the answer
// does not depend on the server telling the truth about itself.
//
// Mirrors internal/proof (RFC 8785 JCS + SHA-256, RFC 6962 Merkle with an odd
// node promoted) and internal/signing (Ed25519 over "roadpass/v1/record\n"+hash).

import type { Step } from "./types";

/** RFC 8785 JSON Canonicalization Scheme for JSON values. */
export function canonicalize(v: unknown): string {
  if (v === null || typeof v !== "object") {
    if (typeof v === "number") {
      if (!Number.isFinite(v)) throw new Error("non-finite number");
      return JSON.stringify(v); // ES number serialisation is what RFC 8785 specifies
    }
    return JSON.stringify(v);
  }
  if (Array.isArray(v)) return `[${v.map(canonicalize).join(",")}]`;
  const o = v as Record<string, unknown>;
  // Keys sorted by UTF-16 code units, as JS sorts by default.
  const keys = Object.keys(o).filter((k) => o[k] !== undefined).sort();
  return `{${keys.map((k) => `${JSON.stringify(k)}:${canonicalize(o[k])}`).join(",")}}`;
}

const enc = new TextEncoder();

export function hex(b: ArrayBuffer | Uint8Array): string {
  const a = b instanceof Uint8Array ? b : new Uint8Array(b);
  return Array.from(a, (x) => x.toString(16).padStart(2, "0")).join("");
}

export function unhex(s: string): Uint8Array {
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
}

function b64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

async function sha256(data: Uint8Array): Promise<Uint8Array> {
  // WebCrypto exists only in secure contexts (HTTPS, localhost); elsewhere fall back
  // to a plain implementation so fingerprints and Merkle paths still verify.
  if (globalThis.crypto?.subtle) return new Uint8Array(await crypto.subtle.digest("SHA-256", data as BufferSource));
  return sha256js(data);
}

const K256 = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

/** FIPS 180-4 SHA-256, for insecure contexts without WebCrypto. */
export function sha256js(msg: Uint8Array): Uint8Array {
  const len = msg.length, blocks = Math.ceil((len + 9) / 64);
  const buf = new Uint8Array(blocks * 64);
  buf.set(msg);
  buf[len] = 0x80;
  const dv = new DataView(buf.buffer);
  dv.setUint32(buf.length - 8, Math.floor(len / 0x20000000));
  dv.setUint32(buf.length - 4, (len * 8) >>> 0);
  const h = new Uint32Array([0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19]);
  const w = new Uint32Array(64);
  const rotr = (x: number, n: number) => (x >>> n) | (x << (32 - n));
  for (let b = 0; b < blocks; b++) {
    for (let i = 0; i < 16; i++) w[i] = dv.getUint32(b * 64 + i * 4);
    for (let i = 16; i < 64; i++) {
      const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
    }
    let [a, bb, c, d, e, f, g, hh] = h;
    for (let i = 0; i < 64; i++) {
      const t1 = (hh + (rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)) + ((e & f) ^ (~e & g)) + K256[i] + w[i]) >>> 0;
      const t2 = ((rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)) + ((a & bb) ^ (a & c) ^ (bb & c))) >>> 0;
      hh = g; g = f; f = e; e = (d + t1) >>> 0; d = c; c = bb; bb = a; a = (t1 + t2) >>> 0;
    }
    h[0] += a; h[1] += bb; h[2] += c; h[3] += d; h[4] += e; h[5] += f; h[6] += g; h[7] += hh;
  }
  const out = new Uint8Array(32);
  const ov = new DataView(out.buffer);
  h.forEach((v, i) => ov.setUint32(i * 4, v));
  return out;
}

/** The content hash of a hash document. */
export async function fingerprint(doc: unknown): Promise<string> {
  return hex(await sha256(enc.encode(canonicalize(doc))));
}

function concat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
  }
  return out;
}

/** Recomputes the Merkle root from a leaf and its audit path. */
export async function merkleRoot(leafHex: string, path: Step[]): Promise<string> {
  let cur = await sha256(concat(new Uint8Array([0]), unhex(leafHex)));
  for (const s of path) {
    const sib = unhex(s.hash);
    cur = await sha256(s.left ? concat(new Uint8Array([1]), sib, cur) : concat(new Uint8Array([1]), cur, sib));
  }
  return hex(cur);
}

/** Ed25519 verification via WebCrypto; null when the browser lacks Ed25519. */
export async function verifyEd25519(pubB64: string, sigB64: string, message: string): Promise<boolean | null> {
  try {
    const key = await crypto.subtle.importKey("raw", b64(pubB64) as BufferSource, { name: "Ed25519" }, false, ["verify"]);
    return await crypto.subtle.verify({ name: "Ed25519" }, key, b64(sigB64) as BufferSource, enc.encode(message) as BufferSource);
  } catch {
    return null;
  }
}

export const recordMessage = (hash: string) => `roadpass/v1/record\n${hash}`;

/**
 * The bytes a receipt signature covers. v1 receipts confirm time and mass; v2
 * adds the intake decision and the SHA-256 of the receiver's note.
 */
export async function receiptMessage(hash: string, receivedAt: string, massKg: number, decision?: string, note = ""): Promise<string> {
  const t = new Date(receivedAt).toISOString().replace(/\.\d{3}Z$/, "Z");
  const grams = Math.round(massKg * 1000);
  if (!decision) return `roadpass/v1/receipt\n${hash}\n${t}\n${grams}`;
  const digest = hex(await sha256(enc.encode(note)));
  return `roadpass/v2/receipt\n${hash}\n${t}\n${grams}\n${decision}\n${digest}`;
}
