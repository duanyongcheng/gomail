import { useEffect, useState } from "react";
import { GmailService, type GmailStatus } from "./mygo";
import { activateGmailInbox, setGmailCapabilities } from "./harness";

export default function GmailPanel() {
  const [status, setStatus] = useState<GmailStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  useEffect(() => {
    GmailService.status().then((s) => { setStatus(s); setGmailCapabilities(s.modify); }).catch((e) => setMessage(String(e)));
  }, []);
  async function login() {
    setBusy(true); setMessage("等待系统浏览器授权（最多 5 分钟）…");
    try {
      const next=await GmailService.login();setStatus(next);setGmailCapabilities(next.modify);setMessage("授权成功，请点击同步。");
    }
    catch (e) { setMessage(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(false); }
  }
  async function sync() {
    setBusy(true);
    try {
      const result = await GmailService.sync();
      await activateGmailInbox();
      setMessage(`本次加载 ${result.count} 封；${result.initial ? "初次同步" : "History 增量"}${result.limited ? "，最近邮件缓存达到上限" : ""}`);
    } catch (e) { setMessage(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(false); }
  }
  return (
    <section data-gmail-panel style={{ padding: "10px 18px", borderBottom: "1px solid #bbb", display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap", color: "var(--color-text-primary)" }}>
      <strong>Gmail {status?.mode === "live" ? "真实账号 · 隔离模式" : "本地 API Demo"}</strong>
      <span>{status?.modify ? "允许显式 flags 修改" : "只读"} · 发信/删除关闭</span>
      <button disabled={busy || status?.mode !== "live"} onClick={() => void login()}>Google 登录</button>
      <button disabled={busy || !status?.connected} onClick={() => void sync()}>同步最近邮件</button>
      <span role="status">{message || status?.message}</span>
    </section>
  );
}
