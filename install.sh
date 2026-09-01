#!/usr/bin/env bash
set -Eeuo pipefail

REPOSITORY="${SERVERPAL_REPO:-778777266/ServerPal}"
RELEASE_BASE="${SERVERPAL_RELEASE_BASE:-https://github.com/${REPOSITORY}/releases/latest/download}"
PANEL_DOMAIN="${SERVERPAL_PANEL_DOMAIN:-}"
NODE_VERSION="${SERVERPAL_NODE_VERSION:-v24.20.0}"
PM2_VERSION="${SERVERPAL_PM2_VERSION:-6.0.14}"
INSTALL_ROOT="/opt/serverpal"
CONFIG_ROOT="/etc/serverpal"
DATA_ROOT="/var/lib/serverpal"
PROJECT_ROOT="/srv/serverpal/apps"
MAINTENANCE_LOCK="/run/serverpal-maintenance.lock"
REPAIR_INSTALL=0

log() { printf '\033[1;32m[ServerPal]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[ServerPal]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[ServerPal] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }
atomic_symlink() {
  local target="$1" link="$2" temporary="${2}.new.$$"
  ln -sfn "${target}" "${temporary}"
  mv -Tf "${temporary}" "${link}"
}

[[ "${EUID}" -eq 0 ]] || fail "请使用 sudo 运行安装脚本"
command -v flock >/dev/null 2>&1 || fail "系统缺少 flock"
exec 9>"${MAINTENANCE_LOCK}"
flock -n 9 || fail "另一个 ServerPal 安装或更新正在进行"

if [[ -x /usr/local/sbin/serverpal-update && -x "${INSTALL_ROOT}/current/serverpal" ]]; then
  if [[ "${SERVERPAL_REINSTALL:-0}" != "1" ]] && command -v curl >/dev/null 2>&1 && \
     curl -fsS --max-time 4 http://127.0.0.1:8787/api/health 2>/dev/null | grep -q '"agent":"ok"'; then
    log "检测到健康的已有安装，转入安全更新流程"
    export SERVERPAL_LOCK_HELD=1
    exec /usr/local/sbin/serverpal-update
  fi
  warn "检测到未完成或不健康的安装，将执行原地修复"
  REPAIR_INSTALL=1
fi

[[ -r /etc/os-release ]] || fail "无法识别操作系统"
# shellcheck disable=SC1091
source /etc/os-release
[[ "${ID:-}" == "ubuntu" ]] || fail "目前只支持 Ubuntu"
case "${VERSION_ID:-}" in
  22.04|24.04|26.04) ;;
  *) fail "仅支持 Ubuntu 22.04、24.04 和 26.04 LTS" ;;
esac
command -v systemctl >/dev/null 2>&1 || fail "系统必须使用 systemd"

case "$(uname -m)" in
  x86_64) ARCH="amd64"; NODE_ARCH="x64" ;;
  aarch64|arm64) ARCH="arm64"; NODE_ARCH="arm64" ;;
  *) fail "不支持的架构：$(uname -m)" ;;
esac

WORK_DIR="$(mktemp -d /tmp/serverpal-install.XXXXXX)"
NODE_STAGE=""
PM2_STAGE=""
cleanup() {
  rm -rf "${WORK_DIR}"
  [[ -z "${NODE_STAGE}" ]] || rm -rf "${NODE_STAGE}"
  [[ -z "${PM2_STAGE}" ]] || rm -rf "${PM2_STAGE}"
}
trap cleanup EXIT
export DEBIAN_FRONTEND=noninteractive
umask 0027

log "安装基础依赖"
apt-get update -qq
apt-get install -y -qq ca-certificates curl gnupg debian-keyring debian-archive-keyring apt-transport-https sqlite3 xz-utils tar

if ! dpkg-query -W -f='${Status}' caddy 2>/dev/null | grep -q 'install ok installed'; then
  log "从 Caddy 官方仓库安装反向代理"
  curl --proto '=https' --tlsv1.2 -1fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key -o "${WORK_DIR}/caddy.gpg.key"
  gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg "${WORK_DIR}/caddy.gpg.key"
  curl --proto '=https' --tlsv1.2 -1fsSL https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt -o /etc/apt/sources.list.d/caddy-stable.list
  chmod 0644 /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -qq
  apt-get install -y -qq caddy
fi

