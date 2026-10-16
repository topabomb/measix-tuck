# 架构与引擎边界

## 产品范围

工作台后端覆盖登录与配置保护、助手/模型/MCP设置、企业接入、任务队列与原生交互、规范化历史与SSE、附件与工作区文件、DAV传输、版本交付、官方引擎管理及发行维护。Go提供服务和维护命令，Vue提供产品页面；Node.js只用于前端构建和开发测试。

当前不提供目标/编排执行器、分布式调度、多用户服务或外部数据库；目标和编排入口保持已有产品状态。参考演练与真实执行分开，实际验证范围见 [验证指南](verification.md)，持久化版本的支持路径见 [迁移约定](persistence-migrations.md)。

## 所有权

Tuck 后端为 [cmd/tuck](../cmd/tuck/) 和一个 [internal/tuck](../internal/tuck/) 业务包，按 owner 分文件。前端在 [frontend/src](../frontend/src/)；tests/fixtures 的 JavaScript 仅生成合成平台/供应商和驱动 Go 测试进程，不是产品后端。渲染语义、界面及样式职责集中在 [前端架构与界面](frontend.md)，开发目录入口见 [贡献指南](../CONTRIBUTING.md#代码与职责)。

Vue → Go HTTP/SSE → Tuck SQLite、配置、文件和交付；Go → Core/Relay/DAV；Go → 官方 Docker Agent CLI/REST/SSE → 原生执行、上下文、审批、子代理和 MCP。

app/settings 装配 HTTP、认证和配置切换；tasks/events/interactions/statistics 持有任务与规范化事实；engine/engine_updates 管理公开引擎进程及官方发行包；platform/models/mcp 负责平台身份、模型授权和企业工具合同；files/deliveries/workspace_tools/enterprise_workspace 负责文件、快照和远端传输；config/store/security 是配置、业务持久化和 Web 身份的唯一 owner。

storage_schema 识别已发布结构并嵌入不可改写的编号 DDL；migrations 只在显式离线命令中协调配置和 Store 迁移，persistence_check 流式核对持久事实与交付文件。正常启动不隐式升级；服务和维护共享进程排他锁，关闭存储后才释放。平台密文格式由 Platform 管理，不把上游 Snapshot 版本当作本机数据版本，详见 [持久化版本与迁移](persistence-migrations.md)。

任务发布在 Store 短事务内写任务、消息和事件，提交后唤醒浏览器。网络和文件复制不持有全局任务锁；每任务队列和交付串行。SSE 的可靠来源是数据库游标，不在每个订阅者内积累事件；每页最多 512 条且约 4 MiB，单个大事件仍可完整发送。

## Docker Agent 合同

引擎来自 Docker 官方 [docker/docker-agent](https://github.com/docker/docker-agent)，项目介绍及致谢见 [README](../README.md#docker-agent-与-tuck)。兼容基线为官方 [v1.150.0](https://github.com/docker/docker-agent/releases/tag/v1.150.0)，源码标签 `5277e7842058fc3fbb9943a6e88bc05a1c8e6fe2`；安装程序匹配 [嵌入发行清单](../internal/tuck/contract/engine-release.json) 的 SHA256。[上游文档](https://docker.github.io/docker-agent/)及较新的官方源码仅作公开能力参考，不扩大已验收范围。

Tuck 不修改、补丁或编译引擎，不导入内部 runtime/store/session 包，不读取引擎 SQLite 表。只使用官方配置、serve api --session-db、Agent 加载/目录/计数、session 创建/读取/恢复、执行流、title、permissions、safety-policy、fork、resume、elicitation 和 steer。后台任务 ID 不能推断为 session ID；缺少原生身份时不显示为已取得。

原生 MCP 发现、连接、恢复、调用重试、审批、子代理和执行生命周期仍由引擎负责。长模型/原生执行/SSE 流没有固定总寿命；连接/响应头和单次操作超时各自生效。Tuck 不另加原生调用重试，不因一次操作结束关闭有效共享连接。

| 公开能力缺口 | 当前最小适配 | 删除条件 |
| --- | --- | --- |
| 企业模型需平台代理身份、准入及凭据隔离 | 回环模型网关、明确来源、admit、Chat/Responses 转发和脱敏 | 官方公开接口可完整承接相同合同 |
| 企业 MCP 需每工具许可、JCS 哈希和平台 admit | 透明 HTTP/SSE 代理、校验目录/授权/schema，保留原始事件 ID 和通知 | 引擎公开扩展完整承接这些合同 |
| MCP 没有 Tuck 当前任务/轮次/空间身份 | 官方 MCP Go SDK 本机交付/DAV 工具，短期 turn header 和调用前后检查 | 原生公开上下文可携带并验证相同事实 |
| 工作台需要稳定 URL、分页、重放及版本交付 | 规范化消息/事件、只读公开历史导入、独立快照 | 原生公开产品接口满足现有合同 |

本机 MCP 使用官方 SDK Streamable HTTP，企业代理兼容公开 HTTP/SSE；SDK 依赖与引擎对齐，不手写会话握手。登记、最近验证、当前连接和实际执行是不同事实，缺少状态时不推断在线或成功。

企业代理保留空数据 SSE priming 帧及其原始 ID，不把没有消息正文的帧当作坏 JSON。POST 通知的202和 DELETE 的204允许真正零字节确认，即使上游仍带 JSON Content-Type；非空正文继续严格校验和错误脱敏，初始化响应不适用空体例外。

基线 SDK 为 v1.7.1-0.20260825122737-c68ad9a4e6e1。本机服务使用 Stateless 及 PropagateRequestCancellation；引擎当前的后台 subscriptions/listen 无法展开每轮动态 header，但官方客户端保留 Cookie。因此 server/discover 只有在有效 turn header 下才签发目录归属 Cookie，按客户端复用仍有 SDK 请求/会话的 Server。Cookie 只定位任务和工具种类的目录，工具执行仍要求精确活跃 turn header 与 task 匹配。

目录变化由 SDK AddTool/RemoveTools 通知，开轮最多等待一次 tools/list 确认、取消或 1 秒；超时不代表已同步，实际调用仍按当前权限拒绝。无订阅/会话且无在途请求时回收 peer，结束一轮不关闭有效共享连接，服务关闭才撤销剩余 SDK 会话。没有轮询或应用重试。官方公开订阅能保留动态上下文并确认目录刷新后，应删除目录 Cookie 和等待适配。

## 生命周期边界

浏览器断开不取消任务；目标停止只取消自己的轮次。重启把原执行收束为 interrupted，暂停用户队列、移除自动补交付队列并退役旧审批，不重放工具或自动恢复执行。关闭先撤销网络 owner、取消任务，再关引擎和存储。

任务、工具调用、问题、原生会话和后台任务分别保留各自身份。只有公开事件或响应提供了子会话 ID 才能进入其历史，不从后台任务 ID 或输出文本推断。立即补充和协作的接收、上下文送达及目标完成分别记录；原生 delivered 不保证模型遵循新指令。具体参数见 [工具参考](tools-reference.md)。

工具调用 ID 按当前执行来源和原生 agent_name 关联，完成后移除在途记录，允许供应商后续复用 ID；计时按所属任务隔离。队列准备完成后核对整条队列项，编辑或移除过的旧内容不发送；收尾期间新接收的消息由原队列 owner 继续启动。配置应用先阻止新执行，再检查当前任务；创建与 fork 的异步响应不能跨越配置切换后发布。

## 平台与安全

Discovery 先验证身份和路径，同部署改地址先无凭据校验。Snapshot v4/v5 校验 schema、闭包、ETag、模型来源和 MCP 合同；内嵌合同及来源见 [client-schemas.json](../internal/tuck/contract/client-schemas.json) 和 [provenance.json](../internal/tuck/contract/provenance.json)。刷新先持久化平台公开幂等键，失响应保留；这不改变原生重试语义。平台绑定用配置的 32 字节密钥加密存入业务库。现行平台枚举允许的 ANDROID 兼容值仍保留，不能擅自新增枚举。

本地登录采用 scrypt、哈希会话令牌、Cookie/CSRF、持久限流和有界密码校验。Host/Origin 校验忽略 Forwarded/X-Forwarded。凭据变化、到期、退出撤销对应流，不结束有效后台任务。配置保护用独立 Cookie，绑定当前登录、固定一小时，并同时检查分类读取和写入。

## API与文件

主接口保持 /api/runtime、/api/connection*、/api/settings/*、/api/tasks*、/api/events、/api/enterprise-workspace*。任务快照带 cursor；SSE 用 seq/taskId/type、since/Last-Event-ID，拒绝未来游标。消息按 index/revision 和块修订定位，媒体从规范化历史按需读取，不改写历史。

Hash URL 是页面位置合同：对话、任务、文件和设置详情能直达，刷新/登录返回恢复目标及筛选分页；不含秘密、提交正文、草稿或本地 File。参考场景在 #/reference，架构在 #/architecture。真实工作台采用对话、任务、设置和企业工作区路由。

具体页面路径以 [router.ts](../frontend/src/app/router.ts) 为准：对话为 `#/chat/conversations/:id`，消息定位为其 `/messages/:message`，任务为 `#/tasks/:id`，设置为 `#/settings/...`，企业工作区为 `#/workspace`。任务交付和对话交付分别在各自详情路径下的 `/delivery`。导航恢复目标和有意义的筛选/分页，不承诺恢复未提交正文或 File；回到嵌套来源时保留对应页面上下文。

附件每条最多 8 件、16 MiB；图片实际解码、40M 像素、方向修正、2048px 静态缩放，动画保留原字节。普通文件下载 64 MiB，编辑/文本预览 2 MiB。os.Root 及发布前父目录/目标身份检查防止 symlink/junction 逃逸。

企业面板代理 Core Files API，工具通过明确绑定空间的 DAV。默认创建 If-None-Match:*，替换需强 ETag。上传失响应标记 outcome_unknown，不重复提交。下载先预检整个目标集合，再逐件临时写入发布，保留已完成结果及失败目标原字节。浏览器文件响应头 15s、体无进展 120s；DAV 文件体无进展 30s，只约束文件操作，不应用于原生 MCP/模型流。

交付属于主助手，每成功调用新版本，1000 Unicode 字符、10 文件、单件 512 MiB、合计 2 GiB。界面只展示已取得事实，不从工具输出文本推测状态。配置、存储与部署细节见对应文档。
