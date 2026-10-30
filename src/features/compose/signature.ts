import { Node, type Editor, type JSONContent } from "@tiptap/react";

// A distinct paragraph survives rich/source mode changes without identifying a
// signature by its text (the body may contain the same words or quoted mail).
export const ComposeSignature = Node.create({
  name: "composeSignature",
  group: "block",
  content: "inline*",
  defining: true,
  isolating: true,
  // Raise the parse rule priority, not the extension priority: Paragraph must
  // remain the schema's default block for empty documents and ordinary Enter.
  parseHTML: () => [{ tag: 'p[data-pebble-signature="auto"]', priority: 60 }],
  renderHTML: () => ["p", {
    "data-pebble-signature": "auto",
    style: "color:var(--color-text-secondary);font-size:13px",
  }, 0],
});

export function signatureHtml(signature: string): string {
  if (!signature) return "";
  const escaped = signature.replace(/&/g, "&amp;").replace(/</g, "&lt;")
    .replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/\r?\n/g, "<br/>");
  return `<p data-pebble-signature="auto" style="color:var(--color-text-secondary);font-size:13px">--<br/>${escaped}</p>`;
}

export function replaceEditorSignature(editor: Editor, signature: string) {
  const ranges: { from: number; to: number }[] = [];
  editor.state.doc.forEach((node, pos) => {
    if (node.type.name === ComposeSignature.name) ranges.push({ from: pos, to: pos + node.nodeSize });
  });
  const content: JSONContent[] = [{ type: "text", text: "--" }];
  for (const line of signature.split(/\r?\n/)) {
    content.push({ type: "hardBreak" });
    if (line) content.push({ type: "text", text: line });
  }
  const chain = editor.chain();
  // Delete backwards so each range still refers to the original document.
  for (const range of ranges.slice().reverse()) chain.deleteRange(range);
  // Insert a typed block, not an HTML string: tiptap-markdown's inline parser
  // unwraps a lone <p>, which would silently discard the signature marker.
  if (signature) chain.insertContentAt(ranges[0]?.from ?? editor.state.doc.content.size, { type: ComposeSignature.name, content });
  chain.run();
}

export function replaceSourceSignature(source: string, signature: string): string {
  const html = signatureHtml(signature);
  let replaced = false;
  // Signature paragraphs contain inline content only. Keep all bytes outside
  // their explicit marker intact, including hand-authored Markdown/HTML.
  const next = source.replace(/<p\b(?=[^>]*\bdata-pebble-signature\s*=\s*(?:"auto"|'auto'))[^>]*>[\s\S]*?<\/p\s*>/gi, () => {
    const replacement = replaced ? "" : html;
    replaced = true;
    return replacement;
  });
  return replaced || !html ? next : `${next}\n\n${html}`;
}
