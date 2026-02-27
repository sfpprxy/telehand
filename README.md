# Telehand 使用说明

Telehand 用于远程协助：接收协助端与发起协助端完成配对后可快速建立会话，随后即可进行命令执行与文件操作。
让你的 AI Agent 阅读并使用 [SKILL.md](SKILL.md)。

## 目录

- [接收协助端](#receiver)
  - [一行命令安装并启动（推荐）](#receiver-quickstart)
  - [安装指定版本（例如 alpha）](#receiver-versioned-install)
  - [手动下载并启动（GUI）](#receiver-manual-gui)
  - [结束远程协助](#receiver-stop)
  - [卸载](#receiver-uninstall)
- [发起协助端](#initiator)
  - [一行命令安装并启动（推荐）](#initiator-quickstart)
  - [标准接入流程（推荐）](#initiator-standard-flow)
  - [自动带码与 GUI 粘贴说明](#initiator-auto-and-gui)
  - [连通性与状态检查](#initiator-health)
  - [常见 error_code](#initiator-error-codes)
- [参考](#references)
  - [AI Agent 协议](#references-agent-protocol)

<a id="receiver"></a>
## 接收协助端

<a id="receiver-quickstart"></a>
### 一行命令安装并启动（推荐）

Windows（PowerShell 管理员）：

```powershell
iwr -useb https://ghfast.top/https://raw.githubusercontent.com/sfpprxy/telehand/main/install.ps1 | iex; .\telehand.exe
```

macOS / Linux：

```bash
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/sfpprxy/telehand/main/install.sh | bash && sudo ./telehand
```

<a id="receiver-versioned-install"></a>
### 安装指定版本（例如 alpha）

<details>
<summary>点击展开安装命令</summary>

版本参数必须带 `v` 前缀（例如 `v0.2.0-alpha.3`）。

Windows（PowerShell，脚本文件方式）：

```powershell
iwr -useb https://ghfast.top/https://raw.githubusercontent.com/sfpprxy/telehand/main/install.ps1 -OutFile install.ps1; .\install.ps1 -Version v0.2.0-alpha.3; .\telehand.exe
```

macOS / Linux：

```bash
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/sfpprxy/telehand/main/install.sh | bash -s -- --version v0.2.0-alpha.3 && sudo ./telehand
```

</details>

<a id="receiver-manual-gui"></a>
### 手动下载并启动（GUI）

<details>
<summary>点击展开手动下载与 GUI 启动步骤</summary>

通用步骤：

1. 打开发布页：`https://github.com/sfpprxy/telehand/releases`
2. 下载对应平台压缩包并解压。
3. 运行 Telehand（无参数默认进入 `serve` 模式）。
4. 如果浏览器未自动打开，手动访问 `http://127.0.0.1:18080`。
5. 在页面粘贴发起协助端发来的配置码，点击“启动远程协助”。
6. 等页面进入 `running` 即可；无需再手动发回会话地址（`虚拟IP:API端口`），发起协助端可在自己的 Web GUI 直接查看并复制。

平台差异说明：

- Windows：下载 `telehand-windows-amd64-vX.Y.Z.zip`，双击 `telehand.exe` 运行。
- macOS：下载 `telehand-darwin-arm64-vX.Y.Z.zip`，若提示安全限制，在“系统设置 -> 隐私与安全性”允许打开，或右键选择“打开”。

</details>

<a id="receiver-stop"></a>
### 结束远程协助

- 点击页面“断开连接”，或终端 `Ctrl+C`。

<a id="receiver-uninstall"></a>
### 卸载

删除可执行文件即卸载。

<a id="initiator"></a>
## 发起协助端

<a id="initiator-quickstart"></a>
### 一行命令安装并启动（推荐）

Windows（PowerShell 管理员）：

```powershell
iwr -useb https://ghfast.top/https://raw.githubusercontent.com/sfpprxy/telehand/main/install.ps1 | iex; .\telehand.exe connect
```

macOS / Linux：

```bash
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/sfpprxy/telehand/main/install.sh | bash && sudo ./telehand connect
```

启动后参照标准接入流程使用

<a id="initiator-standard-flow"></a>
### 标准接入流程

1. 若未使用一行命令安装并启动，则手动下载二进制，在发起协助端运行：

```bash
# macOS / Linux
sudo telehand connect
# Windows PowerShell(Admin)
.\telehand.exe connect
```

2. 程序会输出并自动复制“接收协助端安装+启动命令”（Windows / macOS / Linux）。
3. 把对应命令发给接收协助端执行。
4. 接收协助端进入 `running` 后，发起协助端可在 Web GUI 直接查看连接信息（`虚拟IP:API端口`），无需接收协助端回传。
5. 在 Web GUI 使用复制按钮，一键复制连接信息或“给 AI 的 prompt”，并交给 AI Agent 执行远程控制。

<a id="initiator-auto-and-gui"></a>
### 自动带码与 GUI 粘贴说明

- `serve/connect` 支持自动带码（`EncodedConfig` 自动提交）流程。
- GUI 粘贴配置码流程仍然支持，未移除。
- 两种方式可并存，按现场操作习惯选择即可。
- `peers` 语义为“候选池”，连接时会做单轮延迟探测并按低延迟优先排序；运行中异常会按排序结果做 peer fallback，必要时再切换子网。

<a id="initiator-health"></a>
### 连通性与状态检查

远端会话健康检查：

```bash
curl -sS "http://<IP:PORT>/health"
```

本地 GUI 状态与 peer 快照：

```bash
curl -sS "http://127.0.0.1:18080/api/state"
curl -sS "http://127.0.0.1:18080/api/peer-info"
```

- `/health`：查看 `status`、`phase`、`error`、`error_code`。
- `/api/state`：查看 GUI 会话状态（含 `network_owner`、`network_hash`、`tun_device`、`virtual_subnet` 等字段）。
- `/api/peer-info`：查看当前 peer 快照（用于观察 peer 出现与抖动）。

<a id="initiator-error-codes"></a>
### 常见 error_code

- `windows_not_admin`：Windows 未以管理员身份运行（连接前预检拒绝）。
- `windows_admin_check_failed`：Windows 管理员权限检测失败。
- `windows_tun_init_failed`：Windows 虚拟网卡（TUN/Wintun/Packet）初始化失败。
- `windows_firewall_blocked`：疑似被 Windows 防火墙/策略拦截。
- `easytier_start_failed`：EasyTier 进程启动失败（通用兜底）。
- `easytier_ip_timeout`：超时未获取虚拟 IP（通用兜底）。
- `tun_permission_denied`：TUN 权限不足（需管理员/root）。
- `auth_failed`：网络名/密钥不匹配导致认证失败。
- `peer_unreachable`：peer 不可达（链路或对端状态问题）。
- `route_conflict_detected`：路由/网段冲突。
- `config_expired`：配置码过期。

<a id="references"></a>
## 参考

<a id="references-agent-protocol"></a>
### AI Agent 协议

协议与调用约定见：[SKILL.md](./SKILL.md)。
