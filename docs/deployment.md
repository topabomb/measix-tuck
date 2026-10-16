# 运行与发布

## 本机和发行包

源码启动见 [README](../README.md#快速开始)，开发与构建见 [贡献指南](../CONTRIBUTING.md)。`npm run package` 在 `release/` 生成 Windows amd64、Linux amd64/arm64 三个 tar.gz，采用 `CGO_ENABLED=0` 构建，包含 Tuck 可执行程序、`frontend/dist` 和简要运行说明；运行无需 Go 或 Node.js。

解包后在发行目录依次执行 `tuck init`、`tuck engine install`、`tuck serve`；下文命令在 Windows 使用 `.\tuck.exe`、Linux 使用 `./tuck`。可用 `--config <path>` 选择配置，所有相对路径从配置目录解析。首次引擎安装需要访问官方发行下载地址；配置模型或平台后才可进行实际任务。

默认引擎安装使用 [嵌入清单](../internal/tuck/contract/engine-release.json) 的官方基线。配置其他版本时，安装命令仅接受与当前官方稳定版一致的版本，核对官方发行 URL、SHA256 和实际 version；规则见 [engine_updates.go](../internal/tuck/engine_updates.go)。不编译或补丁引擎。自定义官方程序通过 `engine.binary` 指定，外接公开 API 用 `engine.mode=external` 和 `engine.url`；外接实例更新和配置由宿主管理。Tuck 停止时结束自己管理的引擎，不关闭外接实例。

默认配置没有业务凭据，只有回环监听，模型需接入平台或配置供应商。登录、设置保护和配置密钥见 [配置合同](configuration.md)。启动失败不会抹掉配置或历史；`/healthz` 与 `tuck health` 表示当前服务及引擎就绪，不代替模型/DAV 业务验收。

## 容器

[Dockerfile](../Dockerfile) 的 Node 阶段只构建前端，Go 阶段构建后端，最终 Debian 镜像只有 Go 程序、前端、官方引擎及 git/ripgrep/tini 等工具。运行用户 1000:1000，HOME=/data；不带 Node、编译器、本机配置或凭据。

在源码仓库首次部署时执行：

```powershell
npm run deploy:init
npm run docker:build
npm run docker:up
npm run docker:logs
```

`deploy:init` 创建 `deploy/data` 和 `deploy/config/tuck.config.json`，提示设置登录密码（用户名 admin），配置使用容器路径 `/data` 与 `/opt/tuck/frontend/dist`；已有配置不覆盖。启动前确保宿主两个挂载目录及已有文件可由容器 UID 1000 使用。容器配置只挂载到 `/config`，源码开发配置不直接复用。

恢复或解包已有数据时，权限检查须覆盖目录和已有文件。运行用户除可读写数据库外，还须能将 `tuck.db` 设为 0600、在配置目录原子替换配置，并访问工作文件、交付及原生会话资产。仅确认目录可写或通过 `migrate --check` 不代表这些权限已满足。若文件属主不匹配，停服后按实际挂载路径修正为 UID/GID 1000:1000，再以同一用户检查；不要改用 root 运行服务或忽略权限错误。

[Compose](../compose.yaml) 将宿主 127.0.0.1:8090 映射到容器，配置挂载 /config，持久数据挂载 /data，不使用环境文件。若通过反向代理提供局域网访问，代理必须保留真实 Host 和浏览器 Origin；Tuck 不信任 Forwarded/X-Forwarded 来放宽认证。TLS 在入口终止，禁止通过改请求 Origin 绕过同源。查看日志用 `npm run docker:logs`，停止用 `npm run docker:down`；保留挂载配置与数据。

容器中的 localhost 是容器自身。需要访问宿主服务时用 host.docker.internal；企业地址须能从运行环境解析到正确部署。镜像重建不清空挂载。容器内部原生 shell 拥有该用户的权限，不能把原生 filesystem allow_list 当作 shell 沙箱。

## 更新、回退和数据

设置 → 引擎可检查官方稳定版、下载、校验、应用。下载/校验失败不停止当前引擎。应用先重新检查保护授权，停止目标引擎，备份不透明原生会话资产，再启动并校验公开 API；失败恢复原程序、配置选择和相匹配会话资产。不读取原生 SQLite 内部表。其他待应用设置保持 pending。

正式发布前先停服务及对应引擎，备份配置、完整 dataDir 和全部实际工作目录，再替换程序和前端。正式 Go v1 升级当前 v2，先用新程序执行 `tuck migrate --check`，再执行 `tuck migrate --backup <新的外部备份目录>`，复查后启动。迁移命令的备份只覆盖变更输入，不能代替发布前完整冷备份；未知或封存 Node 格式不导入。中断可续迁，回退恢复相匹配整套资产，不能仅换二进制。执行与恢复细则见 [持久化版本与迁移](persistence-migrations.md)。

每次停服、替换和验收重启前，都要重新确认当前任务和队列状态；首次停服前的检查不能代表服务恢复后的状态。协调维护期间的新提交，有运行任务时先等待结束，或在已明确授权中断的范围内停止。重启不会自动续跑旧执行，不能为验证恢复而重放工具。新版本已经接收新消息或写入文件后，不能再把升级前副本当作最新运行数据覆盖回去。

容器已有数据升级时先 `docker compose stop` 并完成完整冷备份，再用目标镜像执行 `docker compose run --rm --no-deps tuck migrate --check --config /config/tuck.config.json` 和 `docker compose run --rm --no-deps tuck migrate --backup /config/backups/<新目录名> --config /config/tuck.config.json`（备份父目录须提前创建，并确认实际服务名、挂载和权限）。`/config` 与 `/data` 分别挂载，备份须在 dataDir 外且持久保存。检查后再启动目标镜像，不在 entrypoint 中静默升级格式。

发布包不携带本机 tuck.config.json、.env、数据、Node 依赖、测试产物或凭据。Docker 构建上下文按 [.dockerignore](../.dockerignore) 允许目录并显式排除 node_modules、dist、测试和本地资产。

## 实际服务验收

切换后核对运行程序/镜像、前端资源版本、官方引擎版本、监听、数据库完整性和历史数量；实际登录、恢复 URL、打开历史、读写文件、下载已有交付，并使用公开原生接口验证会话。平台/MCP/DAV 要在真实接入环境另验，健康检查不能证明业务成功。

启动脚本和外部探测使用公开 `/healthz` 或容器内 `tuck health --config <实际配置>`。`/api/runtime` 受工作台登录保护，匿名返回 401 是认证生效，不能据此认定服务未启动。Docker Agent 的公开 `GET /api/sessions/:id` 用于读取已保存会话；`GET /api/sessions/:id/status` 只表示当前已装配的原生运行态，重启后对未装配的历史会话返回 404 不代表历史丢失。

本机生产地址、真实挂载、SSH 入口、私密配置和维护手册集中在 Git 忽略的 `.production/`，该目录也排除在 Docker 构建上下文之外，访问权限按私密资料管理。手册只维护当前部署事实和操作方法，不累计逐次交付记录，不在仓库文档中保存真实凭据。清理前核对当前容器挂载、Compose 和计划任务引用，保留仍被使用的启动脚本及当前数据；验收后按已确认范围移除历史备份、旧发行包和临时验证资料，不使用全局 Docker 清理代替项目清理。

自动化容器验证使用独立临时卷，不能等同现有运行数据的迁移或正式部署；检查层次及外部限制见 [验证指南](verification.md#真实服务与已知限制)。
