import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "tiptap-markdown";
import { afterEach, beforeEach, expect, it } from "vitest";
import { ComposeSignature } from "@/features/compose/signature";
import { prepareComposeConversion } from "@/features/compose/mode-conversion";
import { renderMarkdownSnapshot } from "@/features/compose/markdown-snapshot";

let editor: Editor;
beforeEach(() => {
  editor = new Editor({ extensions: [StarterKit, ComposeSignature, Markdown.configure({ html: true, transformPastedText: true })], content: "", editable: false });
});
afterEach(() => editor.destroy());

it.each([
  ["bold", "**Bold**"],
  ["bold italic", "***Bold italic***"],
  ["link", "[Site](https://example.invalid)"],
  ["heading and quote", "# Heading\n\n> Quote"],
  ["bullets", "- One\n- Two"],
  ["numbered list", "1. One\n2. Two"],
  ["code", "```js\nconst a = 1;\n```"],
])("uses the same rendered representation as save/send for supported Markdown %s", (_label, source) => {
  const state = editor.state;
  const conversion = prepareComposeConversion(editor, "markdown", "html", source);
  expect(conversion).not.toBeNull();
  expect(conversion!.source).toBe(renderMarkdownSnapshot(source).html);
  expect(editor.state).toBe(state);
});

it.each([
  '<h2>Heading</h2><blockquote><p>Quote</p></blockquote>',
  '<ul><li><p>One</p></li><li><p>Two</p></li></ul>',
  '<ol><li><p>One</p></li><li><p>Two</p></li></ol>',
  '<pre><code class="language-js">const a = 1;</code></pre>',
  '<p><strong><em>Bold italic</em></strong></p>',
  '<p><em><strong>Bold italic</strong></em></p>',
])("keeps ordinary schema structure when converting HTML to Markdown: %s", (source) => {
  const conversion = prepareComposeConversion(editor, "html", "markdown", source);
  expect(conversion).not.toBeNull();
  expect(prepareComposeConversion(editor, "markdown", "rich", conversion!.source)).not.toBeNull();
});

it.each([
  '<p><u>Underlined</u></p>',
  '<p><a href="https://example.invalid" target="_self">Link</a></p>',
  '<p><a href="https://example.invalid" class="special">Link</a></p>',
])("blocks HTML supported by rich schema but not preserved by Markdown: %s", (source) => {
  expect(prepareComposeConversion(editor, "html", "rich", source)).not.toBeNull();
  expect(prepareComposeConversion(editor, "html", "markdown", source)).toBeNull();
});

it.each([
  '<p>Text <span>unrecognized container</span></p>',
  '<p style="font-size: 22px">Large</p>',
  '<p><em style="color: red"><strong>Styled combined marks</strong></em></p>',
  '<p><small>Small</small><sup>sup</sup><sub>sub</sub></p>',
  '<p data-pebble-signature="auto" style="color: red">Styled signature</p>',
  '<p><a name="anchor">Anchor</a></p>',
  '<picture><source srcset="https://example.invalid/photo 2x"><img src="cid:photo"></picture>',
  '<svg><text>Vector content</text></svg>',
])("compares arbitrary source nodes and attributes against schema output: %s", (source) => {
  expect(prepareComposeConversion(editor, "html", "rich", source)).toBeNull();
  expect(prepareComposeConversion(editor, "html", "markdown", source)).toBeNull();
});
