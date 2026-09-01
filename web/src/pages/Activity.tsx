import { useEffect, useState } from "react";
import { Filter, ScrollText } from "lucide-react";
import { api } from "../api";
import type { AuditLog } from "../types";
import { ErrorPanel, Loading, PageHeader } from "../components/UI";
import { actionLabel, relativeTime } from "./Overview";

export function ActivityPage() {
  const [items, setItems] = useState<AuditLog[] | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  useEffect(() => { api.audit().then(setItems).catch((reason: Error) => setError(reason.message)); }, []);
  if (!items && !error) return <Loading label="正在读取操作审计" />;
  if (!items) return <ErrorPanel message={error} />;
  const filtered = items.filter((item) => `${item.action} ${item.target} ${item.username} ${item.ip}`.toLowerCase().includes(query.toLowerCase()));
  return <div className="page-stack"><PageHeader title="操作审计" subtitle="登录、项目、域名、文件与终端操作记录" actions={<label className="search-field"><Filter size={16} /><input placeholder="筛选记录" value={query} onChange={(event) => setQuery(event.target.value)} /></label>} />
    <section className="panel audit-table"><div className="table-wrap"><table><thead><tr><th>时间</th><th>操作</th><th>目标</th><th>管理员</th><th>来源 IP</th><th>详情</th></tr></thead><tbody>{filtered.map((item) => <tr key={item.id}><td title={new Date(item.createdAt).toLocaleString()}>{relativeTime(item.createdAt)}</td><td><span className="audit-action"><ScrollText size={15} />{actionLabel(item.action)}</span></td><td><strong>{item.target || "--"}</strong></td><td>{item.username}</td><td className="mono">{item.ip || "local"}</td><td className="muted-cell">{item.detail || "--"}</td></tr>)}</tbody></table></div></section>
  </div>;
}
