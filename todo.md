阅读 file:///Users/joe/Sync/Work/handoffs/telehand-handoff.md 并准备实现&发布 0.2.0

# 实现规则

自底向上的进行实现, 完成一个子任务后就用 Markdown 语法标记完成。但是如果在任何过程中遇到了问题，则在子任务下新建一个子任务并描述问题，解决完问题也标记完成。一般来说现有的任务内容不需要改动，如需改动停下来征求我同意。

# 0.1.5现状
被控端运行后目前的行为是自动打开浏览器并展示配置码的粘贴页面 "请将对方发给你的配置码粘贴到下方"，配置码是由控制端生成并交给被控端的。

# 0.2.0 核心目标：组网方式优化
目标分解：
- [x] 抽公共函数层（复用优先）
  - [x] 抽取 `buildConfigFromInputs(networkName, networkSecret, peers)` 纯函数（不 `os.Exit`）
  - [x] 抽取 `encodeConfigOrErr(cfg *Config)`，统一复用 `EncodeConfig`
  - [x] 抽取 `submitEncodedConfig(encoded string)`，统一复用 `GUIServer.SubmitConfigEncoded`
  - [x] `runGenConfig` 与 `runConnect` 复用同一组公共函数
  - [x] `runServe --config` 与 `runServe` 的新参数路径复用同一提交函数
- [x] 新增 CLI 入口（调用公共函数）
  - [x] 新增 `telehand connect [配对码]`
  - [x] 新增 `telehand serve [配对码]`
  - [x] 保持当前 `serve` / `gen-config` 兼容
    - [x] 旧命令 `serve` / `gen-config` 保持可用，原参数不破坏
    - [x] 新旧命令走同一业务函数，输出与返回码保持一致
    - [x] 补充兼容性验收：同参数下新旧命令产出结果一致
- [x] secret 尽可能脱敏（日志/屏幕等展示仅显示部分字符）
- [x] 实现控制端 telehand connect
  - [x] `connect` 时支持以下参数 --network-name <telehand:本机hostname> --network-secret <telehand:本机hostname+随机4位数> --peers `默认使用tcp://39.108.52.138:11010`
  - [x] 未提供配对码时按既定默认规则自动生成 network 参数并连接
  - [x] 参数生成规则
    - [x] `--network-name` 默认 `telehand:<hostname>`
    - [x] `--network-secret` 默认 `telehand:<hostname+随机4位数>`
    - [x] `--peers` 默认 `tcp://39.108.52.138:11010`（允许显式覆盖）
  - [x] 可用性与安全性
    - [x] hostname 读取失败时回退策略（固定前缀+随机4位数后缀）
  - [x] `connect` 输出契约
    - [x] 标准输出包含可直接在被控端执行的命令类似 `iwr -useb https://ghfast.top/https://raw.githubusercontent.com/sfpprxy/telehand/main/install.ps1 | iex; .\telehand.exe serve 配对码` (这里不脱敏) 并复制命令至剪贴板方便粘贴；同时包含多平台
    - [x] 问题：macOS / Linux 侧命令未体现 TUN 需要 root 权限。已统一在 README 与 `connect` 输出命令中补充 `sudo`（`sudo ./telehand` / `sudo ./telehand serve ...`）。
    - [x] 返回码约定：成功0，参数错误/网络错误/服务错误分级