NODE_DIR="${INSTALL_ROOT}/node/${NODE_VERSION}"
install -d -o root -g root -m 0755 "${INSTALL_ROOT}" "${INSTALL_ROOT}/node"
if [[ ! -x "${NODE_DIR}/bin/node" ]]; then
  log "安装 Node.js ${NODE_VERSION} LTS"
  NODE_ASSET="node-${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz"
  NODE_BASE="https://nodejs.org/dist/${NODE_VERSION}"
  curl --proto '=https' --tlsv1.2 -fsSLo "${WORK_DIR}/${NODE_ASSET}" "${NODE_BASE}/${NODE_ASSET}"
  curl --proto '=https' --tlsv1.2 -fsSLo "${WORK_DIR}/SHASUMS256.txt" "${NODE_BASE}/SHASUMS256.txt"
  NODE_CHECKSUM="$(awk -v file="${NODE_ASSET}" '$2 == file { print; exit }' "${WORK_DIR}/SHASUMS256.txt")"
  [[ -n "${NODE_CHECKSUM}" ]] || fail "Node.js 校验信息缺失"
  (cd "${WORK_DIR}" && printf '%s\n' "${NODE_CHECKSUM}" | sha256sum -c -)
  NODE_STAGE="${NODE_DIR}.staging.$$"
  rm -rf "${NODE_STAGE}"
  mkdir -p "${NODE_STAGE}"
  tar -xJf "${WORK_DIR}/${NODE_ASSET}" --strip-components=1 -C "${NODE_STAGE}"
  [[ "$("${NODE_STAGE}/bin/node" --version)" == "${NODE_VERSION}" ]] || fail "Node.js 安装校验失败"
  rm -rf "${NODE_DIR}"
  mv "${NODE_STAGE}" "${NODE_DIR}"
  NODE_STAGE=""
fi
atomic_symlink "${NODE_DIR}" "${INSTALL_ROOT}/node/current"

PM2_DIR="${INSTALL_ROOT}/pm2-runtime"
INSTALLED_PM2=""
if [[ -f "${PM2_DIR}/lib/node_modules/pm2/package.json" ]]; then
  INSTALLED_PM2="$("${NODE_DIR}/bin/node" -p "require('${PM2_DIR}/lib/node_modules/pm2/package.json').version" 2>/dev/null || true)"
fi
if [[ "${INSTALLED_PM2}" != "${PM2_VERSION}" ]]; then
  log "安装 PM2 ${PM2_VERSION}"
  PM2_STAGE="${PM2_DIR}.staging.$$"
  rm -rf "${PM2_STAGE}"
  env PATH="${NODE_DIR}/bin:/usr/local/bin:/usr/bin:/bin" "${NODE_DIR}/bin/npm" install --global --prefix "${PM2_STAGE}" "pm2@${PM2_VERSION}"
  [[ "$("${NODE_DIR}/bin/node" -p "require('${PM2_STAGE}/lib/node_modules/pm2/package.json').version")" == "${PM2_VERSION}" ]] || fail "PM2 安装校验失败"
  rm -rf "${PM2_DIR}"
  mv "${PM2_STAGE}" "${PM2_DIR}"
  PM2_STAGE=""
fi
chown -R root:root "${NODE_DIR}" "${PM2_DIR}"
chmod 0755 "${INSTALL_ROOT}/node"
chmod -R u=rwX,go=rX "${NODE_DIR}" "${PM2_DIR}"

ASSET="serverpal-linux-${ARCH}.tar.gz"
if [[ -n "${SERVERPAL_ARCHIVE:-}" ]]; then
  [[ -f "${SERVERPAL_ARCHIVE}" ]] || fail "SERVERPAL_ARCHIVE 文件不存在"
  cp "${SERVERPAL_ARCHIVE}" "${WORK_DIR}/${ASSET}"
  warn "使用本地安装包；请自行确认其来源"
else
  log "下载 ServerPal 发布包"
  curl --proto '=https' --tlsv1.2 -fsSLo "${WORK_DIR}/${ASSET}" "${RELEASE_BASE}/${ASSET}"
  curl --proto '=https' --tlsv1.2 -fsSLo "${WORK_DIR}/checksums.txt" "${RELEASE_BASE}/checksums.txt"
  RELEASE_CHECKSUM="$(awk -v file="${ASSET}" '$2 == file { print; exit }' "${WORK_DIR}/checksums.txt")"
  [[ -n "${RELEASE_CHECKSUM}" ]] || fail "发布包校验信息缺失"
  (cd "${WORK_DIR}" && printf '%s\n' "${RELEASE_CHECKSUM}" | sha256sum -c -)
