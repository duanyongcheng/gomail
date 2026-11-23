import type { Editor, JSONContent } from "@tiptap/core";
import { DOMParser as SchemaParser, DOMSerializer } from "@tiptap/pm/model";
import TurndownService from "turndown";
import type { EditorMode } from "@/hooks/useComposeEditor";
import { renderMarkdownSnapshot } from "./markdown-snapshot";

const turndown = new TurndownService({
  headingStyle: "atx",
  codeBlockStyle: "fenced",
  // Markdown has no empty paragraph syntax. Keep those blocks as HTML instead
  // of deleting intentional spacing (including a fresh editor's first line).
  blankReplacement: (_content, node) => node.nodeType === 1
    ? `\n\n${(node as HTMLElement).outerHTML}\n\n` : "",
});
// A Markdown hard break at the end of a paragraph gets discarded by the
// renderer. Inline HTML preserves both internal and trailing hard breaks.
turndown.addRule("hardBreak", { filter: "br", replacement: () => "<br>" });
turndown.addRule("composeSignature", {
  filter: (node) => node.nodeName === "P" && node.getAttribute("data-gomail-signature") === "auto",
  replacement: (_content, node) => `\n\n${(node as HTMLElement).outerHTML}\n\n`,
});

type HtmlNode = string | { tag: string; attrs: Record<string, string>; children: HtmlNode[] };
const blocks = new Set(["p", "blockquote", "pre", "ul", "ol", "li", "hr", "h1", "h2", "h3", "h4", "h5", "h6"]);
const aliases: Record<string, string> = { b: "strong", i: "em" };
const commutingMarks = new Set(["strong", "em", "s", "u"]);

function normalizeMarkOrder(node: HtmlNode): HtmlNode {
  // ProseMirror serializes combined marks in schema order, not source order.
  // Attribute-free, directly nested formatting marks commute. Do not normalize
  // styled/attributed marks or unknown wrappers: losing those must still fail.
  const tags: string[] = [];
  let leaf = node;
  while (typeof leaf !== "string" && commutingMarks.has(leaf.tag)
    && !Object.keys(leaf.attrs).length && leaf.children.length === 1) {
    tags.push(leaf.tag);
    leaf = leaf.children[0];
  }
  return tags.sort().reverse().reduce<HtmlNode>((child, tag) => ({ tag, attrs: {}, children: [child] }), leaf);
}

function isBlock(node: HtmlNode): boolean {
  return typeof node !== "string" && blocks.has(node.tag);
}

function childrenOf(parent: ParentNode, pre = false, root = false, trimBoundary = true): HtmlNode[] {
  let children: HtmlNode[] = Array.from(parent.childNodes, (node): HtmlNode => {
    if (node.nodeType === 3) return pre ? node.textContent ?? "" : (node.textContent ?? "").replace(/[\t\n\r\f ]+/g, " ");
    if (node.nodeType !== 1) return { tag: "#other", attrs: {}, children: [node.textContent ?? ""] };
    const element = node as HTMLElement;
    const tag = aliases[element.localName] ?? element.localName;
    const attrs = Object.fromEntries(Array.from(element.attributes, ({ name, value }) => [
      name,
      // Only syntax whitespace, not declarations, is normalized. Unknown CSS
      // must not disappear through CSSOM parsing and falsely compare equal.
      name === "style" ? value.trim().replace(/\s*([:;])\s*/g, "$1").replace(/;$/, "") : value,
    ]));
    return { tag, attrs, children: childrenOf(element, pre || tag === "pre", tag === "li" || tag === "blockquote", blocks.has(tag)) };
  }).map(normalizeMarkOrder);
  if (!pre) {
    children = children.map((node, index) => {
      if (typeof node !== "string") return node;
      const trimStart = (index === 0 && trimBoundary) || (index > 0 && isBlock(children[index - 1]));
      const trimEnd = (index === children.length - 1 && trimBoundary) || (index < children.length - 1 && isBlock(children[index + 1]));
      const text = trimStart ? node.replace(/^ /, "") : node;
      return trimEnd ? text.replace(/ $/, "") : text;
    }).filter((node) => node !== "");
  }
  if (root) {
    // ProseMirror wraps top-level inline content in paragraphs. This adds a
    // container, never removes an unrecognized source element or attribute.
    const wrapped: HtmlNode[] = [];
    for (const node of children) {
      if (isBlock(node)) wrapped.push(node);
      else {
        const last = wrapped[wrapped.length - 1];
        if (last && typeof last !== "string" && last.tag === "#inline") last.children.push(node);
        else wrapped.push({ tag: "#inline", attrs: {}, children: [node] });
      }
    }
    children = wrapped.map((node) => typeof node !== "string" && node.tag === "#inline" ? { ...node, tag: "p" } : node);
    // The schema's required empty paragraph is not added message content.
    if (!children.length) children = [{ tag: "p", attrs: {}, children: [] }];
  }
  return children;
}