- [x] 实现被控端 telehand serve 配对码
  - [x] 如果在被控机上能检测到默认浏览器，则默认浏览器打开web gui及相关界面(且界面信息随实际连接状态而变化，连接成功后的显示内容和0.1.5一样)，此时不需要手动再在web中手动复制粘贴并连接了，因为执行命令时已经做了;如果在被控机是无GUI环境(比如linux服务器)或者无默认浏览器，则所有信息都在终端cli输出/显示
  - [x] CLI 参数与执行链路
    - [x] 定义 `serve` 参数格式（支持直接粘贴配对码）
    - [x] 未提供配对码时按既定默认规则自动生成 network 参数并连接
    - [x] 启动时完成参数校验（缺参/格式错误/过期码）
    - [x] 参数合法时自动触发连接，不再依赖 Web 手动粘贴
  - [x] 连接状态机
    - [x] 状态：初始化 -> 连接中 -> 已连接 -> 失败重试/退出
    - [x] 状态变化统一事件源（CLI 与 Web 复用同一状态）
    - [x] 错误码与错误文案对齐（超时、鉴权失败、peer 不可达）
  - [x] GUI 场景（有默认浏览器）
    - [x] 自动打开 Web GUI 到状态页
      - [x] 问题：macOS 默认浏览器设置为 Arc 时，telehand 仍打开 Safari；默认浏览器探测/调用逻辑可能有误，需修复为严格跟随系统默认浏览器。
        - [x] 底层实现：`openBrowser` 在 macOS 且 `sudo` 提权场景下，改为以 `SUDO_USER` 用户上下文执行 `open`，避免 root 上下文打开 Safari。
        - [x] 单测覆盖：新增 `platform_util_test.go`，覆盖普通用户、`sudo` 场景、`sudo` 缺失回退、Linux 无显示环境校验。
        - [x] 验证：`go test ./...` 全量通过。
    - [x] 页面按连接状态动态切换文案与操作按钮
    - [x] 连接成功后页面显示与 0.1.5 保持一致
  - [x] 无 GUI/无默认浏览器场景
    - [x] 终端完整输出当前状态、下一步提示与错误原因
    - [x] 提供可复制命令/日志位置，便于远程排障
    - [x] 支持无交互运行（systemd/docker）仅通过 stdout/stderr 观测
- [x] 网络信息展示（终端/Web）
  - [x] 统一 PeerInfo 展示字段与数据源
    - [x] 字段对齐 UI：Virtual IPv4 / Hostname / Route Cost / Protocol / Latency / Upload / Download / Loss Rate / Version
    - [x] 角色/本机标记（如 Server/Client、本机高亮）
    - [x] 刷新策略（Web 自动刷新；终端持续刷新）
  - [x] 控制端（client）
    - [x] 终端输出当前网络信息（节点列表 + PeerInfo）
    - [x] Web GUI 展示 Peer Info 表格（节点列表、关键指标、自动刷新）
    - [x] Web GUI 中可以一键复制：在被控端执行的命令
      - [x] 问题：运行中页面只有一个“复制推荐命令”按钮，且 macOS 与 Linux 分开显示；已调整为两个复制按钮（Windows / macOS-Linux）并将平台命名合并为 `macOS / Linux`。
  - [x] 被控端（server）
    - [x] 终端输出当前网络信息（节点列表 + PeerInfo）
    - [x] Web 端（如有）同样展示 Peer Info 表格并自动刷新
- [ ] 联调验收
  - [ ] 控制端 connect -> 被控端 serve 一次成功链路
    - [x] 问题：本机联调时 EasyTier 报 `tun device error: Operation not permitted`，导致无法进入 `running`。已新增 `tun_permission_denied` 错误码与明确文案，定位为运行环境权限前置条件问题（需管理员/root 权限）并完成代码侧兜底。
  - [ ] 有 GUI 与无 GUI 两种环境各跑一轮冒烟
    - [x] 问题：当前执行环境无法稳定提供 GUI+TUN 的联调前置条件。已完成无 GUI 流程验证（状态机、日志、错误码、退出码）与 GUI 页面自动刷新/命令复制功能实现，等待具备权限的环境执行最终冒烟。
  - [ ] 验收网络信息展示：控制端/被控端在终端与 Web（如有）均能看到节点列表与 PeerInfo，字段齐全且刷新正常
    - [x] 问题：受同一 TUN 权限限制，本机无法跑到真实 `running` 做全链路验收。已通过 `peer list` 实际 JSON 字段对齐实现与单测/API 页面联动完成代码级验证，待权限环境做最终联调确认。
  - [ ] 回归 0.1.5 行为一致性（连接成功后的界面与提示）
    - [x] 问题：本机无法完成“连接成功态”截图级回归（同样受 TUN 权限限制）。已保留 0.1.5 的 running 主展示结构（IP/API/日志/断开）并在此基础上扩展 PeerInfo 与命令复制，待权限环境补最后回归。
