# Telehand 使用说明

本页包含两部分：

- 接收协助端（运行 Telehand 的机器）
- 控制端（通过 Agent 发起控制）

## 接收协助端

### 一行安装并启动（推荐）

Windows（PowerShell）：

```powershell
iwr -useb https://ghfast.top/https://raw.githubusercontent.com/sfpprxy/telehand/main/install.ps1 | iex; .\telehand.exe
```

macOS：

```bash
curl -fsSL https://raw.githubusercontent.com/sfpprxy/telehand/main/install.sh | bash && ./telehand
```

### Windows 手动下载详细步骤（GUI）

1. 打开发布页：`https://github.com/sfpprxy/telehand/releases`
2. 下载 `telehand-windows-amd64-vX.Y.Z.zip`。
3. 双击 zip 文件解压。
4. 打开解压后的文件夹，双击 `telehand.exe`。
5. 如果浏览器没有自动打开，手动访问 `http://127.0.0.1:18080`。
6. 在页面粘贴配置码，点击“启动远程协助”。
7. 页面显示 `IP:端口` 后，把这个 `IP:端口` 发给对方。

### macOS 手动下载详细步骤（GUI）

1. 打开发布页：`https://github.com/sfpprxy/telehand/releases`
2. 下载 `telehand-darwin-arm64-vX.Y.Z.zip`。
3. 在 Finder 中双击 zip 文件解压。
4. 在 Finder 中打开解压后的 `telehand` 文件。
5. 如果 macOS 提示安全限制：在“系统设置 -> 隐私与安全性”里允许打开，或右键文件后选择“打开”。
6. 如果浏览器没有自动打开，手动访问 `http://127.0.0.1:18080`。
7. 在页面粘贴配置码，点击“启动远程协助”。
8. 页面显示 `IP:端口` 后，把这个 `IP:端口` 发给对方。

### 结束远程协助

- 点击页面“断开连接”，或关闭程序。

### 常见问题

1. 一直停在“正在连接网络，请稍候...”
- 先等 30 秒。
- Windows 尝试“以管理员身份运行”。
- 检查防火墙弹窗是否已允许。

2. 页面打不开
- 手动访问：`http://127.0.0.1:18080`
- 重启 `telehand` 再试。

## 控制端

### 标准接入流程

1. 生成配置码并发给接收协助端：

```bash
telehand gen-config --network-name "your-net" --network-secret "your-secret" --peers "tcp://<your-peer>:11010"
```

2. 等接收协助端返回 `IP:端口`。
3. 将该地址交给 AI Agent 继续执行远程控制。
4. AI Agent 协议与调用约定见：[`SKILL.md`](./SKILL.md)

### 连通性检查

```bash
curl -sS "http://<IP:PORT>/health"
```

- 返回 `status=ok` 代表服务在线。
- 通过 `phase` 观察当前状态（`config` / `connecting` / `running` / `error`）。
- 当 `phase=error` 时，可读取 `error` 和 `error_code` 做自动化排障。
- 以上命令可由 Agent（如 OpenClaw）封装执行。

常见 `error_code`：

- `windows_not_admin`：Windows 未以管理员身份运行（连接前预检拒绝）。
- `windows_admin_check_failed`：Windows 管理员权限检测失败。
- `windows_tun_init_failed`：Windows 虚拟网卡（TUN/Wintun/Packet）初始化失败。
- `windows_firewall_blocked`：疑似被 Windows 防火墙/策略拦截。
- `easytier_start_failed`：EasyTier 进程启动失败（通用兜底）。
- `easytier_ip_timeout`：超时未获取虚拟 IP（通用兜底）。

### TODO

- [ ] 一键组网（基于 EasyTier）：控制端自动完成配置下发与连接建立，减少手工复制粘贴。
