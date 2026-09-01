import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronRight, Download, File, FileCode2, Folder, FolderOpen, FolderPen, FolderPlus, Pencil, RefreshCw, Save, Trash2, Upload } from "lucide-react";
import { api } from "../api";
import type { FileEntry, FileList, Project } from "../types";
import { Button, EmptyState, ErrorPanel, IconButton, Loading, Modal, PageHeader } from "../components/UI";
import { formatBytes } from "./Overview";

export function FilesPage({ notify }: { notify: (type: "success" | "error", message: string) => void }) {
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [projectID, setProjectID] = useState("");
  const [path, setPath] = useState("");
  const [listing, setListing] = useState<FileList | null>(null);
  const [error, setError] = useState("");
  const [editor, setEditor] = useState<{ path: string; content: string } | null>(null);
  const [dragging, setDragging] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    api.projects().then((items) => { setProjects(items); if (items[0]) setProjectID(items[0].id); }).catch((reason: Error) => setError(reason.message));
  }, []);
  const project = useMemo(() => projects?.find((item) => item.id === projectID), [projects, projectID]);
  const load = useCallback(async () => {
    if (!project) return;
    try { setListing(await api.files(project.rootPath, path)); setError(""); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "无法读取目录"); }
  }, [project, path]);
  useEffect(() => { setListing(null); void load(); }, [load]);

  const open = async (entry: FileEntry) => {
    if (entry.type === "directory") { setPath(entry.path); return; }
    try { setEditor({ path: entry.path, content: await api.fileContent(project!.rootPath, entry.path) }); }
    catch (reason) { notify("error", reason instanceof Error ? reason.message : "无法打开文件"); }
  };
  const uploadFile = async (file: File) => {
    if (!project) return;
    const destination = joinPath(path, file.name);
    try { await api.upload(project.rootPath, destination, file); notify("success", `${file.name} 已上传`); await load(); }
    catch (reason) { notify("error", reason instanceof Error ? reason.message : "上传失败"); }
  };
  const mkdir = async () => {
    if (!project) return;
    const name = window.prompt("新文件夹名称");
    if (!name || name.includes("/") || name === "." || name === "..") return;
    try { await api.mkdir(project.rootPath, joinPath(path, name)); notify("success", "文件夹已创建"); await load(); }
    catch (reason) { notify("error", reason instanceof Error ? reason.message : "创建失败"); }
  };
  const rename = async (entry: FileEntry) => {
    if (!project) return;
    const name = window.prompt("新名称", entry.name);
    if (!name || name === entry.name || name.includes("/")) return;
    const parent = entry.path.includes("/") ? entry.path.slice(0, entry.path.lastIndexOf("/")) : "";
    try { await api.rename(project.rootPath, entry.path, joinPath(parent, name)); notify("success", "已重命名"); await load(); }
    catch (reason) { notify("error", reason instanceof Error ? reason.message : "重命名失败"); }
  };
  const trash = async (entry: FileEntry) => {
    if (!project || !window.confirm(`将“${entry.name}”移入项目回收站？`)) return;
    try { await api.trash(project.rootPath, entry.path); notify("success", "已移入 .serverpal-trash"); await load(); }
    catch (reason) { notify("error", reason instanceof Error ? reason.message : "移动失败"); }
  };

  if (!projects && !error) return <Loading label="正在读取项目目录" />;
  if (!projects) return <ErrorPanel message={error} />;
  if (!projects.length) return <div className="page-stack"><PageHeader title="文件" subtitle="项目目录可视化管理" /><EmptyState title="还没有项目目录" description="添加 Node 项目后即可管理其文件" /></div>;

  const crumbs = path ? path.split("/").filter(Boolean) : [];
  return <div className="page-stack">
    <PageHeader title="文件" subtitle="浏览、编辑与上传项目文件" actions={<><input ref={fileInput} className="visually-hidden" type="file" onChange={(event) => { const file = event.target.files?.[0]; if (file) void uploadFile(file); event.target.value = ""; }} /><IconButton label="刷新目录" onClick={() => void load()}><RefreshCw size={18} /></IconButton><Button variant="secondary" onClick={() => fileInput.current?.click()}><Upload size={16} />上传</Button><Button onClick={() => void mkdir()}><FolderPlus size={16} />新建目录</Button></>} />
    <section className="file-location"><label><span>项目</span><select value={projectID} onChange={(event) => { setProjectID(event.target.value); setPath(""); }}>{projects.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><nav className="breadcrumbs" aria-label="当前路径"><button onClick={() => setPath("")}><FolderOpen size={16} />{project?.name}</button>{crumbs.map((crumb, index) => <span key={`${crumb}-${index}`}><ChevronRight size={15} /><button onClick={() => setPath(crumbs.slice(0, index + 1).join("/"))}>{crumb}</button></span>)}</nav><code>{project?.rootPath}</code></section>
    {error && <ErrorPanel message={error} retry={() => void load()} />}
    <section className={`panel file-panel ${dragging ? "dragging" : ""}`} onDragOver={(event) => { event.preventDefault(); setDragging(true); }} onDragLeave={() => setDragging(false)} onDrop={(event) => { event.preventDefault(); setDragging(false); const file = event.dataTransfer.files[0]; if (file) void uploadFile(file); }}>
      {!listing ? <Loading label="正在读取目录" /> : listing.entries.length ? <div className="table-wrap"><table className="file-table"><thead><tr><th>名称</th><th>权限 / 所有者</th><th>大小</th><th>修改时间</th><th className="actions-cell">操作</th></tr></thead><tbody>{listing.entries.map((entry) => <tr key={entry.path} onDoubleClick={() => void open(entry)}><td><button className="file-name" onClick={() => void open(entry)}>{entry.type === "directory" ? <Folder size={19} className="folder-icon" /> : isCode(entry.name) ? <FileCode2 size={19} /> : <File size={19} />}<strong>{entry.name}</strong></button></td><td><div className="permission"><span className="mono">{entry.mode}</span><small>{entry.owner}</small></div></td><td>{entry.type === "directory" ? "--" : formatBytes(entry.size)}</td><td>{new Date(entry.modifiedAt).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })}</td><td className="actions-cell"><div className="row-actions">{entry.type === "file" && <><IconButton label="编辑" onClick={() => void open(entry)}><Pencil size={16} /></IconButton><IconButton label="下载" onClick={() => void api.download(project!.rootPath, entry.path)}><Download size={16} /></IconButton></>}<IconButton label="重命名" onClick={() => void rename(entry)}><FolderPen size={16} /></IconButton><IconButton label="移入回收站" className="danger-icon" onClick={() => void trash(entry)}><Trash2 size={16} /></IconButton></div></td></tr>)}</tbody></table></div> : <EmptyState title="目录为空" description="上传文件或新建目录" />}
      {dragging && <div className="drop-overlay"><Upload size={28} /><strong>释放以上传文件</strong></div>}
    </section>
    {editor && project && <FileEditor root={project.rootPath} file={editor} onClose={() => setEditor(null)} onSaved={async () => { setEditor(null); await load(); notify("success", "文件已保存"); }} />}
  </div>;
}

function FileEditor({ root, file, onClose, onSaved }: { root: string; file: { path: string; content: string }; onClose: () => void; onSaved: () => Promise<void> }) {
  const [content, setContent] = useState(file.content);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const save = async () => {
    setSaving(true); setError("");
    try { await api.saveFile(root, file.path, content); await onSaved(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "保存失败"); }
    finally { setSaving(false); }
  };
  return <Modal title={file.path.split("/").pop() ?? file.path} description={file.path} onClose={onClose} wide><div className="file-editor"><textarea value={content} onChange={(event) => setContent(event.target.value)} spellCheck={false} autoFocus />{error && <div className="form-error">{error}</div>}<footer className="modal-actions"><Button variant="secondary" onClick={onClose}>取消</Button><Button onClick={() => void save()} disabled={saving}><Save size={16} />{saving ? "保存中" : "保存文件"}</Button></footer></div></Modal>;
}

function joinPath(...parts: string[]): string { return parts.filter(Boolean).join("/").replace(/\/{2,}/g, "/"); }
function isCode(name: string): boolean { return /\.(?:js|mjs|cjs|ts|tsx|jsx|json|css|html|md|env|yaml|yml|toml|sh)$/i.test(name) || name.startsWith("."); }
