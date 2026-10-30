import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "tiptap-markdown";
import { ComposeSignature } from "./signature";

export function renderMarkdownSnapshot(source: string): { html: string; text: string } {
  // Use the compose schema without changing the live editor, selection or undo
  // history. Saving source mode must not replace content behind the textarea.
  const editor = new Editor({
    extensions: [StarterKit, ComposeSignature, Markdown.configure({ html: true, transformPastedText: true })],
    content: "",
    editable: false,
  });
  try {
    editor.commands.setContent(source, { emitUpdate: false });
    return { html: editor.getHTML(), text: editor.getText() };
  } finally {
    editor.destroy();
  }
}
