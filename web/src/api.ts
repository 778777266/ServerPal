import { demoAudit, demoDomains, demoFiles, demoOverview, demoProjects, demoSystem } from "./demo";
import type { AuditLog, AuthStatus, Domain, FileList, Overview, Project, ProjectInput, SystemStatus } from "./types";

export const demoMode = import.meta.env.VITE_DEMO === "true";

let csrfToken = "";

type APIRequestInit = Omit<RequestInit, "body"> & { body?: unknown };
type ListResponse<T> = { items: T[] | null };

export function setCSRFToken(value?: string) {
  csrfToken = value ?? "";
}

async function request<T>(path: string, init: APIRequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  let body = init.body;
  if (body && !(body instanceof Blob) && typeof body !== "string") {
    headers.set("Content-Type", "application/json");
    body = JSON.stringify(body);
  }
  const method = (init.method ?? "GET").toUpperCase();
  if (!["GET", "HEAD", "OPTIONS"].includes(method) && csrfToken) {
    headers.set("X-CSRF-Token", csrfToken);
  }
  const response = await fetch(path, { ...init, body: body as BodyInit | null | undefined, headers, credentials: "same-origin" });
  if (!response.ok) {
    let message = `请求失败 (${response.status})`;
    try {
      const payload = (await response.json()) as { error?: string };
      message = payload.error || message;
    } catch {
      // Keep the HTTP status fallback for non-JSON responses.
    }
    throw new Error(message);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

const pause = () => new Promise((resolve) => setTimeout(resolve, 180));
const listItems = <T>(response: ListResponse<T>): T[] => response.items ?? [];

export const api = {
  async authStatus(): Promise<AuthStatus> {
    if (demoMode) return { authenticated: true, user: { id: "demo", username: "admin" }, csrfToken: "demo" };
    return request<AuthStatus>("/api/auth/status");
  },
  async login(username: string, password: string): Promise<AuthStatus> {
    if (demoMode) return { authenticated: true, user: { id: "demo", username }, csrfToken: "demo" };
    return request<AuthStatus>("/api/auth/login", { method: "POST", body: { username, password } });
  },
  async logout() {
    if (demoMode) return;
    await request("/api/auth/logout", { method: "POST" });
  },
  async overview(): Promise<Overview> {
    if (demoMode) { await pause(); return demoOverview(); }
    return request<Overview>("/api/overview");
  },
  async projects(): Promise<Project[]> {
    if (demoMode) { await pause(); return [...demoProjects]; }
    return listItems(await request<ListResponse<Project>>("/api/projects"));
  },
  async createProject(input: ProjectInput): Promise<Project> {
    if (demoMode) {
      const project: Project = { ...input, id: crypto.randomUUID(), status: "stopped", createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() };
      demoProjects.unshift(project);
      return project;
    }
    return request<Project>("/api/projects", { method: "POST", body: input });
  },
  async updateProject(id: string, input: ProjectInput): Promise<Project> {
    if (demoMode) {
      const current = demoProjects.find((project) => project.id === id)!;
      const updated = { ...current, ...input, updatedAt: new Date().toISOString() };
      demoProjects.splice(demoProjects.findIndex((project) => project.id === id), 1, updated);
      return updated;
    }
    return request<Project>(`/api/projects/${encodeURIComponent(id)}`, { method: "PUT", body: input });
  },
  async deleteProject(id: string) {
    if (demoMode) { const index = demoProjects.findIndex((project) => project.id === id); if (index >= 0) demoProjects.splice(index, 1); return; }
    await request(`/api/projects/${encodeURIComponent(id)}`, { method: "DELETE" });
  },
  async projectAction(id: string, action: string) {
    if (demoMode) {
      const index = demoProjects.findIndex((project) => project.id === id);
      if (index >= 0) demoProjects[index] = { ...demoProjects[index], status: action === "stop" || action === "delete" ? "stopped" : "online", pid: action === "stop" ? 0 : 21000 };
      await pause();
      return;
    }
    await request(`/api/projects/${encodeURIComponent(id)}/action`, { method: "POST", body: { action } });
  },
  async logs(id: string): Promise<string> {
    if (demoMode) return `[ServerPal] PM2 process online\n[app] listening on 127.0.0.1:3100\n[app] health check ready\n[http] GET /healthz 200 3ms`;
    return (await request<{ logs: string }>(`/api/projects/${encodeURIComponent(id)}/logs`)).logs;
  },
  async domains(): Promise<Domain[]> {
    if (demoMode) { await pause(); return [...demoDomains]; }
    return listItems(await request<ListResponse<Domain>>("/api/domains"));
  },
  async createDomain(projectId: string, hostname: string, https: boolean): Promise<Domain> {
    if (demoMode) {
      const domain: Domain = { id: crypto.randomUUID(), projectId, projectName: demoProjects.find((p) => p.id === projectId)?.name ?? "Project", hostname, https, status: "pending", createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() };
      demoDomains.unshift(domain);
      return domain;
    }
    return request<Domain>("/api/domains", { method: "POST", body: { projectId, hostname, https } });
  },
  async deleteDomain(id: string) {
    if (demoMode) { const index = demoDomains.findIndex((domain) => domain.id === id); if (index >= 0) demoDomains.splice(index, 1); return; }
    await request(`/api/domains/${encodeURIComponent(id)}`, { method: "DELETE" });
  },
  async checkDomain(id: string): Promise<{ status: Domain["status"]; expiresAt?: string; issuer?: string; error?: string }> {
    if (demoMode) {
      const expiresAt = new Date(Date.now() + 80 * 86400000).toISOString();
      const index = demoDomains.findIndex((domain) => domain.id === id);
      if (index >= 0) demoDomains[index] = { ...demoDomains[index], status: "active", expiresAt };
      await pause();
      return { status: "active", expiresAt, issuer: "Let's Encrypt" };
    }
    return request(`/api/domains/${encodeURIComponent(id)}/check`, { method: "POST" });
  },
  async applyDomain(id: string) {
    if (demoMode) { await pause(); return; }
    await request(`/api/domains/${encodeURIComponent(id)}/apply`, { method: "POST" });
  },
  async files(root: string, path: string): Promise<FileList> {
    if (demoMode) return demoFiles[path] ?? { root, path, entries: [] };
    return request<FileList>(`/api/files?${new URLSearchParams({ root, path })}`);
  },
  async fileContent(root: string, path: string): Promise<string> {
    if (demoMode) return path === ".env" ? "NODE_ENV=production\nPORT=3100\n" : "{\n  \"name\": \"serverpal-project\",\n  \"private\": true\n}\n";
    return (await request<{ content: string }>(`/api/files/content?${new URLSearchParams({ root, path })}`)).content;
  },
  async saveFile(root: string, path: string, content: string) {
    if (demoMode) { await pause(); return; }
    await request(`/api/files/content?${new URLSearchParams({ root, path })}`, { method: "PUT", body: content, headers: { "Content-Type": "text/plain; charset=utf-8" } });
  },
  async mkdir(root: string, path: string) {
    if (demoMode) return;
    await request("/api/files/mkdir", { method: "POST", body: { root, path } });
  },
  async rename(root: string, from: string, to: string) {
    if (demoMode) return;
    await request("/api/files/rename", { method: "POST", body: { root, from, to } });
  },
  async trash(root: string, path: string) {
    if (demoMode) return;
    await request("/api/files/trash", { method: "POST", body: { root, path } });
  },
  async upload(root: string, path: string, file: File) {
    if (demoMode) { await pause(); return; }
    await request(`/api/files/upload?${new URLSearchParams({ root, path })}`, { method: "POST", body: file, headers: { "Content-Type": "application/octet-stream" } });
  },
  async download(root: string, path: string) {
    if (demoMode) return;
    const headers = new Headers();
    if (csrfToken) headers.set("X-CSRF-Token", csrfToken);
    const response = await fetch(`/api/files/download?${new URLSearchParams({ root, path })}`, { headers, credentials: "same-origin" });
    if (!response.ok) throw new Error("下载失败");
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = path.split("/").pop() ?? "download";
    anchor.click();
    URL.revokeObjectURL(url);
  },
  async audit(): Promise<AuditLog[]> {
    if (demoMode) return demoAudit;
    return listItems(await request<ListResponse<AuditLog>>("/api/audit"));
  },
  async system(): Promise<SystemStatus> {
    if (demoMode) return demoSystem;
    return request<SystemStatus>("/api/system");
  },
};
