# Docker Agent 内置工具参考

本文是 Tuck 对 **Docker Agent 内置工具**的分类、版本清单及调用合同参考，只讨论引擎提供的工具和接入能力，不收录 Tuck 自有工具或企业、第三方 MCP 的实际工具目录。分类名称采用 Tuck 的约定，工具名、功能和参数依据指定版本的官方实现。

## 版本与依据

基线为官方 **v1.152.0**，提交 `e7f1ec394dfa47aea0b066092a35c72564b192cb`，核对日期 **2026-10-23**。二进制版本与 SHA256 已按[发行清单](../internal/tuck/contract/engine-release.json)核对。以下链接均固定到该 tag，不能把较新主分支的行为直接套用到本清单。

证据来源：官方[工具集注册表](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/teamloader/toolsets/toolsets.go)、[装配实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/teamloader/teamloader.go)、各节所引官方源码，以及官方 Windows 二进制公开命令 `debug toolsets <agent-file> --json` 导出的工具名称、描述和参数 schema。名称、参数与装配验证不等于全部工具的真实业务调用验收。

## 分类定义：三类工具加一类接入能力

Docker Agent 以工具集为配置单位，为 Agent 装配工具。引擎内置工具分为“引擎基本工具、引擎配置型工具、市场接入工具”三类；引擎另提供“外部工具接入器”，用于接入由外部动态提供的工具。

| 分类名称 | 定义 | v1.152.0 显式工具集类型数 |
| --- | --- | ---: |
| 引擎基本工具 | 在正常引擎环境中，仅启用 `type` 即可提供工具，无需额外专用配置；工具定义和执行逻辑由引擎提供。市场工具单独归类 | 19 |
| 引擎配置型工具 | 需要专用配置或功能条件，具体工具定义由引擎或本地配置决定。数量可以随本地配置变化，或从引擎预设候选中筛选 | 7 |
| 市场接入工具 | 引擎实现的搜索市场、启用和管理服务等具体工具。接入后获得的服务工具另属外部工具 | 1 |
| 外部工具接入器（能力范围） | 从外部目录或规范取得工具、技能或操作定义，将实际调用交给对应外部服务。接入器本身不计作工具 | 3 |

四行覆盖注册表全部 **30 种显式 `type`**。按本分类，基本工具共有 65 个条目，跨工具集去重后 **62 个**；市场工具初始 **3 个**，启用服务后 **5 个**管理工具。配置型工具数量依赖配置，另外还有功能配置自动装配的工具，见后文。

分类依据是职责和定义来源，不只是是否联网。例如 `api` 按本地配置构造工具，`openapi` 从外部规范生成操作集合；LSP 的 15 个候选工具由引擎实现，服务器仅决定候选中哪些可用，所以属于配置型工具。

分类定义和名称相对稳定；每类关联的工具集、工具名、数量、参数及触发条件均是版本事实，升级时重新核对。这里的“基本”不意味着任何环境都能成功执行：工作目录、操作系统、权限和任务对象等仍必须满足工具要求。

## 配置和参数阅读规则

在 `agents.<agent>.toolsets` 中填写工具集 `type`；在该条目的 `tools` 中筛选具体工具。例如只启用文件读取：

```yaml
toolsets:
  - type: filesystem
    tools: [read_file]
```

