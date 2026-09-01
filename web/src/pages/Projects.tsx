import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Braces, FileText, Pencil, Play, Plus, RefreshCw, RotateCw, Search, Square, Trash2 } from "lucide-react";
import { api } from "../api";
import type { Project, ProjectInput } from "../types";
import { Button, EmptyState, ErrorPanel, IconButton, Loading, Modal, PageHeader, StatusBadge } from "../components/UI";
import { formatBytes } from "./Overview";

const emptyProject: ProjectInput = {
  name: "",
  rootPath: "/srv/serverpal/apps/",
  runtime: "npm",
  entry: "dist/index.js",
  npmScript: "start",
  nodeBinary: "/opt/serverpal/node/current/bin/node",
  port: 3000,
  instances: 1,
  pm2Name: "",
  env: { NODE_ENV: "production" },
};

export function ProjectsPage({ notify }: { notify: (type: "success" | "error", message: string) => void }) {
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<Project | "new" | null>(null);
  const [logs, setLogs] = useState<{ name: string; value: string } | null>(null);
  const [busy, setBusy] = useState("");
  const load = useCallback(async () => {
    try { setProjects(await api.projects()); setError(""); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "无法读取项目"); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  const filtered = useMemo(() => (projects ?? []).filter((project) => `${project.name} ${project.pm2Name} ${project.rootPath}`.toLowerCase().includes(query.toLowerCase())), [projects, query]);

  const action = async (project: Project, value: string) => {
    setBusy(`${project.id}:${value}`);
    try {
      await api.projectAction(project.id, value);
      notify("success", `${project.name} 已${value === "stop" ? "停止" : value === "start" ? "启动" : "重启"}`);
      await load();
    } catch (reason) { notify("error", reason instanceof Error ? reason.message : "操作失败"); }
    finally { setBusy(""); }
  };

  const remove = async (project: Project) => {
    if (!window.confirm(`删除项目“${project.name}”的面板记录？项目文件会保留。`)) return;
    try { await api.deleteProject(project.id); notify("success", "项目记录已删除，文件保持不变"); await load(); }
    catch (reason) { notify("error", reason instanceof Error ? reason.message : "删除失败"); }
  };

  const showLogs = async (project: Project) => {
    setBusy(`${project.id}:logs`);
    try { setLogs({ name: project.name, value: await api.logs(project.id) }); }
    catch (reason) { notify("error", reason instanceof Error ? reason.message : "无法读取日志"); }
    finally { setBusy(""); }
  };

  if (!projects && !error) return <Loading label="正在读取 Node 项目" />;
  if (!projects) return <ErrorPanel message={error} retry={() => void load()} />;

  return <div className="page-stack">
    <PageHeader title="Node 项目" subtitle="通过 PM2 管理进程、实例与运行环境" actions={<><IconButton label="刷新项目" onClick={() => void load()}><RefreshCw size={18} /></IconButton><Button onClick={() => setEditing("new")}><Plus size={17} />添加项目</Button></>} />
    <section className="toolbar-band"><label className="search-field search-wide"><Search size={16} /><input placeholder="搜索项目、目录或 PM2 名称" value={query} onChange={(event) => setQuery(event.target.value)} /></label><div className="toolbar-summary"><span className="status status-online"><span className="status-dot" />{projects.filter((project) => project.status === "online").length} 运行中</span><span>{projects.length} 个项目</span></div></section>
    <section className="panel projects-panel">
      {filtered.length ? <div className="table-wrap"><table className="project-table"><thead><tr><th>项目</th><th>状态</th><th>PID</th><th>监听</th><th>资源</th><th>Node</th><th>实例</th><th className="actions-cell">操作</th></tr></thead><tbody>
        {filtered.map((project) => <tr key={project.id}><td><div className="project-name"><span className="project-icon"><Braces size={18} /></span><div><strong>{project.name}</strong><span className="mono">{project.rootPath}</span></div></div></td><td><StatusBadge status={project.status} /></td><td className="mono">{project.pid || "--"}</td><td><span className="port-value">:{project.port}</span></td><td><div className="resource-value"><span>{(project.cpu ?? 0).toFixed(1)}% CPU</span><span>{formatBytes(project.memory ?? 0)}</span></div></td><td className="mono node-version">{shortNode(project.nodeBinary)}</td><td>{project.instances}</td><td className="actions-cell"><div className="row-actions">
          {project.status === "online" ? <IconButton label="停止" disabled={busy !== ""} onClick={() => void action(project, "stop")}><Square size={16} /></IconButton> : <IconButton label="启动" disabled={busy !== ""} onClick={() => void action(project, "start")}><Play size={16} /></IconButton>}
          <IconButton label="重启" disabled={busy !== ""} onClick={() => void action(project, "restart")}><RotateCw size={16} className={busy === `${project.id}:restart` ? "spin" : ""} /></IconButton>
          <IconButton label="查看日志" disabled={busy !== ""} onClick={() => void showLogs(project)}><FileText size={16} /></IconButton>
          <IconButton label="编辑项目" onClick={() => setEditing(project)}><Pencil size={16} /></IconButton>
          <IconButton label="删除项目" className="danger-icon" onClick={() => void remove(project)}><Trash2 size={16} /></IconButton>
        </div></td></tr>)}
      </tbody></table></div> : <EmptyState title="没有匹配的项目" description={query ? "换一个关键词试试" : "添加第一个 Node 项目开始管理"} action={!query ? <Button onClick={() => setEditing("new")}><Plus size={16} />添加项目</Button> : undefined} />}
    </section>
    {editing && <ProjectModal project={editing === "new" ? null : editing} onClose={() => setEditing(null)} onSaved={async () => { setEditing(null); await load(); notify("success", editing === "new" ? "项目已添加" : "项目配置已更新"); }} />}
    {logs && <Modal title={`${logs.name} · PM2 日志`} description="最近 500 行 stdout 与 stderr" onClose={() => setLogs(null)} wide><pre className="log-viewer">{logs.value || "暂无日志输出"}</pre></Modal>}
  </div>;
}

function ProjectModal({ project, onClose, onSaved }: { project: Project | null; onClose: () => void; onSaved: () => Promise<void> }) {
  const [form, setForm] = useState<ProjectInput>(() => project ? projectInput(project) : { ...emptyProject, env: { ...emptyProject.env } });
  const [envText, setEnvText] = useState(() => Object.entries(form.env).map(([key, value]) => `${key}=${value}`).join("\n"));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const set = <K extends keyof ProjectInput>(key: K, value: ProjectInput[K]) => setForm((current) => ({ ...current, [key]: value }));
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true); setError("");
    try {
      const input = { ...form, env: parseEnv(envText) };
      if (project) await api.updateProject(project.id, input); else await api.createProject(input);
      await onSaved();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "保存失败"); }
    finally { setSaving(false); }
  };
  return <Modal title={project ? "编辑 Node 项目" : "添加 Node 项目"} description="项目进程将以 serverpal-app 用户运行" onClose={onClose} wide><form className="project-form" onSubmit={submit}>
    <div className="form-grid"><label><span>项目名称</span><input value={form.name} onChange={(event) => set("name", event.target.value)} placeholder="例如 API Service" required /></label><label><span>项目端口</span><input type="number" min={1024} max={65535} value={form.port} onChange={(event) => set("port", Number(event.target.value))} required /></label></div>
    <label><span>项目目录</span><input className="mono" value={form.rootPath} onChange={(event) => set("rootPath", event.target.value)} placeholder="/srv/serverpal/apps/my-api" required /></label>
    <div className="form-grid"><label><span>运行方式</span><div className="segmented"><button type="button" className={form.runtime === "npm" ? "active" : ""} onClick={() => set("runtime", "npm")}>npm script</button><button type="button" className={form.runtime === "node" ? "active" : ""} onClick={() => set("runtime", "node")}>Node 入口</button></div></label>{form.runtime === "npm" ? <label><span>npm script</span><input value={form.npmScript} onChange={(event) => set("npmScript", event.target.value)} placeholder="start" required /></label> : <label><span>入口文件</span><input className="mono" value={form.entry} onChange={(event) => set("entry", event.target.value)} placeholder="dist/index.js" required /></label>}</div>
    <div className="form-grid"><label><span>Node 可执行文件</span><input className="mono" value={form.nodeBinary} onChange={(event) => set("nodeBinary", event.target.value)} required /></label><label><span>实例数</span><input type="number" min={1} max={32} value={form.instances} onChange={(event) => set("instances", Number(event.target.value))} /></label></div>
    <label><span>PM2 进程名称 <small>{project ? "创建后固定" : "留空自动生成"}</small></span><input className="mono" value={form.pm2Name} onChange={(event) => set("pm2Name", event.target.value)} placeholder="my-api" disabled={Boolean(project)} /></label>
    <label><span>环境变量</span><textarea className="mono env-editor" value={envText} onChange={(event) => setEnvText(event.target.value)} placeholder="NODE_ENV=production\nDATABASE_URL=..." spellCheck={false} /></label>
    {error && <div className="form-error">{error}</div>}
    <footer className="modal-actions"><Button type="button" variant="secondary" onClick={onClose}>取消</Button><Button type="submit" disabled={saving}>{saving ? "正在保存" : "保存项目"}</Button></footer>
  </form></Modal>;
}

function projectInput(project: Project): ProjectInput {
  return { name: project.name, rootPath: project.rootPath, runtime: project.runtime, entry: project.entry, npmScript: project.npmScript, nodeBinary: project.nodeBinary, port: project.port, instances: project.instances, pm2Name: project.pm2Name, env: project.env };
}

function parseEnv(value: string): Record<string, string> {
  const env: Record<string, string> = {};
  for (const line of value.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const index = trimmed.indexOf("=");
    if (index < 1) throw new Error(`环境变量格式错误：${line}`);
    env[trimmed.slice(0, index).trim()] = trimmed.slice(index + 1);
  }
  return env;
}

function shortNode(path: string): string {
  const match = path.match(/v\d+(?:\.\d+){0,2}/);
  return match?.[0] ?? path.split("/").slice(-2).join("/");
}
