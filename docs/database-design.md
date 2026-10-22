# 存储设计与维护

## 所有权与结构

Tuck 只拥有 <storage.dataDir>/tuck.db 一个业务 SQLite，Store 持有短写事务，编号 DDL 由 storage_schema.go 嵌入。application_id=0x5455434b、user_version=4；完整实际结构也须匹配。正常启动只初始化空库或打开当前格式；正式 Go v1/v2/v3 使用显式离线迁移，未知或封存结构拒绝且不修改。支持路径、数据版本、备份及中断恢复见 [持久化版本与迁移](persistence-migrations.md)。

| 表 | 关键约束与职责 |
| --- | --- |
| tasks | id 主键，created_at_ms/status/archived/native_session_id 类型列、object payload_json；managed_workspace_id / parent_task_id 从 JSON 生成，前者外键 RESTRICT 指向 managed_workspaces |
| managed_workspaces | id 主键、root、唯一 path、identity、state（allocating/attached/pending）、cleanup_allowed 布尔整数、last_error；独立于原始对话的目录归属与回收责任 |
| messages | task_id 外键、非负 position/revision、object JSON；主键(task_id,position)，规范化消息 |
| events | AUTOINCREMENT seq、可空 task_id 外键、type、created_at_ms、object JSON、生成的 payload_bytes；短期同步缓存 |
| task_deliveries | id 主键、唯一(task_id,version)、turn_id、时间、摘要、file_count、total_bytes、files_json |
| web_sessions | token_hash、identity、csrf、expires_at；只存令牌哈希 |
| login_limits | key、failures、until_at、expires_at；账户/IP 持久限流 |
| platform_session | id=1、installation_id、encrypted_payload、format_version=2；平台绑定、快照、刷新键和 applied 回执；密文认证绑定安装身份 |
| mcp_catalog | resource_key、fingerprint、最多 4 MiB 的数组 catalog_json、verified_at_ms；最近成功验证，非实时连接 |

完整可执行 DDL 以 [001.sql](../internal/tuck/contract/storage/001.sql)、[002.sql](../internal/tuck/contract/storage/002.sql)、[003.sql](../internal/tuck/contract/storage/003.sql)、[004.sql](../internal/tuck/contract/storage/004.sql) 顺序执行为唯一来源，不复制第二套最新 SQL。已发布步骤不修改。task_listing 支持归档/创建时间；task_workspace / task_parent 支持目录引用和主子树查询；消息主键支持分页；event_retention 支持事件容量计算与顺序裁剪；delivery_turn 支持轮次交付定位。

tasks JSON 的 `lifecycle` 保存 `purpose`（conversation/temporary）、`keep`、`lastActivityAt`、`archivedAt`；时间为非负 Unix 毫秒。旧记录缺时间时，`activityObservedAt` / `archiveObservedAt` 保存首次有效策略检查的观察起点；`lastDeleteError` 只保存最近删除错误。顶层 `managedWorkspaceId` 是托管目录稳定 ID，fork 与子会话沿用；生成列没有第二个写入口。旧任务可缺这些字段，不能根据目录名称补造归属。规则只在配置文件，期限和预计日期不复制进数据库。

轮次统计由 tasks JSON 中的 historyTiming（之前轮次的累计时长、数量、不完整标记）及当前 turnTiming 组成。开始下一轮时，在现有任务发布事务中将上一轮折入累计值；同一 turnId 不重复折入。工具数仍从规范化消息块查询，usage、模型次数和运行耗时继续使用任务已有累计字段。不保存每轮状态副本，不增加统计 owner 或后台聚合器。

Docker Agent 拥有原生执行、模型上下文、审批及子代理；Tuck 保存产品任务、队列、待处理交互、交付及规范化历史。当前引擎公开历史接口返回完整会话，Tuck 的 messages 支持分页、稳定位置/修订及附件识别等产品元数据，因此这份投影有明确用途。每条消息只保留当前版本；图片继续随消息保存，避免引入文件引用和垃圾回收链。原生 SSE 在消费时归一化，不另存完整 native 日志；即使进度尚无展示内容，也只更新任务最新观测时间，保留中断统计依据。

