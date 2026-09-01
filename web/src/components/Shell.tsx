import type { ReactNode } from "react";
import { useEffect, useState } from "react";
import {
  Activity, Boxes, ChevronLeft, FolderTree, Globe2, LayoutDashboard, LogOut,
  Menu, Moon, ScrollText, Server, Settings2, ShieldCheck, SquareTerminal, Sun, X,
} from "lucide-react";
import type { SystemStatus, User } from "../types";
import { IconButton } from "./UI";

export type Section = "overview" | "projects" | "domains" | "files" | "terminal" | "activity" | "system";

const navItems: Array<{ id: Section; label: string; icon: typeof LayoutDashboard }> = [
  { id: "overview", label: "总览", icon: LayoutDashboard },
  { id: "projects", label: "Node 项目", icon: Boxes },
  { id: "domains", label: "域名与 SSL", icon: Globe2 },
  { id: "files", label: "文件", icon: FolderTree },
  { id: "terminal", label: "终端", icon: SquareTerminal },
  { id: "activity", label: "操作审计", icon: ScrollText },
  { id: "system", label: "系统状态", icon: Settings2 },
];

export function Shell({ section, setSection, user, system, theme, setTheme, onLogout, children }: {
  section: Section;
  setSection: (section: Section) => void;
  user: User;
  system: SystemStatus | null;
  theme: "dark" | "light";
  setTheme: (theme: "dark" | "light") => void;
  onLogout: () => void;
  children: ReactNode;
}) {
  const [mobileOpen, setMobileOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(false);

  useEffect(() => setMobileOpen(false), [section]);

  const navigate = (value: Section) => {
    setSection(value);
  };

  return (
    <div className={`app-shell ${collapsed ? "sidebar-collapsed" : ""}`}>
      {mobileOpen && <button className="mobile-scrim" aria-label="关闭导航" onClick={() => setMobileOpen(false)} />}
      <aside className={`sidebar ${mobileOpen ? "mobile-open" : ""}`}>
        <div className="brand-row">
          <div className="brand-mark"><ShieldCheck size={20} /></div>
          <div className="brand-copy"><strong>ServerPal</strong><span>Node Operations</span></div>
          <IconButton label="关闭导航" className="mobile-close" onClick={() => setMobileOpen(false)}><X size={18} /></IconButton>
        </div>
        <div className="server-identity">
          <span className="server-icon"><Server size={17} /></span>
          <div><strong>{system?.os === "linux" ? "Ubuntu Server" : "Local Preview"}</strong><span>{system ? `${system.architecture} · ${system.agentVersion}` : "正在连接"}</span></div>
          <span className={`connection-dot ${system ? "online" : ""}`} title={system ? "服务正常" : "正在连接"} />
        </div>
        <nav className="main-nav" aria-label="主导航">
          <span className="nav-label">工作区</span>
          {navItems.slice(0, 5).map((item) => <NavItem key={item.id} item={item} active={section === item.id} onClick={() => navigate(item.id)} />)}
          <span className="nav-label nav-label-secondary">管理</span>
          {navItems.slice(5).map((item) => <NavItem key={item.id} item={item} active={section === item.id} onClick={() => navigate(item.id)} />)}
        </nav>
        <div className="sidebar-footer">
          <button className="account-row" onClick={onLogout} title="退出登录">
            <span className="avatar">{user.username.slice(0, 1).toUpperCase()}</span>
            <span className="account-copy"><strong>{user.username}</strong><small>管理员</small></span>
            <LogOut size={16} />
          </button>
          <IconButton label={collapsed ? "展开侧栏" : "收起侧栏"} className="collapse-button" onClick={() => setCollapsed((value) => !value)}>
            <ChevronLeft size={18} />
          </IconButton>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <IconButton label="打开导航" className="mobile-menu" onClick={() => setMobileOpen(true)}><Menu size={19} /></IconButton>
          <div className="topbar-status"><Activity size={16} /><span>控制面在线</span><strong>{system?.pm2Available ? "PM2 已连接" : "PM2 检查中"}</strong></div>
          <div className="topbar-actions">
            <IconButton label={theme === "dark" ? "切换浅色主题" : "切换深色主题"} onClick={() => setTheme(theme === "dark" ? "light" : "dark")}>
              {theme === "dark" ? <Sun size={18} /> : <Moon size={18} />}
            </IconButton>
          </div>
        </header>
        <main className="page-content">{children}</main>
      </div>
    </div>
  );
}

function NavItem({ item, active, onClick }: { item: (typeof navItems)[number]; active: boolean; onClick: () => void }) {
  const Icon = item.icon;
  return <button className={`nav-item ${active ? "active" : ""}`} onClick={onClick} title={item.label}><Icon size={18} /><span>{item.label}</span></button>;
}