fi
mkdir -p "${WORK_DIR}/release"
tar -xzf "${WORK_DIR}/${ASSET}" -C "${WORK_DIR}/release"
[[ -x "${WORK_DIR}/release/serverpal" ]] || fail "发布包中缺少 ServerPal 可执行文件"
VERSION="$("${WORK_DIR}/release/serverpal" version | awk 'NR==1 {print $2}')"
[[ "${VERSION}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || fail "ServerPal 版本号无效"

log "创建隔离的系统用户和目录"
getent group serverpal >/dev/null || groupadd --system serverpal
id serverpal >/dev/null 2>&1 || useradd --system --gid serverpal --home-dir "${DATA_ROOT}" --shell /usr/sbin/nologin serverpal
getent group serverpal-app >/dev/null || groupadd --system serverpal-app
id serverpal-app >/dev/null 2>&1 || useradd --system --gid serverpal-app --home-dir "${DATA_ROOT}/app-home" --shell /usr/sbin/nologin serverpal-app
APP_GROUP_REMOVED=0
if id -nG serverpal-app | tr ' ' '\n' | grep -Fxq serverpal; then
  gpasswd -d serverpal-app serverpal >/dev/null
  APP_GROUP_REMOVED=1
fi
install -d -o root -g serverpal -m 0750 "${CONFIG_ROOT}"
install -d -o serverpal -g serverpal -m 0751 "${DATA_ROOT}"
install -d -o serverpal -g serverpal -m 0750 "${DATA_ROOT}/backups"
install -d -o serverpal-app -g serverpal-app -m 0750 "${DATA_ROOT}/app-home" "${DATA_ROOT}/pm2"
install -d -o serverpal-app -g serverpal-app -m 0750 "${PROJECT_ROOT}"
install -d -o root -g root -m 0755 "${INSTALL_ROOT}" "${INSTALL_ROOT}/releases" "${INSTALL_ROOT}/releases/${VERSION}"
install -m 0755 "${WORK_DIR}/release/serverpal" "${INSTALL_ROOT}/releases/${VERSION}/serverpal.new"
mv -f "${INSTALL_ROOT}/releases/${VERSION}/serverpal.new" "${INSTALL_ROOT}/releases/${VERSION}/serverpal"
atomic_symlink "${INSTALL_ROOT}/releases/${VERSION}" "${INSTALL_ROOT}/current"
install -m 0755 "${WORK_DIR}/release/serverpal-update" /usr/local/sbin/serverpal-update

if [[ ! -f "${CONFIG_ROOT}/config.json" ]]; then
  install -o root -g serverpal -m 0640 /dev/null "${CONFIG_ROOT}/config.json"
  cat >"${CONFIG_ROOT}/config.json" <<EOF
{
  "listen": "127.0.0.1:8787",
  "dataDir": "${DATA_ROOT}",
  "agentSocket": "/run/serverpal/agent.sock",
  "agentGroup": "serverpal",
  "agentTokenFile": "${CONFIG_ROOT}/agent.token",
  "allowedRoots": ["${PROJECT_ROOT}"],
  "projectUser": "serverpal-app",
  "projectHome": "${DATA_ROOT}/app-home",
  "pm2Binary": "${INSTALL_ROOT}/pm2-runtime/bin/pm2",
  "pm2Home": "${DATA_ROOT}/pm2",
  "nodeBinary": "${INSTALL_ROOT}/node/current/bin/node",
  "caddyBinary": "/usr/bin/caddy",
  "caddyConfig": "/etc/caddy/Caddyfile",
  "caddySitesDir": "/etc/caddy/sites.d",
  "caddyGroup": "caddy",
  "terminalEnabled": true,
  "releaseBaseUrl": "https://github.com/${REPOSITORY}/releases"
}
EOF
fi

log "初始化数据库和管理员"
INIT_OUTPUT="$("${INSTALL_ROOT}/current/serverpal" init --config "${CONFIG_ROOT}/config.json")"
chown root:serverpal "${CONFIG_ROOT}/config.json" "${CONFIG_ROOT}/agent.token"
chmod 0640 "${CONFIG_ROOT}/config.json" "${CONFIG_ROOT}/agent.token"
find "${DATA_ROOT}" -maxdepth 1 -name 'serverpal.db*' -exec chown serverpal:serverpal {} +
find "${DATA_ROOT}" -maxdepth 1 -name 'serverpal.db*' -exec chmod 0640 {} +
printf '%s\n' "${INIT_OUTPUT}"

log "配置 Caddy"
install -d -o root -g caddy -m 0750 /etc/caddy/sites.d
CADDY_BACKUP=""
PANEL_BACKUP=""
if ! grep -Fq '/etc/caddy/sites.d/*.caddy' /etc/caddy/Caddyfile; then
  CADDY_BACKUP="/etc/caddy/Caddyfile.serverpal-backup.$(date +%s)"
  cp -a /etc/caddy/Caddyfile "${CADDY_BACKUP}"
  printf '\n# ServerPal managed sites\nimport /etc/caddy/sites.d/*.caddy\n' >> /etc/caddy/Caddyfile
fi
if [[ -n "${PANEL_DOMAIN}" ]]; then
  [[ "${PANEL_DOMAIN}" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]{1,251})[A-Za-z0-9]$ && "${PANEL_DOMAIN}" == *.* ]] || fail "SERVERPAL_PANEL_DOMAIN 格式无效"
  if [[ -f /etc/caddy/sites.d/serverpal-panel.caddy ]]; then
    PANEL_BACKUP="${WORK_DIR}/serverpal-panel.caddy"
    cp -a /etc/caddy/sites.d/serverpal-panel.caddy "${PANEL_BACKUP}"
  fi
  cat >/etc/caddy/sites.d/serverpal-panel.caddy <<EOF
