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
| 存储和迁移 | 未知库拒绝且不修改，原子联合发布、取消等锁、孤儿清理、冻结 Go v1 夹具、混合状态续迁、WAL 一致备份、错误密钥/DDL/配置保存失败、交付字节、进程占用与崩溃释放、读取上限；`http_store_test.go`、`migrations_test.go` |
| 平台和模型 | schema/JCS、快照版本、身份/来源/许可/admit、刷新幂等与失响应、Chat/Responses、大媒体、流式脱敏；`platform*_test.go`、`enterprise_parity_test.go` |
| MCP 和文件 | 原生恢复与共享连接、SSE priming/空体确认、精确目录及权限；图片解码、路径/文件身份、条件写入、取消、未知写入结果；`mcp_test.go`、`local_tools_review_test.go`、`workspace*_test.go`、`files_delivery_test.go` |
| 前端语义 | 身份、当前计划与历史、精确交互关联、拟议调用无虚构状态、子会话独立及父子结果分离、流式增量和中立层边界；[前端单测](../frontend/tests/) |
| 产品页面 | URL 直达/刷新/登录返回/前进后退、筛选分页、历史窗口、交付版本、设置/保护、文件草稿/预览、真实 select 菜单；[浏览器测试](../frontend/e2e/) |

表中的 Go 文件位于 [internal/tuck/](../internal/tuck/)。夹具 JavaScript 只合成外部服务并驱动 Go，不是另一个产品实现。存储变化还须核对 [迁移约定](persistence-migrations.md#后续修改约定) 的支持范围与失败边界。

## 附件识别链路

附件识别另由 `attachment_inspection_test.go` 验证 Chat/Responses 交叉协议、多图顺序及实际字节、纯文本主模型接收文字、后续上下文和分支、只读子代理独立选择及上游错误回到 Agent。覆盖任务内任意路径、越界链接、平台默认/准入、许可与取消。`TestNativeTextOnlyToolImageBehavior` 验证原生 text-only 请求不带工具图片，而公开历史仍保留图片。

构建后运行 `node scripts/project.mjs e2e attachment-inspection.spec.ts attachments.spec.ts --workers=1`，检查全局/Agent 配置、上传与识别、停用、失败提示、历史刷新和 1365/390/320px；截图在 `.local/attachment-inspection/`。协议与验收边界见[附件识别](attachment-inspection.md)。

## 生图链路

`image_generation_test.go` 覆盖资源配置及保护、协议请求、无效响应、图片 URL 网络边界、平台准入、工具许可、取消、发布失败清理、原图字节和官方引擎的 Chat/Responses 图片输入。`TestImageGenerationNativeContext` 使用未修改的官方程序与合成供应商，逐字节检查工具返回后的模型请求及后续轮次中的图片表示，并核对规范化媒体和 HTTP 返回内容；不导入引擎内部包或查询其数据库。

构建前端后运行 `node scripts/project.mjs e2e image-generation.spec.ts --workers=1`。通过实际设置页添加资源、操作默认下拉菜单、保存和刷新，再发起生图任务；核对工具图片、放大预览、刷新后的历史、原图及配套提示词下载、对话与产物预览中的提示词复制，以及 320px、390px 和宽屏的溢出与图片解码。还验证 `none` 模式禁用附件入口且直接 API 写入被拒绝。截图在 `.local/image-generation/`，逐张查看实际呈现。合成供应商只验证线协议与传递链，不证明真实外部供应商的图片质量、模型理解效果或计费。

目录规范见 [任务文件](task-files.md)。`TestNoneWorkspaceRejectsTuckWrites` 覆盖上传、编辑、下载、生图的无目录拒绝和工具目录筛选；`TestArtifactDirectoryLinksAndDownloadScope` 覆盖固定目录链接、下载子目录越界和返回路径合同。DAV 原生调用、覆盖、取消及中途目录重定向继续由 workspace/native parity 测试验收。失败发布须证明原图与提示词均不残留，且共享目录内其他文件不变。

`TestImageCancellationDuringResponseBody` 覆盖成功/失败响应体读取中的取消，保持取消分类；`TestImagePlatformRuntimePath` 覆盖平台生图运行路径的快照及调用前校验。预览须区分对话中的模型表示与任务文件中的原图，不能把缩放后的历史图片标作原图。

`TestImageGenerationNativeContext` 在 Chat/Responses 下分别验证 0、1、4 张参考图；多参考图逐字节及按顺序核对 multipart image[]，四张结果检查原图、提示词、公开历史媒体和后续轮次上下文。分支通过官方会话 API 的 multi_content 恢复图片，验证分支媒体 HTTP 字节、提示词及继续对话的模型上下文；`TestImageHistoryRepeatedCallIDs` 检查重复调用 ID 不串提示词。`TestImageReferenceValidationAndNoFallback` 验证非法数量/旧参数、路径越界及目录链接、重复文件身份、伪图片/超限、取消，以及供应商拒绝后不切回纯文字、不重试、不发布文件。浏览器参考图用例验证四张生成结果、刷新后的十二个文件（四张输入、四张输出及四份提示词）、三种宽度的实际预览，以及创建分支后的图片、提示词复制与刷新保留。

真实服务验收另用已授权的 OpenAI 兼容生图资源和支持 IMAGE 输入的聊天模型，检查实际生成文件及下一次请求；无对应资源时明确记录未执行，不能把合成图片称为真实 AI 生图。

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
- 曾在 2026-10-15 的外部部署观察到匹配强 ETag 的 `If-None-Match` 文件 GET 返回 502，直连 Core 也如此。Tuck 的 304/空体/ETag 转发有合同测试；此记录不代表当前部署仍有同样故障，部署验收需复核，不加静默回退。
- 官方引擎缓存与故障夹具不能证明公网下载通路；交叉编译不能证明 ARM64 实机行为；单元测试和冷备恢复不能证明真实断电或坏盘恢复。

每次验证随变更记录提交/构建、命令、环境、失败和未执行项。日志、截图和 trace 保存在 `.local/`、测试临时目录或 `release/`，不加入 Git；不要把它们或当前部署地址、真实凭据写回长期指南。

## 模型可见内容与统一 MCP 的验收

对应回归入口：`TestTuckToolNamesOfficial` 覆盖 Chat/Responses 工具组合和续聊；`TestUnifiedMCPInstructionsAfterApply` 覆盖已有会话应用配置后的工具与说明；`TestUnifiedMCPActorPermissions`、`TestLocalMCPTaskTurnOwnership` 和 `TestLocalMCPCatalogCookieTurnAndPeerLifecycle` 覆盖调用者、轮次、目录变化及共享连接；`TestLocalMCPReconnectInventory` 验证保留旧订阅后，新初始化仍说明当前目录；`TestSessionToolResultMCP` 和 `TestSessionToolUnavailableTargetModelMCP` 覆盖 accepted 及目标模型失效；`TestCollaborationSubmissionUnconfirmed` 验证实际提交后结果未确认、前置拒绝与单次发送；`TestModelContentMCPResult` 验证文字和模型图片统一披露；`TestOfficialNativeFullLoop` 检查审批拒绝原因实际进入模型工具结果。自动交付继续由 tasks 与 native parity 的交付续轮、开关、暂停、子代理和审批测试验证。

`TestLocalMCPSecondCatalogClientAcrossTurns` 另验证两个独立 Cookie Jar 跨三轮重连，以及过期、缺失和其他任务执行凭据的拒绝；目录 Cookie 只定位共享目录，不授予工具执行权。

`TestSessionMessageUnicodeLength` 验证中文和 emoji 按 Unicode 字符计数，并核对协作标记加入后的完整消息上限；超限输入不能进入目标队列。

架构边界以 [唯一内容入口](architecture.md#模型可见内容的唯一入口) 和 [Tuck MCP 工具集](architecture.md#tuck-原生工具集与-mcp) 为准。实现验收必须覆盖以下行为，不能仅以已有描述资源测试代替：

- 每个 Agent 的原生配置最多一个 Tuck MCP toolset，没有同名 Tuck API 工具装配；统一服务不扩大主/子代理、只读和工具开关的许可范围，外部 MCP 保持原合同。
- 空集、单工具、部分工具和七工具组合：instructions 是字符串，名称与对应装配集合完全一致且顺序稳定；空集省略，单工具语法正确，不带 `etc.`、其他来源工具、工具描述全文或额外执行规则。
- 使用官方引擎抓取 Chat/Responses 实际模型请求，核对初始化说明和工具定义；应用启停配置后的重新装配取得新说明，普通续轮不重复追加说明。单独收到 tools/list_changed 不能作为 instructions 已更新的证据。
- 默认指令、附件说明和图片标签、协作标记、独立识图提示词、自动交付消息及结果投影均从 model_content 入口取得；调用者不绕过入口调用子模块，用户正文和外部合同不被当作自有模板改写。
- 自动补交付策略根据正常根终止、本轮未交付及开关事实提出请求，Manager 发送前复核关联轮次、开关及许可，最多一次且不触发自身；运行中关闭清理未执行自动项，保留自动来源和真实用户消息的区别，不把 requireDelivery 固化到初始化说明。策略不直接修改任务或队列，Manager 不内嵌提示词和业务策略。
- 跨会话消息在 working 时走原生 steer，空闲时经 Submit 排队并遵守暂停，等待审批/回答时拒绝；同一请求只进入一个执行队列。accepted、已进入原生上下文及已完成分别依据对应事实验证，不能互相替代。
- 模型结果只披露合同字段，错误和警告不包含界面身份、凭据或内部诊断；生图和识图建议不引用已禁用工具，实际图片及顺序、后续轮次和分支历史继续验证。
- 任务和调用者并发隔离，过期轮次、权限或绑定变化时拒绝；取消沿原操作传播，不关闭仍有效的共享连接，也不新增轮询、应用重试或轮次重连机制。

后续接入首次准备、模型审查或原生 followup 时，另验收 [编排合同](architecture.md#上下文披露与编排)：初始工具指导与实际工具集合一致，普通续聊、恢复和分支不重复生成已有背景；明确原生 session_start、turn_start 与产品会话/轮次的差异；审查材料来自对应轮次，失败不变成业务结论，迟到结果不误用于新轮次；新策略替换旧策略且不双重触发。原生队列接管须验证现有编辑、暂停、排序、恢复及逐轮授权，接管后删除对应执行适配。上述后续能力不属于本期完成声明，静态配置和模型模拟响应不能替代实际链路验收。

## 工具描述及 Runcove 扩展

`tool_revision_test.go` 验证描述资源、所有参数说明、列表数量、远程快照和识别上下文；`tool_names_test.go` 使用官方引擎验证七个无前缀名称。`runcove-tools.spec.ts` 覆盖无本地目录下远程图片输入的 1365/390/320px 展示、点击预览、刷新和交付后远端修改仍可下载旧副本。平台生图测试断言 size=auto；参考图明确拒绝且不触发模型请求。以上隔离验证与真实平台部署验收分别记录。

`tool_results_test.go` 验证七工具的精简模型投影、英文错误、无值字段省略、上游分类和秘密过滤、响应体超时、会话工具结果、部分下载及分支预览身份歧义。`TestParityNativeDeliveryContinuation` 使用官方引擎验证自动补交付不能再次触发自动补交付，真实用户输入仍有独立的一次机会；浏览器 `delivery-versions.spec.ts` 核对实际请求次数、提示条数和刷新后的展示，不通过隐藏历史消息掩盖重复执行。

生图资源覆盖由 TestAgentImageSelection、TestImageResourceConfiguration 和 TestNativeTextOnlyToolImageBehavior 验证继承、失效不回退、配置保护及官方引擎实际调用；image-generation.spec.ts 通过设置页面选择 Agent 覆盖、刷新、生成图片、恢复全局默认，核对请求模型和工具描述，并检查 1365/390/320px 呈现。

TestDeliveryRequiresExplicitRootCompletion 验证正常根终止、未知/空原因、缺失/陌生/其他主会话身份、已知子会话结束、工具结果、EOF、取消、步数/输出上限、模型错误，以及运行中开关变化。TestParityNativeDeliveryContinuation 同时核对官方公开终止事件的根会话 ID、reason=normal 和每轮一次事实。

七工具独立审查的回归：`workspace_download_integrity_test.go` 验证正常 EOF 下截短、超长和 ETag 不一致的下载均不发布、不覆盖旧文件；`inspection_authorization_test.go` 验证 Chat/Responses 模型等待期间远程绑定变化或撤权后不返回成功。`tool-ui-regressions.spec.ts` 验证弹窗内剪贴板降级复制的实际内容、资源 ID 更名不串改引用及受保护配置；`attachment-inspection.spec.ts` 检查识别正文的 Markdown、简洁错误与原始详情，以及三种宽度下的输入和结果展示。

真实七工具验收应使用独立测试对话及远程目录，记录实际模型、资源来源和逐项工具返回，不以模型自述代替执行证据。覆盖自动/指定/无目录、默认及边界数量、非法路径和类型、覆盖冲突与显式覆盖、部分下载、参考图、Agent 资源覆盖和工具关闭。上传/下载核对字节，交付后修改源文件并重新下载旧快照；原图与提示词逐份核对，查看输入缩略图、预览、复制、刷新及窄屏。取消需观察到调用已开始且尚无结果，再检查中断、无残留及后续轮次恢复；自动补交付核对正常结束事实、两轮上限和一条提示。结束后恢复临时配置。真实环境不能稳定触发的超时、撤权竞态、未知写入结果和畸形上游响应仍由隔离测试验证，分别标明证据来源。
