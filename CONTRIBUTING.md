# 贡献指南

## 开发环境

先阅读 [工作约定](AGENTS.md)。需要 Go 1.26、Node.js 22.17+、pnpm 10.15.1，版本以 [go.mod](go.mod)、[根 package.json](package.json) 和 [前端 package.json](frontend/package.json) 为准。首次安装见 [快速开始](README.md#快速开始)。

在仓库根目录执行：

```powershell
npm run dev
```

该命令构建前后端并启动服务，不提供 Go 自动重载。前端热更新需保持 Go 服务运行，在另一终端执行 `pnpm --dir frontend dev`；打开 Vite 输出的地址，`/api` 代理到本机 8090。后端变更后重启服务。

## 代码与职责

| 路径 | 职责 |
| --- | --- |
| [cmd/tuck/](cmd/tuck/) | 程序入口 |
| [internal/tuck/](internal/tuck/) | Go 服务，按配置、平台、任务、文件、引擎等 owner 分文件 |
| [internal/tuck/contract/](internal/tuck/contract/) | 默认配置、官方引擎清单、平台合同及编号 DDL |
| [frontend/src/](frontend/src/) | Vue 产品页面、共用组件、渲染 Engine 和隔离的参考演练 |
| [frontend/tests/](frontend/tests/)、[frontend/e2e/](frontend/e2e/) | 前端单测与浏览器测试 |
| [tests/](tests/) | 合同和合成平台/模型夹具，不提供产品后端 |
| [scripts/](scripts/) | 构建、测试和打包编排 |

## 检查与提交

```powershell
npm test
npm run verify
```

`test` 执行 Go 和前端单测；`verify` 还执行 Go 静态检查、前端类型与生产构建、Go 夹具上的浏览器回归。首次浏览器测试前执行 `pnpm --dir frontend exec playwright install chromium`。定向测试、性能门槛和真实服务验收边界见 [验证指南](docs/verification.md)。

变更说明写清问题、最终行为、实际检查和未验证边界。行为变化补充有意义的回归；纯文档变更检查源码、命令、链接、锚点、示例及 `git diff --check`。UI 变更还需查看实际浏览器的宽窄布局。

配置、凭据、数据库、附件、工作目录、交付快照和构建测试产物不进 Git，自定义数据目录也须排除。个人生产运维资料放在已忽略的 `.production/`；不要用通用清理命令删除运行数据。发布前检查待推送内容及历史中的隐私，已公开的敏感数据按 [GitHub 清理说明](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository) 处理。

## 文档维护

每个主题只维护一处正文，其他文档必要时链接。参数、DDL、版本和文案以实际配置或源码为准，不逐字复制完整副本。当前正文使用中文，不建立翻译占位、平行文档站点或空的规划文件。

| 文档 | 负责的内容 |
| --- | --- |
| [README](README.md) | 项目介绍、界面、使用场景和快速开始 |
| [AGENTS](AGENTS.md) | 简短工作约定 |
| [架构](docs/architecture.md) | 所有权、引擎边界、模型内容、MCP 与上下文编排 |
| [配置](docs/configuration.md) | 配置项、资源选择、保存应用、密码与超时 |
| [部署](docs/deployment.md) | 安装、发行、升级和实际服务验收 |
| [存储设计](docs/database-design.md) | DDL、事务、游标、数据所有权与清理 |
| [迁移](docs/persistence-migrations.md) | 版本、离线迁移、完整备份与回退 |
| [工具参考](docs/tools-reference.md) | 工具输入、调用行为、权限和动态工具发现 |
| [工具返回合同](docs/tool-results.md) | Tuck 工具输出字段、状态与错误码 |
| [任务文件](docs/task-files.md) | 文件布局、命名、落盘与交付副本边界 |
| [附件识别](docs/attachment-inspection.md) | 识别请求、来源身份和模型上下文 |
| [前端](docs/frontend.md) | 渲染、状态、布局、来源与依赖补丁 |
| [验证](docs/verification.md) | 测试入口、验收方法和性能门槛 |
| [引擎补丁](patches/docker-agent/README.md) | 临时补丁规则、当前补丁证据与退出条件 |

现行文档描述已实现行为和必要限制。临时调查、一次性验收记录和已完成方案不作为长期指南保留；仍有效的决定归入上述正文。需要评审的集中方案须标明未实现范围，实施后合并并移除。项目许可由维护者决定。

发行包的简要运行说明由 [scripts/project.mjs](scripts/project.mjs) 生成，CLI 或升级步骤变化时同步其中独立运行所需的说明。

README 媒体放在 `docs/assets/readme/`，产品职责图维护 SVG。安装依赖和官方引擎、构建前端，准备 Playwright Chromium 与 PATH 中的 FFmpeg 后，运行 `node scripts/capture-readme.mjs` 重新采集。脚本使用隔离 Go 夹具和合成数据，原始录像留在已忽略的 `.local/readme-showcase/`。界面变化时刷新图片与说明，发布前逐张检查截图、录屏及体积；夹具演示不能代替真实接入验收。
