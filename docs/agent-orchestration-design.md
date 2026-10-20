# 代理分类、编排与定时任务设计

状态：待评审方案，2026-10-20。本文描述目标设计，不代表已实现。当前实现与所有权以[架构](architecture.md)为准；实施后把有效合同合并到对应主题文档并移除本方案，避免维护两份正文。

## 目标与原则

Tuck 提供主代理、子代理、编排代理和入口代理四种产品概念，继续使用 Docker Agent 执行模型、工具、审批、子代理和会话。Tuck 负责产品身份、触发条件、授权边界、任务关联、结果反馈和持久化自动化。

复用现有 Manager、Store、统一 MCP 和 `model_content`；确定性规则直接用代码，需要语义判断或探索时才运行编排代理。不增加第二套代理执行器、原生工具重试或通用工作流 DSL。

## 代理职责与会话

| 产品概念 | 职责 | 会话与生命周期 | 展示 |
| --- | --- | --- | --- |
| 主代理 | 承接用户任务，执行、沟通和交付 | 独立 Tuck 任务与原生会话，支持续聊 | 对话、任务频道 |
| 子代理 | 完成所属代理委派的局部工作 | 原生子会话，同步或后台执行 | 所属任务中的委派记录及详情 |
| 编排代理 | 准备背景、选择指导、审查结果、提出后续动作 | 按需运行；需要隔离时使用独立原生会话，只向目标提交必要结果 | 编排频道及关联任务记录 |
| 入口代理 | 接待、澄清、派发主任务、跟踪并反馈 | 自己的接待会话与派发出的主任务分别运行 | 入口对话、任务链接及结果 |

“主／编排／入口”是产品职责，“父／子”是原生执行关系。不要让同一个四选一枚举同时决定权限、可见性和生命周期。编排代理可以拥有原生子代理；一次后台执行也不会自动成为新的主任务。

普通对话可直接使用主代理，不强制经过入口。定时任务是触发方式，不增加“定时代理”类型。

入口派发的主任务必须具有独立产品身份。入口结束一轮或断开连接不取消主任务；保存入口请求与目标任务的关联，依据现有任务事件跟踪。状态区分接收、执行、等待用户、执行结束和交付，不能把模型停止等同业务成功。

入口所需的创建任务、读取结果和结果回传能力通过统一 MCP 调用现有业务 owner。外部 bot 的身份、目标会话映射和消息投递属于 Tuck 接入能力；入口代理不能代替鉴权或扩张用户授权。

## 编排与执行

调用关系为：

```text
用户／入口／时间／业务事件
  → 已授权的任务请求或运行事实
  → 确定性编排策略，必要时调用编排代理
  → 校验目标、轮次、配置和许可
  → model_content 构造内容
  → Manager 的现有队列与 Docker Agent 公开执行接口
  → 任务事件、交付及必要的结果反馈
```

| 场景 | 决策方式 |
| --- | --- |
| 是否已交付、目标是否有效、工具是否获准 | 业务 owner 提供事实，代码判断 |
| 启动前整理背景、选择资料与指导 | 模板优先；需要语义选择时调用编排代理 |
| 检查成果是否满足要求 | 编排代理提供结论、证据、缺项和建议 |
| 是否补充执行、投递到何处 | Tuck 校验建议，经原队列提交 |

编排结果只表达有限动作及其依据，例如继续、补充、需要用户输入；不能直接修改任务或许可。失败和证据不足不解释为“检查通过”。

首次准备指新会话首次执行目标代理前，续聊、恢复、分支需识别已有背景；每次模型调用和每次代理激活都不等于首次准备。提交异步结果前重新核对输入是否仍有效。

交付存在性检查与成果质量审查分别表达。新增语义审查时由同一编排入口决定跟进，替换相应固定策略，避免双重触发；保留自动跟进不触发自身及有界执行的约束。

## 定时任务

自动化定义表达“何时触发、作用于谁、输入什么、如何处理结果”。到期检查只负责派发，执行仍由 Manager 与 Docker Agent 承担。

| 维度 | 初始建议 |
| --- | --- |
| 时间 | 单次、固定间隔、日历规则；显式 IANA 时区，内部记录 UTC，展示后续执行时间 |
| 目标 | 新建主任务、向指定任务追加一轮、运行编排检查 |
| 上下文 | 新任务默认独立历史；续办明确绑定目标，按需读取已有上下文 |
| 重叠 | 同计划默认不并发；需要跟进时最多合并保留一个待执行项 |
| 错过执行 | 有效时间窗口；巡检合并补查、过期提醒跳过、业务批处理显式补跑 |
| 通知 | 有产出、重要变化、失败或需要用户处理时通知，无变化保持安静 |
| 管理 | 暂停、恢复、立即执行、编辑、删除和运行历史 |

