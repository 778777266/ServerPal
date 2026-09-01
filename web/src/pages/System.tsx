import { CheckCircle2, CircleAlert, Cpu, Database, HardDrive, RefreshCw, ServerCog, ShieldCheck, SquareTerminal } from "lucide-react";
import type { SystemStatus } from "../types";
import { IconButton, PageHeader } from "../components/UI";

export function SystemPage({ status, reload }: { status: SystemStatus | null; reload: () => void }) {
  return <div className="page-stack"><PageHeader title="系统状态" subtitle="控制面组件与运行环境" actions={<IconButton label="重新检测" onClick={reload}><RefreshCw size={18} /></IconButton>} />
    <section className="system-grid">
      <SystemItem icon={<ServerCog size={20} />} label="ServerPal Agent" value={status?.agentVersion ?? "检测中"} ok={Boolean(status)} />
      <SystemItem icon={<Cpu size={20} />} label="操作系统" value={status ? `${status.os} / ${status.architecture}` : "检测中"} ok={status?.os === "linux"} />
      <SystemItem icon={<Database size={20} />} label="Node.js" value={status?.node ?? "检测中"} ok={status?.node !== "unavailable"} />
      <SystemItem icon={<HardDrive size={20} />} label="PM2" value={status?.pm2 ?? "检测中"} ok={status?.pm2Available ?? false} />
      <SystemItem icon={<ShieldCheck size={20} />} label="Caddy 与 SSL" value={status?.caddy ?? "检测中"} ok={status?.caddyReady ?? false} />
      <SystemItem icon={<SquareTerminal size={20} />} label="项目终端" value={status?.terminal ? "已启用" : "已禁用"} ok={status?.terminal ?? false} />
    </section>
    <section className="panel boundary-panel"><header className="panel-header"><div><h2>权限边界</h2><p>当前服务运行方式</p></div></header><div className="boundary-flow"><div><span>Web</span><strong>普通用户</strong></div><i /><div><span>Unix Socket</span><strong>令牌认证</strong></div><i /><div><span>Agent</span><strong>白名单操作</strong></div><i /><div><span>Node 项目</span><strong>业务用户</strong></div></div></section>
  </div>;
}

function SystemItem({ icon, label, value, ok }: { icon: React.ReactNode; label: string; value: string; ok: boolean }) {
  return <article className="system-item"><span className="system-item-icon">{icon}</span><div><small>{label}</small><strong>{value}</strong></div><span className={`system-check ${ok ? "ok" : "warn"}`}>{ok ? <CheckCircle2 size={18} /> : <CircleAlert size={18} />}</span></article>;
}
