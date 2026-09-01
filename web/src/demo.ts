import type { AuditLog, Domain, FileList, Overview, Project, SystemStatus } from "./types";

const now = new Date();

export const demoProjects: Project[] = [
  {
    id: "demo-api",
    name: "Tarot API",
    rootPath: "/srv/serverpal/apps/tarot-api/current",
    runtime: "npm",
    entry: "",
    npmScript: "start",
    nodeBinary: "/opt/serverpal/node/v24.7.0/bin/node",
    port: 3100,
    instances: 2,
    pm2Name: "tarot-api",
    env: { NODE_ENV: "production" },
    status: "online",
    pid: 18432,
    cpu: 2.8,
    memory: 128_849_018,
    createdAt: new Date(now.getTime() - 14 * 86400000).toISOString(),
    updatedAt: now.toISOString(),
  },
  {
    id: "demo-worker",
    name: "Event Worker",
    rootPath: "/srv/serverpal/apps/event-worker/current",
    runtime: "node",
    entry: "dist/worker.js",
    npmScript: "",
    nodeBinary: "/opt/serverpal/node/v22.19.0/bin/node",
    port: 3200,
    instances: 1,
    pm2Name: "event-worker",
    env: { NODE_ENV: "production" },
    status: "stopped",
    pid: 0,
    cpu: 0,
    memory: 0,
    createdAt: new Date(now.getTime() - 8 * 86400000).toISOString(),
    updatedAt: new Date(now.getTime() - 3600000).toISOString(),
  },
  {
    id: "demo-admin",
    name: "Admin Console",
    rootPath: "/srv/serverpal/apps/admin-console/current",
    runtime: "npm",
    entry: "",
    npmScript: "preview",
    nodeBinary: "/opt/serverpal/node/v24.7.0/bin/node",
    port: 3300,
    instances: 1,
    pm2Name: "admin-console",
    env: { NODE_ENV: "production" },
    status: "online",
    pid: 19607,
    cpu: 1.1,
    memory: 92_274_688,
    createdAt: new Date(now.getTime() - 4 * 86400000).toISOString(),
    updatedAt: now.toISOString(),
  },
];

export const demoDomains: Domain[] = [
  {
    id: "domain-api",
    projectId: "demo-api",
    projectName: "Tarot API",
    hostname: "api.example.com",
    https: true,
    status: "active",
    expiresAt: new Date(now.getTime() + 61 * 86400000).toISOString(),
    createdAt: new Date(now.getTime() - 12 * 86400000).toISOString(),
    updatedAt: now.toISOString(),
  },
  {
    id: "domain-admin",
    projectId: "demo-admin",
    projectName: "Admin Console",
    hostname: "console.example.com",
    https: true,
    status: "active",
    expiresAt: new Date(now.getTime() + 73 * 86400000).toISOString(),
    createdAt: new Date(now.getTime() - 4 * 86400000).toISOString(),
    updatedAt: now.toISOString(),
  },
];

export const demoAudit: AuditLog[] = [
  { id: 4, username: "admin", action: "project.restart", target: "Tarot API", detail: "PM2", ip: "10.0.0.12", createdAt: new Date(now.getTime() - 8 * 60000).toISOString() },
  { id: 3, username: "admin", action: "domain.bind", target: "console.example.com", detail: "Admin Console", ip: "10.0.0.12", createdAt: new Date(now.getTime() - 2 * 3600000).toISOString() },
  { id: 2, username: "admin", action: "file.content", target: "ecosystem.config.cjs", detail: "", ip: "10.0.0.12", createdAt: new Date(now.getTime() - 5 * 3600000).toISOString() },
  { id: 1, username: "admin", action: "auth.login", target: "session", detail: "", ip: "10.0.0.12", createdAt: new Date(now.getTime() - 8 * 3600000).toISOString() },
];

export function demoOverview(): Overview {
  return {
    metrics: {
      cpuPercent: 12.8,
      cpuCores: 4,
      load1: 0.42,
      load5: 0.36,
      load15: 0.29,
      memoryUsed: 2_845_716_480,
      memoryTotal: 8_589_934_592,
      memoryPercent: 33.1,
      diskUsed: 51_324_239_872,
      diskTotal: 214_748_364_800,
      diskPercent: 23.9,
      uptimeSeconds: 1_209_840,
    },
    summary: { projects: demoProjects.length, activeProjects: demoProjects.filter((p) => p.status === "online").length, domains: demoDomains.length },
    processes: demoProjects.filter((p) => p.status === "online").map((p, index) => ({
      name: p.pm2Name,
      pm2Id: index,
      pid: p.pid ?? 0,
      status: p.status ?? "stopped",
      cpu: p.cpu ?? 0,
      memory: p.memory ?? 0,
      restarts: index,
      uptimeUnix: Math.floor((now.getTime() - (index + 2) * 86400000) / 1000),
    })),
    recent: demoAudit.slice(0, 4),
    warning: "",
  };
}

export const demoFiles: Record<string, FileList> = {
  "": {
    root: demoProjects[0].rootPath,
    path: "",
    entries: [
      { name: "dist", path: "dist", type: "directory", size: 4096, mode: "drwxr-x---", owner: "serverpal-app:serverpal-app", modifiedAt: now.toISOString() },
      { name: "node_modules", path: "node_modules", type: "directory", size: 4096, mode: "drwxr-x---", owner: "serverpal-app:serverpal-app", modifiedAt: new Date(now.getTime() - 86400000).toISOString() },
      { name: ".env", path: ".env", type: "file", size: 184, mode: "-rw-r-----", owner: "serverpal-app:serverpal-app", modifiedAt: now.toISOString() },
      { name: "package.json", path: "package.json", type: "file", size: 1228, mode: "-rw-r-----", owner: "serverpal-app:serverpal-app", modifiedAt: now.toISOString() },
      { name: "package-lock.json", path: "package-lock.json", type: "file", size: 148224, mode: "-rw-r-----", owner: "serverpal-app:serverpal-app", modifiedAt: new Date(now.getTime() - 86400000).toISOString() },
    ],
  },
  dist: {
    root: demoProjects[0].rootPath,
    path: "dist",
    entries: [
      { name: "index.js", path: "dist/index.js", type: "file", size: 84612, mode: "-rw-r-----", owner: "serverpal-app:serverpal-app", modifiedAt: now.toISOString() },
      { name: "index.js.map", path: "dist/index.js.map", type: "file", size: 166902, mode: "-rw-r-----", owner: "serverpal-app:serverpal-app", modifiedAt: now.toISOString() },
    ],
  },
};

export const demoSystem: SystemStatus = {
  agentVersion: "0.1.0",
  os: "linux",
  architecture: "amd64",
  node: "v24.7.0",
  pm2: "6.0.8",
  caddy: "v2.10.2",
  pm2Available: true,
  caddyReady: true,
  terminal: true,
};
