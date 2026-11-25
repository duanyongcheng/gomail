/**
 * The muted "original" line printed under a translated paragraph.
 *
 * The echo used to carry its look as an inline `style` attribute on a `<span>`.
 * That made it depend on seven declarations surviving two trips through the
 * sanitizer and then outranking whatever the message says about its own text —
 * and it lost that fight in the app: the original came out as plain body text
 * running on from the translation (measured from a reader's screenshot: the
 * echoed English is painted in the surrounding 16px body colour rather than
 * 12px `#8a8a8a`, and it does not start a line of its own).
 *
 * The rule now lives in the shadow root next to the message, where the
 * sanitizer never sees it and a message cannot address it, and the translation
 * only emits the class. Nothing about the look has to survive anything.
 */
export const SOURCE_ECHO_CLASS = "gomail-source-echo";
export const BILINGUAL_TRANSLATION_CLASS = "gomail-bilingual-translation";
export const TRANSLATED_BLOCK_CLASS = "gomail-translated-block";
export const TRANSLATED_ONLY_CLASS = "gomail-translated-only";

/**
 * Injected by `ShadowDomEmail` alongside the message.
 *
 * No `!important` anywhere: the echo/translation is a direct child of the block,
 * so these declarations already beat the sender's inherited typography.
 */
export const SOURCE_ECHO_CSS = `.${SOURCE_ECHO_CLASS} {
          display: block;
          margin-top: 2px;
          padding-left: 8px;
          border-left: 2px solid rgba(128, 128, 128, 0.45);
          font-size: 12px;
          font-weight: normal;
          line-height: 1.6;
          color: #8a8a8a;
        }
        .${BILINGUAL_TRANSLATION_CLASS} {
          display: block;
          margin-top: 6px;
          margin-bottom: 4px;
          padding: 5px 10px;
          border-left: 2.5px solid var(--color-accent, #0064e0);
          border-radius: 0 5px 5px 0;
          background: rgba(0, 100, 224, 0.055);
          color: #1d1d1f;
          font-family: -apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", "Hiragino Sans GB", "Segoe UI", "Microsoft YaHei UI", system-ui, sans-serif;
          font-size: 14px;
          font-weight: 400;
          line-height: 1.65;
          text-align: start;
          white-space: pre-wrap;
          word-break: break-word;
        }
        :host-context([data-theme="dark"]) .${BILINGUAL_TRANSLATION_CLASS} {
          background: rgba(74, 158, 255, 0.11);
          color: #1d1d1f;
          border-left-color: #0064e0;
        }
        .gomail-email-content.${TRANSLATED_ONLY_CLASS} .${TRANSLATED_BLOCK_CLASS} {
          font-size: 0;
          line-height: 0;
        }
        .gomail-email-content.${TRANSLATED_ONLY_CLASS} .${TRANSLATED_BLOCK_CLASS} > :not(.${BILINGUAL_TRANSLATION_CLASS}) {
          display: none;
        }
        .gomail-email-content.${TRANSLATED_ONLY_CLASS} .${TRANSLATED_BLOCK_CLASS} > .${BILINGUAL_TRANSLATION_CLASS} {
          margin-top: 0;
          margin-bottom: 0;
          padding: 0;
          border-left: none;
          background: transparent;
          font-size: 14px;
          line-height: 1.7;
        }`;
