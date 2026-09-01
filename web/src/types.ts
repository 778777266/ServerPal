export type User = {
  id: string;
  username: string;
};

export type AuthStatus = {
  authenticated: boolean;
  user?: User;
  csrfToken?: string;
};

export type Metrics = {
  cpuPercent: number;
  cpuCores: number;
  load1: number;
  load5: number;
  load15: number;
  memoryUsed: number;
  memoryTotal: number;
  memoryPercent: number;
  diskUsed: number;
  diskTotal: number;
  diskPercent: number;
  uptimeSeconds: number;
};

export type Process = {
  name: string;
  pm2Id: number;
  pid: number;
  status: string;
  cpu: number;
  memory: number;
  restarts: number;
  uptimeUnix: number;
};

export type Project = {
  id: string;
  name: string;
  rootPath: string;
  runtime: "node" | "npm";
  entry: string;
  npmScript: string;
  nodeBinary: string;
  port: number;
  instances: number;
  pm2Name: string;
  env: Record<string, string>;
  status?: string;
  pid?: number;
  cpu?: number;
  memory?: number;
  createdAt: string;
  updatedAt: string;
};

export type ProjectInput = Omit<Project, "id" | "status" | "pid" | "cpu" | "memory" | "createdAt" | "updatedAt">;

export type Domain = {
  id: string;
  projectId: string;
  projectName: string;
  hostname: string;
  https: boolean;
  status: "pending" | "active" | "error" | "disabled";
  expiresAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type AuditLog = {
  id: number;
  username: string;
  action: string;
  target: string;
  detail: string;
  ip: string;
  createdAt: string;
};

export type Overview = {
  metrics: Metrics;
  summary: { projects: number; activeProjects: number; domains: number };
  processes: Process[];
  recent: AuditLog[];
  warning: string;
};

export type FileEntry = {
  name: string;
  path: string;
  type: "file" | "directory" | "symlink";
  size: number;
  mode: string;
  owner: string;
  modifiedAt: string;
};

export type FileList = {
  root: string;
  path: string;
  entries: FileEntry[];
};

export type SystemStatus = {
  agentVersion: string;
  os: string;
  architecture: string;
  node: string;
  pm2: string;
  caddy: string;
  pm2Available: boolean;
  caddyReady: boolean;
  terminal: boolean;
};