# ServerPal control plane
${PANEL_DOMAIN} {
  encode zstd gzip
  reverse_proxy 127.0.0.1:8787
}
EOF
  chown root:caddy /etc/caddy/sites.d/serverpal-panel.caddy
  chmod 0640 /etc/caddy/sites.d/serverpal-panel.caddy
fi
if ! caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null; then
  if [[ -n "${PANEL_BACKUP}" ]]; then
    cp -a "${PANEL_BACKUP}" /etc/caddy/sites.d/serverpal-panel.caddy
  else
    rm -f /etc/caddy/sites.d/serverpal-panel.caddy
  fi
  if [[ -n "${CADDY_BACKUP}" ]]; then cp -a "${CADDY_BACKUP}" /etc/caddy/Caddyfile; fi
  fail "Caddy 配置校验失败，原配置已恢复"
fi

log "安装并启动 systemd 服务"
install -m 0644 "${WORK_DIR}/release/systemd/serverpal-agent.service" /etc/systemd/system/serverpal-agent.service
install -m 0644 "${WORK_DIR}/release/systemd/serverpal-web.service" /etc/systemd/system/serverpal-web.service
install -m 0644 "${WORK_DIR}/release/systemd/serverpal-pm2.service" /etc/systemd/system/serverpal-pm2.service
systemctl daemon-reload
systemctl enable --now caddy.service
systemctl reload caddy.service
systemctl enable --now serverpal-pm2.service serverpal-agent.service serverpal-web.service
if [[ "${APP_GROUP_REMOVED}" -eq 1 ]]; then
  systemctl restart serverpal-pm2.service serverpal-agent.service serverpal-web.service
elif [[ "${REPAIR_INSTALL}" -eq 1 ]]; then
  systemctl restart serverpal-agent.service serverpal-web.service
fi

if command -v ufw >/dev/null 2>&1 && ufw status | grep -q '^Status: active'; then
  ufw allow 80/tcp >/dev/null
  ufw allow 443/tcp >/dev/null
fi

HEALTHY=0
for _ in $(seq 1 30); do
  HEALTH_RESPONSE="$(curl -fsS --max-time 4 http://127.0.0.1:8787/api/health 2>/dev/null || true)"
  if [[ "${HEALTH_RESPONSE}" == *'"agent":"ok"'* && "${HEALTH_RESPONSE}" == *"\"version\":\"${VERSION}\""* ]]; then
    HEALTHY=1
    break
  fi
  sleep 1
done
[[ "${HEALTHY}" -eq 1 ]] || fail "服务未通过健康检查，请运行 journalctl -u serverpal-agent -u serverpal-web"

printf '\n'
log "安装完成：ServerPal ${VERSION}"
if [[ -n "${PANEL_DOMAIN}" ]]; then
  printf '控制台地址: https://%s\n' "${PANEL_DOMAIN}"
else
  SERVER_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
  printf '面板默认仅监听本机。请从电脑执行: ssh -L 8787:127.0.0.1:8787 root@%s\n' "${SERVER_IP:-SERVER_IP}"
  printf '然后访问: http://127.0.0.1:8787\n'
fi
printf '安全更新: sudo serverpal-update\n'
