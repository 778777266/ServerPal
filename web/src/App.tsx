import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import { api, setCSRFToken } from "./api";
import { Login } from "./components/Login";
import { Shell, type Section } from "./components/Shell";
import { Loading, Notice } from "./components/UI";
import { ActivityPage } from "./pages/Activity";
import { DomainsPage } from "./pages/Domains";
import { FilesPage } from "./pages/Files";
import { OverviewPage } from "./pages/Overview";
import { ProjectsPage } from "./pages/Projects";
import { SystemPage } from "./pages/System";
import type { AuthStatus, SystemStatus, User } from "./types";

const validSections: Section[] = ["overview", "projects", "domains", "files", "terminal", "activity", "system"];
const TerminalPage = lazy(() => import("./pages/Terminal").then((module) => ({ default: module.TerminalPage })));

export function App() {
  const [auth, setAuth] = useState<AuthStatus | null>(null);
  const [authError, setAuthError] = useState("");
  const [authLoading, setAuthLoading] = useState(true);
  const initial = window.location.hash.slice(1) as Section;
  const [section, setSection] = useState<Section>(validSections.includes(initial) ? initial : "overview");
  const [system, setSystem] = useState<SystemStatus | null>(null);
  const [theme, setThemeState] = useState<"dark" | "light">(() => localStorage.getItem("serverpal-theme") === "light" ? "light" : "dark");
  const [notice, setNotice] = useState<{ type: "success" | "error"; message: string } | null>(null);

  const navigate = useCallback((next: Section) => {
    window.history.replaceState(null, "", `#${next}`);
    setSection(next);
  }, []);

  const notify = useCallback((type: "success" | "error", message: string) => {
    setNotice({ type, message });
    window.setTimeout(() => setNotice(null), 4200);
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("serverpal-theme", theme);
  }, [theme]);

  useEffect(() => {
    const syncHash = () => {
      const next = window.location.hash.slice(1) as Section;
      if (validSections.includes(next)) setSection(next);
    };
    window.addEventListener("hashchange", syncHash);
    return () => window.removeEventListener("hashchange", syncHash);
  }, []);

  useEffect(() => {
    api.authStatus().then((status) => {
      setAuth(status);
      setCSRFToken(status.csrfToken);
    }).catch((error: Error) => setAuthError(`无法连接 ServerPal：${error.message}`)).finally(() => setAuthLoading(false));
  }, []);

  useEffect(() => {
    if (!auth?.authenticated) return;
    api.system().then(setSystem).catch(() => setSystem(null));
  }, [auth?.authenticated]);

  const login = async (username: string, password: string) => {
    setAuthLoading(true);
    setAuthError("");
    try {
      const status = await api.login(username, password);
      setCSRFToken(status.csrfToken);
      setAuth(status);
    } catch (error) {
      setAuthError(error instanceof Error ? error.message : "登录失败");
    } finally {
      setAuthLoading(false);
    }
  };

  const logout = async () => {
    try { await api.logout(); } finally {
      setCSRFToken();
      setAuth({ authenticated: false });
    }
  };

  if (authLoading && !auth) return <div className="boot-screen"><Loading label="正在连接控制面" /></div>;
  if (!auth?.authenticated || !auth.user) return <Login loading={authLoading} error={authError} onLogin={login} />;

  const pages: Record<Section, React.ReactNode> = {
    overview: <OverviewPage navigate={navigate} />,
    projects: <ProjectsPage notify={notify} />,
    domains: <DomainsPage notify={notify} />,
    files: <FilesPage notify={notify} />,
    terminal: <Suspense fallback={<Loading label="正在加载终端" />}><TerminalPage notify={notify} enabled={system?.terminal ?? true} /></Suspense>,
    activity: <ActivityPage />,
    system: <SystemPage status={system} reload={() => api.system().then(setSystem).catch((error: Error) => notify("error", error.message))} />,
  };

  return (
    <>
      <Shell section={section} setSection={navigate} user={auth.user as User} system={system} theme={theme} setTheme={setThemeState} onLogout={logout}>
        {pages[section]}
      </Shell>
      <Notice notice={notice} onClose={() => setNotice(null)} />
    </>
  );
}
