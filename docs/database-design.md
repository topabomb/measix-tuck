# 存储设计与维护

## 所有权与结构

Tuck 只拥有 <storage.dataDir>/tuck.db 一个业务 SQLite，Store 持有短写事务，编号 DDL 由 storage_schema.go 嵌入。application_id=0x5455434b、user_version=2；完整实际结构也须匹配。正常启动只初始化空库或打开当前格式；正式 Go v1 使用显式离线迁移，未知或封存结构拒绝且不修改。支持路径、数据版本、备份及中断恢复见 [持久化版本与迁移](persistence-migrations.md)。

| 表 | 关键约束与职责 |
| --- | --- |
| tasks | id 主键，created_at_ms/status/archived/native_session_id 类型列、object payload_json；产品状态与元数据 |
| messages | task_id 外键、非负 position/revision、object JSON；主键(task_id,position)，规范化消息 |
| events | AUTOINCREMENT seq、可空 task_id 外键、type、created_at_ms、object JSON；可靠事件游标 |
| task_deliveries | id 主键、唯一(task_id,version)、turn_id、时间、摘要、file_count、total_bytes、files_json |
| web_sessions | token_hash、identity、csrf、expires_at；只存令牌哈希 |
| login_limits | key、failures、until_at、expires_at；账户/IP 持久限流 |
| platform_session | id=1、installation_id、encrypted_payload、format_version=2；平台绑定、快照、刷新键和 applied 回执；密文认证绑定安装身份 |
| mcp_catalog | resource_key、fingerprint、最多 4 MiB 的数组 catalog_json、verified_at_ms；最近成功验证，非实时连接 |

完整可执行 DDL 以 [001.sql](../internal/tuck/contract/storage/001.sql) 和 [002.sql](../internal/tuck/contract/storage/002.sql) 的顺序执行为唯一来源，不复制第二套最新 SQL。已发布步骤不修改。task_listing 支持归档/创建时间；消息主键支持分页；task_events(task_id,type,seq) 支持恢复与类型查询；delivery_turn 支持轮次交付定位。

轮次统计从事件原始 turnTiming 自动生成 turn_id / turn_started_ms / turn_end_ms / turn_incomplete STORED 列。event_turns 是只覆盖 created/state 且有 turn_id 的部分索引，直接分组统计，不新增统计表、缓存 owner 或后台聚合器。工具数仍从规范化消息块查询，原 JSON 是事实来源。

附件识别将远程预览所需的非秘密身份事实存于既有 messages 的 tool-call presentation.workspace；模型结果只保存识别文字。Fork 从父对话明确匹配的调用继承，缺失或冲突则不推测。没有新表、索引或引擎存储依赖，也不改写已发布 DDL。模型可见返回与产品元数据的边界见[工具返回合同](tool-results.md)。

SQLite 使用 WAL、synchronous=FULL、foreign_keys=ON、busy_timeout=5000，最多四连接和一个写互斥 owner。文件名先做 URI 转义再附加 SQLite 参数。事务使用请求上下文，等待写锁后取消不能提交。数据库和文件用本机磁盘，一个数据目录只由一个 Tuck 服务使用；服务及离线维护以操作系统排他锁约束配置和数据目录，崩溃自动释放，锁文件不手动删除。

## 提交、游标与恢复

任务、消息及事件在一个短事务提交，成功后广播。HTTP 读取副本，消息位置不随归档改变。工作台补页最多 64 条、前端窗口 128 条；内部历史读取默认 2048/最多 4096 条。事件按 seq 和有界页发送，慢客户端只持游标，网络/模型/文件 IO 不进入写事务。

列表、任务和消息快照先取得重放游标，再读取数据，游标是重放下界。并发发布可能同时出现在快照和后续事件中，前端按事件 seq、消息 index/revision 处理重复；不能在读取旧数据后附上更新的游标而跳过变化。快照读取不为此持有覆盖历史解码或网络发送的全局锁。

当前事件持续保留，统计、补充回执及恢复仍依赖事件，不能直接删除 native/state。以后裁剪必须先定义统计替代事实及过期游标快照合同。

