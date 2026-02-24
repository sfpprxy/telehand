# Telehand - AI Agent 操作指南

## 连接信息

- **本地调试地址**: `http://127.0.0.1:<PORT>`（API 启动后即可用）
- **远程访问地址**: `http://<EASYTIER_VIRTUAL_IP>:<PORT>`（组网成功后可用）
- 业务接口使用 `POST`，健康检查接口使用 `GET /health`
- 无需鉴权

## API 列表

### 1. 健康检查 `GET /health`

返回当前运行状态，便于判活和排障。

**响应示例**:
```json
{
  "status": "ok",
  "phase": "running",
  "virt_ip": "10.126.126.2",
  "api_port": 8080,
  "gui_port": 18080
}
```

`phase` 取值：`config` / `connecting` / `running` / `error`

- 当 `phase=error` 时，响应会携带 `error` 与 `error_code`，用于自动化判错。

### 2. 提交配置并自动连网 `POST /connect`

在无需 GUI 手工粘贴时，通过 API 提交 base64 配置码，驱动 `config -> connecting -> running`。

**请求**:
```json
{
  "config": "<base64-config>"
}
```

**响应**:
```json
{
  "ok": true
}
```

- 当实例已在 `connecting/running` 或已有待处理配置时，返回 `HTTP 409`
- 若预检失败（如 Windows 非管理员），返回 `HTTP 400` 且响应内包含 `error_code`

### 3. 执行命令 `POST /exec`

在远程机器上执行 shell 命令。Windows 使用 PowerShell，macOS 使用默认 shell。

**请求**:
```json
{
  "cmd": "whoami",
  "cwd": "C:\\Users", // 可选，工作目录
  "timeout_sec": 30      // 可选，默认 30，最大 600
}
```

**响应**:
```json
{
  "stdout": "joe\n",
  "stderr": "",
  "code": 0
}
```

- `code` 为 0 表示成功，非 0 表示失败
- `code` 为 -1 表示进程启动失败
- `code` 为 124 表示命令超时被终止

### 4. 二进制上传 `POST /upload`

用于上传二进制文件（如 exe/zip），`data` 使用 base64 编码；支持 `append` 分块上传。

**请求**:
```json
{
  "path": "C:\\Users\\Public\\telehand-dev\\telehand.exe",
  "data": "<base64-bytes>",
  "append": false
}
```

**响应**:
```json
{
  "ok": true,
  "bytes": 12345
}
```

### 5. 二进制下载 `POST /download`

按 offset/limit 分块下载文件，返回 base64 数据。

**请求**:
```json
{
  "path": "C:\\Users\\Public\\telehand-dev\\telehand.exe",
  "offset": 0,
  "limit": 1048576
}
```

**响应**:
```json
{
  "data": "<base64-bytes>",
  "size": 1048576,
  "total_size": 31870464,
  "offset": 0,
  "eof": false
}
```

### 6. 读文件 `POST /read`

读取文件内容，支持按行范围读取以节约上下文。

**请求**:
```json
{
  "path": "C:\\Users\\joe\\file.txt",
  "offset": 0,   // 可选，起始行号（0-based），默认 0
  "limit": 50    // 可选，读取行数，默认读取全部
}
```

**响应**:
```json
{
  "content": "line1\nline2\nline3",
  "total_lines": 100
}
```

- 先用 `offset=0, limit=0`（或不传 offset/limit）获取 `total_lines`，再按需分段读取
- `offset` 是 0-based 行索引

### 7. 写文件 `POST /write`

创建或覆盖整个文件。自动创建不存在的父目录。

**请求**:
```json
{
  "path": "C:\\Users\\joe\\new_file.txt",
  "content": "file content here"
}
```

**响应**:
```json
{
  "ok": true
}
```

### 8. 按行编辑 `POST /edit`

替换文件中指定行范围的内容。行号从 1 开始。

**请求**:
```json
{
  "path": "C:\\Users\\joe\\file.txt",
  "start_line": 5,
  "end_line": 8,
  "content": "new line 5\nnew line 6"
}
```

**行为说明**:
- 删除第 `start_line` 到 `end_line` 行（含两端），插入 `content`
- 若 `content` 为空字符串，则纯删除这些行
- 若要在第 N 行前插入（不删除任何行），设 `start_line=N, end_line=N-1`
- `start_line` 范围: 1 到 total_lines+1
- `end_line` 范围: start_line-1 到 total_lines

