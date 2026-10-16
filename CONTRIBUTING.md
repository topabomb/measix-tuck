# 贡献指南

## 开发环境

需要 Go 1.26、Node.js 22.17+、pnpm 10.15.1。版本约束分别以 [go.mod](go.mod)、[根 package.json](package.json) 和 [前端 package.json](frontend/package.json) 为准。首次安装及完整服务启动见 [README](README.md#快速开始)。

日常开发在仓库根目录执行：

```powershell
npm run dev
```

该命令构建前后端并启动服务，不提供 Go 自动重载。需要前端热更新时，保持 Go 服务运行，在另一终端执行：

```powershell
pnpm --dir frontend dev
```

打开 Vite 输出的地址，`/api` 代理到本机 8090。后端变更后重启服务；前端也可单独执行 `pnpm --dir frontend install --frozen-lockfile`、`test` 或 `build`。

## 代码与职责

| 路径 | 职责 |
| --- | --- |
| [cmd/tuck/](cmd/tuck/) | 程序入口 |
| [internal/tuck/](internal/tuck/) | Go 服务；按配置、平台、任务、文件、引擎等 owner 分文件 |
| [internal/tuck/contract/](internal/tuck/contract/) | 默认配置、官方引擎清单、平台合同及编号 DDL |
| [frontend/src/](frontend/src/) | Vue 产品页面、共用组件、渲染 Engine 和隔离的参考演练 |
| [frontend/tests/](frontend/tests/)、[frontend/e2e/](frontend/e2e/) | 前端单测与浏览器测试 |
| [tests/](tests/) | 合同和合成平台/模型夹具；不提供产品后端 |
| [scripts/](scripts/) | 构建、测试和打包编排 |
| [docs/](docs/) | 当前版本的核心文档 |

先阅读 [AGENTS.md](AGENTS.md) 和 [架构](docs/architecture.md)。Docker Agent 是黑盒运行时，只使用官方程序和公开配置、CLI、HTTP/SSE，不修改或编译定制引擎，不读取其 SQLite 内部表。新增适配先核对实际兼容版本的公开能力，记录缺口与退出条件。

修改存储、事务、游标或恢复前，阅读 [存储设计](docs/database-design.md)、其链接的实际 DDL 和 [迁移约定](docs/persistence-migrations.md)。已发布编号 SQL 不改写；结构或数据解释变化追加显式迁移，覆盖失败和续迁。不要在普通启动中猜测旧格式或重置密钥。

## 检查与提交

```powershell
npm test
npm run verify
```

`test` 执行 Go 和前端单测；`verify` 还执行 Go 静态检查、前端类型与生产构建、Go 夹具上的浏览器回归。首次浏览器测试前执行 `pnpm --dir frontend exec playwright install chromium`。定向测试、竞争检测和性能检查见 [验证指南](docs/verification.md)。

变更说明写清具体问题、最终行为、实际执行的检查及尚未验证的边界。行为变化补充有意义的回归；纯文档变更核对源码、命令、链接、锚点、示例和 `git diff --check`，无需重跑完整产品测试。UI 变更还需查看实际浏览器中的宽窄布局，自动检查不能代替真机或真实企业服务验收。

配置、密码、Token、数据库、附件、工作目录、交付快照、`.local/`、发行包和测试产物不进 Git。自定义数据目录也要排除；不要用通用清理命令删除运行数据。个人生产运维资料放在已忽略的 `.production/`，具体维护边界见 [部署文档](docs/deployment.md#实际服务验收)。

公开仓库前同时检查待推送分支和标签的历史、提交说明、作者邮箱及历史截图；只删除当前文件不能清除旧提交中的隐私。首次发布采用全新历史时，旧 Git 数据仅保存在已忽略的私密备份中，构建和测试不得依赖已移除的提交。只推送已检查的分支，不使用镜像推送；已公开的敏感数据按 [GitHub 清理说明](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository) 处理。

## 文档维护

README 是唯一总入口，按读者任务导航。每个主题只维护一处正文，其他文档链接引用；参数、DDL、版本和常量优先链接实际来源，不维护第二份完整副本。前端也使用根文档目录，不再保留平行的架构、验证和样式说明入口。

发行包内的简要运行说明由 [scripts/project.mjs](scripts/project.mjs) 生成。CLI 或升级步骤变化时同步这段文字，只保留独立运行所需的说明，不复制整套指南。

现行文档描述已实现行为和必要限制。临时调查、一次性验收计数、机器性能实测和已完成方案不作为长期文档保留；有持续价值的约束归入对应指南，其余移除。构建、验证和长期指南以仓库当前文件为准，不依赖已封存的演示或历史提交。

确有跨模块设计决策需要评审时，可暂存一份集中方案，标明尚未实现的范围；实施后将仍有效的决定合入核心文档并移除方案。不要为目录完整而增加空的路线图、FAQ、变更日志或专门的文档站点。项目许可须由维护者决定，不能在文档整理中自行指定。

README 的截图与操作演示放在 `docs/assets/readme/`，七产品职责图直接维护 SVG。安装开发依赖和官方引擎、构建前端后，准备 Playwright Chromium 与 PATH 中的 FFmpeg，运行 `node scripts/capture-readme.mjs` 可重新采集；脚本编译 Go 测试夹具，启动隔离工作台并使用合成会话和企业文件响应，不调用真实模型或企业服务。运行数据与原始录像留在已忽略的 `.local/readme-showcase/`。发布前逐张查看宽窄截图、检查录屏和文件体积，不把夹具演示当作真实接入验收；界面能力或操作入口变化时同步刷新图片与说明。

### 中英双语规划

当前正文以中文为准，英文版本尚未提供，不创建翻译占位或空目录。后续有实际翻译时采用以下结构：

| 内容 | 当前中文位置 | 规划的英文位置 |
| --- | --- | --- |
| 项目入口 | `README.md` | `README.en.md` |
| 贡献指南 | `CONTRIBUTING.md` | `CONTRIBUTING.en.md` |
| 核心指南 | `docs/<topic>.md` | `docs/en/<topic>.md` |

保留当前中文路径，英文指南使用相同文件名；语言切换只链接实际存在的对应页，尚未翻译时链接中文来源并说明。两种语言共用代码、配置模板、DDL 和示例来源，翻译随同一行为变更维护。`AGENTS.md` 保持单一规则来源，不复制成双语执行规则；无需在同一正文逐段交错中英文。
