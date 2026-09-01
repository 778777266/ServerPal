import { useEffect, useRef, useState } from "react";
import { Eraser, PlugZap, RefreshCw, SquareTerminal } from "lucide-react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { api, demoMode } from "../api";
import type { Project } from "../types";
import { EmptyState, IconButton, Loading, PageHeader } from "../components/UI";

export function TerminalPage({ notify, enabled }: { notify: (type: "success" | "error", message: string) => void; enabled: boolean }) {
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [projectID, setProjectID] = useState("");
  const [session, setSession] = useState(0);
  useEffect(() => { api.projects().then((items) => { setProjects(items); if (items[0]) setProjectID(items[0].id); }).catch((reason: Error) => notify("error", reason.message)); }, [notify]);
  if (!projects) return <Loading label="正在准备项目终端" />;
  if (!enabled) return <div className="page-stack"><PageHeader title="终端" subtitle="项目用户交互式控制台" /><EmptyState title="终端已禁用" description="在 ServerPal 配置中启用后才能建立会话" /></div>;
  if (!projects.length) return <div className="page-stack"><PageHeader title="终端" subtitle="项目用户交互式控制台" /><EmptyState title="没有可用项目" description="添加项目后即可打开受限终端" /></div>;
  return <TerminalWorkspace key={session} projects={projects} projectID={projectID} setProjectID={setProjectID} notify={notify} reconnect={() => setSession((value) => value + 1)} />;
}

function TerminalWorkspace({ projects, projectID, setProjectID, notify, reconnect }: { projects: Project[]; projectID: string; setProjectID: (id: string) => void; notify: (type: "success" | "error", message: string) => void; reconnect: () => void }) {
  const container = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const [connection, setConnection] = useState<"connecting" | "online" | "closed">("connecting");
  const project = projects.find((item) => item.id === projectID) ?? projects[0];

  useEffect(() => {
    if (!container.current || !project) return;
    const terminal = new Terminal({
      cursorBlink: true,
      cursorStyle: "bar",
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      lineHeight: 1.45,
      scrollback: 5000,
      allowProposedApi: false,
      theme: { background: "#07090b", foreground: "#d7dde4", cursor: "#31c56d", selectionBackground: "#315f4555", black: "#11151a", red: "#f05b63", green: "#31c56d", yellow: "#d6a33b", blue: "#5c9ef6", magenta: "#b77ee8", cyan: "#4fbfcb", white: "#d7dde4", brightBlack: "#64707c", brightWhite: "#ffffff" },
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(container.current);
    terminalRef.current = terminal;
    fitRef.current = fit;
    window.setTimeout(() => fit.fit(), 0);
    const resizeObserver = new ResizeObserver(() => {
      fit.fit();
      const socket = socketRef.current;
      if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "resize", cols: terminal.cols, rows: terminal.rows }));
    });
    resizeObserver.observe(container.current);

    if (demoMode) {
      setConnection("online");
      terminal.writeln("\x1b[38;2;49;197;109mServerPal project shell\x1b[0m");
      terminal.writeln(`cwd: ${project.rootPath}\r\n`);
      terminal.write("\x1b[38;2;92;158;246mserverpal-app\x1b[0m:\x1b[38;2;49;197;109m~/" + project.name.toLowerCase().replace(/\s+/g, "-") + "\x1b[0m$ ");
      const subscription = terminal.onData((data) => {
        if (data === "\r") terminal.write("\r\n\x1b[38;2;92;158;246mserverpal-app\x1b[0m:\x1b[38;2;49;197;109m~/app\x1b[0m$ ");
        else terminal.write(data);
      });
      return () => { subscription.dispose(); resizeObserver.disconnect(); terminal.dispose(); };
    }

    setConnection("connecting");
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(`${protocol}//${window.location.host}/api/terminal?projectId=${encodeURIComponent(project.id)}`);
    socket.binaryType = "arraybuffer";
    socketRef.current = socket;
    socket.onopen = () => {
      setConnection("online");
      socket.send(JSON.stringify({ type: "resize", cols: terminal.cols, rows: terminal.rows }));
    };
    socket.onmessage = (event) => terminal.write(event.data instanceof ArrayBuffer ? new Uint8Array(event.data) : String(event.data));
    socket.onerror = () => notify("error", "终端连接失败");
    socket.onclose = () => { setConnection("closed"); terminal.writeln("\r\n\x1b[38;2;240;91;99m[会话已关闭]\x1b[0m"); };
    const subscription = terminal.onData((data) => {
      if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "input", data }));
    });
    return () => {
      subscription.dispose(); resizeObserver.disconnect(); socket.close(); socketRef.current = null; terminal.dispose(); terminalRef.current = null;
    };
  }, [project, notify]);

  return <div className="page-stack terminal-page"><PageHeader title="终端" subtitle="以 serverpal-app 身份运行，不提供 root shell" actions={<><IconButton label="清屏" onClick={() => terminalRef.current?.clear()}><Eraser size={18} /></IconButton><IconButton label="重新连接" onClick={reconnect}><RefreshCw size={18} /></IconButton></>} />
    <section className="terminal-toolbar"><label><SquareTerminal size={16} /><select value={projectID} onChange={(event) => setProjectID(event.target.value)}>{projects.map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}</select></label><code>{project?.rootPath}</code><span className={`terminal-state ${connection}`}><PlugZap size={15} />{connection === "online" ? "已连接" : connection === "connecting" ? "连接中" : "已断开"}</span></section>
    <section className="terminal-frame"><div className="terminal-chrome"><span /><span /><span /><strong>{project?.name} · bash</strong></div><div ref={container} className="terminal-container" /></section>
  </div>;
}