以上是 Agent 配置片段，不是完整配置文件。省略 `tools` 或填写 `tools: []` 均表示不筛选；停用整组应移除工具集条目。相同工具名按配置顺序保留第一项，不合并后续实例的权限、路径或处理逻辑。只读过滤、启动状态和自动装配还会改变最终模型目录。依据：[筛选](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/teamloader/filter.go)、[同名去重](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/agent/agent.go#L485)。

本文逐工具记录功能、调用参数及重要返回/执行边界，遵循以下约定：

- **必填**依据工具自身输入 schema 的 `required`；**可选**表示 schema 不要求该字段。业务要求、默认值和范围另行注明，不能仅凭名称推断。
- 参数类型使用 `string`、`integer`、`boolean`、`object`、`string[]` 等。数组/指针字段在原始 schema 中可能同时允许 `null`，但 `required` 与是否允许 `null` 是两个维度，业务校验仍可能拒绝空值。表中以正常调用值说明，不将 schema 可空视为业务成功保证。
- **无参数 `{}`**也是明确的调用约定。原始目录可能表现为空对象 schema，也可能 `parameters: null`，后者表示未声明参数 schema，不表示没有调用合同。
- 安装配置与调用参数分开：例如 Webhook URL 是工具集配置，调用只传消息；`open_url` 调用不接受 URL。
- 引擎可以为部分工具添加可选 `description: string`，用于说明本次调用；模型供应商的严格 schema 转换也可能调整可选/可空表示。表格描述业务参数，模型实际请求中的 schema 是当次调用的直接依据。[说明参数实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/description.go)
- 动态命名工具也有功能说明和参数 schema；这些内容从配置生成，不存在可在版本文档中写死的真实名称/字段全集。相关条目明确列出生成规则及标为示例的具体合同。

## 引擎基本工具

### filesystem：9 个

路径相对引擎工作目录解析；访问同时受 `allow_list`、`deny_list`、符号链接检查等限制。`shell` 不继承这些文件工具的路径范围。不要把源文件读取、模型理解能力和专用格式解析混为同一个操作。

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `directory_tree` | 递归返回文件和目录的 JSON 树，节点含名称、类型及子节点 | `path: string`：起始目录 | 无 |
| `list_directory` | 返回指定目录的文件及子目录列表 | `path: string`：目录 | 无 |
| `read_file` | 读取文件；文本返回内容，jpg/png/gif/webp 可作为图片内容返回 | `path: string`：文件 | `line: integer`：文本起始行，1 起算，默认第一行；`limit: integer`：最多读取行数，默认到末尾。行参数仅用于文本 |
| `read_multiple_files` | 一次读取多份文件，返回汇总结果 | `paths: string[]`：文件路径列表 | `json: boolean`：是否以 JSON 形式组织结果，默认 false |
| `search_files_content` | 在目录范围中搜索文件正文，返回匹配信息 | `path: string`：起始目录；`query: string`：搜索内容 | `is_regex: boolean`：默认 false；`excludePatterns: string[]`：排除模式。没有独立的 `glob` 调用参数 |
| `write_file` | 创建文件或完整覆盖已有文件内容 | `path: string`；`content: string` | 无；这是整文件替换 |
| `edit_file` | 对文件执行精确文本替换，返回编辑结果 | `path: string`；`edits: object[]`，每项必填 `oldText: string`、`newText: string` | 无；`oldText` 是要匹配的原文，不是行号或正则表达式 |
| `create_directory` | 创建一个或多个目录，支持嵌套目录 | `paths: string[]`：目录路径 | 无 |
| `remove_directory` | 删除一个或多个空目录 | `paths: string[]`：目录路径 | 无；不提供递归删除非空目录的参数 |

依据：[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/filesystem/filesystem.go)、[输入类型](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/filesystem/types/types.go)。

### file：3 个，与 filesystem 重复

`file` 复用 `filesystem` 实现，仅暴露文件操作；不增加不同名工具。每个工具的参数和行为与上表相同：

| 工具 | 功能 | 调用参数 |
| --- | --- | --- |
| `read_file` | 读取单文件正文或图片内容 | 必填 `path: string`；可选 `line: integer`、`limit: integer` |
| `write_file` | 创建或完整覆盖文件 | 必填 `path: string`、`content: string` |
| `edit_file` | 精确替换文件文本 | 必填 `path: string`、`edits: {oldText: string, newText: string}[]` |

同时装配两组时三个名称去重，而非合并两套路径配置。[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/file/file.go)

### shell：1 个

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `shell` | 在引擎主机执行命令，返回命令输出或执行错误 | `cmd: string`：命令文本 | `cwd: string`：工作目录，默认 `.`；`timeout: integer`：超时秒数，默认 30。Shell 及语法随主机环境而定 |

调用不是沙箱声明，也不继承 `filesystem.allow_list`。需异步保留进程时使用下一组后台工具。[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/shell/shell.go)

### background_jobs：5 个

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `run_background_job` | 异步启动命令，立即返回 job ID | `cmd: string`：命令 | `cwd: string`：默认 `.` |
| `list_background_jobs` | 列出后台命令的状态和运行时间等信息 | 无参数 `{}` | 原始目录未声明参数 schema |
| `view_background_job` | 查看某个后台命令的状态和已捕获输出 | `job_id: string` | 使用启动工具返回的 ID |
| `stop_background_job` | 终止后台命令及其子进程 | `job_id: string` | 这是停止操作，不是停止等待 |
| `wait_background_job` | 等待命令完成，返回退出码和输出；已完成时立即返回已有结果 | `job_id: string` | `timeout: integer`：等待秒数，默认 60；超时返回当前输出与超时信息，不等同于停止进程 |

这里的 job 是操作系统命令进程，区别于后台 Agent 任务。[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/backgroundjobs/backgroundjobs.go)

### git：5 个

这些工具只读当前配置工作目录所对应的 Git 仓库，不提供提交或推送操作。

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `git_status` | 当前分支及暂存、未暂存、未跟踪文件状态 | 无参数 `{}` | 无 |
| `git_log` | 最近提交的哈希、日期、作者和主题 | 无 | `limit: integer`：默认 20，上限 200；`path: string`：只看涉及该路径的提交 |
| `git_branches` | 本地分支列表，并标记当前分支 | 无参数 `{}` | 无 |
| `git_show` | 提交元数据、消息、变更文件及增删行数 | 无 | `ref: string`：默认 `HEAD` |
| `git_blame` | 指定文件逐行对应的提交和作者 | `path: string`：相对仓库根目录的文件路径 | `rev: string`：默认 `HEAD` |

依据：[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/git/git.go)。

### todo：4 个

用于执行中的待办整理。状态为 `pending`、`in-progress`、`completed`，与 `tasks` 的状态拼写不同。

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `create_todo` | 创建单个待办，返回创建项和当前待办 | `description: string`：事项描述 | 无 |
| `create_todos` | 批量创建，返回创建项和当前待办 | `descriptions: string[]` | 无 |
| `update_todos` | 按 ID 更新状态，返回更新项、未找到项和当前待办 | `updates: object[]`，每项必填 `id: string`、`status: string` | 状态取上述三个值 |
| `list_todos` | 返回当前待办及未完成事项提醒 | 无参数 `{}` | 原始目录未声明参数 schema |

依据：[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/todo/todo.go)。

### tasks：8 个

管理可持久化的任务和依赖，工具集可配置 `path`，缺省使用 `tasks.json`。状态为 `pending`、`in_progress`、`done`、`blocked`；优先级为 `critical`、`high`、`medium`、`low`。它们不是 Agent 会话，也不是后台子代理。

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `create_task` | 创建任务，可设置优先级及前置依赖 | `title: string` | `description: string`：正文；`path: string`：从文件读取正文，给出时优先于 description；`priority: string`：默认 medium；`dependencies: string[]`：前置任务 ID |
| `get_task` | 返回单个任务完整信息及计算后的有效状态 | `id: string` | 未完成依赖会使任务表现为 blocked |
| `update_task` | 修改指定任务字段 | `id: string` | `title: string`、`description: string`、`path: string`、`priority: string`、`status: string`、`dependencies: string[]`；dependencies 替换依赖列表 |
| `delete_task` | 删除任务，同时从其他任务的依赖列表中移除该 ID | `id: string` | 无 |
| `list_tasks` | 按优先级排序列出任务，阻塞任务置后 | 无 | `status: string`：按有效状态筛选；`priority: string`：按优先级筛选 |
| `next_task` | 返回最高优先级、未完成且未阻塞的可执行任务 | 无参数 `{}` | 原始目录未声明参数 schema |
| `add_dependency` | 添加依赖，使一个任务等待另一个任务完成 | `taskId: string`：依赖方；`dependsOnId: string`：前置任务 | 注意字段为 camelCase |
| `remove_dependency` | 移除指定依赖关系 | `taskId: string`；`dependsOnId: string` | 不删除任务本身 |

依据：[实现及参数类型](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/tasks/tasks.go)。

### plan：8 个

管理跨 Agent 共享的具名计划，默认落盘到引擎数据目录。`name` 使用小写字母、数字、`-` 和 `_`；正文是 Markdown。`status` 为自定义字符串，不限定为 todo/tasks 的状态集合。写入和状态修改增加 revision，`last_known_revision` 是可选乐观锁，须与当前版本匹配。

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `write_plan` | 创建或完整替换计划正文 | `name: string`；`content: string` | `title: string`、`author: string`、`status: string`、`last_known_revision: integer`；未提供的元数据沿用已有值 |
| `read_plan` | 返回正文、标题、作者、状态及 revision | `name: string` | 无 |
| `update_plan_from_file` | 从文件读取正文，创建或完整替换计划 | `name: string`；`path: string` | `title: string`、`author: string`、`status: string`、`last_known_revision: integer` |
| `export_plan_to_file` | 将计划正文写入指定文件 | `name: string`；`path: string` | 返回写入结果，不把完整正文再次作为工具输出返回 |
| `set_plan_status` | 修改已有计划状态，保留正文 | `name: string`；`status: string` | `last_known_revision: integer` |
| `get_plan_status` | 只读取状态和 revision，不读取完整正文 | `name: string` | 无 |
| `list_plans` | 列出计划名称、标题、作者、状态及 revision | 无参数 `{}` | 原始目录未声明参数 schema |
| `delete_plan` | 删除指定计划 | `name: string` | `last_known_revision: integer` |

依据：[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/plan/plan.go)。

### session_context：2 个

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `list_sessions` | 按最近优先列出原生历史会话的 ID、标题、创建时间及消息数；排除当前会话 | 无 | `limit: integer`：默认 20，上限 100 |
| `read_session` | 读取历史会话文本以补充上下文 | `session_id: string`：会话 ID，或 `-1`、`-2` 等相对引用 | 长记录保留较新的消息并注明截断；默认文本上限 24000 字符，不提供修改历史的参数 |

依据：[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/sessioncontext/sessioncontext.go)。

### memory：5 个

管理引擎持久化记忆；可配置存储 `path`，缺省由引擎选择数据目录内的位置。调用通过引擎公开工具完成。

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `add_memory` | 新增记忆 | `memory: string`：记忆正文 | `category: string`：分类 |
| `get_memories` | 获取全部已存记忆 | 无参数 `{}` | 原始目录未声明参数 schema |
| `delete_memory` | 按 ID 删除记忆 | `id: string` | 无 |
| `search_memories` | 根据关键词和分类检索 | 无 | `query: string`：空格分隔关键词，全部需要匹配；`category: string`：分类过滤 |
| `update_memory` | 更新指定记忆正文，可同时更新分类 | `id: string`；`memory: string` | `category: string`；不能仅因为功能描述提到更新分类就省略 schema 必填的 memory |

依据：[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/memory/memory.go)、[默认路径](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/memory/create.go)。

### 单工具组：think、environment、datetime、calculator、random、fetch、user_prompt

下表每一行对应一个独立 `type`，不是一个名为“单工具组”的工具集。

| 工具集 / 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `think` / `think` | 记录思考内容用于会话推理，不获取新信息或修改外部数据 | `thought: string` | 无 |
| `environment` / `get_environment_info` | 返回操作系统及实际用于命令执行的 Shell 信息 | 无参数 `{}` | 无 |
| `datetime` / `get_datetime` | 返回按布局和时区格式化的当前时间字符串 | `format: string`：Go 时间布局，例如 `2006-01-02` | `timezone: string`：IANA 时区，例如 `Asia/Shanghai`；缺省或 Local 使用主机时区。不是 strftime 格式 |
| `calculator` / `calculate` | 用精确有理数运算计算表达式，返回精确值和十进制表示；舍入会标明 | `expression: string`：最长 8192 字节 | 支持 `+ - * / % ^`、括号、科学记数法；`%` 为整数余数，`^` 为整数幂；不执行代码，不支持变量或函数调用 |
| `random` / `random_int` | 返回闭区间内均匀随机整数 | `min: integer`；`max: integer` | 每次都需提供上下界，允许负数，必须 min ≤ max；不能把 schema 的 nullable 表示当作省略边界 |
| `fetch` / `fetch` | 通过 HTTP(S) GET 获取 URL，返回正文和响应元数据 | `urls: string[]`：至少 1 项；`format: string`：text、markdown 或 html | `timeout: integer`：请求超时秒数，默认 30，范围 1–300；受工具集配置的网络访问限制约束 |
| `user_prompt` / `user_prompt` | 请求用户回答并等待结果，通过原生交互机制返回回答 | `message: string`：问题 | `title: string`：默认 Question；`schema: object`：约束回答结构的 JSON Schema。schema 是回答合同，不是新的工具定义 |

依据：[Think](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/think/think.go)、[Environment](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/environment/environment.go)、[Datetime](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/datetime/datetime.go)、[Calculator](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/calculator/calculator.go)、[Random](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/random/random.go)、[Fetch](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/fetch/fetch.go)、[User Prompt](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/userprompt/userprompt.go)。

### background_agents：5 个

这些工具管理后台子代理，不是后台 Shell 进程。工具定义无需专用 toolset 参数即可装配；实际启动时，目标必须是配置中可委派的 Agent。后台原生子代理非交互执行，需要确认的调用不能当作已批准。

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `run_background_agent` | 启动后台子代理，立即返回 task ID | `agent: string`：目标子代理；`task: string`：任务说明 | `expected_output: string`：期望成果 |
| `list_background_agents` | 列出后台子代理任务的状态和运行时间 | 无参数 `{}` | 原始目录未声明参数 schema |
| `view_background_agent` | 返回执行中的缓冲输出或已完成的最终结果及状态 | `task_id: string` | 使用启动工具返回的后台任务 ID，不是原生会话 ID |
| `wait_background_agents` | 等待指定任务全部结束，按输入顺序返回状态和受限输出，含 all_done | `task_ids: string[]`：1–100 个不同任务 ID | `timeout: integer`：整组共享的等待秒数，默认 300，0 使用默认值，上限 3600。失败/停止任务不使其他任务提前结束等待；等待超时或取消不停止任务 |
| `stop_background_agent` | 请求停止指定后台子代理 | `task_id: string` | 与取消等待是不同操作 |

依据：[定义及执行](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/agent/agent.go)、[等待实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/agent/wait.go)。

### scheduler：3 个

调度的是未来交给 Agent 的指令，不是模型直接提供的 Shell 命令。调度由该工具集运行期维护，不能据此承诺跨进程重启的持久化调度。

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `create_schedule` | 安排稍后或周期性投递指令，返回计划信息 | `prompt: string`：届时要执行的指令；`when: string`：时间表达式 | `name: string`：显示名称。when 支持 `in:<dur>`、`at:<RFC3339>`、`every:<dur>`，以及 minutely/hourly/daily/weekly；周期最短 1 分钟 |
| `list_schedules` | 返回活动计划的 ID、表达式和下次触发时间 | 无参数 `{}` | 无 |
| `cancel_schedule` | 按 ID 取消计划 | `id: string` | 使用 create_schedule 返回的 ID |

依据：[工具及运行循环](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/scheduler/scheduler.go)、[时间表达式](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/scheduler/schedule.go)。

## 引擎配置型工具

显式 `type` 为 **api、script、webhook、model_picker、open_url、rag、lsp** 七种。以下每节同时说明装配配置和模型调用参数，二者不能混用。

### api：每项配置一个命名工具

装配配置位于 `api_config`：`name` 确定工具名，`instruction` 为工具功能说明，`endpoint`、`method`、`headers` 描述 HTTP 请求，`args` 定义输入属性，`required` 定义必填参数，`output_schema` 可描述返回结构。引擎按照配置执行 HTTP 请求，运行结果仍是响应正文字符串；声明 output_schema 不会自动将正文变成经过验证的业务对象。

| 工具 | 功能与结果 | 调用参数 |
| --- | --- | --- |
| 配置的 `api_config.name` | 按预定义请求访问 API，返回响应正文或错误 | 完全由 `api_config.args` 和 `required` 生成；无输入属性时约定为空对象 `{}`，不能虚构通用的 url 或 body 调用参数 |

例如工具名配置为 `lookup_record`、instruction 为“按 key 查询记录”、args 仅定义 `key: string` 且 required 为 `[key]`，则该实例的具体工具是 `lookup_record(key: string)`。这个名称和参数是示例，不是引擎预置工具。

依据：[类型定义](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/config/latest/types.go)、[参数与执行实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/api/client/client.go)。

### script：每个本地脚本定义一个工具

装配配置是 `shell.<工具名>` 映射：`cmd` 指定预设命令，`description` 描述用途，`args` 与 `required` 定义调用参数，`env`、`working_dir` 配置执行环境。没有 description 时，引擎由命令构造说明。工具数等于配置的脚本定义数。

| 工具 | 功能与结果 | 调用参数 |
| --- | --- | --- |
| `shell` 映射中的每个键 | 执行该键对应的预设脚本，返回执行输出或错误 | 由该项 args/required 生成；参数用于命令模板；无输入属性的脚本使用 `{}`。cmd 和 working_dir 属于安装配置，不自动成为模型参数 |

例如配置 `echo_text`，说明“输出给定文字”，命令引用 text，args 定义 `text: string` 且 required 为 `[text]`，则具体工具为 `echo_text(text: string)`。这是示例实例，不是一个固定名为 script 的工具。配置时应显式写出 required；以实际导出 schema 核对必填字段，不依赖描述文字推测。

依据：[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/shell/script_shell.go)。

### webhook：1 个

在 `webhook_config` 配置 `url`、`provider`、可选 headers；Telegram 还需 chat_id。调用者不选择目的 URL。支持的 provider 包括 generic（默认）、slack、discord、ifttt、telegram、mattermost、rocketchat、googlechat、teams。

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `send_webhook` | 向配置的目标发送通知；引擎负责交付重试及最终失败反馈 | `message: string`：通知文字 | `value2: string`、`value3: string`：仅 IFTTT 使用。调用被接受不应直接解释为远端已收到 |

依据：[定义、接收及交付实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/webhook/webhook.go)。

### model_picker：2 个

工具集 `models` 必须提供至少一个允许切换的模型引用；这些是配置，不是模型自行构造的供应商连接。

| 工具 | 功能与结果 | 调用参数 |
| --- | --- | --- |
| `change_model` | 请求切换当前会话使用的模型，返回切换反馈 | 必填 `model: string`，必须属于允许列表 |
| `revert_model` | 恢复会话原始模型 | 无参数 `{}`；原始目录未声明参数 schema |

依据：[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/modelpicker/modelpicker.go)。

### open_url：每项配置一个工具

工具集必须配置固定 `url`，可通过 `name` 改名。

| 工具 | 功能与结果 | 调用参数 |
| --- | --- | --- |
| `open_url`，或配置的 name | 在引擎所在主机的默认浏览器打开配置 URL，返回操作结果；不返回网页正文 | 无参数 `{}`。调用不接受任意 url，目标来自配置 |

依据：[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/openurl/openurl.go)。

### rag：每个知识库来源一个检索工具

工具集需要 `rag_config` 或 `ref`，配置文档来源、检索和所需模型等。最终名称优先使用知识库配置的工具名，其次工具集 name，缺省为 rag；功能说明随知识库配置生成。

| 工具 | 功能与结果 | 调用参数 |
| --- | --- | --- |
| 对应知识库的工具名（缺省 `rag`） | 检索该知识库，返回匹配内容及检索结果信息；不执行任意外部工具 | 必填 `query: string`：检索问题或关键词 |

多个知识库可以产生多个不同名检索工具，但每个来源的检索调用结构由引擎定义。依据：[实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/rag/rag.go)。

### lsp：15 个内置候选工具

工具集需配置语言服务器 `command`，以及按需配置 args、env、file_types 等。初始化后按服务器能力筛选以下候选；它不是从服务器导入任意 MCP 工具目录。位置参数统一为 `file: string`（源文件绝对路径）、`line: integer`（从 1 开始）、`character: integer`（行内位置，从 1 开始）。

| 工具 | 功能与结果 | 必填参数 | 可选参数及重要约定 |
| --- | --- | --- | --- |
| `lsp_workspace` | 返回工作区及语言服务器能力信息 | 无参数 `{}` | 无 |
| `lsp_hover` | 返回指定位置的类型签名、文档及悬停信息 | `file: string`、`line: integer`、`character: integer` | 无 |
| `lsp_definition` | 查找符号定义，返回文件路径和位置 | `file: string`、`line: integer`、`character: integer` | 无 |
| `lsp_references` | 查找符号在代码中的引用 | `file: string`、`line: integer`、`character: integer` | `include_declaration: boolean`：默认 true，是否包括声明本身 |
| `lsp_document_symbols` | 返回文件中的函数、类型、变量等符号层级 | `file: string` | 无 |
| `lsp_workspace_symbols` | 在工作区模糊搜索符号 | `query: string` | 无 |
| `lsp_diagnostics` | 返回指定文件的编译错误、警告和提示 | `file: string` | 无；诊断可能来自服务器推送 |
| `lsp_rename` | 跨工作区重命名符号，实际修改磁盘文件 | `file: string`、`line: integer`、`character: integer`、`new_name: string` | 写操作 |
| `lsp_code_actions` | 查询指定行或范围可用的修复、重构建议 | `file: string`、`start_line: integer` | `end_line: integer`：默认 start_line。此工具返回建议，不等同自动执行全部建议 |
| `lsp_format` | 按语言服务器规则格式化文件，实际修改磁盘文件 | `file: string` | 写操作 |
| `lsp_call_hierarchy` | 查询谁调用该符号或该符号调用谁 | `file: string`、`line: integer`、`character: integer`、`direction: string` | direction 为 incoming 或 outgoing |
| `lsp_type_hierarchy` | 查询父类型或子类型 | `file: string`、`line: integer`、`character: integer`、`direction: string` | direction 为 supertypes 或 subtypes |
| `lsp_implementations` | 查找接口或抽象方法的具体实现 | `file: string`、`line: integer`、`character: integer` | 无 |
| `lsp_signature_help` | 获取调用位置的函数签名及参数帮助 | `file: string`、`line: integer`、`character: integer` | 位置应落在函数调用参数区域 |
| `lsp_inlay_hints` | 获取文件或指定行范围的类型、参数名等内联提示 | `file: string` | `start_line: integer`：默认 1；`end_line: integer`：默认文件末尾 |

`lsp_workspace` 和 `lsp_diagnostics` 始终保留，其余候选按服务能力过滤。这里的 15 是完整候选数，不是每个语言服务器都提供 15 个可用操作的承诺。依据：[参数、定义及能力筛选](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/lsp/lsp.go)。

### 功能配置自动装配的引擎工具

以下工具同属引擎配置型工具，但没有对应的独立注册 `type`，所以不增加前述 7 种配置型工具集或 30 种总类型数。它们也不计入 62 个基本工具。表中列出 9 个工具名称；实际暴露取决于功能条件，不能简单相加为某个 Agent 的工具总数。

| 工具 | 装配条件及功能 | 调用参数与重要约定 |
| --- | --- | --- |
| `transfer_task` | 配置 sub_agents 后装配；同步委派任务并返回子代理结果 | 必填 `agent: string`、`task: string`、`expected_output: string`。该版本 expected_output 的文字说明虽写 optional，实际 schema 列入 required，调用须提供 |
| `handoff` | 配置 handoffs 后装配；将当前会话交给目标 Agent | 必填 `agent: string`，来自允许的 handoffs；与隔离子会话委派不同 |
| `read_skill` | 加载有效 Skills 后装配；读取指定技能正文 | 必填 `name: string`：技能名。技能正文可以按引擎规则展开配置的内容 |
| `read_skill_file` | 至少一个技能带支持文件时装配；读取技能目录内的支持文件 | 必填 `skill_name: string`、`path: string`；path 相对该技能目录，受路径边界校验 |
| `run_skill` | 至少一个技能声明 fork 模式时装配；在独立子上下文执行技能任务 | 必填 `name: string`、`task: string`；目标必须是 fork 技能 |
| `search_tool` | 配置 defer 后装配；按名称或说明搜索延迟工具 | 必填 `query: string`；搜索结果描述候选工具，不直接执行候选 |
| `add_tool` | 配置 defer 后装配；把延迟工具加入活动集合 | 必填 `name: string`：待激活工具的精确名称 |
| `run_tools_with_javascript` | 启用 Code Mode 后装配；运行 JavaScript 来组合调用已装配工具，返回脚本执行结果 | 必填 `script: string`。这是组合调用入口；部分不适合包装的工具仍单独暴露，不能简单把它与全部被包装工具累加 |
| `__structured_output__` | 启用工具模式结构化输出；按配置 schema 提交最终答案 | 参数对象严格由 structured_output.schema 定义；说明结合配置的 name/description 生成。有效调用结束该轮，必须单独调用，不能与其他工具混在同一响应中 |

依据：[自动装配](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/teamloader/teamloader.go)、[Transfer Task 参数](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/transfertask/types/types.go)、[Handoff 参数](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/handoff/types/types.go)、[Skills](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/skills/skills.go)、[Deferred](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/deferred/deferred.go)、[Code Mode](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/codemode/codemode.go)、[Structured Output](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/structuredoutput/structuredoutput.go)。

## 市场接入工具

### mcp_catalog：初始 3 个，启用服务后 5 个管理工具

这是内置 MCP 服务器市场入口。v1.152.0 从 Docker MCP Catalog 筛选远程 Streamable HTTP 服务，将 **34 个服务**的名称、说明、地址和认证方式作为 servers.json 静态快照编译进程序。34 是服务数量，不是工具数量。搜索发生在本地目录快照中；不是扫描用户已配置的 MCP，也不是每次联网搜索完整市场。allowed_servers / blocked_servers 可进一步限制目录范围。

| 工具 | 功能与结果 | 调用参数 | 提供条件 |
| --- | --- | --- | --- |
| `search_remote_mcp_servers` | 按 ID、标题、说明、分类和标签搜索市场，返回服务 ID、说明及认证要求 | 必填 `query: string`；不区分大小写，空字符串表示列出整个可见目录 | 初始提供 |
| `list_remote_mcp_servers` | 列出该工具集已启用的远程服务及连接状态 | 无参数 `{}` | 初始提供 |
| `enable_remote_mcp_server` | 连接选定服务并完成必要认证；成功后服务工具加入活动集合 | 必填 `id: string`：市场服务 ID，从搜索结果取得 | 初始提供 |
| `disable_remote_mcp_server` | 停用服务，移除它贡献的工具 | 必填 `id: string`：目录服务 ID | 至少一个服务已启用 |
| `reset_remote_mcp_server_auth` | 清除服务已保存的认证，供后续重新启用时建立新认证 | 必填 `id: string`：目录服务 ID | 至少一个服务已启用 |

这些管理工具由引擎实现，属于内置工具。启用服务后获取的工具由服务提供，属于外部工具，不计入上述 3/5 个管理工具。服务目录源为 [Docker MCP Catalog](https://desktop.docker.com/mcp/catalog/v3/catalog.json)；版本事实以[内置快照](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/mcpcatalog/servers.json)、[加载代码](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/mcpcatalog/servers.go)、[工具及参数实现](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/mcpcatalog/mcpcatalog.go)为准。

## 外部工具接入器：只记录能力边界

这三项不属于内置工具清单。接入器自身不是一个可调用工具名称；成功读取外部定义后才会形成实际工具，每个实际工具仍有名称、功能说明和输入约定。

| 接入器 `type` | 外部定义及执行来源 | 配置与数量边界 |
| --- | --- | --- |
| `mcp` | 从 MCP 服务获取工具目录、说明和 schema，由对应服务执行调用 | 配置 command、remote 或 ref 等连接信息；目录动态变化。原生支持 stdio、远程 HTTP/SSE、Docker MCP Gateway 等方式 |
| `a2a` | 从 Agent Card 读取技能，包装成工具并由远端 Agent 执行 | 配置 url 等；按实际 Card 投影。本版本 Card 没有技能时提供整个 Agent 的调用入口 |
| `openapi` | 从外部 OpenAPI 规范导入受支持的操作，由对应 API 服务执行 | 配置规范 url 等；按操作生成工具名，优先 operationId，缺省由 HTTP 方法和路径派生；参数依操作定义生成 |

没有与外部配置无关的具体工具名/参数全集，因此此处不虚构固定调用清单。它们与 api/script 的区别在于定义来自外部目录/规范，而非本地逐工具定义。mcp_catalog 使用 MCP 接入能力，不构成第四种协议或接入机制。

依据：[MCP](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/mcp/mcp.go)、[A2A](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/a2a/a2a.go)、[OpenAPI](https://github.com/docker/docker-agent/blob/v1.152.0/pkg/tools/builtin/openapi/openapi.go)。

## 审查与升级维护

本文是引擎侧参考，不代表宿主产品默认启用了所有条目。维护时以当前官方版本为单位更新同一正文，不复制多套长期清单；分类标准保留，版本事实重新核对。

1. 核对 tag、提交、二进制 version 与发行校验值，并比对工具集注册表及自动装配入口。避免只数 builtin 目录或只数可配置 type。
2. 在隔离临时配置及数据目录中，通过公开 CLI 导出目录。基本工具逐组与合并核对；配置型工具使用明确标注的合成配置，读取 schema 不执行真实 API、Webhook 或模型请求。
3. 每个具体工具必须有功能说明、参数合同（无参也明确写 `{}`）、必填/可选和重要默认值/范围。动态命名工具记录生成规则，实例示例标明不是固定内置名。说明文字与 schema 冲突时核对 schema 和 handler，不凭印象消解。
4. 核对同名去重、自动注入、能力过滤和模型协议转换。重要副作用按实现说明，例如 rename/format 写文件、等待超时不停止后台任务。
5. 检查文档链接、名称/参数覆盖及 diff；注明目录导出、源码审查和真实调用验收各自的证据范围。未运行真实外部服务不能写成已完成对应工具业务验收。

本版已核对基本工具逐组名称、描述及 schema，并用官方 CLI 合并导出确认 62 个不同名工具；另以合成配置导出了 api/script 示例、webhook、open_url、model_picker、transfer_task、handoff 的描述和 schema。LSP、RAG、条件出现的市场管理工具及其余自动装配工具依据固定 tag 的定义与参数代码核对。未执行这些工具的全部业务操作，未进行真实语言服务器、知识库、Webhook 或市场服务验收。