Tuck 历史不能完整重建原生上下文。引擎会话存储由官方 Docker Agent 独立管理，Tuck 不读内部 schema。runtime/agent.yaml 是可重建输入，平台秘密不进入其中；具体资产路径由 engine.go 的启动参数确定。工作文件在 workspaceRoot 或指定目录，交付快照在 `<storage.dataDir>/deliveries/`。

交付快照是与业务库配套的运行数据，不提交Git。默认 `/data/` 和仓库根 `/deliveries/` 已排除；将数据目录或工作目录设在仓库其他位置时，也须排除相应运行数据。Git忽略不改变交付保留规则或完整备份范围。

重启只收束未结束任务及其仍为 live 的消息、暂停用户队列、清理自动续轮和旧审批，不执行工具。终止或断流后未取得结果的工具输出标为 interrupted，子会话及父会话的委派展示同步收束，不补造成功结果。恢复补充消息回执时只保留相关消息；仅存在未确认补充时，才按现有 task_events 索引查询对应游标后的 user_message 原生事件，不解码整段工具事件历史。导入公开原生历史修复规范化元数据时保留位置、调用身份与实际结果，重复修复幂等。

## 交付与清理

本地文件和已授权 Runcove 文件均先做独立文件快照、fsync、目录原子发布，再提交版本和最新状态；每成功调用新版本，同任务串行。失败取消回收暂存和未提交快照，源文件不变。数据库与文件系统不是联合原子事务，启动按已提交引用恢复。

文件限制 10 件、每件 512 MiB、合计 2 GiB，另预留 256 MiB 空间；摘要 1000 Unicode 字符。只接受可读普通文件，拒绝目录、逃逸及重复路径/同一文件身份。旧版本从快照读取，不回读源文件。

裁剪旧记录和新版本同事务，成功后清理无引用快照；保留数 0 仍留下最新版本。清理失败不撤销成功版本，交付 owner 内部记录 cleanup_pending，模型可见结果不包含维护状态；启动和下次成功交付继续清理。只删除 owner 下匹配公开 ID 且确认无引用的目录，不处理其他文件。

任务/消息/工作目录持续保留，归档不删除。Web 会话和限流按到期、撤销及数量限制清理；保护临时授权不落库。MCP 最近成功目录按配置/身份/授权指纹失效，失败不伪造在线。

## 备份与维护规则

任务内上传、生图及下载按 [任务文件与产物目录规范](task-files.md) 保存，复用现有 uploads/artifacts 与媒体 provenance 事实，不新增生成任务表。Files 独占创建原图及提示词，发布失败只清理本次文件；不将供应商临时 URL 当作永久文件，不改写历史路径或编号 DDL。

停服后备份配置、完整 dataDir 和外部工作目录，包括 Tuck 库、快照、工作文件、credentialKey 和不透明引擎数据。运行中 Tuck SQLite 可用 VACUUM INTO 一致备份；直接复制活跃主文件不可靠。需要完整文件一致性时先停服。引擎数据只在停止引擎后按不透明文件复制，不查询其 SQLite。

恢复后检查 Tuck quick_check/foreign_key_check、任务/消息数量、交付版本和文件字节，再用公开 API 检查原生会话。配置密钥须与加密平台绑定匹配。引擎升级回退恢复匹配的不透明会话资产，不能只换旧程序。

修改 schema、索引、事务、发布、清理或游标时同步本文、迁移约定与测试，结构版本改变前定义支持范围。迁移支持正式 Go v1，不接管封存 Node 格式；配置、数据库、平台密文各有明确版本边界。自动化验证取消、事务失败、一致 DB+快照备份和恢复；真实断电和磁盘故障不由单元测试代替。

远程交付沿用既有快照文件和 files_json 字段，不保存可变远程下载链接。工具输入中的 source/path 属于原生公开工具消息正文，保留原值；远程识别预览所需的非秘密工作区身份事实保存在 Tuck messages 的 tool-call presentation.workspace，不进入识别结果。浏览器预览按此身份重新检查授权，不另建身份映射表或重解释旧记录。
