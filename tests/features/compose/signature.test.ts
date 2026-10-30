import { describe, expect, it } from "vitest";
import { replaceSourceSignature, signatureHtml } from "../../../src/features/compose/signature";

describe("automatic signature source blocks", () => {
  it("escapes arbitrary text and normalizes CRLF without creating active HTML", () => {
    const html = signatureHtml('<img src=x onerror="bad()">\r\nsecond & final');
    expect(html).toContain('&lt;img src=x onerror=&quot;bad()&quot;&gt;<br/>second &amp; final');
    expect(html).not.toContain("<img");
    expect(html).not.toContain("\r");
  });

  it("preserves surrounding source byte-for-byte, not matching signature-like body text", () => {
    const prefix = "## Hand-authored heading\n\n  Body says Signature A.\n\n";
    const suffix = "\n\n> Quoted Signature A.\n";
    const source = prefix + signatureHtml("Signature A") + suffix;
    expect(replaceSourceSignature(source, "Signature B")).toBe(prefix + signatureHtml("Signature B") + suffix);
    expect(replaceSourceSignature(source, "")).toBe(prefix + suffix);
  });
});
