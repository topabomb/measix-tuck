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
