import { useState, type FormEvent } from "react";
import { ArrowRight, LockKeyhole, ShieldCheck, UserRound } from "lucide-react";
import { Button } from "./UI";

export function Login({ loading, error, onLogin }: { loading: boolean; error: string; onLogin: (username: string, password: string) => Promise<void> }) {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    await onLogin(username, password);
  };
  return (
    <main className="login-screen">
      <section className="login-panel">
        <div className="login-brand"><span className="brand-mark"><ShieldCheck size={22} /></span><strong>ServerPal</strong></div>
        <div className="login-heading"><span>SERVER CONTROL PANEL</span><h1>登录服务器控制台</h1><p>使用安装时生成的管理员凭据。</p></div>
        <form onSubmit={submit} className="login-form">
          <label><span>用户名</span><div className="input-with-icon"><UserRound size={17} /><input autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} required /></div></label>
          <label><span>密码</span><div className="input-with-icon"><LockKeyhole size={17} /><input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required autoFocus /></div></label>
          {error && <div className="form-error">{error}</div>}
          <Button type="submit" disabled={loading}>{loading ? "正在验证" : "进入控制台"}<ArrowRight size={17} /></Button>
        </form>
        <div className="login-foot"><span className="connection-dot online" />仅通过本机受限代理执行系统操作</div>
      </section>
    </main>
  );
}