远程识别的来源事实保存在现有 messages 的 tool-call presentation.workspace，不新增表或引擎存储依赖；来源和分支匹配规则见 [附件识别](attachment-inspection.md#输入预览与来源身份)。

SQLite 使用 WAL、synchronous=FULL、foreign_keys=ON、busy_timeout=5000，最多四连接和一个写互斥 owner。文件名先做 URI 转义再附加 SQLite 参数。事务使用请求上下文，等待写锁后取消不能提交。数据库和文件用本机磁盘，一个数据目录只由一个 Tuck 服务使用；服务及离线维护以操作系统排他锁约束配置和数据目录，崩溃自动释放，锁文件不手动删除。

## 提交、游标与恢复

任务、消息及事件在一个短事务提交，成功后广播。HTTP 读取副本，消息位置不随归档改变。工作台补页最多 64 条，内部历史读取默认 2048/最多 4096 条；前端窗口与缓存见 [前端](frontend.md#历史投影与滚动)。事件按 seq 发送，每页最多 512 条且约 4 MiB，单个大事件仍可完整发送；慢客户端只持游标，网络/模型/文件 IO 不进入写事务。

列表、任务和消息快照先取得重放游标，再读取数据，游标是重放下界。并发发布可能同时出现在快照和后续事件中，前端按事件 seq、消息 index/revision 处理重复；不能在读取旧数据后附上更新的游标而跳过变化。快照读取不为此持有覆盖历史解码或网络发送的全局锁。

事件保留目标为最近 24 小时且 JSON 正文合计不超过 128 MiB。Store 每分钟检查，按 seq 删除最旧前缀，每个写事务最多 512 条；容量允许在检查间隔内暂时超出。新事件时间使用本地提交时间，不依赖引擎 timestamp。游标高水位取 sqlite_sequence，删除全部事件也不会倒退或复用。

请求游标早于保留范围时，SSE 发送 reset 并关闭该流；前端重新读取任务快照及当前历史窗口，再从快照游标订阅。未加载的历史仍正常分页，草稿与阅读位置保留。事件与快照可能重复但不能跳过；重置期间的新变更由后续重放补齐。事件是缓存，裁剪不改任务、消息、审批或交付，不影响统计和重启恢复。

Tuck 历史不能完整重建原生上下文。引擎会话存储由官方 Docker Agent 独立管理，Tuck 不读内部 schema。runtime/agent.yaml 是可重建输入，平台秘密不进入其中；具体资产路径由 engine.go 的启动参数确定。工作文件在 workspaceRoot 或指定目录，交付快照在 `<storage.dataDir>/deliveries/`。

交付快照是与业务库配套的运行数据，不提交Git。默认 `/data/` 和仓库根 `/deliveries/` 已排除；将数据目录或工作目录设在仓库其他位置时，也须排除相应运行数据。Git忽略不改变交付保留规则或完整备份范围。

重启只收束未结束任务及其仍为 live 的消息、暂停用户队列、清理自动续轮和旧审批，不执行工具。终止或断流后未取得结果的工具输出标为 interrupted，子会话及父会话的委派展示同步收束，不补造成功结果。补充消息的确认状态与原生 messageId 直接提交到对应消息；回执早于 HTTP 返回时，首次消息提交即携带确认事实。未取得持久确认的补充在恢复时标为 unknown，不根据保留日志猜成功。v2 的旧回执只在离线迁移中从旧事件提取。导入公开原生历史修复规范化元数据时保留位置、调用身份与实际结果，重复修复幂等。

## 交付与清理

本地文件和已授权 Runcove 文件均先做独立文件快照、fsync、目录原子发布，再提交版本和最新状态；每成功调用新版本，同任务串行。失败取消回收暂存和未提交快照，源文件不变。数据库与文件系统不是联合原子事务，启动按已提交引用恢复。

交付输入和容量限制见 [交付工具](tools-reference.md#对话交付deliver_result)。旧版本只从快照读取，不回读源文件；创建快照另预留 256 MiB 可用空间。

裁剪旧记录和新版本同事务，成功后清理无引用快照；保留数 0 仍留下最新版本。清理失败不撤销成功版本，交付 owner 内部记录 cleanup_pending，模型可见结果不包含维护状态；启动和下次成功交付继续清理。只删除 owner 下匹配公开 ID 且确认无引用的目录，不处理其他文件。

任务/消息默认持续保留；自动归档只修改 Tuck 的 archived 及生命周期事实，不调用引擎、不删除历史和工作文件。Web 会话和限流按到期、撤销及数量限制清理；保护临时授权不落库。MCP 最近成功目录按配置/身份/授权指纹失效，失败不伪造在线。

## 归档、保留与自动维护

普通对话默认在主对话及全部子孙会话最后实际活动满 30 天后归档；永久删除默认关闭，明确启用后按归档时间加保留天数计算。标记保留、执行、审批/问题、暂停队列、后台运行和在途修改都会阻止自动处理。消息、执行和收尾更新活动时间，浏览与 SSE 订阅不延期。恢复归档清空归档时钟，并从恢复时重新计算不活跃时间。未知历史时间从策略首次检查起观察，迁移不猜测过去时间。配置与确认规则见[配置](configuration.md#归档与保留)。

明确临时用途的记录完成后可归档，失败未处理时整个子树保留；当前没有创建临时记录、调度或入口绑定的业务 owner。未来启用这些功能前，创建方必须明确用途，持续绑定的目标须先接入删除保护，不能用后台来源推断可以删除。

App 在引擎和既有恢复完成后检查一次，随后每 24 小时检查。任务和目录分别按每页 32 条读取，串行处理；到执行前重验已应用策略、子树和引用，失败留待后续检查。自动删除复用下面的统一入口；不查询引擎列表跟删，也不重放失败任务。关闭先取消维护和生命周期请求，并关闭 HTTP 连接解除慢客户端写入阻塞；等待在途操作退出后，再关闭 Manager、工具、引擎和 Store。

## 统一删除对话

手动删除由用户在界面二次确认后发起；自动删除只依据明确启用的保留策略。两者经 App 同一协调入口执行。只允许删除主对话，范围为该主对话和 parentTaskId 关联的全部子孙会话。独立创建的分支（forkedFrom）保留，即使它与原对话共用工作目录。子会话不提供独立删除入口，避免父对话的委派历史残缺。

删除前检查整个范围：执行已结束、无待审批/问题、无队列（包括暂停队列）、无仍在运行的后台代理及并发原生修改。操作期间阻止该范围的新执行和修改。通过 Docker Agent 的公开 DELETE /api/sessions/{id}?wait=true 接口删除，已知子会话先于父会话处理；引擎负责原生子树和存储媒体的删除。仅明确的会话 GET 404 可以作为已不存在的依据，列表缺失、状态接口 404、断流或其他错误均不能代替。此判断只用于用户已确认的删除操作；普通运行遇到原生会话消失仍保留 Tuck 历史。

原生删除确认后，Store 在一个短事务中删除对应 messages、events、task_deliveries 和 tasks，为最后引用已解除的目录标记 pending，并写入无 task_id 外键的 deleted 事件（仅含被删除 ID），保持事件序号单调。前端收到事件或重新取得快照时清除对应列表、历史缓存及草稿；旧事件补读已删除历史返回 404 时重新读取权威任务快照，避免停留在失效游标。统计随永久删除移除该对话；其他对话的历史、统计、审批、队列和恢复不变。

引擎和 Tuck 库不是联合事务。原生请求失败、停止未确认或本地提交失败时保留 Tuck 记录和文件，返回错误；用户可以重试同一删除，已不存在的原生会话可继续完成本地删除。过程中可能已删除部分原生会话，不承诺自动回滚或可继续原对话，不自动重建引擎历史。

交付 owner 在业务提交后清理对应 snapshots/<task>/<delivery> 下无引用副本，失败由既有启动恢复继续清理。Files 随后按已应用策略处理待回收托管目录；维护批次只在末尾处理一次目录，避免对同一失败项反复尝试。文件清理失败不撤销已成功删除的对话，UI 明示尚有文件未回收。引擎自有媒体由引擎接口负责，Tuck 不读取或清理原生 SQLite。

托管目录先写 allocating 责任，再独占创建并记录系统目录身份；任务提交把它改为 attached。创建中断或原生创建结果不明的 allocating 项保留并展示，不能自动认定无原生使用。已纳管目录最后一个任务引用解除在同一事务改为 pending，重新引用则改回 attached；清理成功后才删除目录记录。关闭回收策略只暂停执行，不丢弃 pending。未纳管目录在最后引用解除时仅移除目录元数据，文件永久保留；已纳管但身份缺失或变化的目录保留记录及错误。状态只是跨数据库/文件系统的最小责任事实，不是任务调度状态机。

目录回收排他准入与任务/文件使用登记共用 Manager 锁；已知同路径、父子路径及别名引用均保护，包括 specified。回收只对稳定身份匹配的目录使用 `os.Root` 删除内容；Windows 比较卷、文件索引和创建时间，Linux 比较设备、inode 和创建时间。不支持稳定身份的文件系统仍可创建任务，但拒绝自动回收。具体模式与外部进程边界见[任务文件](task-files.md#保留、交付与维护)。

入口代理、编排代理和定时任务后续复用任务状态、来源、消息回执和交付事实；需要的调度定义与执行结果由对应业务 owner 管理，不把短期 events 当作调度账本。本次不引入新的代理分类、调度器、冷热库或归档回拉协议。

## 备份与维护规则

工作文件的目录及发布规则见 [任务文件](task-files.md)，不属于业务库或交付存储。

完整备份和回退步骤只维护在 [迁移文档](persistence-migrations.md#完整备份与回退)。运行中的 Tuck SQLite 可用 VACUUM INTO 做一致数据库备份，直接复制活跃主文件不可靠；完整文件一致性仍需停服。不透明引擎资产只在停止引擎后复制，不查询内部 SQLite。

删除事件释放的页供 SQLite 后续复用，不承诺主文件立即缩小。首次升级可在备份、迁移和校验完成后，停服对 Tuck 业务库执行 VACUUM 回收磁盘空间，需预留临时磁盘空间；在线维护不运行全库 VACUUM。

修改 schema、索引、事务、发布、清理或游标时同步本文、迁移约定与测试，结构版本改变前定义支持范围。迁移支持正式 Go v1/v2/v3，不接管封存 Node 格式；配置、数据库、平台密文各有明确版本边界。自动化验证取消、事务失败、一致 DB+快照备份和恢复；真实断电和磁盘故障不由单元测试代替。
