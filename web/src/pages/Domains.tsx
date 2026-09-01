import { useCallback, useEffect, useState, type FormEvent } from "react";
import { CalendarClock, Globe2, Link2, Plus, RefreshCw, RotateCw, Search, ShieldCheck, Trash2 } from "lucide-react";
import { api } from "../api";
import type { Domain, Project } from "../types";
import { Button, EmptyState, ErrorPanel, IconButton, Loading, Modal, PageHeader, StatusBadge } from "../components/UI";

export function DomainsPage({ notify }: { notify: (type: "success" | "error", message: string) => void }) {
  const [domains, setDomains] = useState<Domain[] | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState("");
  const load = useCallback(async () => {
    try {
      const [domainItems, projectItems] = await Promise.all([api.domains(), api.projects()]);
      setDomains(domainItems); setProjects(projectItems); setError("");
    } catch (reason) { setError(reason instanceof Error ? reason.message : "无法读取域名"); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  const check = async (domain: Domain) => {
    setBusy(domain.id);
    try {
      const status = await api.checkDomain(domain.id);
      notify(status.status === "active" ? "success" : "error", status.status === "active" ? `${domain.hostname} 证书有效` : status.error || "证书尚未签发");
      await load();
    } catch (reason) { notify("error", reason instanceof Error ? reason.message : "检查失败"); }
    finally { setBusy(""); }
  };

  const remove = async (domain: Domain) => {
    if (!window.confirm(`解除 ${domain.hostname} 的绑定？`)) return;
    try { await api.deleteDomain(domain.id); notify("success", "域名绑定已移除"); await load(); }
    catch (reason) { notify("error", reason instanceof Error ? reason.message : "删除失败"); }
  };

  if (!domains && !error) return <Loading label="正在读取域名与证书" />;
  if (!domains) return <ErrorPanel message={error} retry={() => void load()} />;
  const filtered = domains.filter((domain) => `${domain.hostname} ${domain.projectName}`.toLowerCase().includes(query.toLowerCase()));
  return <div className="page-stack">
    <PageHeader title="域名与 SSL" subtitle="由 Caddy 配置反向代理、签发并自动续期证书" actions={<><IconButton label="刷新域名" onClick={() => void load()}><RefreshCw size={18} /></IconButton><Button onClick={() => setAdding(true)} disabled={!projects.length}><Plus size={17} />绑定域名</Button></>} />
    <section className="ssl-banner"><span className="ssl-icon"><ShieldCheck size={22} /></span><div><strong>自动 HTTPS 已托管</strong><p>域名解析生效后，Caddy 自动申请证书并在到期前续签。</p></div><span className="ssl-count">{domains.filter((domain) => domain.status === "active").length}<small>有效证书</small></span></section>
    <section className="toolbar-band"><label className="search-field search-wide"><Search size={16} /><input placeholder="搜索域名或项目" value={query} onChange={(event) => setQuery(event.target.value)} /></label><span>{domains.length} 个域名</span></section>
    <section className="panel domains-panel">{filtered.length ? <div className="table-wrap"><table><thead><tr><th>域名</th><th>项目</th><th>上游</th><th>HTTPS</th><th>证书到期</th><th className="actions-cell">操作</th></tr></thead><tbody>{filtered.map((domain) => {
      const project = projects.find((item) => item.id === domain.projectId);
      return <tr key={domain.id}><td><div className="domain-name"><span><Globe2 size={17} /></span><div><strong>{domain.hostname}</strong><small>https://{domain.hostname}</small></div></div></td><td>{domain.projectName}</td><td className="mono">127.0.0.1:{project?.port ?? "--"}</td><td><StatusBadge status={domain.status} /></td><td>{domain.expiresAt ? <span className="expiry"><CalendarClock size={15} />{new Date(domain.expiresAt).toLocaleDateString("zh-CN")}</span> : <span className="muted-cell">等待签发</span>}</td><td className="actions-cell"><div className="row-actions"><IconButton label="检查证书" disabled={busy !== ""} onClick={() => void check(domain)}><RefreshCw size={16} className={busy === domain.id ? "spin" : ""} /></IconButton><IconButton label="重新应用配置" disabled={busy !== ""} onClick={async () => { setBusy(domain.id); try { await api.applyDomain(domain.id); notify("success", "Caddy 配置已重新加载"); } catch (reason) { notify("error", reason instanceof Error ? reason.message : "应用失败"); } finally { setBusy(""); } }}><RotateCw size={16} /></IconButton><IconButton label="解除绑定" className="danger-icon" onClick={() => void remove(domain)}><Trash2 size={16} /></IconButton></div></td></tr>;
    })}</tbody></table></div> : <EmptyState title="还没有域名绑定" description="绑定域名后会自动配置反向代理和 HTTPS" action={<Button onClick={() => setAdding(true)} disabled={!projects.length}><Link2 size={16} />绑定域名</Button>} />}</section>
    {adding && <DomainModal projects={projects} onClose={() => setAdding(false)} onSaved={async () => { setAdding(false); await load(); notify("success", "域名已绑定，证书将在 DNS 生效后自动签发"); }} />}
  </div>;
}

function DomainModal({ projects, onClose, onSaved }: { projects: Project[]; onClose: () => void; onSaved: () => Promise<void> }) {
  const [projectID, setProjectID] = useState(projects[0]?.id ?? "");
  const [hostname, setHostname] = useState("");
  const [https, setHTTPS] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const submit = async (event: FormEvent) => {
    event.preventDefault(); setSaving(true); setError("");
    try { await api.createDomain(projectID, hostname, https); await onSaved(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "绑定失败"); }
    finally { setSaving(false); }
  };
  const project = projects.find((item) => item.id === projectID);
  return <Modal title="绑定域名" description="DNS 需指向当前服务器的公网 IP" onClose={onClose}><form className="project-form" onSubmit={submit}>
    <label><span>Node 项目</span><select value={projectID} onChange={(event) => setProjectID(event.target.value)}>{projects.map((item) => <option value={item.id} key={item.id}>{item.name} · :{item.port}</option>)}</select></label>
    <label><span>域名</span><input autoFocus value={hostname} onChange={(event) => setHostname(event.target.value.toLowerCase().trim())} placeholder="api.example.com" required /></label>
    <div className="upstream-preview"><span>反向代理</span><strong className="mono">{hostname || "your-domain.com"}</strong><Link2 size={16} /><strong className="mono">127.0.0.1:{project?.port ?? 3000}</strong></div>
    <label className="toggle-row"><span><strong>自动 HTTPS</strong><small>签发并自动续期 Let's Encrypt 证书</small></span><input type="checkbox" checked={https} onChange={(event) => setHTTPS(event.target.checked)} /><i /></label>
    {error && <div className="form-error">{error}</div>}
    <footer className="modal-actions"><Button type="button" variant="secondary" onClick={onClose}>取消</Button><Button type="submit" disabled={saving}>{saving ? "正在应用" : "绑定并应用"}</Button></footer>
  </form></Modal>;
}
