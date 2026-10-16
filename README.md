# Measix Tuck

Measix Tuck 是使用 [Docker Agent](https://github.com/docker/docker-agent) 作为执行引擎的自托管 Agent 工作台，使用 Go 后端和 Vue 前端，在浏览器中管理对话、任务、工作文件和交付成果。

## Docker Agent 与 Tuck

[Docker Agent（曾用名 cagent）](https://docs.docker.com/ai/docker-agent/) 是 Docker 团队开源的 Agent 构建与运行框架，可用 YAML 定义助手的模型、指令、工具和协作关系，支持多种模型供应商、内置工具及 MCP。它本身提供功能丰富的 [终端界面](https://docker.github.io/docker-agent/features/tui/)，已有会话管理、审批、消息队列和立即补充等能力。

Tuck 是独立维护的浏览器工作台，通过官方程序和公开接口使用 Docker Agent。模型执行、原生会话上下文、审批、子代理及 MCP 生命周期沿用上游；Tuck 在此基础上增加工作台、文件和成果管理，以及 Measix 平台接入。引擎兼容基线与职责见 [架构](docs/architecture.md#docker-agent-合同)。

## Tuck 的工作台体验

| 使用需要 | Tuck 提供的改善 |
| --- | --- |
| 从浏览器查看和处理工作 | 桌面与手机响应式界面，集中处理对话、任务、审批和提问；对话、消息、文件及设置可用稳定地址直达，刷新和登录后返回目标位置 |
| 跟进多个任务 | 对话页保留交流过程，任务页集中查看进度、阻断和结果；服务运行期间，关闭浏览器不会取消后台任务，重新打开后可继续查看 |
| 检查和编辑工作文件 | 管理附件与任务目录；企业工作区提供文本/源码编辑，以及 Markdown、HTML、CSV、PDF、图片和音视频预览，按文件保留页面内的阅读状态 |
| 留下可回看的成果 | 摘要与关键文件组成版本交付，文件保存独立快照；源文件后续改动不会改变旧版，可在对话和任务中查看、切换和下载 |
| 配置模型、工具和企业资源 | 设置页维护常用模型、Agent 与 MCP 配置，提供登录和分类配置保护；Measix 接入保留平台身份、模型准入、工具许可及企业工作区/DAV 合同 |

这些改善面向浏览器中的持续工作和成果回看。终端操作、直接维护原生 YAML，或需要上游完整配置能力时，也可直接使用 Docker Agent；Tuck 不承诺暴露其全部功能。官方及社区界面的定位见 [相关界面与项目](docs/frontend.md#相关界面与项目)。

当前面向单用户工作台；目标和编排频道仍为占位入口，不提供目标调度或编排执行器。企业功能需要相应平台授权；重启服务会收束中断执行，不会自动续跑旧任务。

## 快速开始

### 从源码运行

需要 Go 1.26、Node.js 22.17+ 和 pnpm 10.15.1。在仓库根目录执行：

```powershell
npm run setup
npm run build
npm start
```

打开 <http://127.0.0.1:8090/>，在设置中配置模型供应商，或提交平台生成的企业接入材料，再创建对话。初始配置没有模型凭据。

`setup` 安装依赖、仅在配置不存在时初始化，并下载及校验官方 Docker Agent；需要网络访问。`build` 构建前后端，`start` 运行已构建程序。前端热更新和测试见 [贡献指南](CONTRIBUTING.md)。

### 使用发行包

解包后在发行目录执行；Windows 使用 `.\tuck.exe`，Linux 使用 `./tuck`：

```powershell
.\tuck.exe init
.\tuck.exe engine install
.\tuck.exe serve
```

发行运行无需 Go 或 Node.js。容器安装、外部引擎、升级和回退见 [运行与发布](docs/deployment.md)。

## 配置与数据

唯一用户配置是 `tuck.config.json`，通过 `--config <path>` 选择其他位置，所有相对路径从配置目录解析。Tuck 业务库为 `<storage.dataDir>/tuck.db`；工作文件、交付快照和引擎自己的不透明会话资产也须保留。

默认只监听本机回环。开放局域网访问前，停服执行 `go run ./cmd/tuck password login` 设置登录密码，再修改监听地址并启动。密码通过终端隐藏输入或标准输入提供，不进入命令参数。配置、密钥和运行数据不得提交到 Git。

已有安装升级前须完整备份。当前配置和业务库均为 v2，正式 Go v1 使用显式离线迁移，正常启动不会自动升级；步骤见 [持久化版本与迁移](docs/persistence-migrations.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [贡献指南](CONTRIBUTING.md) | 开发、目录、检查、文档维护及双语组织约定 |
| [配置合同](docs/configuration.md) | 配置项、模型、助手、MCP、密码及保存应用 |
| [运行与发布](docs/deployment.md) | 二进制、容器、维护、升级和服务验收 |
| [工具参考](docs/tools-reference.md) | 原生工具、会话协作、DAV 和版本交付 |
| [架构与引擎边界](docs/architecture.md) | 职责、公开协议、必要适配、生命周期和安全 |
| [存储设计与维护](docs/database-design.md) | 实际 DDL、事务、游标、交付保留和数据所有权 |
| [持久化版本与迁移](docs/persistence-migrations.md) | 版本、离线检查、迁移、完整备份和回退 |
| [前端架构与界面](docs/frontend.md) | 相关界面定位、渲染语义、状态、响应式布局和依赖补丁 |
| [验证指南](docs/verification.md) | 测试命令、覆盖、性能门槛及外部验收边界 |

## 致谢

感谢 Docker 团队与 Docker Agent 社区提供并持续维护开放的 Agent 执行基础。Tuck 的原生运行能力建立在这项工作之上：

- [Docker Agent 源码](https://github.com/docker/docker-agent) · [官方文档](https://docker.github.io/docker-agent/) · [官方发行](https://github.com/docker/docker-agent/releases)。
- 前端初始渲染基础来自 [topabomb/demo1](https://github.com/topabomb/demo1/tree/34d103e496ddddeb0826130d6ef9f8a93d21a099)，感谢其长历史消息渲染与参考演练设计；来源和后续依赖补丁见 [前端说明](docs/frontend.md#来源与依赖补丁)。

## 许可

仓库目前没有项目级 `LICENSE`，尚未声明统一的开源许可。前端来源与依赖补丁见 [前端说明](docs/frontend.md#来源与依赖补丁)；项目许可需由维护者明确，不能以依赖或来源项目的许可代替。
