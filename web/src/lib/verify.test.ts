import { describe, expect, it } from "vitest";
import { webcrypto } from "node:crypto";
import { sha256js } from "./verify";

const hex = (b: Uint8Array) => [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
const enc = new TextEncoder();

describe("sha256js", () => {
  it("matches the FIPS 180-4 vectors", () => {
    expect(hex(sha256js(enc.encode("")))).toBe("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
    expect(hex(sha256js(enc.encode("abc")))).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    expect(hex(sha256js(enc.encode("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"))))
      .toBe("248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1");
  });
  it("matches WebCrypto at every padding boundary", async () => {
    for (let n = 0; n < 200; n++) {
      const m = new Uint8Array(n).map((_, i) => (i * 31 + n) & 0xff);
      const want = new Uint8Array(await webcrypto.subtle.digest("SHA-256", m));
      expect(hex(sha256js(m))).toBe(hex(want));
    }
  });
});

import { receiptMessage } from "./verify";

describe("receiptMessage", () => {
  // Same vector as internal/signing TestReceiptMessageV2Vector.
  it("matches the Go signer byte for byte", async () => {
    expect(await receiptMessage("ab12", "2026-10-15T10:00:00Z", 18250.5, "accepted_with_note", "Wet bales")).toBe(
      "roadpass/v2/receipt\nab12\n2026-10-15T10:00:00Z\n18250500\naccepted_with_note\n309dafaa7affcf9af73d9f7b67e5b76a8a34d073e43c122bec1a4afb8d974ded");
    expect(await receiptMessage("ab12", "2026-10-15T10:00:00.000Z", 18250.5)).toBe("roadpass/v1/receipt\nab12\n2026-10-15T10:00:00Z\n18250500");
  });
});
