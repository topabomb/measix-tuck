# 架构与引擎边界

## 产品范围

Tuck 提供单用户 Web 工作台：登录与配置、平台接入、任务队列和原生交互、历史与 SSE、文件及版本交付。Go 提供服务和维护命令，Vue 提供产品页面，Node.js 只用于构建和开发测试。目标和编排频道仍为占位入口，没有目标调度、多用户服务或分布式执行器。

## 所有权

调用关系为「Vue → Go HTTP/SSE → Tuck 配置、SQLite 与文件」，以及「Go → Core/Relay/DAV」和「Go → Docker Agent CLI/HTTP/SSE → 原生执行」。代码目录见 [贡献指南](../CONTRIBUTING.md#代码与职责)，前端内部职责见 [前端架构](frontend.md)。

| owner | 职责 |
| --- | --- |
| app/settings | HTTP 装配、认证和配置切换 |
| tasks/events/interactions/statistics | 任务、产品队列、规范化运行事实和交互 |
| engine/engine_updates | 公开引擎进程与官方发行管理 |
| platform/models/model_resources/mcp | 平台身份、资源访问、准入和企业工具合同 |
| engine_config/model_gateway | 公开引擎配置投影与聊天转发 |
| image_generation/attachment_inspection | 独立模型操作 |
| files/deliveries/workspace_tools/enterprise_workspace | 本地文件、交付快照与远端文件访问 |
| config/store/security | 配置、业务持久化和 Web 身份 |

网络和文件复制不持有全局任务锁，每任务队列和交付串行。任务、消息、事件在 Store 短事务内联合发布，提交后唤醒浏览器。SSE 的可靠来源是持久化游标，不在每个订阅者中积累事件。DDL、事务及清理归 [存储设计](database-design.md)，离线升级归 [迁移约定](persistence-migrations.md)。

## 模型操作的目标架构与实施范围

Tuck 拥有资源配置，Docker Agent 消费其公开投影；不从生成的 YAML 或引擎内部状态反向建立业务资源。聊天与独立模型操作分别声明协议及用途，复用供应商连接和认证；聊天图片输出不等同于 Images API，音频输入也不等同于独立转录服务。

当前实现聊天、独立生图和附件识别。独立操作沿「资源解析与准入 → 一次模型请求 → 文件或文字结果」执行，经统一 MCP 暴露给 Agent，不创建原生会话、第二套任务队列或重试框架。业务 owner 校验权限、轮次、文件和结果事实，`model_content` 决定模型表示，协议层编码发送。原图与模型表示分别准备，浏览器展示与实际模型输入分别验收。平台短期凭据不复制到用户配置，独立操作用量不混入原生聊天 token 统计。

资源选择和协议配置见 [配置](configuration.md)，生图调用见 [工具参考](tools-reference.md#文生图1-个)，识别上下文见 [附件识别](attachment-inspection.md)，文件布局见 [任务文件](task-files.md)。当前没有 TTS、ASR 或独立生图页面；新增操作须有明确资源及线协议合同，不按模型名猜协议或静默降级。

官方公开能力覆盖相同的资源选择、平台认证、结果、取消和文件语义后，应切换并删除对应适配；原生支持某种媒体输出本身不足以满足退出条件。

## 模型可见内容的唯一入口

Tuck 自组织的模型内容统一经 `model_content.go`，业务和协议代码不能直接调用其内部子模块或自行拼装提示词、描述、消息包装和结果投影。此规则覆盖主对话、子代理及独立模型操作，不受 system、user、tool 或 MCP 承载位置限制。

入口管理默认 Agent 指令、工具定义与参数说明、MCP 服务级 instructions、工具指导与任务背景、附件标签及消息包装、协作标记、自动交付提示、独立模型操作指令，以及工具结果披露的字段、结构、错误和警告。路径、版本、识别正文和图片等动态事实仍由业务 owner 提供。

调用方向为「业务事实与许可 → 编排决定（需要时）→ model_content → 原生配置、MCP 或模型协议」。直接工具调用和结果无需经过编排。入口不执行网络或文件操作，不维护任务、权限或连接状态，也不在模型网关中改写引擎已组装的完整会话。

用户原文、自定义 Agent 指令、模型输出、原始附件、外部 MCP 和 Docker Agent 内容保持来源与语义；入口只管理 Tuck 添加的包装、选择和投影。网页专用文案不进入模型上下文，模型生成的数据也不自动提升为平台规则。

| 文件 | 职责 |
| --- | --- |
| `model_content.go` | 唯一调用界面、类型及内容组合 |
| `model_content_text.go` | 命名文字资源、默认指令、模板与参数解析 |
| `model_content_tools.go` | 工具描述、输入 schema 和定义构造 |
| `model_content_results.go` | 结果字段、错误及警告投影 |

资源标识保持稳定，缺失资源或必要参数明确失败。工具说明从最终装配集合派生，不另存工具名清单或许可规则。指导及警告依据实际可用工具生成，不建议调用已禁用工具。不同生命周期的内容在实际触发时构造，不全部塞入服务级 instructions。

## 上下文披露与编排

代理分类、入口、持久化定时任务及进一步上下文编排的待评审方案见[代理与编排设计](agent-orchestration-design.md)，其中规划不代表当前已实现能力。

`orchestration.go` 根据已取得的运行事实决定是否补充内容；Manager 提供事实、管理队列并在提交前核对目标、轮次、配置及许可；`model_content` 构造正文及来源包装；Docker Agent 执行和消费消息。策略不能直接修改任务、操作队列或拼接消息，Manager 不持有不断增长的业务策略。以上为职责划分，不各建服务或执行循环。

当前自动补交付只在执行流正常返回且取得明确匹配根会话 ID 的 `stream_stopped` 后判断。基线正常事件为 `reason=normal, finish_reason=stop`，只有 normal 映射为 completed。EOF、工具结果、子代理结束、缺失/未知身份、空原因、错误、取消和执行上限均不能代替正常根终止事实。未知终止身份不改变根状态，子会话只更新自身。原生事件消费后只持久化业务投影，保留规则见 [存储设计](database-design.md)。

正常结束后读取当前编排开关、工具许可及本轮交付事实；需要补交付时进入既有队列，发送前再次核对。每个真实用户根轮次最多补充一次，自动轮次不能触发自身；运行中关闭开关须移除未执行自动项，保留用户队列。跨会话消息的目标与接收规则见 [工具参考](tools-reference.md#send_conversation_message)，accepted、进入原生上下文和完成分别记录。

未来首次准备、背景生成、模型审查和工具描述优化尚未实现。接入时优先验证原生 hooks、Agent 路由和消息接口，不增加第二套执行器。首次准备指新会话首次执行目标 Agent 前，不能以模型级 `turn_start` 代替；指导按实际工具集合生成，续聊、恢复和分支不重复已有背景。新事实以明确来源和更新意图提交，异步决定提交前重新核对轮次与许可，失败不能解释成业务结论。

立即补充和结束后跟进分别表达 steer/followup 意图，不按内容类别建立子系统。原生 followup 接管前须覆盖现有队列编辑、暂停、排序、恢复、来源和逐轮授权；同一请求只能有一个队列 owner。审查策略接入后替换固定策略，不双重触发；同会话路由不等同隔离上下文。原生定时能力不能推断为产品事件调度或跨重启持久化。不为未实现能力增加空接口、模板 DSL 或状态表。

## Tuck 原生工具集与 MCP

七个 Tuck 自实现工具由一套进程内 MCP 服务提供，复用 `LocalTools` 和官方 SDK。每个 Agent 最多装配一个 Tuck toolset；集合为空时不装配。会话、文件、交付和模型操作仍由各业务 owner 执行，不按功能另建 MCP 服务、子进程或同名 API 工具装配路径。原生工具及外部 MCP 保持各自所有权。

协议入口为模型网关 `/tuck-tools?agent=<agent>`，通过网关认证和 `X-Tuck-Turn` 绑定调用者及轮次。目录按 Agent、子代理树、只读限制、工具开关及资源条件过滤；执行时再核对任务、轮次和授权。业务 HTTP API 不提供内部令牌旁路，连接和请求上下文按 SDK 语义隔离。

服务级说明使用标准 [InitializeResult.instructions](https://modelcontextprotocol.io/specification/2025-11-25/schema#initializeresult)，是可选字符串，不是数组或 capabilities。客户端如何加入模型上下文由其实现决定。Tuck 当前只从最终工具集合生成稳定顺序的英文工具名清单：多工具以 `The Tuck toolset provides the following tools:` 开头，单工具用 `tool:`，空集省略。不能混入其他来源工具、描述全文、使用规则或交付要求。

说明与工具定义均经 `model_content` 构造，在初始化及配置应用后的重新装配时更新；单独的 `tools/list_changed` 不能证明初始化说明已刷新。工具用途与输入归 [工具参考](tools-reference.md)，输出归 [工具返回合同](tool-results.md)。

## Docker Agent 合同

兼容基线为官方 [v1.150.0](https://github.com/docker/docker-agent/releases/tag/v1.150.0)，提交 `5277e7842058fc3fbb9943a6e88bc05a1c8e6fe2`；安装校验以 [嵌入发行清单](../internal/tuck/contract/engine-release.json) 为准。[上游文档](https://docker.github.io/docker-agent/)及较新源码不扩大已验收范围。

生成的原生代理 YAML 和内置工具发现临时配置使用格式版本 16，由同一官方基线解析；该数字独立于引擎版本、Tuck 配置版本和数据库版本。声明新格式不自动启用路由、hooks 或其他可选能力。

默认使用官方发行，只通过公开配置、CLI、HTTP/SSE 使用引擎，不导入内部 runtime/store/session 包，不读取或修改原生 SQLite。对接 `serve api --session-db`、Agent 加载与目录、会话创建/读取/恢复、执行流、title、permissions、safety-policy、fork、resume、elicitation 和 steer。已复现且尚无可用官方修复的缺陷，须先取得用户明确同意，再按 [临时补丁约定](../patches/docker-agent/README.md) 处理；当前补丁及验收证据只在该文档维护。

原生 MCP 发现、连接、恢复、调用重试、审批、子代理及执行生命周期由引擎负责。Tuck 不另加原生调用重试，也不为禁止原生重试而接管生命周期；长模型/执行/SSE 流没有固定总寿命，单次操作结束不关闭有效共享连接。

| 公开能力缺口 | 当前适配 | 删除条件 |
| --- | --- | --- |
| 企业模型身份、准入及凭据隔离 | 回环模型网关、明确来源、admit、Chat/Responses 转发与脱敏 | 官方公开能力完整承接同一合同 |
| 企业 MCP 每工具许可、JCS 哈希和 admit | 透明 HTTP/SSE 代理，校验授权与 schema，保留事件 ID 和通知 | 公开扩展完整承接平台合同 |
| MCP 缺少 Tuck 任务/轮次/空间身份 | 统一 MCP、短期 turn header 与调用前后校验 | 原生上下文可携带并验证相同事实 |
| 独立生图与专用识图的资源和文件语义 | 单次模型操作，统一 MCP 返回结果 | 覆盖资源选择、平台认证、文件权限、取消和失败合同 |
| 工作台稳定 URL、分页、重放和版本交付 | 公开历史只读导入、规范化消息/事件与独立快照 | 原生产品接口满足现有合同 |

本机使用 SDK Streamable HTTP，企业代理兼容 HTTP/SSE，不手写握手。企业代理保留空数据 SSE priming 帧及 ID；POST 通知的 202、DELETE 的 204 可为真正零字节确认，即使带 JSON Content-Type。非空正文仍校验和脱敏，初始化响应不适用空体例外。

基线 SDK 为 `v1.7.1-0.20260825122737-c68ad9a4e6e1`，本机服务使用 Stateless 和 PropagateRequestCancellation。基线后台 subscriptions/listen 无法展开每轮动态 header，但客户端保留 Cookie：server/discover 仅在有效 turn header 下签发目录归属 Cookie，绑定复用 peer 的稳定目录身份，供后续客户端定位任务及 Agent 目录。Cookie 不授予执行权，调用仍要求精确活跃 turn header 和 task 匹配。

目录通过 SDK AddTool/RemoveTools 通知；开轮有界等待目录确认，具体等待值见 [超时与取消](configuration.md#超时与取消)。无会话/订阅且无在途请求时回收 peer，结束轮次不关闭有效共享连接，服务关闭撤销剩余会话。官方公开订阅支持动态上下文和目录确认后删除 Cookie 及等待适配。

## 生命周期边界

浏览器断开不取消任务，停止只取消目标轮次。服务重启将旧执行收束为 interrupted、暂停用户队列、移除自动交付项并退役旧审批，不重放工具或自动续跑。关闭先撤销网络 owner、取消任务，再关引擎和存储。

任务、工具调用、问题、原生会话和后台任务保持各自身份。只有公开事件或响应给出子会话 ID 才可读取其历史，不从后台任务 ID 或输出文本推断。登记、最近验证、当前连接与实际执行分别展示，未取得的状态不推断为在线或成功。

工具调用按执行来源及原生 agent_name 关联，完成后移除在途记录，允许后续复用调用 ID。队列准备后重新核对整项，已编辑或删除的旧内容不发送；收尾期间的新消息由原队列 owner 继续处理。配置应用先阻止新执行，再检查当前任务；创建和 fork 的异步结果不能跨配置切换发布。

## 平台与安全

Discovery 验证身份及路径，同部署改地址先无凭据校验。Snapshot v4/v5 校验 schema、闭包、ETag、模型来源和 MCP 合同，来源见 [client-schemas.json](../internal/tuck/contract/client-schemas.json) 和 [provenance.json](../internal/tuck/contract/provenance.json)。刷新先持久化平台幂等键，失响应保留；不改变原生重试语义。平台绑定由配置的 32 字节密钥加密存入业务库，合同允许的 ANDROID 兼容枚举继续保留，不擅自新增枚举。

登录使用 scrypt、哈希会话令牌、Cookie/CSRF、持久限流和有界密码校验。Host/Origin 检查不信任 Forwarded/X-Forwarded。凭据变化、到期和退出撤销对应流，不结束有效后台任务。配置保护独立于登录，读取和写入均核对授权；操作规则见 [配置保护与密码](configuration.md#配置保护与密码)。

## API与文件

主接口为 `/api/runtime`、`/api/connection*`、`/api/settings/*`、`/api/tasks*`、`/api/events`、`/api/enterprise-workspace*`。任务快照带 cursor；SSE 使用 seq/taskId/type 和 since/Last-Event-ID，拒绝未来游标。消息通过 index/revision 及块修订定位，媒体按需读取，不改写历史。

Hash URL 的页面合同以 [router.ts](../frontend/src/app/router.ts) 为准：支持详情直达、刷新/登录返回及筛选分页恢复，不在 URL 中保存秘密、草稿、提交正文或本地 File。导航与渲染归 [前端](frontend.md)。

Files 通过 os.Root 和发布前目录/目标身份校验阻止 symlink/junction 越界。企业面板代理 Core Files API，工具访问明确绑定空间的 DAV；未知写入结果不得自动重发。文件布局与原图/快照边界见 [任务文件](task-files.md)，工具限制见 [工具参考](tools-reference.md)，阶段超时见 [配置](configuration.md#超时与取消)。预览身份只存在 Tuck presentation，不进入引擎上下文。
