import { useCallback, useEffect, useState } from "react";
import { Activity, ArrowUpRight, Boxes, Cpu, Globe2, HardDrive, MemoryStick, RefreshCw, Server, TimerReset } from "lucide-react";
import { api } from "../api";
import type { Overview } from "../types";
import type { Section } from "../components/Shell";
import { ErrorPanel, IconButton, Loading, PageHeader, StatusBadge } from "../components/UI";

export function OverviewPage({ navigate }: { navigate: (section: Section) => void }) {
  const [data, setData] = useState<Overview | null>(null);
  const [error, setError] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const load = useCallback(async (quiet = false) => {
    if (!quiet) setRefreshing(true);
    try {
      const overview = await api.overview();
      setData(overview);
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法读取服务器状态");
    } finally {
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(true), 5000);
    return () => window.clearInterval(timer);
  }, [load]);

  if (!data && !error) return <Loading label="正在读取服务器状态" />;
  if (!data) return <ErrorPanel message={error} retry={() => void load()} />;
  const { metrics } = data;
  return (
    <div className="page-stack">
      <PageHeader title="服务器总览" subtitle="资源使用、Node 进程与近期变更" actions={
        <IconButton label="刷新状态" onClick={() => void load()} disabled={refreshing}><RefreshCw size={18} className={refreshing ? "spin" : ""} /></IconButton>
      } />
      {data.warning && <div className="inline-warning"><Server size={17} />{data.warning}</div>}
      <section className="metric-grid" aria-label="服务器资源">
        <MetricCard icon={<Cpu size={19} />} label="CPU" value={`${metrics.cpuPercent.toFixed(1)}%`} detail={`${metrics.cpuCores} 核心 · 负载 ${metrics.load1.toFixed(2)}`} percent={metrics.cpuPercent} tone="green" />
        <MetricCard icon={<MemoryStick size={19} />} label="内存" value={`${metrics.memoryPercent.toFixed(1)}%`} detail={`${formatBytes(metrics.memoryUsed)} / ${formatBytes(metrics.memoryTotal)}`} percent={metrics.memoryPercent} tone="blue" />
        <MetricCard icon={<HardDrive size={19} />} label="磁盘" value={`${metrics.diskPercent.toFixed(1)}%`} detail={`${formatBytes(metrics.diskUsed)} / ${formatBytes(metrics.diskTotal)}`} percent={metrics.diskPercent} tone="amber" />
        <MetricCard icon={<Activity size={19} />} label="运行时间" value={formatUptime(metrics.uptimeSeconds)} detail={`负载 ${metrics.load1.toFixed(2)} / ${metrics.load5.toFixed(2)} / ${metrics.load15.toFixed(2)}`} percent={Math.min(metrics.load1 / Math.max(metrics.cpuCores, 1) * 100, 100)} tone="cyan" />
      </section>
      <section className="summary-strip">
        <SummaryItem icon={<Boxes size={18} />} label="Node 项目" value={data.summary.projects} hint={`${data.summary.activeProjects} 个运行中`} onClick={() => navigate("projects")} />
        <SummaryItem icon={<Globe2 size={18} />} label="绑定域名" value={data.summary.domains} hint="Caddy 自动维护证书" onClick={() => navigate("domains")} />
        <SummaryItem icon={<TimerReset size={18} />} label="进程状态" value={data.processes.length} hint="由 PM2 守护" onClick={() => navigate("projects")} />
      </section>
      <div className="overview-columns">
        <section className="panel process-panel">
          <header className="panel-header"><div><h2>PM2 进程</h2><p>当前资源占用</p></div><button className="text-action" onClick={() => navigate("projects")}>全部项目<ArrowUpRight size={15} /></button></header>
          <div className="table-wrap">
            <table><thead><tr><th>进程</th><th>状态</th><th>PID</th><th>CPU</th><th>内存</th></tr></thead>
              <tbody>{data.processes.length ? data.processes.map((process) => <tr key={process.name}><td><strong>{process.name}</strong></td><td><StatusBadge status={process.status} /></td><td className="mono">{process.pid || "--"}</td><td>{process.cpu.toFixed(1)}%</td><td>{formatBytes(process.memory)}</td></tr>) : <tr><td colSpan={5} className="table-empty">暂无运行中的 PM2 进程</td></tr>}</tbody>
            </table>
          </div>
        </section>
        <section className="panel activity-panel">
          <header className="panel-header"><div><h2>近期操作</h2><p>管理员变更记录</p></div><button className="text-action" onClick={() => navigate("activity")}>查看审计<ArrowUpRight size={15} /></button></header>
          <div className="activity-list">{data.recent.length ? data.recent.map((item) => <div className="activity-item" key={item.id}><span className="activity-marker" /><div><strong>{actionLabel(item.action)}</strong><span>{item.target || "ServerPal"}</span></div><time>{relativeTime(item.createdAt)}</time></div>) : <div className="table-empty">暂无操作记录</div>}</div>
        </section>
      </div>
    </div>
  );
}

function MetricCard({ icon, label, value, detail, percent, tone }: { icon: React.ReactNode; label: string; value: string; detail: string; percent: number; tone: string }) {
  return <article className={`metric-card metric-${tone}`}><div className="metric-copy"><span className="metric-label">{icon}{label}</span><strong>{value}</strong><p>{detail}</p></div><div className="metric-ring" style={{ "--metric-value": `${Math.max(0, Math.min(percent, 100)) * 3.6}deg` } as React.CSSProperties}><span>{Math.round(percent)}</span></div></article>;
}

function SummaryItem({ icon, label, value, hint, onClick }: { icon: React.ReactNode; label: string; value: number; hint: string; onClick: () => void }) {
  return <button className="summary-item" onClick={onClick}><span className="summary-icon">{icon}</span><span><small>{label}</small><strong>{value}</strong><em>{hint}</em></span><ArrowUpRight size={17} /></button>;
}

export function formatBytes(value: number): string {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index > 2 ? 1 : 0)} ${units[index]}`;
}

function formatUptime(seconds: number): string {
  if (!seconds) return "--";
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  return days ? `${days}天 ${hours}小时` : `${hours}小时`;
}

export function relativeTime(value: string): string {
  const delta = Math.max(0, Date.now() - new Date(value).getTime());
  if (delta < 60000) return "刚刚";
  if (delta < 3600000) return `${Math.floor(delta / 60000)} 分钟前`;
  if (delta < 86400000) return `${Math.floor(delta / 3600000)} 小时前`;
  return `${Math.floor(delta / 86400000)} 天前`;
}

export function actionLabel(action: string): string {
  const labels: Record<string, string> = { "auth.login": "管理员登录", "auth.logout": "退出登录", "project.create": "创建项目", "project.start": "启动项目", "project.restart": "重启项目", "project.stop": "停止项目", "project.delete": "删除项目", "domain.bind": "绑定域名", "domain.remove": "移除域名", "file.content": "修改文件", "terminal.open": "打开终端" };
  return labels[action] ?? action;
}
