import type { ButtonHTMLAttributes, ReactNode } from "react";
import { AlertCircle, CheckCircle2, Inbox, LoaderCircle, X } from "lucide-react";

export function Button({ children, className = "", variant = "primary", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "danger" | "ghost" }) {
  return <button className={`button button-${variant} ${className}`} {...props}>{children}</button>;
}

export function IconButton({ label, children, className = "", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; children: ReactNode }) {
  return <button className={`icon-button ${className}`} title={label} aria-label={label} {...props}>{children}</button>;
}

export function Modal({ title, description, children, onClose, wide = false }: { title: string; description?: string; children: ReactNode; onClose: () => void; wide?: boolean }) {
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <section className={`modal ${wide ? "modal-wide" : ""}`} role="dialog" aria-modal="true" aria-labelledby="modal-title">
        <header className="modal-header">
          <div>
            <h2 id="modal-title">{title}</h2>
            {description && <p>{description}</p>}
          </div>
          <IconButton label="关闭" onClick={onClose}><X size={18} /></IconButton>
        </header>
        <div className="modal-body">{children}</div>
      </section>
    </div>
  );
}

export function StatusBadge({ status }: { status?: string }) {
  const normalized = status || "unknown";
  const labels: Record<string, string> = {
    online: "运行中", stopped: "已停止", stopping: "停止中", launching: "启动中",
    errored: "异常", unavailable: "代理离线", active: "有效", pending: "签发中",
    disabled: "未启用", error: "异常", unknown: "未知",
  };
  return <span className={`status status-${normalized}`}><span className="status-dot" />{labels[normalized] ?? normalized}</span>;
}

export function EmptyState({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return (
    <div className="empty-state">
      <Inbox size={28} strokeWidth={1.5} />
      <strong>{title}</strong>
      <span>{description}</span>
      {action}
    </div>
  );
}

export function Loading({ label = "正在加载" }: { label?: string }) {
  return <div className="loading"><LoaderCircle size={20} className="spin" /><span>{label}</span></div>;
}

export function Notice({ notice, onClose }: { notice: { type: "success" | "error"; message: string } | null; onClose: () => void }) {
  if (!notice) return null;
  return (
    <div className={`notice notice-${notice.type}`} role="status">
      {notice.type === "success" ? <CheckCircle2 size={18} /> : <AlertCircle size={18} />}
      <span>{notice.message}</span>
      <IconButton label="关闭提示" onClick={onClose}><X size={15} /></IconButton>
    </div>
  );
}

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle: string; actions?: ReactNode }) {
  return (
    <header className="page-header">
      <div><h1>{title}</h1><p>{subtitle}</p></div>
      {actions && <div className="page-actions">{actions}</div>}
    </header>
  );
}

export function ErrorPanel({ message, retry }: { message: string; retry?: () => void }) {
  return (
    <div className="error-panel">
      <AlertCircle size={20} />
      <span>{message}</span>
      {retry && <Button variant="secondary" onClick={retry}>重试</Button>}
    </div>
  );
}
