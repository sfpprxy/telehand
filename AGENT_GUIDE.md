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

### 2. 执行命令 `POST /exec`

在远程机器上执行 shell 命令。Windows 使用 PowerShell，macOS 使用默认 shell。

**请求**:
```json
{
  "cmd": "whoami",
  "cwd": "C:\\Users"  // 可选，工作目录
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

### 3. 读文件 `POST /read`

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

### 4. 写文件 `POST /write`

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

### 5. 按行编辑 `POST /edit`

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

### 6. 查找替换 `POST /patch`

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
- 若 `old` 未找到，返回 404 错误

### 7. 列目录 `POST /ls`

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
  "error": "错误描述"
}
```

HTTP 状态码（业务接口）：
- 400: 请求参数错误
- 404: 文件/目录不存在，或 old text 未找到
- 405: HTTP 方法错误（所有接口只接受 POST）
- 500: 服务器内部错误

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
- `/exec` 的 `cmd` 在 Windows 上通过 `powershell.exe -Command` 执行
- `/exec` 的 `cmd` 在 macOS 上通过默认 shell（通常 zsh）的 `-c` 执行
- 文件编码统一为 UTF-8
- `/read` 的 `offset` 是 0-based，`/edit` 的 `start_line` 是 1-based