“每天 9 点”与“每隔 24 小时”分别处理。自然语言生成计划后展示可读规则、时区和后续时间，避免用户只能审阅表达式。

计划定义及每次触发与任务／轮次的关联保存在现有 `tuck.db`。采用唯一触发身份防止重复派发；计划变更后，尚未派发项按当前版本重新核对。具体 DDL 留待实施设计，不修改已发布编号 SQL，遵循[存储设计](database-design.md)和[迁移约定](persistence-migrations.md)。

Tuck 数据库事务不能与原生 HTTP 执行联合提交。需要区分尚未派发、已确认接收和结果未知；执行结果未知时先通过公开事实核对，不自动重放工具。产品派发去重不承诺外部副作用恰好一次。

重启恢复计划不等于自动续跑中断任务。暂停计划仅阻止未来触发，取消在途任务另行操作。停服期间无法准时执行，恢复后按错过执行策略处理。执行时重新校验当前许可；需要交互审批时记录等待用户，不静默放权。

单次运行记录与长期调度定义分开保留；已实现的生命周期基础见[存储设计](database-design.md#归档、保留与自动维护)。临时执行创建方、调度和入口绑定尚未实现；启用时必须明确用途并接入目标对话的删除保护，不按代理身份另建清理规则。

## 上下文统一

业务 owner 获取已授权事实；编排选择内容；`model_content` 构造 Tuck 添加的表示；Docker Agent 维护原生上下文和执行循环。模型网关不重写原生完整会话。

| 内容 | 承载建议 |
| --- | --- |
| 稳定角色和约束 | 原生 Agent instruction |
| 日期、环境、项目规则 | 优先原生配置与 hooks，避免重复注入 |
| 首次背景和编排简报 | 首次执行前准备，通过公开消息接口提交并保留来源 |
| 本轮更新、跨任务补充 | 区分 steer 和 followup 意图，保留原队列 owner |
| 工具用途及输入合同 | 基于最终获准工具集合生成 MCP 定义 |
| 查询结果和附件 | 保留来源及真实媒体类型，经原生协议提交 |

记录 Tuck 添加内容的来源、适用任务／轮次、生成时间、依据版本及替代关系，优先复用任务和消息元数据；短期同步事件不能承担长期来源或执行记录，见[存储设计](database-design.md)。编排生成的推断不升级为系统规则，不覆盖用户原文。

入口只取得接待所需目标与结果；编排只取得判断所需证据；主代理取得任务原文、有效约束和必要背景；子代理取得局部任务与预期输出。摘要与引用优先，详细材料按需读取。

工具指导与工具合同分开：模型可提出使用建议，不能修改平台工具名、schema、许可或合同哈希。MCP 初始化 instructions 保持现行稳定工具清单语义，不承载每轮背景或交付规则。

## 实施顺序与验收

1. 明确产品职责、原生关系、来源及任务关联，不建立新执行循环。
2. 打通首次准备和结束后审查，校验隔离、过期结果及有界跟进。
3. 增加入口派发、结果查询及事件反馈，验证断开入口不影响主任务。
4. 增加持久化时间触发；再按具体场景接入业务事件，不先建设通用 DAG 编辑器。

原生复用需验证配置加载、实际模型输入、恢复和分支、权限与准入、取消及错误路径。文档能力、Tuck 适配和端到端验收分别记录；原生能力满足同等合同后移除相应适配。

## 原生能力核对

### 证据与版本边界

以[发行清单](../internal/tuck/contract/engine-release.json)固定的 Docker Agent **v1.150.0** 为基线，tag 对应提交 `5277e7842058fc3fbb9943a6e88bc05a1c8e6fe2`。以下链接固定到该 tag，不用 main 的能力扩大基线。

本次核对包含公开文档、Tuck 源码和本机官方 Windows 二进制的无模型配置解析检查。二进制报告相同版本与提交，SHA256 与发行清单一致。未启动模型任务、连接企业资源、操作生产服务或读取原生数据库；解析通过不代表运行与恢复验收通过。

必须区分引擎版本与 YAML 配置版本：Tuck 的 [WriteConfig](../internal/tuck/engine_config.go) 已调整为输出 `version: "16"`，工具发现临时配置同步调整；同一个 v1.150.0 二进制支持两种格式。以下保留两种格式的解析对比，不代表可选编排能力已启用。

| 合成配置检查 | 版本 15 | 版本 16 |
| --- | --- | --- |
| 日期／环境／prompt files、`user_prompt_submit` + `add_context`、`structured_output.mode: tool` | 解析通过 | 解析通过；除版本号外规范化结果相同 |
| `type: model` 的上下文 hook | 解析通过 | 未重复检查 |
| `handoffs`、`force_handoff`、顶层 `budget` | 解析通过 | 未重复检查 |
| `before_agent_run` + `routing.allowed_agents` | 拒绝：`unknown field "before_agent_run"` | 解析通过 |
| 顶层 `evaluators` | 拒绝：`unknown field "evaluators"`，提示使用版本 16 | 解析通过 |

检查通过公开 `version`、`toolsets --format json` 和 `debug config <合成配置>` 执行，使用独立配置／数据／缓存目录，无真实凭据。临时输入和结果位于已忽略的 `.local/research-agent-orchestration/`，不作为正式运行资产。工具目录也确认注册了 background_agents、scheduler、session_context、tasks、webhook。

### 可以直接复用的执行基础

这里的“直接复用”指不用自建执行机制；新增用途仍需按目标场景验收。

| 需求 | 原生能力 | Tuck 使用方式及边界 |
| --- | --- | --- |
| 主任务和入口自身执行 | [API 会话创建、指定 Agent 执行、HTTP/SSE](https://github.com/docker/docker-agent/blob/v1.150.0/docs/features/api-server/index.md) | 复用当前 Engine/Manager；入口派发的独立任务各有会话，不依赖入口保持一轮运行 |
| 同步局部委派 | [`sub_agents` / `transfer_task`](https://github.com/docker/docker-agent/blob/v1.150.0/docs/tools/transfer-task/index.md) | 原生负责子上下文、执行和结果返回；Tuck 保留归属及展示 |
| 并行局部委派 | [`background_agents`](https://github.com/docker/docker-agent/blob/v1.150.0/docs/tools/background-agents/index.md) | 复用启动、查询、等待和停止；后台任务 ID 不作为 Tuck taskId。需交互审批的后台工具调用会被原生拒绝，不能按前台审批能力推断 |
| Agent 模型、指令和工具差异 | [Agent 配置](https://github.com/docker/docker-agent/blob/v1.150.0/docs/configuration/agents/index.md) | 四类职责无需四个运行时；配置、目录和许可仍由现有 owner 投影 |
| 稳定环境与按需指导 | `add_date`、`add_environment_info`、`add_prompt_files`、[skills](https://github.com/docker/docker-agent/blob/v1.150.0/docs/features/skills/index.md) | 原生负责装配；Tuck 只组织自身内容和授权范围，不重复加载整份规则或技能正文 |
| 上下文压缩 | [session_compaction、compaction_threshold、compaction_model](https://github.com/docker/docker-agent/blob/v1.150.0/docs/guides/compaction/index.md) | 原生负责历史压缩；Tuck 维护任务事实和来源，不用工作台历史重建原生完整上下文 |
| 审批及 MCP 生命周期 | 原生 permissions、safety policy、resume、elicitation、MCP | 继续使用现行对接；编排与定时身份不绕过认证、准入或工具许可 |

### 原生提供机制，但需要 Tuck 适配

| 需求 | 可用机制 | 缺口与采用建议 |
| --- | --- | --- |
| 有限、机器可读的编排结论 | [structured_output](https://github.com/docker/docker-agent/blob/v1.150.0/docs/configuration/structured-output/index.md) | 优先复用；需要先用工具取证时选择 `mode: tool`。Tuck 仍校验目标、证据、动作许可和结果是否过期；合法 JSON 不等于正确决定 |
| 每条请求补充背景 | [user_prompt_submit / user_steering_messages_submit / user_followup_submit](https://github.com/docker/docker-agent/blob/v1.150.0/docs/configuration/hooks/index.md) | 可用于当前调用的临时背景。Tuck 要绑定任务与来源；首次背景应有可恢复记录。普通 user_prompt_submit 不覆盖原生子会话启动 |
| 简单模型判断或补充文字 | 原生 `type: model` hook | 可作为候选，先验收模型别名、网关准入、取消与使用量；它不是有工具循环的完整编排代理。默认自由文本会进入 additional_context，不把未核验模型推断自动注入为权威规则 |
| 同会话流水线 | [handoffs / force_handoff](https://github.com/docker/docker-agent/blob/v1.150.0/docs/concepts/multi-agent/index.md) | 适合明确允许共享历史的固定阶段。当前 Tuck 工具许可只遍历主代理及 sub_agents，需明确验证路由目标身份后适配，不能为使其通过而关闭校验 |
| 激活前选择代理、完成后路由审查 | `before_agent_run` / `after_agent_complete` | 配置格式 16 已满足，仍需公开配置投影及路由身份支持；仅用于同会话路线。不能直接承担隔离准备、并行分支或检查后反复返回原代理 |
| 立即补充与后续轮次 | [steer / followup](https://github.com/docker/docker-agent/blob/v1.150.0/docs/features/api-server/index.md) | steer 已用于 Tuck 协作；followup 需覆盖产品队列编辑、暂停、排序、来源、恢复与逐轮授权后才能接管，避免双队列 |
| 入口及编排跟踪进度 | 原生执行 SSE、会话状态／快照及 events API | 继续由 Tuck 规范化业务事实并提供短期事件重放；不能假设所有 serve api 会话的 events 流都包含完整前台执行事件 |
| 执行预算 | [budget / budgets](https://github.com/docker/docker-agent/blob/v1.150.0/docs/configuration/budget/index.md) | 原生支持每次运行及共享命名预算；Tuck 当前不投影这些顶层块，需增加选择和投影。跨多次定时触发的累计上限仍由 Tuck 管理 |
| 固定目的地通知 | [webhook / send_webhook](https://github.com/docker/docker-agent/blob/v1.150.0/docs/tools/webhook/index.md) | 可选用于授权的固定通知；其返回表示排队，公开合同只在最终失败时反馈，不提供产品所需的完整确认。原生已有重试，不在其外叠加同一投递重试 |

结构化输出 tool 模式由引擎提供终结工具和 schema 校验，适合“读取证据后给出审查结论”。Tuck 无需再造 JSON 纠正循环；不支持、拒答、预算耗尽或未形成有效输出，均不得按通过处理。

路由与 hooks 的几个限制决定采用范围：

- `turn_start` 是每次模型调用，`before_agent_run` 是每次激活，均不是 Tuck 任务首次准备的判据。`session_start` 的恢复行为也必须实测，不能单凭名称建立只执行一次的合同。
- hook 路由保留同一会话历史；静态路由和 force_handoff 不允许形成环，且 pinned 子会话不支持这种路由。隔离准备及有界补充回合应由 Tuck 使用独立原生执行组织。
- 完成后路由不能撤回此前已流出的文字；“审查后才向用户发布”还需要 Tuck 的结果发布策略。
- 部分 hook 附加内容以 system 上下文进入模型，且仅在当次生效。外部资料、模型推断与可信规则应分别包装；需要长期保留的业务事实不能只依赖临时注入。

原生 API 的两个限制也需保留：

- followup 支持 `Idempotency-Key`，但公开文档还区分 `queued_streaming` 与 `queued_idle`；后者说明等待下一次运行。不能把接收回执解释为立即启动，也没有据此确认跨进程重启的去重保证。
- `--listen` 与 `serve api` 会话事件日志覆盖不同；后者按需建立的日志可只包含带外事件，普通执行仍依赖对应 POST 的 SSE。当前 Tuck 持久化事件流不能直接替换为原生 events 订阅。

### 必须由 Tuck 补齐的产品能力

| 能力 | 原生没有覆盖的合同 | 最小归属 |
| --- | --- | --- |
| 角色与频道 | 产品职责、入口与主任务关联、编排记录可见性 | config、Manager、现有前端 |
| 入口派发独立主任务 | 创建 Tuck taskId、用户来源、任务目录、并发准入、结果归属 | Manager + 统一 MCP |
| 首次准备及隔离审查 | 首次判断、证据选择、输入版本、失效结果、有界跟进和结果发布 | orchestration + Manager + model_content |
| 交付业务判断 | 交付存在性、用户要求、质量证据、版本及文件快照 | 现有交付 owner + 编排决策 |
| 长期定时与业务触发 | 持久定义、日历时区、错过执行、重叠、触发去重、编辑撤销及运行关联 | 一个调度 owner + Store，派发到 Manager |
| 结果反馈与外部入口 | bot 身份校验、入口消息映射、相关任务访问、通知目的地和状态 | Tuck 接入与事件 owner |
| 上下文来源与授权 | 业务事实、作用域、有效性、投递意图与可信程度 | 事实 owner + model_content |

原生 [scheduler](https://github.com/docker/docker-agent/blob/v1.150.0/docs/tools/scheduler/index.md) 只在支持 recall 的运行会话中有效，计划不跨重启保留；every/daily 等是创建时起算的间隔，不是带时区的日历规则。它可用于明确的会话内临时提醒，但长期计划不双写到原生 scheduler 和 Tuck 调度器。

### 不作为当前需求的直接替代

| 能力 | 核对结果与决定 |
| --- | --- |
| [原生 tasks](https://github.com/docker/docker-agent/blob/v1.150.0/docs/tools/tasks/index.md) | 保存 JSON 待办、优先级和依赖；不会创建 Tuck 执行任务。不引入它作为第二套任务事实来源 |
| [session_context](https://github.com/docker/docker-agent/blob/v1.150.0/docs/tools/session_context/index.md) | 原生 list_sessions/read_session 可读历史，使用原生会话 ID 和存储范围。Tuck 协作工具使用 list_conversations/send_conversation_message，公开 conversation_id 沿用产品任务身份；当前默认不启用 session_context，后续历史查询仍须核对产品授权范围 |
| [evaluators](https://github.com/docker/docker-agent/blob/v1.150.0/docs/configuration/evaluators/index.md) | 原生类型化评估能力可用，但需配置版本 16 和独立评估协议；Tuck 当前网关仅支持 Chat Completions/Responses，不支持对应评估端点。暂不为分类／审查新增此协议，优先普通 Agent + structured_output |
| [board](https://github.com/docker/docker-agent/blob/v1.150.0/docs/features/board/index.md) | 是基于 tmux、Git worktree 和独立 CLI 进程的看板宿主；可参考交互，不嵌入 Tuck 成为第二套进程和恢复 owner |

### 对当前 Tuck 的具体改动边界

1. [engine_config.go](../internal/tuck/engine_config.go) 当前只投影 providers、models、agents、permissions，固定版本 15；[config.go](../internal/tuck/config.go) 顶层也有明确字段校验。路由、预算及 evaluator 不能假定已具备产品入口。只为实际采用的功能增加投影，不批量透传全部原生顶层配置。
2. [LocalTools.actorAllowed](../internal/tuck/local_tools.go) 目前依据任务主代理及 sub_agents 闭包校验。采用同会话路由前定义真实路由身份和许可范围；不要把路由目标伪装成子代理或扩成全部 Agent 都可调用。
3. [Manager.Create](../internal/tuck/tasks.go) 当前从工作台 AssistantCatalog 选择助手。内部编排运行需要明确用途及准入，复用创建和执行能力，不为绕过目录约束而把内部代理加入普通用户助手列表。
4. [model_gateway.go](../internal/tuck/model_gateway.go) 只接收两种聊天协议。hook 的模型选择、压缩模型和预算接入需沿现有资源配置投影；不得使用示例中的直连 provider 绕过平台身份和准入。
5. [orchestration.go](../internal/tuck/orchestration.go) 当前是自动补交付的纯策略。扩展时保持“事实 → 决定”，I/O、执行与队列仍由现有 owner 负责；Tuck 添加的模型内容继续统一构造。

### 采用与退出条件

优先实施“普通原生 Agent + 必要工具 + structured_output”的编排路径，以及现有公开会话接口上的入口派发。只有确定需要共享会话流水线时，才增加原生路由和配置版本 16；持久化定时是独立的产品需求，不以等待原生 scheduler 扩展为前提。

| 适配 | 删除或收敛条件 |
| --- | --- |
| Tuck 后续轮次调度适配 | 原生公开接口完整覆盖产品队列操作、来源、逐轮授权及恢复后，迁移到唯一原生队列 owner |
| Tuck 持久化时间触发 | 原生公开调度覆盖持久计划、日历时区、错过执行、重叠、去重及产品授权后，再评估迁移 |
| Tuck 隔离编排组织 | 原生公开编排可表达相同隔离、输入版本、结果确认与生命周期时，收敛适配 |
| 产品身份、平台准入、交付和来源 | 属于 Tuck 职责，原生增加通用工具不构成删除条件 |

实施前至少验收：首次准备在续聊／恢复／分支下不重复；编排失败不形成业务结论；路由后的 MCP 目录和权限准确；结构化结论与实际模型输入一致；入口断开后任务可持续跟踪；followup 的忙闲、确认和重启语义；调度编辑／取消与临界派发、重复触发及结果未知的处理。

## 外部实践依据

- [Anthropic：Building effective agents](https://www.anthropic.com/engineering/building-effective-agents)：确定性流程与动态代理分工，支持本方案按需引入编排代理。
- [Anthropic：Context engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)：按需检索和隔离探索，支持只向目标传递必要背景。
- [OpenClaw：Automation payloads](https://docs.openclaw.ai/automation/cron-jobs/payloads)：执行上下文与结果目的地分别选择，作为入口和定时任务的设计参考。
- [Temporal：Schedule](https://docs.temporal.io/schedule)：借鉴重叠、错过执行和暂停语义；当前方案不引入 Temporal 服务。
