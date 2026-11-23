import "@testing-library/jest-dom/vitest";
import { useRef, useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import ComposeAiAssistant from "@/features/ai/ComposeAiAssistant";
import { aiHelpWrite, aiPolish, aiProofread, aiTranslate } from "@/lib/api";
import { profileLocalStorage } from "@/lib/profileStorage";
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));
vi.mock("@/lib/api", () => ({ aiPolish: vi.fn(), aiProofread: vi.fn(), aiTranslate: vi.fn(), aiHelpWrite: vi.fn() }));
function Harness() {
  const [rawSource, setRawSource] = useState("Keep my draft");
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  return <><textarea aria-label="body" ref={textareaRef} value={rawSource} onChange={(e) => setRawSource(e.target.value)} /><ComposeAiAssistant editor={null} editorMode="markdown" rawSource={rawSource} setRawSource={setRawSource} textareaRef={textareaRef} quotedReplyHtml="" /></>;
}
beforeEach(() => { vi.clearAllMocks(); localStorage.clear(); });
it.each([false, true])("never opens AI forms or sends draft text, even with old privacy consent (%s)", (acknowledged) => {
  if (acknowledged) profileLocalStorage.setItem("gomail-ai-privacy-ack", "1");
  render(<Harness />);
  const button = screen.getByRole("button", { name: "ai.menuTooltip" });
  expect(button).toBeDisabled();
  expect(button.getAttribute("title")).toMatch(/not available/);
  fireEvent.click(button);
  expect(screen.queryByRole("menu")).toBeNull();
  expect(screen.queryByRole("dialog")).toBeNull();
  for (const request of [aiHelpWrite, aiPolish, aiProofread, aiTranslate]) expect(request).not.toHaveBeenCalled();
  expect(screen.getByLabelText("body")).toHaveValue("Keep my draft");
  fireEvent.change(screen.getByLabelText("body"), { target: { value: "Still editable" } });
  expect(screen.getByLabelText("body")).toHaveValue("Still editable");
});
