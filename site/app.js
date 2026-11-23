(() => {
  const REPO_URL = "https://github.com/duanyongcheng/gomail";
  const RELEASES_LATEST = `${REPO_URL}/releases/latest`;
  const DEFAULT_VERSION = "v0.2.2";

  const PLATFORMS = {
    "macos-arm64": {
      zh: "下载 macOS 版 (Apple Silicon .dmg)",
      en: "Download for macOS (Apple Silicon .dmg)",
      match: (name) => /darwin[-_]?arm64.*\.dmg$/i.test(name) || (/arm64.*\.dmg$/i.test(name) && !/win|linux/i.test(name)),
      fallback: RELEASES_LATEST,
    },
    "macos-amd64": {
      zh: "下载 macOS 版 (Intel .dmg)",
      en: "Download for macOS (Intel .dmg)",
      match: (name) => /darwin[-_]?(amd64|x64|x86_64).*\.dmg$/i.test(name),
      fallback: RELEASES_LATEST,
    },
    "windows-x64": {
      zh: "下载 Windows 版 (x64 Setup.exe)",
      en: "Download for Windows (x64 Setup.exe)",
      match: (name) => /(win|windows)[-_]?(amd64|x64).*setup.*\.exe$/i.test(name) || (/setup.*\.exe$/i.test(name) && !/arm64/i.test(name)),
      fallback: RELEASES_LATEST,
    },
    "windows-arm64": {
      zh: "下载 Windows 版 (ARM64 Setup.exe)",
      en: "Download for Windows (ARM64 Setup.exe)",
      match: (name) => /arm64.*setup.*\.exe$/i.test(name),
      fallback: RELEASES_LATEST,
    },
    "linux-deb": {
      zh: "下载 Linux 版 (x64 .deb 安装包)",
      en: "Download for Linux (x64 .deb Package)",
      match: (name) => /(amd64|x64|x86_64).*\.deb$/i.test(name) || (/\.deb$/i.test(name) && !/arm64|aarch64/i.test(name)),
      fallback: RELEASES_LATEST,
    },
    "linux-arm64": {
      zh: "下载 Linux 版 (ARM64 .deb 安装包)",
      en: "Download for Linux (ARM64 .deb Package)",
      match: (name) => /(arm64|aarch64).*\.deb$/i.test(name),
      fallback: RELEASES_LATEST,
    },
  };

  const DEMO_MESSAGES = [
    {
      id: "m-release",
      account: "gmail",
      unread: true,
      trackersBlocked: 3,
      time: { zh: "10:42", en: "10:42" },
      avatar: { zh: "发", en: "GM" },
      sender: { zh: "GoMail 发布团队", en: "GoMail Release Team" },
      email: "release@gomail.local",
      recipient: { zh: "收件人：林默 <lin.mo@gmail.com>", en: "To: Lin Mo <lin.mo@gmail.com>" },
      subject: {
        zh: "GoMail v0.2.2：签名增量更新与应用内 OAuth 加密配置",
        en: "GoMail v0.2.2: Signed Delta Updates & In-App Encrypted OAuth",
      },
      snippet: {
        zh: "纯本地 SQLite WAL 存储与 Ed25519 签名 bsdiff 增量补丁现已就绪…",
        en: "Pure-Go SQLite WAL storage and Ed25519-signed bsdiff delta patches are ready…",
      },
      body: {
        zh: [
          "你好，欢迎在本地桌面体验 GoMail（示例演示信件）。所有邮件归档、FTS5 全文检索索引、HTML 隐私清洗以及加密草稿均完整保存在你的电脑中，不经过任何第三方云服务器。",
          "在 v0.2.2 版本中，我们引入了基于 Ed25519 校验的 bsdiff 增量自动更新机制，并支持在「设置 → 账户」中直接加密保存 Google OAuth 客户端凭据（由系统钥匙串 dev.bary.gomail 托管主密钥，修改后免重启立即生效）。"
        ],
        en: [
          "Welcome to GoMail on your desktop (interactive sample message). Your mail archive, FTS5 search index, Go HTML sanitizer, and encrypted drafts live entirely on your machine—never relayed through cloud middleware.",
          "Version 0.2.2 brings Ed25519-verified bsdiff incremental updates and in-app Google OAuth credential storage sealed by your OS keyring (dev.bary.gomail), applying changes immediately without restarting."
        ],
      },
      attachment: { name: "SHA256SUMS.txt", size: "4.2 KB" },
    },
    {
      id: "m-arch",
      account: "imap",
      unread: true,
      trackersBlocked: 0,
      time: { zh: "09:15", en: "09:15" },
      avatar: { zh: "陈", en: "CW" },
      sender: { zh: "陈蔚 · 基础设施组", en: "Chen Wei · Core Infra" },
      email: "chen.wei@harbor-corp.io",
      recipient: { zh: "收件人：架构评审组 <eng@harbor-corp.io>", en: "To: Architecture Review <eng@harbor-corp.io>" },
      subject: {
        zh: "纯 Go SQLite 读写分离池与断网队列核验纪要",
        en: "Pure-Go SQLite Split Pool & Offline Queue Verification Notes",
      },
      snippet: {
        zh: "1 个 WAL/FULL 写连接配合 4 个只读连接，断网期间归档与发信均安全入队…",
        en: "1 WAL/FULL writer paired with 4 read-only connections keeps offline writes durable…",
      },
      body: {
        zh: [
          "我们已经完成 modernc.org/sqlite（CGO_ENABLED=0）在 macOS、Windows 与 Linux 双架构下的持久性核验：写连接强制启用 WAL 与 synchronous=FULL，确保断电或进程异常退出时不丢失任何本地状态。",
          "当网络中断或超时结果未知时，发件箱（Durable Outbox）会将邮件标记为「结果待核验」而非盲目自动重发，彻底杜绝商务邮件重复投递。"
        ],
        en: [
          "We verified modernc.org/sqlite (CGO_ENABLED=0) across macOS, Windows, and Linux: a single WAL + synchronous=FULL writer paired with four read-only connections guarantees zero silent state loss.",
          "Whenever a network drop leaves a send in an unknown state, the Durable Outbox holds it for explicit Check Sent reconciliation instead of blindly resending."
        ],
      },
      attachment: { name: "sqlite-wal-audit-v33.pdf", size: "318 KB" },
    },
    {
      id: "m-billing",
      account: "outlook",
      unread: false,
      trackersBlocked: 2,
      time: { zh: "昨天", en: "Yesterday" },
      avatar: { zh: "N", en: "NS" },
      sender: { zh: "Northstar 账单中心", en: "Northstar Billing" },
      email: "billing@northstar.example",
      recipient: { zh: "收件人：财务组 <ops@outlook.com>", en: "To: Operations <ops@outlook.com>" },
      subject: {
        zh: "11 月企业网络专线账单与分账户 SOCKS5 路由确认",
        en: "November Dedicated Line Statement & Per-Account SOCKS5 Routing",
      },
      snippet: {
        zh: "已拦截邮件内嵌的 2 个营销追踪像素，附件发票已就绪可供本地另存…",
        en: "2 marketing tracking pixels blocked in Go before reaching WebView…",
      },
      body: {
        zh: [
          "本月电子账单已生成。GoMail 在邮件进入 WebView 之前已由 Go 后端剥离了全部外部追踪像素与内联样式外链请求，仅通过受限的 gomail-attachment:// 协议安全加载内嵌图片。",
          "你可以针对单个企业账户独立指定 HTTP CONNECT 或 SOCKS5 代理，同时为其它个人邮箱保持直连。"
        ],
        en: [
          "Your monthly statement is ready. GoMail stripped external tracking pixels and CSS url() leaks in Go before rendering, serving inline CID assets exclusively via gomail-attachment://.",
          "Configure custom HTTP CONNECT or SOCKS5 proxies per account—or enforce a global proxy—while keeping local mail fully browsable offline."
        ],
      },
      attachment: { name: "invoice-2026-11.pdf", size: "184 KB" },
    },
    {
      id: "m-design",
      account: "gmail",
      unread: false,
      trackersBlocked: 1,
      time: { zh: "11月20日", en: "Nov 20" },
      avatar: { zh: "林", en: "LX" },
      sender: { zh: "林汐 · 设计周刊", en: "Lin Xi · Design Digest" },
      email: "digest@chartroom.design",
      recipient: { zh: "收件人：林默 <lin.mo@gmail.com>", en: "To: Lin Mo <lin.mo@gmail.com>" },
      subject: {
        zh: "Harbor Chartroom：为什么我们将阅读模板与应用外壳解耦",
        en: "Harbor Chartroom: Decoupling Reading Templates from the App Shell",
      },
      snippet: {
        zh: "点击右上角阅读模板按钮，体验「跟随界面 / 书信衬线 / 极客终端」实时切换…",
        en: "Try switching between Follow UI, Serif Letter, and Console in the reader toolbar…",
      },
      body: {
        zh: [
          "长信阅读需要充裕的呼吸感。GoMail 将整座桌面工作台统一在清爽克制的 Harbor Chartroom 三阶亮度体系之下，同时保留了 9 套独立的邮件正文阅读模板。",
          "不妨点击上方工具栏右侧的「跟随界面 / 书信 / 终端」按钮：正文区会立刻切换字体与纸面气质，而左侧导航与邮件列表始终保持稳定统一。"
        ],
        en: [
          "Long correspondence deserves generous breathing room. GoMail unifies the entire desktop workspace under Harbor Chartroom's three stepped luminance planes while preserving 9 independent message-reading templates.",
          "Try clicking Follow UI, Letter, or Console in the reader toolbar above—the reading sheet adapts immediately while the surrounding workspace stays calm and consistent."
        ],
      },
      attachment: null,
    },
  ];

  const TERMINAL_SNIPPETS = {
    "build-local": {
      zh: `# 克隆仓库并在本地构建 ad-hoc 签名的 macOS 应用包 (CGO_ENABLED=0)
git clone https://github.com/duanyongcheng/gomail.git
cd gomail
./scripts/build-local.sh

# 启动构建产物（Apple Silicon 示例）
open "build/darwin-arm64/GoMail.app"`,
      en: `# Clone & package an ad-hoc signed macOS app bundle (CGO_ENABLED=0)
git clone https://github.com/duanyongcheng/gomail.git
cd gomail
./scripts/build-local.sh

# Launch the verified app bundle (Apple Silicon example)
open "build/darwin-arm64/GoMail.app"`,
    },
    dev: {
      zh: `# 安装前端依赖、生成 Go <-> TypeScript 绑定并启动开发模式
bun install
bun run generate
bun run dev

# 运行全量质量门禁（Go 单测 + 竞态检测 + 前端测试 + 5 平台交叉编译）
bun run check`,
      en: `# Install dependencies, generate MyGO bindings & start dev mode
bun install
bun run generate
bun run dev

# Run full quality gate (Go tests, race detector, Vitest & 5 cross-builds)
bun run check`,
    },
    linux: {
      zh: `# 下载最新 Linux 安装脚本或直接使用 dpkg 安装 .deb 包
curl -fsSL https://github.com/duanyongcheng/gomail/releases/latest/download/install.sh | sh

# 或校验 SHA-256 后手动安装 Debian / Ubuntu 软件包
sha256sum --check --ignore-missing SHA256SUMS.txt
sudo dpkg -i GoMail_*_amd64.deb`,
      en: `# Run the release install script or install the .deb package directly
curl -fsSL https://github.com/duanyongcheng/gomail/releases/latest/download/install.sh | sh

# Or verify SHA-256 checksums and install on Debian / Ubuntu
sha256sum --check --ignore-missing SHA256SUMS.txt
sudo dpkg -i GoMail_*_amd64.deb`,
    },
  };

  const state = {
    lang: "zh",
    theme: "light",
    selectedOs: "macos-arm64",
    version: DEFAULT_VERSION,
    releaseAssets: {},
    accountFilter: "all",
    searchQuery: "",
    selectedMsgId: "m-release",
    msgTemplate: "app",
    imagesLoadedOnce: false,
    offlineMode: false,
    termTab: "build-local",
  };

  function detectInitialLang() {
    try {
      const saved = localStorage.getItem("gomail-site-lang");
      if (saved === "zh" || saved === "en") return saved;
    } catch (_) {}
    const navLang = (navigator.language || "").toLowerCase();
    return navLang.startsWith("en") ? "en" : "zh";
  }

  function detectInitialTheme() {
    try {
      const saved = localStorage.getItem("gomail-site-theme");
      if (saved === "light" || saved === "dark") return saved;
    } catch (_) {}
    return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches
      ? "dark"
      : "light";
  }

  async function detectInitialOs() {
    const ua = (navigator.userAgent || "").toLowerCase();
    const platform = (navigator.platform || "").toLowerCase();

    if (ua.includes("win") || platform.includes("win")) {
      if (ua.includes("arm64") || ua.includes("aarch64")) return "windows-arm64";
      return "windows-x64";
    }
    if (ua.includes("linux") && !ua.includes("android")) {
      return "linux-deb";
    }
    if (ua.includes("mac") || platform.includes("mac")) {
      if (navigator.userAgentData && typeof navigator.userAgentData.getHighEntropyValues === "function") {
        try {
          const hints = await navigator.userAgentData.getHighEntropyValues(["architecture"]);
          if (hints.architecture === "x86") return "macos-amd64";
        } catch (_) {}
      }
      return "macos-arm64";
    }
    return "macos-arm64";
  }

  function applyTheme(theme, skipSave = false) {
    state.theme = theme;
    const root = document.documentElement;
    root.setAttribute("data-theme-switching", "");
    root.setAttribute("data-theme", theme);
    if (!skipSave) {
      try {
        localStorage.setItem("gomail-site-theme", theme);
      } catch (_) {}
    }
    const sunIcon = document.getElementById("icon-sun");
    const moonIcon = document.getElementById("icon-moon");
    const themeBtn = document.getElementById("theme-toggle");
    if (sunIcon && moonIcon) {
      sunIcon.style.display = theme === "dark" ? "block" : "none";
      moonIcon.style.display = theme === "dark" ? "none" : "block";
    }
    if (themeBtn) {
      themeBtn.setAttribute(
        "aria-label",
        state.lang === "zh"
          ? theme === "dark" ? "切换为浅色模式" : "切换为深色模式"
          : theme === "dark" ? "Switch to light mode" : "Switch to dark mode"
      );
    }
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        root.removeAttribute("data-theme-switching");
      });
    });
  }

  function applyLang(lang, skipSave = false) {
    state.lang = lang;
    document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
    document.title =
      lang === "zh"
        ? "GoMail — 本地优先的跨平台桌面邮箱客户端"
        : "GoMail — Local-First Desktop Email Client";

    if (!skipSave) {
      try {
        localStorage.setItem("gomail-site-lang", lang);
      } catch (_) {}
    }

    const searchInput = document.getElementById("demo-search-input");
    if (searchInput) {
      searchInput.placeholder =
        lang === "zh" ? "本地 FTS5 搜索邮件…" : "Search local FTS5 index…";
      searchInput.setAttribute(
        "aria-label",
        lang === "zh" ? "搜索示例邮件" : "Search sample messages"
      );
    }

    const langBtnLabel = document.getElementById("lang-toggle-label");
    if (langBtnLabel) {
      langBtnLabel.textContent = lang === "zh" ? "EN" : "中文";
    }

    updateHeroCta();
    renderDemoWorkspace();
    renderTerminal();
    applyTheme(state.theme, true);
  }

  function resolvePlatformUrl(osKey) {
    const spec = PLATFORMS[osKey] || PLATFORMS["macos-arm64"];
    return state.releaseAssets[osKey] || spec.fallback;
  }

  function updateHeroCta() {
    const spec = PLATFORMS[state.selectedOs] || PLATFORMS["macos-arm64"];
    const btn = document.getElementById("hero-download-btn");
    const label = document.getElementById("hero-download-label");
    if (btn && label) {
      label.textContent = `${spec[state.lang]} · ${state.version}`;
      btn.href = resolvePlatformUrl(state.selectedOs);
    }

    document.querySelectorAll("[data-os-select]").forEach((pill) => {
      const active = pill.getAttribute("data-os-select") === state.selectedOs;
      pill.setAttribute("aria-pressed", active ? "true" : "false");
    });

    document.querySelectorAll("[data-version-badge]").forEach((el) => {
      el.textContent = state.version;
    });
  }

  function getFilteredMessages() {
    const q = state.searchQuery.trim().toLowerCase();
    return DEMO_MESSAGES.filter((msg) => {
      if (state.accountFilter !== "all" && msg.account !== state.accountFilter) {
        return false;
      }
      if (!q) return true;
      const hay = [
        msg.sender.zh,
        msg.sender.en,
        msg.subject.zh,
        msg.subject.en,
        msg.snippet.zh,
        msg.snippet.en,
        msg.email,
      ]
        .join(" ")
        .toLowerCase();
      return hay.includes(q);
    });
  }

  function renderDemoWorkspace() {
    const listEl = document.getElementById("demo-mail-list");
    const countBadge = document.getElementById("demo-list-count");
    if (!listEl) return;

    const filtered = getFilteredMessages();
    if (countBadge) countBadge.textContent = String(filtered.length);

    if (filtered.length > 0 && !filtered.some((m) => m.id === state.selectedMsgId)) {
      state.selectedMsgId = filtered[0].id;
      state.imagesLoadedOnce = false;
    }

    document.querySelectorAll("[data-acct-filter]").forEach((btn) => {
      const isCurrent = btn.getAttribute("data-acct-filter") === state.accountFilter;
      btn.setAttribute("aria-current", isCurrent ? "true" : "false");
    });

    if (filtered.length === 0) {
      listEl.innerHTML = `<div class="hw-empty-search">${
        state.lang === "zh"
          ? "未找到匹配的本地邮件"
          : "No matching messages in local FTS5 index"
      }</div>`;
      return;
    }

    listEl.innerHTML = filtered
      .map((msg) => {
        const selected = msg.id === state.selectedMsgId;
        return `
        <button type="button" class="hw-mail-row" role="option"
          data-msg-id="${msg.id}"
          data-unread="${msg.unread ? "true" : "false"}"
          tabindex="${selected ? "0" : "-1"}"
          aria-selected="${selected ? "true" : "false"}">
          ${msg.unread ? '<span class="unread-pip" aria-hidden="true"></span>' : ""}
          <div class="hw-row-top">
            <span class="hw-sender">${escapeHtml(msg.sender[state.lang])}</span>
            <span class="hw-time">${escapeHtml(msg.time[state.lang])}</span>
          </div>
          <div class="hw-subject">${escapeHtml(msg.subject[state.lang])}</div>
          <div class="hw-snippet">${escapeHtml(msg.snippet[state.lang])}</div>
        </button>`;
      })
      .join("");

    const rows = Array.from(listEl.querySelectorAll("[data-msg-id]"));
    rows.forEach((row, idx) => {
      row.addEventListener("click", () => {
        const nextId = row.getAttribute("data-msg-id");
        if (nextId && nextId !== state.selectedMsgId) {
          state.selectedMsgId = nextId;
          state.imagesLoadedOnce = false;
          renderDemoWorkspace();
        }
      });

      row.addEventListener("keydown", (e) => {
        let nextIdx = -1;
        if (e.key === "ArrowDown") nextIdx = Math.min(rows.length - 1, idx + 1);
        else if (e.key === "ArrowUp") nextIdx = Math.max(0, idx - 1);
        else if (e.key === "Home") nextIdx = 0;
        else if (e.key === "End") nextIdx = rows.length - 1;
        if (nextIdx >= 0 && nextIdx !== idx) {
          e.preventDefault();
          const targetMsgId = rows[nextIdx].getAttribute("data-msg-id");
          if (targetMsgId) {
            state.selectedMsgId = targetMsgId;
            state.imagesLoadedOnce = false;
            renderDemoWorkspace();
            const refreshed = listEl.querySelector(`[data-msg-id="${targetMsgId}"]`);
            if (refreshed) refreshed.focus();
          }
        }
      });
    });

    const activeMsg =
      DEMO_MESSAGES.find((m) => m.id === state.selectedMsgId) || filtered[0];
    renderActiveMessage(activeMsg);
    renderOfflineState();
  }

  function renderActiveMessage(msg) {
    const readerPane = document.getElementById("demo-reader-pane");
    const privacyStrip = document.getElementById("demo-privacy-strip");
    const privacyText = document.getElementById("demo-privacy-text");
    const privacyBtn = document.getElementById("demo-privacy-action");
    const titleEl = document.getElementById("demo-msg-title");
    const avatarEl = document.getElementById("demo-msg-avatar");
    const senderEl = document.getElementById("demo-msg-sender");
    const recipientEl = document.getElementById("demo-msg-recipient");
    const timeEl = document.getElementById("demo-msg-time");
    const bodyEl = document.getElementById("demo-msg-body");
    const attWrap = document.getElementById("demo-msg-attachment");

    if (readerPane) {
      readerPane.setAttribute("data-msg-tpl", state.msgTemplate);
    }

    document.querySelectorAll("[data-tpl-choice]").forEach((btn) => {
      btn.setAttribute(
        "aria-pressed",
        btn.getAttribute("data-tpl-choice") === state.msgTemplate ? "true" : "false"
      );
    });

    if (privacyStrip && privacyText && privacyBtn) {
      if (msg.trackersBlocked > 0) {
        privacyStrip.hidden = false;
        if (state.imagesLoadedOnce) {
          privacyText.textContent =
            state.lang === "zh"
              ? `已单次加载非追踪图片 · 仍为您拦截 ${msg.trackersBlocked} 个追踪器域名`
              : `Loaded non-tracker images once · ${msg.trackersBlocked} tracker domains still blocked`;
          privacyBtn.textContent =
            state.lang === "zh" ? "恢复严格拦截" : "Restore Strict mode";
        } else {
          privacyText.textContent =
            state.lang === "zh"
              ? `Go 净室隐私盾已拦截 ${msg.trackersBlocked} 个远程追踪像素 · 外部图片默认未加载`
              : `Go sanitizer blocked ${msg.trackersBlocked} tracking pixels · Remote images held`;
          privacyBtn.textContent =
            state.lang === "zh" ? "仅本次加载图片" : "Load images once";
        }
      } else {
        privacyStrip.hidden = true;
      }
    }

    if (titleEl) titleEl.textContent = msg.subject[state.lang];
    if (avatarEl) avatarEl.textContent = msg.avatar[state.lang];
    if (senderEl) senderEl.textContent = `${msg.sender[state.lang]} <${msg.email}>`;
    if (recipientEl) recipientEl.textContent = msg.recipient[state.lang];
    if (timeEl) timeEl.textContent = msg.time[state.lang];

    if (bodyEl) {
      bodyEl.innerHTML = msg.body[state.lang]
        .map((p) => `<p>${escapeHtml(p)}</p>`)
        .join("");
    }

    if (attWrap) {
      if (msg.attachment) {
        attWrap.style.display = "inline-flex";
        const nameNode = attWrap.querySelector(".att-name");
        const sizeNode = attWrap.querySelector(".att-size");
        if (nameNode) nameNode.textContent = msg.attachment.name;
        if (sizeNode) sizeNode.textContent = msg.attachment.size;
      } else {
        attWrap.style.display = "none";
      }
    }
  }

  function renderOfflineState() {
    const offlineBanner = document.getElementById("demo-offline-banner");
    const offlineToggle = document.getElementById("demo-offline-toggle");
    const statusDot = document.getElementById("demo-status-dot");
    const statusText = document.getElementById("demo-status-text");

    if (offlineBanner) offlineBanner.hidden = !state.offlineMode;
    if (offlineToggle) {
      offlineToggle.setAttribute("aria-pressed", state.offlineMode ? "true" : "false");
    }
    if (statusDot) {
      statusDot.setAttribute("data-offline", state.offlineMode ? "true" : "false");
    }
    if (statusText) {
      if (state.offlineMode) {
        statusText.textContent =
          state.lang === "zh"
            ? "离线模式 · 本地缓存、FTS5 搜索与加密草稿照常可用（1 项远端操作已排队）"
            : "Offline · Local SQLite cache, FTS5 search & encrypted drafts active (1 op queued)";
      } else {
        statusText.textContent =
          state.lang === "zh"
            ? "本地就绪 · SQLite WAL 已同步 · 零云端连接（交互演示数据）"
            : "Ready · Local SQLite WAL synced · Zero cloud connections (interactive sample)";
      }
    }
  }

  function renderTerminal() {
    const codeEl = document.getElementById("term-code-content");
    const panelEl = document.getElementById("term-tabpanel");
    if (!codeEl) return;
    const raw = (TERMINAL_SNIPPETS[state.termTab] || TERMINAL_SNIPPETS["build-local"])[
      state.lang
    ];
    codeEl.innerHTML = raw
      .split("\n")
      .map((line) => {
        if (line.startsWith("#")) {
          return `<span class="term-comment">${escapeHtml(line)}</span>`;
        }
        if (!line.trim()) return "";
        return `<span class="term-prompt">$ </span>${escapeHtml(line)}`;
      })
      .join("\n");

    document.querySelectorAll("[data-term-tab]").forEach((tab) => {
      const isSelected = tab.getAttribute("data-term-tab") === state.termTab;
      tab.setAttribute("aria-selected", isSelected ? "true" : "false");
      tab.setAttribute("tabindex", isSelected ? "0" : "-1");
      if (isSelected && panelEl && tab.id) {
        panelEl.setAttribute("aria-labelledby", tab.id);
      }
    });
  }

  function getRawTerminalCommands() {
    const raw = (TERMINAL_SNIPPETS[state.termTab] || TERMINAL_SNIPPETS["build-local"])[
      state.lang
    ];
    return raw
      .split("\n")
      .filter((line) => line.trim() && !line.trim().startsWith("#"))
      .join("\n");
  }

  async function hydrateLatestRelease() {
    try {
      const cached = sessionStorage.getItem("gomail-latest-release");
      let release = cached ? JSON.parse(cached) : null;
      if (!release) {
        const res = await fetch(
          "https://api.github.com/repos/duanyongcheng/gomail/releases/latest",
          { headers: { Accept: "application/vnd.github+json" } }
        );
        if (!res.ok) return;
        release = await res.json();
        sessionStorage.setItem("gomail-latest-release", JSON.stringify(release));
      }
      if (release && typeof release.tag_name === "string") {
        state.version = release.tag_name;
      }
      if (release && Array.isArray(release.assets)) {
        for (const [osKey, spec] of Object.entries(PLATFORMS)) {
          const found = release.assets.find((a) => spec.match(a.name || ""));
          if (found && found.browser_download_url) {
            state.releaseAssets[osKey] = found.browser_download_url;
          }
        }
        const shaAsset = release.assets.find((a) =>
          /^SHA256SUMS\.txt$/i.test(a.name || "")
        );
        if (shaAsset && shaAsset.browser_download_url) {
          document.querySelectorAll("[data-checksum-link]").forEach((a) => {
            a.href = shaAsset.browser_download_url;
          });
        }
        document.querySelectorAll("[data-release-os]").forEach((row) => {
          const key = row.getAttribute("data-release-os");
          if (key && state.releaseAssets[key]) {
            row.href = state.releaseAssets[key];
          }
        });
      }
      updateHeroCta();
    } catch (_) {
      /* Offline or rate-limited: fallback links to /releases/latest stay intact */
    }
  }

  function escapeHtml(str) {
    return String(str)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  async function init() {
    state.lang = detectInitialLang();
    state.theme = detectInitialTheme();
    state.selectedOs = await detectInitialOs();

    applyTheme(state.theme, true);
    applyLang(state.lang, true);

    const langBtn = document.getElementById("lang-toggle");
    if (langBtn) {
      langBtn.addEventListener("click", () => {
        applyLang(state.lang === "zh" ? "en" : "zh");
      });
    }

    const themeBtn = document.getElementById("theme-toggle");
    if (themeBtn) {
      themeBtn.addEventListener("click", () => {
        applyTheme(state.theme === "dark" ? "light" : "dark");
      });
    }

    document.querySelectorAll("[data-os-select]").forEach((pill) => {
      pill.addEventListener("click", () => {
        const nextOs = pill.getAttribute("data-os-select");
        if (nextOs && PLATFORMS[nextOs]) {
          state.selectedOs = nextOs;
          updateHeroCta();
        }
      });
    });

    document.querySelectorAll("[data-acct-filter]").forEach((btn) => {
      btn.addEventListener("click", () => {
        state.accountFilter = btn.getAttribute("data-acct-filter") || "all";
        renderDemoWorkspace();
      });
    });

    const searchInput = document.getElementById("demo-search-input");
    if (searchInput) {
      searchInput.addEventListener("input", (e) => {
        state.searchQuery = e.target.value || "";
        renderDemoWorkspace();
      });
    }

    document.querySelectorAll("[data-tpl-choice]").forEach((btn) => {
      btn.addEventListener("click", () => {
        state.msgTemplate = btn.getAttribute("data-tpl-choice") || "app";
        renderDemoWorkspace();
      });
    });

    const privacyBtn = document.getElementById("demo-privacy-action");
    if (privacyBtn) {
      privacyBtn.addEventListener("click", () => {
        state.imagesLoadedOnce = !state.imagesLoadedOnce;
        renderDemoWorkspace();
      });
    }

    const offlineBtn = document.getElementById("demo-offline-toggle");
    if (offlineBtn) {
      offlineBtn.addEventListener("click", () => {
        state.offlineMode = !state.offlineMode;
        renderOfflineState();
      });
    }

    const termTabs = Array.from(document.querySelectorAll("[data-term-tab]"));
    termTabs.forEach((tab, idx) => {
      tab.addEventListener("click", () => {
        state.termTab = tab.getAttribute("data-term-tab") || "build-local";
        renderTerminal();
      });

      tab.addEventListener("keydown", (e) => {
        let nextIdx = -1;
        if (e.key === "ArrowRight") nextIdx = (idx + 1) % termTabs.length;
        else if (e.key === "ArrowLeft") nextIdx = (idx - 1 + termTabs.length) % termTabs.length;
        else if (e.key === "Home") nextIdx = 0;
        else if (e.key === "End") nextIdx = termTabs.length - 1;
        if (nextIdx >= 0) {
          e.preventDefault();
          const nextTab = termTabs[nextIdx];
          state.termTab = nextTab.getAttribute("data-term-tab") || "build-local";
          renderTerminal();
          nextTab.focus();
        }
      });
    });

    const copyBtn = document.getElementById("term-copy-btn");
    let copyTimer = null;
    if (copyBtn) {
      copyBtn.addEventListener("click", async () => {
        try {
          await navigator.clipboard.writeText(getRawTerminalCommands());
          copyBtn.setAttribute("data-copied", "true");
          const zhNode = copyBtn.querySelector("[data-copy-zh]");
          const enNode = copyBtn.querySelector("[data-copy-en]");
          if (zhNode) zhNode.textContent = "已复制";
          if (enNode) enNode.textContent = "Copied";
          if (copyTimer) clearTimeout(copyTimer);
          copyTimer = setTimeout(() => {
            copyBtn.removeAttribute("data-copied");
            if (zhNode) zhNode.textContent = "复制命令";
            if (enNode) enNode.textContent = "Copy";
          }, 1800);
        } catch (_) {}
      });
    }

    hydrateLatestRelease();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
