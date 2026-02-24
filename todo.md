阅读 file:///Users/joe/Sync/Work/handoffs/telehand-handoff.md 并准备实现&发布 0.2.0

# 0.1.5现状
被控端运行后目前的行为是自动打开浏览器并展示配置码的粘贴页面 "请将对方发给你的配置码粘贴到下方"，配置码是由控制端生成并交给被控端的。

# 0.2.0 核心目标：组网方式优化
目标分解：
- [ ] 实现被控端 telehand -server -join-network 配对码
  - [ ] 如果在被控机上能检测到默认浏览器，则默认浏览器打开web gui及相关界面(且界面信息随实际连接状态而变化，连接成功后的显示内容和0.1.5一样)，此时不需要手动再在web中手动复制粘贴并连接了，因为执行命令时已经做了;如果在被控机是无GUI环境(比如linux服务器)或者无默认浏览器，则所有信息都在终端cli输出/显示
  - [ ] CLI 参数与执行链路
    - [ ] 定义 `-join-network` 参数格式（支持直接粘贴配对码）
    - [ ] 启动时完成参数校验（缺参/格式错误/过期码）
    - [ ] 参数合法时自动触发 join，不再依赖 Web 手动粘贴
  - [ ] 连接状态机
    - [ ] 状态：初始化 -> 连接中 -> 已连接 -> 失败重试/退出
    - [ ] 状态变化统一事件源（CLI 与 Web 复用同一状态）
    - [ ] 错误码与错误文案对齐（超时、鉴权失败、peer 不可达）
  - [ ] GUI 场景（有默认浏览器）
    - [ ] 自动打开 Web GUI 到状态页
    - [ ] 页面按连接状态动态切换文案与操作按钮
    - [ ] 连接成功后页面显示与 0.1.5 保持一致
  - [ ] 无 GUI/无默认浏览器场景
    - [ ] 终端完整输出当前状态、下一步提示与错误原因
    - [ ] 提供可复制命令/日志位置，便于远程排障
    - [ ] 支持无交互运行（systemd/docker）仅通过 stdout/stderr 观测
- [ ] 实现控制端 telehand -client -create-network
  - [ ] create-network 时使用的参数如下 --network-name <telehand:本机hostname> --network-secret <telehand:本机hostname+随机4位数> --peers `默认使用tcp://39.108.52.138:11010`
  - [ ] 参数生成规则
    - [ ] `--network-name` 默认 `telehand:<hostname>`
    - [ ] `--network-secret` 默认 `telehand:<hostname+随机4位数>`
    - [ ] `--peers` 默认 `tcp://39.108.52.138:11010`（允许显式覆盖）
  - [ ] 可用性与安全性
    - [ ] hostname 读取失败时回退策略（固定前缀+随机4位数后缀）
  - [ ] create-network 输出契约
    - [ ] 标准输出包含可直接在被控端执行的命令类似 `iwr -useb https://ghfast.top/https://raw.githubusercontent.com/sfpprxy/telehand/main/install.ps1 | iex; .\telehand.exe -server -join-network 配对码` (这里不脱敏) 并复制命令至剪贴板方便粘贴；同时包含多平台
    - [ ] 返回码约定：成功0，参数错误/网络错误/服务错误分级
- [ ] 网络信息展示（终端/Web）
  - [ ] 统一 PeerInfo 展示字段与数据源
    - [ ] 字段对齐 UI：Virtual IPv4 / Hostname / Route Cost / Protocol / Latency / Upload / Download / Loss Rate / Version
    - [ ] 角色/本机标记（如 Server/Client、本机高亮）
    - [ ] 刷新策略（Web 自动刷新；终端持续刷新）
  - [ ] 控制端（client）
    - [ ] 终端输出当前网络信息（节点列表 + PeerInfo）
    - [ ] Web GUI 展示 Peer Info 表格（节点列表、关键指标、自动刷新）
    - [ ] Web GUI 中可以一键复制：在被控端执行的命令
  - [ ] 被控端（server）
    - [ ] 终端输出当前网络信息（节点列表 + PeerInfo）
    - [ ] Web 端（如有）同样展示 Peer Info 表格并自动刷新
- [ ] secret 尽可能脱敏（日志/屏幕等展示仅显示部分字符）
- [ ] 联调验收
  - [ ] 控制端 create-network -> 被控端 join-network 一次成功链路
  - [ ] 有 GUI 与无 GUI 两种环境各跑一轮冒烟
  - [ ] 验收网络信息展示：控制端/被控端在终端与 Web（如有）均能看到节点列表与 PeerInfo，字段齐全且刷新正常
  - [ ] 回归 0.1.5 行为一致性（连接成功后的界面与提示）
