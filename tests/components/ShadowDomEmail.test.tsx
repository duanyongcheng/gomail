import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ShadowDomEmail, shadowEmailCss } from "@/components/ShadowDomEmail";
import { getMessageTheme } from "@/lib/messageThemes";
import ToastContainer from "@/components/ToastContainer";
import { useToastStore } from "@/stores/toast.store";

const mocks = vi.hoisted(() => ({
  call: vi.fn(),
  openMailtoUrl: vi.fn(),
  reapplyInlineStyles: vi.fn(),
}));

// Keep the real command table, adapter and generated binding. Only the native
// transport is substituted; a pending command cannot falsely pass this test.
vi.mock("mygo-runtime", () => ({ call: mocks.call, event: vi.fn() }));

vi.mock("@/app/useMailtoOpen", () => ({
  openMailtoUrl: mocks.openMailtoUrl,
}));

// Only the call site is swapped; the sanitizer itself stays real.
vi.mock("@/lib/sanitizeHtml", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/sanitizeHtml")>();
  return { ...actual, reapplyInlineStyles: mocks.reapplyInlineStyles };
});

describe("ShadowDomEmail", () => {
  beforeEach(() => {
    mocks.call.mockReset();
    useToastStore.setState({ toasts: [] });
    mocks.openMailtoUrl.mockReset();
    mocks.reapplyInlineStyles.mockReset();
    mocks.call.mockResolvedValue({});
    mocks.openMailtoUrl.mockResolvedValue(true);
  });

  it("uses app theme variables instead of hardcoded light text styles", async () => {
    document.documentElement.setAttribute("data-theme", "dark");

    const { container } = render(<ShadowDomEmail html="<p>Hello</p>" />);
    const host = container.firstChild as HTMLDivElement | null;

    await waitFor(() => {
      expect(host?.shadowRoot).not.toBeNull();
    });

    const shadowMarkup = host!.shadowRoot!.innerHTML;
    expect(shadowMarkup).toContain("var(--color-text-primary)");
    expect(shadowMarkup).toContain("var(--color-accent)");
    expect(shadowMarkup).not.toContain("color: #1a1a1a");
  });

  it("themes horizontal overflow inside email content", async () => {
    const { container } = render(<ShadowDomEmail html="<pre>long code line</pre>" />);
    const host = container.firstChild as HTMLDivElement | null;

    await waitFor(() => {
      expect(host?.shadowRoot).not.toBeNull();
    });

    const shadowMarkup = host!.shadowRoot!.innerHTML;
    expect(shadowMarkup).toContain("scrollbar-width: thin");
    expect(shadowMarkup).toContain("::-webkit-scrollbar-thumb");
  });

  it("keeps light-authored email html readable in dark theme", async () => {
    document.documentElement.setAttribute("data-theme", "dark");

    const { container } = render(
      <ShadowDomEmail html={'<div style="color: #000000">Dark inline text</div>'} />,
    );
    const host = container.firstChild as HTMLDivElement | null;

    await waitFor(() => {
      expect(host?.shadowRoot).not.toBeNull();
    });

    const shadowMarkup = host!.shadowRoot!.innerHTML;
    expect(shadowMarkup).toContain('class="gomail-email-content"');
    expect(shadowMarkup).toContain(':host-context([data-theme="dark"]) .gomail-email-content');
    expect(shadowMarkup).toContain("color-scheme: light");
    expect(shadowMarkup).toContain("background: #fff");
    expect(shadowMarkup).toContain("color: #202124");
  });

  /// The echoed original's look has to come from here, not from the echo itself.
  /// Carried as an inline `style` it never reached the paint in the app, so the
  /// reader saw the original running on from the translation instead of a muted
  /// line of its own. This rule is emitted independently of any theme.
  it("styles the echoed original from the shadow root", async () => {
    const html = '<p>Body<span class="gomail-source-echo">Original</span></p>';
    const { container } = render(<ShadowDomEmail html={html} />);
    const host = container.firstChild as HTMLDivElement | null;

    await waitFor(() => {
      expect(host?.shadowRoot?.querySelector(".gomail-email-content")).not.toBeNull();
    });

    const shadowMarkup = host!.shadowRoot!.innerHTML;
    expect(shadowMarkup).toContain(".gomail-source-echo {");
    expect(shadowMarkup).toContain("color: #8a8a8a");

    // The sanitizer has to keep the class the translation puts on the echo —
    // the class is now the only thing the look depends on.
    const echo = host!.shadowRoot!.querySelector(".gomail-source-echo")!;
    expect(echo.textContent).toBe("Original");
    expect(echo.getAttribute("class")).toBe("gomail-source-echo");
  });

  it("prevents full-height email wrappers from painting a gray viewport canvas", async () => {
    const html = `
      <table height="100%" style="height: 100%; background: #f1f1f1">
        <tbody><tr><td>Cloudflare content</td></tr></tbody>
      </table>
    `;

    const { container } = render(<ShadowDomEmail html={html} />);
    const host = container.firstChild as HTMLDivElement | null;

    await waitFor(() => {
      expect(host?.shadowRoot?.querySelector(".gomail-email-content")).not.toBeNull();
    });

    const shadowMarkup = host!.shadowRoot!.innerHTML;
    expect(shadowMarkup).toContain('.gomail-email-content > table[height="100%"]');
    // The attribute stays: the stylesheet matches on it. What makes the clamp
    // actually reach the paint is the CSSOM copy check below.
    expect(shadowMarkup).toContain('style="height: 100%; background: #f1f1f1"');
    expect(shadowMarkup).toContain("height: auto !important");
    expect(shadowMarkup).toContain("min-height: 0 !important");
  });

  /// The mount is the only place that knows the message is about to be read
  /// under the app's CSP, where a `style` attribute is refused. Declarations
  /// that stay in the attribute alone therefore paint nothing at all — which is
  /// how a marketing email lost its `display:none` overlay and its tile sizing.
  it("re-applies the message's inline styles once it is in the shadow root", async () => {
    const { container } = render(<ShadowDomEmail html={'<p style="color: red">Hello</p>'} />);
    const host = container.firstChild as HTMLDivElement | null;

    await waitFor(() => {
      expect(host?.shadowRoot?.querySelector(".gomail-email-content")).not.toBeNull();
    });

    expect(mocks.reapplyInlineStyles).toHaveBeenCalled();
    expect(mocks.reapplyInlineStyles.mock.calls[0][0]).toBe(host!.shadowRoot);
  });

  it("renders approved email CSS inside the shadow content", async () => {
    const html = `
      <style>.hero { color: red; }</style>
      <link rel="stylesheet" href="https://cdn.example.com/mail.css">
      <p class="hero">Styled body</p>
    `;

    const { container } = render(<ShadowDomEmail html={html} />);
    const host = container.firstChild as HTMLDivElement | null;

    await waitFor(() => {
      expect(host?.shadowRoot?.querySelector(".gomail-email-content")).not.toBeNull();
    });

    const content = host!.shadowRoot!.querySelector(".gomail-email-content")!;
    expect(content.querySelector("style")?.textContent).toContain(".hero");
    expect(content.querySelector("link")?.getAttribute("href")).toBe("https://cdn.example.com/mail.css");
    expect(content.querySelector(".hero")?.textContent).toBe("Styled body");
  });

  /// The `<style>` element is inline content, and this app's CSP carries a
  /// nonce in `style-src`, which makes `'unsafe-inline'` inert — the webview
  /// refused it, and the echo came out as plain running-on body text. An
  /// adopted sheet is not inline content and no directive covers it.
  it("hands the stylesheet to the shadow root as an adopted sheet", async () => {
    const originalSheetCtor = globalThis.CSSStyleSheet;
    const hadPrototypeProp = "adoptedStyleSheets" in ShadowRoot.prototype;
    class FakeSheet {
      cssText = "";
      replaceSync(text: string) {
        this.cssText = text;
      }
    }
    Object.defineProperty(globalThis, "CSSStyleSheet", {
      value: FakeSheet,
      writable: true,
      configurable: true,
    });
    if (!hadPrototypeProp) {
      Object.defineProperty(ShadowRoot.prototype, "adoptedStyleSheets", {
        value: [],
        writable: true,
        configurable: true,
      });
    }

    try {
      const { container } = render(<ShadowDomEmail html="<p>Hello</p>" />);
      const host = container.firstChild as HTMLDivElement | null;

      await waitFor(() => {
        expect(host?.shadowRoot?.querySelector(".gomail-email-content")).not.toBeNull();
      });

      const shadow = host!.shadowRoot!;
      expect(shadow.adoptedStyleSheets).toHaveLength(1);
      expect(
        (shadow.adoptedStyleSheets[0] as unknown as { cssText: string }).cssText,
      ).toContain(".gomail-source-echo {");
      // Nothing inline is left to be refused.
      expect(shadow.querySelector("style")).toBeNull();
    } finally {
      Object.defineProperty(globalThis, "CSSStyleSheet", {
        value: originalSheetCtor,
        writable: true,
        configurable: true,
      });
      if (!hadPrototypeProp) {
        delete (ShadowRoot.prototype as unknown as Record<string, unknown>).adoptedStyleSheets;
      }
    }
  });

  it("keeps the echo rule in the stylesheet itself, independent of the theme", () => {
    const css = shadowEmailCss(null);
    expect(css).toContain(".gomail-source-echo {");
    expect(css).toContain("color: #8a8a8a");
    expect(css).toContain("display: block");
    // A theme only ever adds to it; the echo rule cannot depend on one.
    expect(shadowEmailCss(getMessageTheme("card"))).toContain(".gomail-source-echo {");
  });

  it("opens http and https links through the external URL command", async () => {
    const { container } = render(
      <ShadowDomEmail html={'<a href="http://example.com/">GoMail</a>'} />,
    );
    const host = container.firstChild as HTMLDivElement | null;

    await waitFor(() => {
      expect(host?.shadowRoot?.querySelector("a")).not.toBeNull();
    });

    fireEvent.click(host!.shadowRoot!.querySelector("a")!);

    await waitFor(() => expect(mocks.call).toHaveBeenCalledExactlyOnceWith("AppService.OpenExternal", {
      url: "http://example.com/",
    }));
  });

  it("opens mailto links through the compose mailto handler", async () => {
    const { container } = render(
      <ShadowDomEmail html={'<a href="mailto:qingj1314@163.com">qingj1314@163.com</a>'} />,
    );
    const host = container.firstChild as HTMLDivElement | null;

    await waitFor(() => {
      expect(host?.shadowRoot?.querySelector("a")).not.toBeNull();
    });

    fireEvent.click(host!.shadowRoot!.querySelector("a")!);

    expect(mocks.openMailtoUrl).toHaveBeenCalledWith("mailto:qingj1314@163.com");
    expect(mocks.call).not.toHaveBeenCalled();
  });

  it("routes nested target-blank clicks exactly once and cleans listeners across rerenders", async () => {
    const html = '<a target="_blank" href="https://example.invalid/path?token=private"><span>Read</span></a>';
    const view = render(<ShadowDomEmail html={html} />);
    view.rerender(<ShadowDomEmail html={html + '<p>updated</p>'} />);
    const shadow = (view.container.firstChild as HTMLDivElement).shadowRoot!;
    const click = new MouseEvent("click", { bubbles: true, cancelable: true });
    shadow.querySelector("span")!.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(true);
    await waitFor(() => expect(mocks.call).toHaveBeenCalledExactlyOnceWith("AppService.OpenExternal", {
      url: "https://example.invalid/path?token=private",
    }));
  });

  it("handles middle clicks without a WebView fallback and leaves right-click menus alone", async () => {
    const { container } = render(<ShadowDomEmail html={'<a href="https://example.invalid">Read</a>'} />);
    const anchor = (container.firstChild as HTMLDivElement).shadowRoot!.querySelector("a")!;
    anchor.dispatchEvent(new MouseEvent("auxclick", { bubbles: true, cancelable: true, button: 1 }));
    anchor.dispatchEvent(new MouseEvent("auxclick", { bubbles: true, cancelable: true, button: 2 }));
    await waitFor(() => expect(mocks.call).toHaveBeenCalledTimes(1));
  });

  it("shows a sanitized visible error through the real adapter without logging URL or bridge diagnostics", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      mocks.call.mockRejectedValueOnce(new Error("OS failure: https://example.invalid/?token=private"));
      const { container, findByRole } = render(<><ShadowDomEmail html={'<a href="https://example.invalid/?token=private">Read</a>'} /><ToastContainer /></>);
      const anchor = (container.firstChild as HTMLDivElement).shadowRoot!.querySelector("a")!;
      fireEvent.click(anchor);
      const alert = await findByRole("alert");
      expect(alert.textContent).toContain("Could not open this link");
      expect(alert.textContent).not.toMatch(/private|example.invalid|OS failure/);
      expect(mocks.call).toHaveBeenCalledTimes(1);
      expect(warn).not.toHaveBeenCalled();
      expect(error).not.toHaveBeenCalled();
    } finally { warn.mockRestore(); error.mockRestore(); }
  });

  it("cannot navigate relative or credential-bearing links when validation rejects", async () => {
    mocks.call.mockRejectedValue(new Error("rejected"));
    const { container } = render(<ShadowDomEmail html={'<a href="/relative">Relative</a><a href="https://user:secret@example.invalid">Credentials</a>'} />);
    const links = (container.firstChild as HTMLDivElement).shadowRoot!.querySelectorAll("a");
    for (const link of links) {
      const click = new MouseEvent("click", { bubbles: true, cancelable: true });
      link.dispatchEvent(click);
      expect(click.defaultPrevented).toBe(true);
    }
    await waitFor(() => expect(useToastStore.getState().toasts).toHaveLength(2));
    expect(mocks.call).toHaveBeenCalledTimes(2);
  });
});