function preservesNodes(source: HtmlNode[], output: HtmlNode[], root = false): boolean {
  // StarterKit's trailingNode plugin may append a default empty paragraph. It
  // cannot remove any source block, including intentional empty paragraphs.
  const extraParagraphs = root && output.slice(source.length).every((node) =>
    typeof node !== "string" && node.tag === "p" && !Object.keys(node.attrs).length && !node.children.length);
  return (source.length === output.length || (output.length > source.length && extraParagraphs)) && source.every((node, index) => {
    const next = output[index];
    if (typeof node === "string" || typeof next === "string") return node === next;
    return node.tag === next.tag
      && Object.entries(node.attrs).every(([key, value]) => next.attrs[key] === value)
      && preservesNodes(node.children, next.children);
  });
}

function inertHtml(html: string): DocumentFragment | null {
  // Fragment parsing discards document wrappers and their attributes before the
  // schema can inspect them. Refuse them rather than falsely claiming fidelity.
  if (/<\s*(?:\/?(?:html|head|body)\b|!doctype\b)/i.test(html)) return null;
  const template = document.createElement("template");
  template.innerHTML = html;
  return template.content;
}

function htmlSnapshot(editor: Editor, html: string) {
  const source = inertHtml(html);
  if (!source) return null;
  const doc = SchemaParser.fromSchema(editor.schema).parse(source);
  // Both input and serializer output belong to an inert template document. Do
  // not validate via a mounted Editor, DOMParser document, image or iframe.
  const output = DOMSerializer.fromSchema(editor.schema).serializeFragment(doc.content, { document: source.ownerDocument });
  const nodes = childrenOf(output, false, true);
  if (!preservesNodes(childrenOf(source, false, true), nodes)) return null;
  const container = source.ownerDocument.createElement("div");
  container.append(output);
  return { doc, html: container.innerHTML, source, nodes };
}

function markdownSnapshot(editor: Editor, source: string) {
  // tiptap-markdown exposes its renderer in storage (not its public TS
  // declarations). Preflight its string output in an inert template before
  // the save/send renderer can parse or mount any remote-loading markup.
  const parser = (editor.storage as unknown as {
    markdown: { parser: { md: { render: (source: string) => string } } };
  }).markdown.parser;
  if (!htmlSnapshot(editor, parser.md.render(source))) return null;
  // Now that all raw markup is schema-safe, use the existing save/send renderer
  // including Markdown normalization and StarterKit's trailing-node plugin.
  return htmlSnapshot(editor, renderMarkdownSnapshot(source).html);
}

export interface ComposeConversion {
  source: string;
  content?: JSONContent;
}

/** Prepare without a live-editor transaction. null means keep ALL current state. */
export function prepareComposeConversion(editor: Editor, mode: EditorMode, target: EditorMode, source: string): ComposeConversion | null {
  try {
    if (mode === "rich" && target === "html") return { source: editor.getHTML() };
    const snapshot = mode === "markdown"
      ? markdownSnapshot(editor, source)
      : htmlSnapshot(editor, mode === "rich" ? editor.getHTML() : source);
    if (!snapshot) return null;
    if (target === "rich") return { source, content: snapshot.doc.toJSON() };
    if (target === "html") return { source: snapshot.html };

    // HTML -> Markdown -> HTML alone is NOT enough: Markdown's save/send
    // renderer also uses the compose schema. Check that actual schema output.
    const markdown = turndown.turndown(snapshot.source);
    const converted = markdownSnapshot(editor, markdown);
    if (!converted || !preservesNodes(childrenOf(snapshot.source, false, true), converted.nodes, true)) return null;
    return { source: markdown };
  } catch {
    // No source text or parser exception in UI/logs. Uncertain means no change.
    return null;
  }
}
