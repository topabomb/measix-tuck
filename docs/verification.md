# 验证指南

本文维护可重复的检查入口和通过条件，不累积单次验收报告。开发环境和启动见 [贡献指南](../CONTRIBUTING.md)，实际部署验收见 [运行与发布](deployment.md#实际服务验收)。

## 前提与命令

使用 Go 1.26、Node.js 22.17+、pnpm 10.15.1，以及未修改的官方 Docker Agent，兼容基线以 [发行清单](../internal/tuck/contract/engine-release.json) 为准。首次执行 `npm run setup`，浏览器测试前执行 `pnpm --dir frontend exec playwright install chromium`。

测试默认从 `tests/runtime/` 读取引擎；npm 的 test、verify、test:e2e 在该处缺失时，从根配置选择 setup 已安装的程序。直接运行 Go 或前端 Playwright 且默认位置没有引擎时，须自行通过测试专用 `TUCK_TEST_ENGINE` 指定已校验程序的绝对路径。该变量仅供夹具，不是产品配置入口。常规测试使用隔离数据和合成模型/Core/MCP；显式启用的 live 视频测试会访问指定真实服务并产生任务及文件写入，范围见 [真实服务与已知限制](#真实服务与已知限制)。

| 检查 | 仓库根目录入口 |
| --- | --- |
| 常规测试 | `npm test`：Go 测试及前端单测 |
| 综合验证 | `npm run verify`：go vet、Go JSON 测试、前端单测/类型/生产构建、Go 夹具上的 Chromium 回归 |
| Go 静态检查 / 测试 | `go vet ./...`；`go test ./...`；不使用旧结果证明本次检查 |
| 竞争检测 | `go test -race -count=1 ./...`；需要支持 CGO/race 的环境，Windows 缺少 C 编译器时使用 Linux/WSL |
| 前端单测 / 构建 | `pnpm --dir frontend test`；`pnpm --dir frontend build` |
| 浏览器 | 先 `npm run build`，再 `npm run test:e2e`；根脚本会构建 Go 测试夹具，默认两个 worker |
| 发行 | `npm run package`；`npm run docker:build`，并实际检查包和镜像的运行行为 |

例如定向验证手机布局与滚动，在构建后执行：

```powershell
node scripts/project.mjs e2e product-navigation.spec.ts mobile-chat.spec.ts composer-bottom-layout.spec.ts scroll-ownership.spec.ts --workers=1
```

核对日志中的实际 worker 数。直接在 frontend 运行 Playwright 不会构建 Go 夹具；使用根脚本可避免运行旧的夹具程序。测试选项和环境入口以 [项目脚本](../scripts/project.mjs)、[Playwright 配置](../frontend/playwright.config.ts) 和 [夹具桥接](../tests/fixtures/bridge.mjs) 为准。

`npm run verify` 重新生成 `.local/go-test.json`，以本次 Go 测试的退出码判断是否通过；不使用历史日志或已移除的提交作为验证前提。现有 Go 回归直接检查当前实现的行为与失败边界。

## 按职责选择回归

| 范围 | 必须关注的行为与现有入口 |
| --- | --- |
| 任务和原生交互 | 创建、助手并发、目录、标题/归档、队列编辑/移除/停止/恢复、fork、立即补充；审批、类型化问题、执行上限、子会话/后台身份、失败和重启收束；`tasks_test.go`、`native*_test.go`、`native_parity_test.go` |
| 安全和配置 | Host/Origin、LAN/代理登录、Cookie/CSRF、退出/改密/过期撤销、分类读取与部分保存、在途授权失效、配置应用门与启动资源；`security*_test.go`、`runtime_settings_test.go`、`backend_review_test.go` |
| 存储和迁移 | 未知库拒绝且不修改，原子联合发布、取消等锁、孤儿清理、冻结 Go v1/v2 DDL 夹具、统计及回执提取、事件保留边界和游标重置、混合状态续迁、WAL 一致备份、错误密钥/DDL/配置保存失败、交付字节、进程占用与崩溃释放、读取上限；`http_store_test.go`、`migrations_test.go`、`storage_retention_test.go`、`replay-reset.spec.ts` |
| 平台和模型 | schema/JCS、快照版本、身份/来源/许可/admit、刷新幂等与失响应、Chat/Responses、大媒体、流式脱敏；`platform*_test.go`、`enterprise_parity_test.go` |
| MCP 和文件 | 原生恢复与共享连接、SSE priming/空体确认、精确目录及权限；图片解码、路径/文件身份、条件写入、取消、未知写入结果；`mcp_test.go`、`local_tools_review_test.go`、`workspace*_test.go`、`files_delivery_test.go` |
| 前端语义 | 身份、当前计划与历史、精确交互关联、拟议调用无虚构状态、子会话独立及父子结果分离、流式增量和中立层边界；[前端单测](../frontend/tests/) |
| 产品页面 | URL 直达/刷新/登录返回/前进后退、筛选分页、历史窗口、交付版本、设置/保护、文件草稿/预览、真实 select 菜单；[浏览器测试](../frontend/e2e/) |

表中的 Go 文件位于 [internal/tuck/](../internal/tuck/)。夹具 JavaScript 只合成外部服务并驱动 Go，不是另一个产品实现。存储变化还须核对 [迁移约定](persistence-migrations.md#后续修改约定) 的支持范围与失败边界。

生命周期使用 `resource_lifecycle_test.go` 验证可注入时间下的到期、主子活动/保留/队列阻止、未知时间观察、恢复重新计时、策略重验、配置热应用及 CAS、v3 事件不重裁、托管目录共享/替换/链接/在途使用、失败责任恢复和关闭等待。`retention.spec.ts` 用官方引擎及合成模型验证确认/取消、保留、归档恢复、续聊、多窗口和 1365/390px；与删除回归一起检查历史和共享文件。Windows 不支持链接时的跳过不能代替 Linux 上实际运行。

`TestMigrationV3OfficialHistoryResumeAndDelete` 用已发布 v3 DDL 和冻结配置构造旧业务库，经离线迁移后核对公开历史不变、同一原生会话恢复和续聊的实际模型输入，再验证统一删除及旧工作文件保留；只通过公开 API 操作官方引擎，不读取其 SQLite。

空闲对象释放用 `TUCK_PERF=1 go test ./internal/tuck -run '^TestIdleTaskHeapRetention$' -count=1 -v -memprofile <本机临时路径>` 对比固定 64 条约 512 KiB payload 的按需读取和完整快照持有内存，并核对空闲缓存为空；列表快照和启动恢复的瞬时内存另计。race 与内存测量分开；还需通过增量计划、统计、审批、队列、原生停止及 MCP 生命周期回归，不能只用 map 长度证明资源已全部释放。

`TestIdleTaskOperationsReleaseWithoutAnotherRead` 检查审批模式成功/失败、清空暂停队列、空闲停止、分支选项及审批响应与原生完成竞争后的释放，不用下一次读取触发清理掩盖收尾遗漏。

`TestLifecycleCloseDrainsHTTPBeforeStore` 使用真实 TCP 连接及等待连接取消的 HTTP 处理函数，验证关闭先释放 HTTP 连接，等待处理函数退出后才关闭 Store。

显式删除覆盖 `task_delete_test.go`、`conversation-delete.test.ts` 和 `conversation-delete.spec.ts`：主子会话范围、独立分支、暂停队列/审批/后台执行拦截、并发修改、原生失败与本地事务回滚后的重试、共享源文件与符号链接保护、删除事件及过期快照同步、迟到响应与默认会话缓存不能复活已删除对话、旧事件补读已删除历史后的快照恢复。浏览器使用官方引擎和合成模型，检查确认/取消、归档与任务频道入口、多窗口同步以及 1365/390px 布局；文件链接测试在支持符号链接的平台运行。

## 附件识别链路

`attachment_inspection_test.go` 核对 Chat/Responses 交叉协议、1–4 图顺序与实际字节、纯文本主模型接收文字、后续轮次和分支、只读子代理独立选择、任意任务内路径、越界链接、平台准入及取消。`tool_revision_test.go` 补充远程输入、无本地目录和绑定变更，`inspection_authorization_test.go` 验证等待模型时撤权或更换绑定后不返回成功。`TestNativeTextOnlyToolImageBehavior` 分别核对公开历史图片和 text-only 模型实际输入。

构建后运行 `node scripts/project.mjs e2e attachment-inspection.spec.ts attachments.spec.ts runcove-tools.spec.ts --workers=1`。检查全局/Agent 选择、纯文本上传与识别、停用、Markdown 正文、简洁错误和原始详情；远程输入缩略图、点击预览、刷新及分支来源；覆盖 1365/390/320px。合成供应商不能证明识别质量，真实验收分别记录供应商请求、结果、上下文和界面。

## 生图链路

`image_generation_test.go` 覆盖资源及保护、标准请求、非法响应、URL 网络边界、平台准入、工具许可、取消和失败发布清理。`TestAgentImageSelection`、`TestImageResourceConfiguration` 核对资源继承及明确失效不回退；平台请求必须使用 size=auto，平台参考图在读取和请求前拒绝。

`TestImageGenerationNativeContext` 使用官方基线和合成供应商，分别验证 Chat/Responses 下 0、1、4 张参考图的 multipart 顺序与原字节，生成图片的原图、提示词、公开历史和后续轮次。通过公开会话 API 验证分支恢复后的实际模型输入，不导入引擎内部包或查询 SQLite。`TestImageHistoryRepeatedCallIDs` 核对调用 ID 复用不串提示词；`TestImageReferenceValidationAndNoFallback` 检查数量、旧参数、路径/链接、重复身份、伪图片、超限和失败不重试/降级。`TestImageCancellationDuringResponseBody` 覆盖响应体取消，`TestImagePlatformRuntimePath` 核对调用前平台路径与快照。

构建后运行 `node scripts/project.mjs e2e image-generation.spec.ts --workers=1`。实际操作设置菜单、全局选择及 Agent 覆盖，保存刷新后核对请求资源；检查四图结果、输入/输出/提示词文件、放大、复制、下载、刷新及分支，覆盖 1365/390/320px。原图与历史模型表示分别核对，不把缩放图标为原图。

真实服务使用已授权的兼容生图资源及 IMAGE 聊天模型，检查实际文件和下一次模型请求。没有资源就记录未执行；合成图片不证明模型理解、真实图片质量或计费。引擎临时补丁的跨工具图片排序回归另见 [补丁记录](../patches/docker-agent/README.md)，不能用 Tuck 基线用例替代其验收。

## 性能与布局门槛

性能测量与 race 分开运行，记录数据集、平台、实际构建和测量方法。以下为验收条件，不是所有部署的延迟承诺。

| 项目 | 检查与通过条件 |
| --- | --- |
| 存储 | `TUCK_PERF=1` 下执行 `go test ./internal/tuck -run '^TestStoreLargeIndexedDataset$' -count=1 -v`；10k 任务/100k 消息/1m 事件，含真实轮次形状和 1 KiB 状态；任务+64 条消息+统计+512 事件组合 p95 ≤ 100ms |
| 大交付 | `TestDeliveryTwoGiBStreaming` 默认实际复制四个 512 MiB 文件，精确核对 2 GiB 快照；每 5ms 采样 RSS，增长 < 256 MiB |
| 工作区并发 | `TestEnterpriseWorkspaceConcurrentLargeRangeControls`：两条 2 GiB Range 流活动期间，projection/list/HEAD/PUT ≤ 2s |
| 长连接 | 原生 MCP/SSE 超过 120s 仍有效；单次操作结束不关闭有效共享连接，流式执行不套固定总寿命 |
| SSE 背压 | 有界回放、600×16 KiB、超过 1 MiB 的单事件、暂停读者与并发发布；游标连续、取消和关闭释放资源 |
| 渲染 | DOM 挂载行 < 180、锚点偏移 < 4px、相邻行重叠及页面横向溢出 ≤ 1px、热运行时 ≤ 3；虚拟包装层 block margin/padding 为 0 |

PowerShell 启用大数据测试可在单独终端执行 `$env:TUCK_PERF='1'` 后运行表中命令；Linux/WSL 可使用 `TUCK_PERF=1 go test ...`。该用例默认跳过，普通测试通过不能算作大数据验收。RSS 采样不等于覆盖所有瞬时峰值，Reference 的百万合成消息只验证渲染规模；产品/Reference 预算见 [前端说明](frontend.md#历史投影与滚动)。

UI 变更按影响核对：

- 320×760、390×760 和 1280/1365px 宽屏；760px 两侧、展开侧栏后窄内容区、长标题/路径/代码、抽屉、摘要内滚动和关闭。
- 空闲、运行、排队、审批、问题、失败、归档及子会话只读；草稿保留、停止和真实模型选择菜单，不能只用 `selectOption()` 代替打开菜单验收。
- 浅色、深色及高对比，焦点、状态名称、系统滚动条；公共主题规则变化时覆盖所有内置主题。
- 流式 Markdown/思考/终端、异步媒体、展开/收起、远跳/补页/Latest、会话切换和宽度重排；使用产品渲染器，不能只验 Reference。
- 文件切换和阅读位置、保存冲突、未保存离开确认、专注退出；短预览视口 320×240，模拟手机可视高度由 760 收缩到 420 后恢复。

布局和滚动可优先运行 `layout-integrity.spec.ts`、`product-density.spec.ts`、`scroll-ownership.spec.ts`、`composer-bottom-layout.spec.ts`、`mobile-chat.spec.ts`、`enterprise-workspace-channel.spec.ts`。实际 Android/iOS 软键盘、输入法、旋转和安全区单独验收；桌面模拟不是实机证据。

## 真实服务与已知限制

真实平台验收限定已授权的测试资源，核对实际 HTTP/工具事件、身份和独立文件字节。登录与健康、模型、MCP、Core Files 和 DAV 分别验证；`accepted`、原生 delivered 或模型自述不能代替目标完成和字节核对。操作失响应先确认结果，不自动重复未知写入。

- Responses、图片输入等需要当前接入环境实际发布对应模型；合成合同测试不能证明外部模型可用。
- [可选大视频测试](../frontend/e2e/enterprise-workspace-live.spec.ts) 仅在同时设置 `TUCK_LIVE_VIDEO` 和 `TUCK_LIVE_URL` 时启用，使用指定服务及其已接入的真实 Core 工作区。它会创建真实任务、上传视频、读取及播放，并在结束时尝试删除本轮专用远端目录；任务记录保留，清理失败需单独核对。启用前确认服务、账号和测试资源的授权；未启用时记录跳过，小 MP4 成功不能替代它。
- 条件读取失败时，以同一对象比较 Tuck、Core 和 DAV 的普通 GET、HEAD、条件 GET、Range，核对状态、ETag、长度、请求 ID 与耗时；静态代码或单次 502 不能确定故障归属。
- 官方引擎缓存与故障夹具不能证明公网下载通路；交叉编译不能证明 ARM64 实机行为；单元测试和冷备恢复不能证明真实断电或坏盘恢复。

每次验证随变更记录提交/构建、命令、环境、失败和未执行项。日志、截图和 trace 保存在 `.local/`、测试临时目录或 `release/`，不加入 Git；不要把它们或当前部署地址、真实凭据写回长期指南。

## 模型可见内容与统一 MCP 的验收

| 回归入口 | 核对范围 |
| --- | --- |
| `TestTuckToolNamesOfficial`、`TestUnifiedMCPInstructionsAfterApply` | Chat/Responses 实际名称、空/单/部分/全工具组合、配置应用及续聊后的说明 |
| `TestUnifiedMCPActorPermissions`、`TestLocalMCPTaskTurnOwnership` | 主/子代理、只读、任务及轮次许可 |
| `TestLocalMCPCatalogCookieTurnAndPeerLifecycle`、`TestLocalMCPReconnectInventory`、`TestLocalMCPSecondCatalogClientAcrossTurns` | 多客户端、跨轮次目录、过期/缺失/其他任务凭据、共享连接释放 |
| `TestConversationToolResultMCP`、`TestConversationToolUnavailableTargetModelMCP`、`TestCollaborationSubmissionUnconfirmed`、`TestSessionMessageUnicodeLength` | 列表到发送的 conversation_id 合同、旧参数拒绝、接收与目标完成的区别、失效目标、未知提交、Unicode 长度及单次发送 |
| `TestModelContentMCPResult`、`TestToolTextResources`、`TestOfficialNativeFullLoop` | 内容组合、资源解析、参数说明、模型图片、审批拒绝原因进入实际结果 |
| `TestDeliveryRequiresExplicitRootCompletion`、`TestParityNativeDeliveryContinuation` | 正常根终止及反例、开关变化、每轮一次且不自触发 |

结合 [架构](architecture.md#模型可见内容的唯一入口) 核对调用路径：每 Agent 最多一个 Tuck MCP toolset，内容统一经入口，用户及外部合同保持来源。抓取实际模型请求验证 instructions 为字符串、仅列最终工具集合且顺序稳定；不能只凭 tools/list_changed 或资源存在推断其已进入模型请求。核对过期决定拒绝、取消传播和共享连接，不增加应用重试或第二队列。

自动交付同时核对根会话 ID、正常终止原因、缺失或陌生身份、子会话、EOF、取消、错误和输出/步数上限；浏览器 `delivery-versions.spec.ts` 核对请求次数、提示条数和刷新后的事实，不能通过隐藏历史掩盖重复执行。

## 工具结果与文件访问

`tool_results_test.go` 核对七工具输出字段、英文错误、空字段省略、HTTP 分类及秘密过滤、部分下载、未知写入和预览身份歧义；`tool_revision_test.go` 核对参数说明、列表数量、远程快照及上下文。`tool-ui-regressions.spec.ts` 检查剪贴板降级复制、资源标识更名、受保护设置及实际结果呈现。

`TestNoneWorkspaceRejectsTuckWrites` 验证无目录时上传、编辑、下载和生图拒绝，`TestArtifactDirectoryLinksAndDownloadScope` 验证固定目录链接、下载范围及返回路径。`workspace_download_integrity_test.go` 核对正常 EOF 下截短、超长及 ETag 不一致均不发布、不覆盖旧文件。失败发布要证明本次原图和提示词无残留，共享目录其他文件不变。远程交付后修改源文件，再下载旧快照核对原字节。

文件传输测试分别覆盖请求体发完后的延迟确认、持续进展、连续空闲、关闭请求体后继续等响应，以及取消后下一操作仍可用。阶段时限以 [配置](configuration.md#超时与取消) 为准，不新增写入重试或长连接总寿命。

真实七工具验收用独立测试对话及远程目录，记录实际模型、资源来源和逐项结果。按变更范围覆盖目录模式、默认/边界数量、非法路径、覆盖冲突、部分下载、参考图、资源覆盖和工具停用；文件核对字节，页面查看预览、复制、刷新及窄屏。取消须观察调用已开始且尚无结果，再检查中断、清理及续轮；自动交付核对终止事实和一次补充上限。结束后恢复临时配置；不稳定竞态、超时及畸形上游响应由隔离测试覆盖，分别注明证据来源。
