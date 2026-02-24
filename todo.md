阅读 file:///Users/joe/Sync/Work/handoffs/telehand-handoff.md 并准备实现&发布 0.2.0

# 0.1.5现状
被控端运行后目前的行为是自动打开浏览器并展示配置码的粘贴页面 "请将对方发给你的配置码粘贴到下方"，配置码是由控制端生成并交给被控端的。

# 0.2.0 核心目标：组网方式优化
目标分解：
- [ ] 实现被控端 telehand -server -join-network 配对码
  - [ ] 如果在被控机上能检测到默认浏览器，则默认浏览器打开web gui及相关界面(且界面信息随实际连接状态而变化，连接成功后的显示内容和0.1.5一样)，此时不需要手动再在web中手动复制粘贴并连接了，因为执行命令时已经做了;如果在被控机是无GUI环境(比如linux服务器)或者无默认浏览器，则所有信息都在终端cli输出/显示
- [ ] 实现控制端 telehand -client -create-network
  - [ ] create-network 时使用的参数如下 --network-name <telehand:本机hostname> --network-secret <telehand:本机hostname+随机4位数> --peers `默认使用tcp://39.108.52.138:11010`
