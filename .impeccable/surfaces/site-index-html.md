---
version: 1
slug: "site-index-html"
primary_target: "site/index.html"
related_targets: ["site/styles.css","site/app.js"]
---

# Surface Brief — GoMail Official Homepage (GitHub Pages)

- **Scope:** Standalone static software homepage in `site/` (`site/index.html`, `site/styles.css`, `site/app.js`, `site/assets/`) deployed via GitHub Actions to `https://duanyongcheng.github.io/gomail/`.
- **Visitor mode:** Persuade (end-user desktop download conversion paired with interactive live product proof and open-source architectural transparency).
- **Audience & job:** Everyday desktop email users and privacy-conscious developers deciding within seconds what GoMail is, why local-first Go + SQLite + OS keyring email beats cloud-relayed webmail, and downloading the right signed installer for macOS, Windows, or Linux (`v0.2.2`).
- **Constraints:** Bilingual runtime toggle (`zh` / `en`, Chinese-first / browser-detected), light and dark mode toggle (`light` / `dark` Harbor & Deep-Harbor palettes), zero external network CDNs for fonts or icons, pure HTML/CSS/SVG interactive client previews inspired by `damail.app`'s airy layout cadence while preserving GoMail's Harbor Chartroom tokens, Marine Cerulean focus, and Terracotta privacy/offline signal doctrine, and strictly truthful feature claims.
- **Confirmed code-led build path (verbatim user answers):** `A1: "刚才首张图还在加载，其实认可「悬浮圆角顶栏 + 居中双行大标题 + 多平台下载 + 圆角客户端画框 + Bento 矩阵」这套结构，直接开写即可"` and `A2: "(Recommended) 吸收 damail.app 的通透现代感：采用细腻背景双色微光 + 18px 悬浮毛玻璃导航 + 26px 套嵌纯代码交互界面（同时融合 GoMail 海蓝主色与赤陶隐私信号，支持浅/深色切换）"`.

## Direction contract

THESIS: GoMail's homepage marries `damail.app`'s airy, modern open-source product cadence with GoMail's Harbor Chartroom coastal precision—proving local-first desktop mail through razor-sharp, interactive HTML/CSS workspace views rather than blurry screenshots or cloud-SaaS hype.

OWN-WORLD: Luminous Chalk Sea and Harbor Mist canvas (`#f7fafb` / `#ebf1f4`, Deep Harbor `#0b131c` / `#0e1721` in dark mode) kissed by a subtle dual coastal radial atmosphere (cerulean `#155e8a` and soft sea-slate), topped by a sticky `18px`-radius frosted Harbor bar (`1120px` max-width, `1px` inset hairline), concentric `26px` -> `20px` -> `14px` -> `8px` nested surfaces, Marine Cerulean (`#155e8a` / dark `#62b3e3`) primary actions, and warm Terracotta (`#b84a24` / dark `#ee936e` on `#fdf4ee` / `#2a1d19`) reserved exclusively for privacy tracker blocking and offline/outbox operational beacons. Local humanist sans-serif + tabular monospace stack, crisp `1.75px` inline SVG iconography, zero eyebrow kickers above headings, zero gradient text, and themed selection, caret, focus rings, and scrollbars.

STORY: Visitors immediately grasp that every mailbox (Gmail OAuth PKCE, IMAP/SMTP, POP3, Outlook) lives on their own computer with zero cloud servers, click the auto-detected native installer for their OS, test the live interactive Harbor Chartroom workspace (switching views, reading templates, privacy shield, language, and day/night theme), inspect the asymmetric Bento proofs and 3-step local encryption flow, and grab platform binaries or build from source in seconds.

FIRST VIEWPORT: Centered `1120px` sticky `56px` floating `18px`-radius translucent navigation bar (GoMail pebble mark, 4 section anchors, `中 / EN` switch, Sun/Moon theme toggle, and GitHub `v0.2.2` button); spacious centered two-line display `<h1>` ("所有邮箱归于一处，/ 只停泊在你的电脑里" with solid Marine Cerulean second line, no eyebrow kicker above it), concise bilingual subtitle, primary OS-detected download CTA (`下载 macOS 版 v0.2.2 (.dmg)`) beside `GitHub 源码与校验`, and an interactive platform selector pill strip (`macOS Apple Silicon · macOS Intel · Windows Setup.exe · Linux .deb · install.sh · SHA-256`); directly anchored below is a `1120px`-wide `26px`-radius cushioned live Harbor Chartroom desktop workspace (`60px`/`210px` mailbox rail, `300px` focus-isolated message list, pure-white/deep-harbor reading sheet with active Terracotta Privacy Signal Strip, interactive message/template switching, and floating bottom-right AES-256-GCM keyring vault status card). Signature interaction and motion grammar: crisp `160ms–200ms` exponential ease-out (`cubic-bezier(0.22, 1, 0.36, 1)`) interactive message selection, live privacy-mode & reading-template switching inside the hero workspace, instant zero-smear light/dark & `zh`/`en` switching, and copy-command confirmation feedback—all respecting `prefers-reduced-motion: reduce`.

FORM: Harbor Floating Island & Full-Frame Interactive Stage (`damail.app`-referenced concentric showcase + Asymmetric Bento + 3-Step Local Custody Flow), candidate 5 of 7 (re-roll 1 lead), `seed:547054c09c08cf37`.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