**响应**:
```json
{
  "ok": true
}
```

### 9. 查找替换 `POST /patch`

在文件中查找文本并替换。

**请求**:
```json
{
  "path": "C:\\Users\\joe\\file.txt",
  "old": "原始文本",
  "new": "替换后文本",
  "replace_all": false  // 可选，默认 false，只替换第一处
}
```

**响应（正常）**:
```json
{
  "replaced": 1
}
```

**响应（多处匹配但只替换了第一处）**:
```json
{
  "replaced": 1,
  "warning": "multiple matches found (3 total), only replaced first occurrence",
  "matches": [12, 45, 78]
}
```

- `matches` 数组包含所有匹配位置的行号（1-based）
- 收到 warning 时，建议改用 `/edit` 按行号精确编辑
- `old` 支持跨行匹配（用 `\n` 分隔）
- 若 `path` 文件不存在或 `old` 未找到，返回 400 错误

### 10. 列目录 `POST /ls`

**请求**:
```json
{
  "path": "C:\\Users\\joe"
}
```

**响应**:
```json
{
  "entries": [
    {"name": "Documents", "is_dir": true, "size": 0},
    {"name": "file.txt", "is_dir": false, "size": 1234}
  ]
}
```

## 错误响应格式

所有 API 在出错时返回：
```json
{
  "error": "错误描述",
  "error_code": "optional_code"
}
```

HTTP 状态码（业务接口）：
- 400: 请求参数错误
- 409: `POST /connect` 状态冲突（已在 connecting/running 或已有待处理配置）
- 404: API 路径不存在（例如 URL 写错）
- 405: HTTP 方法错误（业务接口只接受 POST；`GET /health` 只接受 GET）
- 500: 服务器内部错误

常见 `error_code`：
- `windows_not_admin`: Windows 未以管理员身份运行（连接前预检拒绝）
- `windows_admin_check_failed`: Windows 管理员权限检测失败
- `windows_tun_init_failed`: Windows 虚拟网卡（TUN/Wintun/Packet）初始化失败
- `windows_firewall_blocked`: 疑似被 Windows 防火墙/策略拦截
- `easytier_start_failed`: EasyTier 启动失败（通用兜底）
- `easytier_ip_timeout`: 超时未拿到虚拟 IP（通用兜底）

语义约定（避免“业务未命中”和 HTTP 语义混淆）：
- `POST /read` 文件不存在时，返回 `HTTP 200` + `{"error":"...not found..."}`。
- `POST /ls` 目录不存在时，返回 `HTTP 200` + `{"error":"...not found..."}`。
- `POST /download` 文件不存在时，返回 `HTTP 200` + `{"error":"...not found..."}`。

## 典型工作流

### 探索远程文件系统
```
POST /exec {"cmd": "echo %USERPROFILE%"}     → 获取用户目录（Windows）
POST /ls {"path": "C:\\Users\\joe"}           → 列出目录
POST /read {"path": "...", "limit": 30}       → 预览文件前30行
```

### 编辑远程文件
```
POST /read {"path": "...", "offset": 0, "limit": 0}  → 获取总行数
POST /read {"path": "...", "offset": 10, "limit": 20} → 读取第10-30行
POST /edit {"path": "...", "start_line": 15, "end_line": 18, "content": "..."} → 精确编辑
```

### 查找替换
```
POST /patch {"path": "...", "old": "foo", "new": "bar"}
→ 如果返回 warning，查看 matches 行号
→ 改用 /read + /edit 按行号精确操作
```

## 注意事项

- Windows 路径使用 `\\` 或 `/`，PowerShell 两种都支持
- 调试验证优先使用普通目录（如 `C:/Users/Public/telehand-sandbox-*`），避免系统目录（如 `C:/Windows/*`）
- `/exec` 的 `cmd` 在 Windows 上通过 `powershell.exe -Command` 执行
- `/exec` 的 `cmd` 在 macOS 上通过默认 shell（通常 zsh）的 `-c` 执行
- `telehand serve --config <base64>` 可在启动后自动进入连网流程（无需 GUI 手输）；macOS / Linux 侧建议使用 `sudo telehand serve --config <base64>`
- `telehand serve --no-browser` 可禁用自动打开浏览器，适合远程无头场景
- 文件编码统一为 UTF-8
- `/read` 的 `offset` 是 0-based，`/edit` 的 `start_line` 是 1-based
