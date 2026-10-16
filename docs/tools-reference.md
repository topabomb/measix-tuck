# 工具参考

本文列出官方基线 Docker Agent 默认配置中的 21 个常用原生工具，以及 Tuck 提供的 5 个工具（会话协作 2 个、交付 1 个、工作区文件传输 2 个）。设置 → Agent → 工具以“Docker Agent 原生工具”和“Tuck 工具”两张卡片组织；MCP 独立选择。原生参数以当前运行版本实际发给模型的工具目录为准，以下清单不是引擎准入白名单；企业和本地配置 MCP 的工具由连接动态发现，不属于固定清单。配置 engine.mode=external 时，以该运行实例提供的工具定义为准。

下文 `?` 表示可采用默认值。原生工具经过模型协议转换后，部分可选字段会出现在 `required` 中并允许 `null`；此时应按模型收到的 schema 填写 `null`，不能照抄省略字段的示意调用。返回格式由各工具负责，只有 DAV 工具统一使用本文中的 `status` / `reason`。

## 装配与工作目录

| 默认 Agent | 原生工具 | Tuck 工具 |
| --- | --- | --- |
| root / video / image / publishing | 本地文件、shell、待办、用户提问、后台子代理、任务委派 | 会话协作与交付；企业 DAV 可用时提供上传和下载 |
| worker | 本地文件、shell、待办 | 企业 DAV 可用时提供上传和下载 |
| reviewer | 目录树、列目录、单文件读取、多文件读取、内容搜索 | 不提供会话协作和 DAV 传输 |

任务工作目录支持自动生成、指定已有目录和 `none`，详见 [工作目录配置](configuration.md#模型助手与工作目录)。自动模式按任务独立生成，指定模式使用用户选择的目录；`none` 不向原生会话设置工作目录，也不承诺 Tuck 本地附件、带文件交付或 DAV 传输能力。

在有明确工作目录时，原生文件工具默认以该目录为基准，配置 `allow_list: ["."]`，用户附件、工具产物和 DAV 下载文件可在其中使用；这里不采用 Android 的受管 `/upload/<file>` 路径约定。`shell` 在后端所在环境运行，拥有该进程的权限，不继承文件工具的 `allow_list` 路径限制；`none` 也不关闭原生工具或构成 Docker 沙箱。

企业文件管理界面通过 Core Files API 管理远端工作区；两个 DAV 工具通过该空间明确关联的 DAV 上传单文件、下载一批文件。两条链路指向同一企业空间，本地工作目录仍由 Tuck 管理。MCP 连接的远端文件系统和凭据属于各自服务，不能凭同名路径或工具名推断它们就是企业工作区。

工具调用沿用原生审批模式。实际可用工具还取决于 Agent 配置、企业授权和连接状态；助手名称不会自动赋予视频生成、浏览器操作或发布能力。配置入口见 [助手配置](configuration.md) 和 [原生对接](architecture.md)。

## 本地文件系统：9 个

| 工具 | 参数 | 行为和结果 |
| --- | --- | --- |
| `directory_tree` | `path: string` | 返回指定目录的递归 JSON 树；当前固定版本没有 `max_depth` 参数 |
| `list_directory` | `path: string` | 列出指定目录中的文件和子目录 |
| `read_file` | `path: string`，`line?: integer`，`limit?: integer` | 读取文件；文本行号从 1 开始，默认从首行读到末尾；支持的图片返回图片内容 |
| `read_multiple_files` | `paths: string[]`，`json?: boolean` | 读取多个文件；`json` 控制结果包装格式 |
| `search_files_content` | `path: string`，`query: string`，`is_regex?: boolean`，`excludePatterns?: string[]` | 从指定目录搜索文本内容，可使用正则和排除模式 |
| `write_file` | `path: string`，`content: string` | 创建或完整替换文本文件；空字符串创建空文件 |
| `edit_file` | `path: string`，`edits: {oldText: string, newText: string}[]` | 按精确文本序列修改；原文不匹配时返回错误，应重新读取后修改 |
| `create_directory` | `paths: string[]` | 创建目录及缺失的父目录 |
| `remove_directory` | `paths: string[]` | 删除空目录；不递归删除非空目录 |

前 5 个是 reviewer 的默认文件工具。普通读取不等于解析任意二进制格式；PDF、Office、音视频等内容理解取决于后续使用的解析工具或模型能力。

## 命令、待办和用户输入：6 个

| 工具 | 参数 | 行为和结果 |
| --- | --- | --- |
| `shell` | `cmd: string`，`cwd?: string`，`timeout?: integer` | 执行命令，返回输出或错误；工作目录默认 `.`，超时单位秒，默认 30；Windows 默认使用 PowerShell |
| `create_todo` | `description: string` | 创建一个待办，返回创建项和当前待办 |
| `create_todos` | `descriptions: string[]` | 批量创建待办，返回创建项和当前待办 |
| `update_todos` | `updates: {id: string, status: string}[]` | 按待办 ID 更新状态：`pending`、`in-progress`、`completed`；结果包含当前待办及未找到项等反馈 |
| `list_todos` | 无 | 返回当前待办及相关提示 |
| `user_prompt` | `message: string`，`title?: string`，`schema?: object` | 通过原生 elicitation 请求用户输入；标题默认 `Question`，可用 JSON Schema 描述答案结构，返回用户回答 |

待办记录用于组织执行，不证明工作已经完成。`user_prompt` 用于缺失信息或确实需要用户判断的事项；用户回答由现有工作台阻断流程提交，不把未回答视为同意。

## 原生子代理：6 个

| 工具 | 参数 | 行为和结果 |
| --- | --- | --- |
| `transfer_task` | `agent: string`，`task: string`，`expected_output: string` | 同步委派给配置中的子代理，等待并返回其结果；固定版本 schema 要求填写 `expected_output` |
| `run_background_agent` | `agent: string`，`task: string`，`expected_output?: string` | 启动后台子代理，立即返回后台任务 ID |
| `list_background_agents` | 无 | 返回后台任务及最近报告状态 |
| `view_background_agent` | `task_id: string` | 查看该后台任务的状态与已有输出 |
| `wait_background_agents` | `task_ids: string[]`，`timeout?: integer` | 等待 1–100 个不同后台任务 ID，返回任务结果与 `all_done`；输出受原生工具限制 |
| `stop_background_agent` | `task_id: string` | 请求停止指定后台任务，返回执行反馈 |

`agent` 必须来自当前 Agent 的 `sub_agents`，默认主助手可使用 worker 和 reviewer。后台任务使用子代理自己的模型和工具配置。后台执行不能交互处理审批，需按原生后台权限规则运行。

等待超时单位秒，默认 300，`0` 使用默认值，上限 3600。超时或取消等待不会停止后台任务；某个任务失败也不会提前结束对其余任务的等待。需要停止时使用 `stop_background_agent`。

后台任务 ID、原生会话 ID 和 Tuck `task_id` 是不同身份。后台工具只使用 `run_background_agent` 返回的 ID；独立子会话及历史可能稍后才可用。启动成功或最近状态报告不能被描述为持续执行或已完成，详见 [生命周期边界](architecture.md#生命周期边界)。

## 会话协作：2 个

### `list_sessions`

无参数。返回 `{sessions: [...]}`，每项包含 `task_id`、`title`、`agent`、`assistant_name`、`status`、`queued_prompts` 和 `pending_interaction`。只列出当前未归档主会话，不包含原生子会话。后续发送使用这里的精确 `task_id`，不能使用标题或后台任务 ID 代替。 Tuck 会话 ID 为服务端生成的 16 位随机 base62；原生工具调用、后台任务和问题的 `elicitation_id` 保持原生值，不能按 Tuck ID 格式改写或推断。

工具通过 POST 调用既有 `/api/tools/list-sessions`，避免原生 GET API 工具只接受字符串参数的限制；HTTP GET 入口继续可用。额外字段忽略，不会扩大列表范围或启用过滤。

### `send_session_message`

| 参数 | 必填 | 含义 |
| --- | --- | --- |
| `target_task_id: string` | 是 | `list_sessions` 返回的目标主会话 ID |
| `message: string` | 是 | 去除首尾空白后非空的消息；输入长度最多 100,000 字符 |

目标必须是未归档主会话。执行中的会话收到立即补充；空闲会话启动新轮；等待审批或回答等交互的会话拒绝投递。消息带协作来源标记，成功结果如下：

```json
{"accepted":true,"target_task_id":"<列表返回的ID>","delivery":"steer"}
```

`delivery` 为 `steer` 或 `next_turn`；`accepted` 只表示已接收，不表示目标任务完成。仅在用户明确要求联系该会话时发送，不因为收到协作消息而自动回复。工具不负责跨会话复制文件。

目标 ID 去除首尾空白后查找；目标 ID 和消息仍必须是字符串。额外字段忽略，不改变目标、投递方式或等待交互时的拒绝规则。

## 企业 DAV 文件交换：2 个

原生模型目录中的名称是 `workspace_webdav_upload` 和 `workspace_webdav_download_files`；本地 MCP 定义为 `webdav_upload` / `webdav_download_files`。连接由 Core `WorkspaceProjection.fileExchangeWebdavResourceId` 和 Snapshot v5 的 `webdav[]` 明确确定，不接收连接 ID。没有确定可用目标时两者一起隐藏；只读 DAV 仍成对提供，上传返回 `webdav_read_only`。reviewer 默认不装配这两个写入工具，下载也会写本地文件。

每轮捕获企业身份、空间、DAV 授权和当前任务目录；调用及下载发布前重新核对授权、配置与登录状态。本轮中途切换目标不会使调用转向新空间；条件变化时拒绝执行。没有目录同步、差异比较、后台同步或自动重试。协议与实现见 [企业工作区](architecture.md#api与文件)。

### `workspace_webdav_upload`

| 参数 | 必填 | 含义 |
| --- | --- | --- |
| `file_path: string` | 是 | 当前任务目录内现有普通文件的精确相对路径 |
| `remote_path: string` | 是 | 企业 DAV 根目录内的相对目标文件路径，包含文件名 |
| `overwrite: boolean` | 否 | 默认 `false`；明确允许替换已有远端文件 |

上传不改变本地文件，自动创建缺失的远端父目录；父路径是文件或目标是目录时失败。新建使用 `If-None-Match: *` 防止并发覆盖；显式替换使用服务器强 ETag 和 `If-Match`。XML 属性缺少有效强 ETag 时通过 HEAD 获取实际 HTTP ETag，仍不可用就失败。

```json
{"file_path":"reports/report.pdf","remote_path":"reports/report.pdf","overwrite":false}
```

成功结果中 `bytes` 是上传字节数，`overwritten` 表示实际是否替换了已有文件：

```json
{"status":"completed","remote_path":"reports/report.pdf","bytes":102400,"overwritten":false}
```

### `workspace_webdav_download_files`

| 参数 | 必填 | 含义 |
| --- | --- | --- |
| `remote_paths: string[]` | 是 | 1–20 个企业 DAV 根目录内的相对源文件路径，不接受目录、通配符展开或重复目标文件名 |
| `local_directory: string` | 是 | 当前任务目录内的相对目标目录，如 `downloads/reports`；`"."` 表示工作目录本身 |
| `overwrite: boolean` | 否 | 默认 `false`；一个顶层参数统一作用于整个 `remote_paths` 数组，明确允许替换已有本地普通文件 |

自动创建缺失的本地目录，按源文件 basename 原样保存。未传 `overwrite` 或为 `false` 时，已有目标返回 `local_file_exists`；`true` 允许整批替换已有本地普通文件，不允许替换目录或符号链接。所有路径、目录边界、已有目标和批内同名冲突都在创建目录及下载 GET 前检查；Windows 同名检查不区分大小写。批内同名仍返回请求级失败，本批不写入。每个文件流式下载到临时文件，完成后再次核对授权和目标目录的真实路径；真实路径改变时拒绝发布。默认以不可覆盖的原子操作发布，明确覆盖时原子替换目标。失败或取消保留该文件的原有内容，清理本次临时文件；已经完成的替换保留。

```json
{"remote_paths":["reports/report.pdf","tables/data.csv"],"local_directory":"downloads/review","overwrite":false}
```

返回按输入顺序排列的 `files`。全部成功为 `completed`，部分成功为 `partial`，全部失败为 `failed`；`partial` 和 `failed` 标记 MCP 工具错误。请求级预检失败还包括 `reason/detail`，`files: []` 表示没有开始逐项下载。逐项缺失或服务错误继续处理其余文件；取消或授权失效停止后续下载，将未开始项标记 `not_attempted`。已经成功的文件保留，修复原因后只重试失败项，避免重传成功项触发同名保护。

```json
{"status":"partial","local_directory":"downloads/review","completed_count":1,"failed_count":1,"files":[{"remote_path":"reports/report.pdf","status":"completed","file":{"path":"downloads/review/report.pdf","name":"report.pdf","mime_type":"application/pdf","bytes":102400}},{"remote_path":"tables/data.csv","status":"failed","reason":"webdav_file_not_found","detail":"The remote file does not exist."}]}
```

`file.path` 是后续本地工具或上传使用的路径。批量工具只返回元数据，包括图片，不把多张图片自动加入上下文；需要读取时使用本地文件工具。原始落盘字节不改变。传输不套用聊天附件的 16 MiB 限制，继续受 Core 授权、存储和服务限制约束；没有新增目录同步、总容量配额或自动重试。

### 路径、容错与失败

- 远端路径必须是 DAV 根目录内相对文件路径，拒绝绝对路径、空段、`.`、`..`、反斜杠和控制字符。
- 本地路径必须留在当前任务真实工作目录内，符号链接不能越界；Windows 本地路径兼容反斜杠，拒绝无效 Windows 文件名。
- 可选参数为 `null` 时使用默认值；批量下载的 `remote_paths` 和 `local_directory` 必填。上传与下载的 `overwrite` 均默认为 `false`，兼容字符串 `"false"` / `"true"`，忽略其大小写和首尾空白；其他类型或含糊值仍返回 `invalid_arguments`。
- 调用只使用当前工具定义的参数，其他字段忽略，不新增其暗示的功能；例如 `create_parents`、`timeout_secs`、`connection_id` 不改变既有父目录创建、超时或固定企业目标。上传只有明确的 `overwrite=true` 才允许远端条件覆盖；下载只有明确的 `overwrite=true` 才允许替换整批的已有本地普通文件。
- 缺少必需参数、已定义参数的错误类型及非法路径仍返回 `invalid_arguments`；跨任务路径、保护目录与访问授权规则不因参数兼容而放宽。

普通失败返回 JSON，并标记 MCP 工具错误：

```json
{"status":"failed","reason":"webdav_file_exists","detail":"远端文件已存在；如需替换，请设置 overwrite=true。"}
```

| `reason` | 含义与处理 |
| --- | --- |
| `invalid_arguments` | 已定义参数的类型或路径无效；按 schema 修正，额外字段按上述容错规则忽略 |
| `local_file_not_found` | 本地源不存在、不是普通文件或无法读取 |
| `local_file_exists` | 本地下载目标已存在；换用其他目录，或从重试清单移除已成功文件 |
| `local_name_conflict` | 批内多个源文件映射到同一本地文件名；拆分到不同目录下载 |
| `cancelled` / `not_attempted` | 当前文件已取消或停止后未开始，前面成功的文件保留 |
| `webdav_file_not_found` | 远端源文件不存在 |
| `webdav_not_file` | 指定远端目标是目录 |
| `webdav_parent_not_directory` | 远端父路径不是目录或不能建立所需目录 |
| `webdav_file_exists` | 未允许同名替换，或上传条件写入返回 412；核实当前文件再决定是否替换 |
| `webdav_version_unavailable` | 缺少安全替换所需的强 ETag |
| `webdav_version_changed` | 下载读取前远端版本变化；重新核实源文件 |
| `webdav_read_only` | 当前 DAV 无写入权限 |
| `webdav_path_not_permitted` | 远端路径包含 `.agent-space*` 保留目录；先用远程工作区工具将选定截图或产物复制到普通项目目录，再传输该文件；不自动重试受保护路径 |
| `tool_not_permitted` | 本轮身份、任务目录、配置、登录或访问授权失效 |
| `webdav_unavailable` | 企业文件服务或属性查询不可用 |
| `webdav_upload_failed` / `webdav_download_failed` | 服务未确认该操作成功；依据 detail 核实目标 |
| `outcome_unknown` | 远端写入可能已经完成；返回 `status: "unknown"`，先通过企业文件管理器核实，不自动重试 |

取消沿原生取消流程处理；已经发起的远端写入无法确认时返回未知结果，不承诺远端回滚。

## 动态 MCP 与维护

企业 MCP 从当前 Snapshot 和服务器工具目录发现；`ALL` / `ALLOWLIST` 按平台下发规则过滤，实际调用再次检查企业授权。原始工具名可能带服务命名空间和映射后缀，应使用模型当前收到的精确名称与 schema。默认只读 Agent 仅获得符合只读规则的工具。

访问令牌刷新与资源更新见 [资源更新与续聊](architecture.md#平台与安全)；MCP 会话恢复及调用重试由官方引擎管理，见 [MCP 生命周期](architecture.md#docker-agent-合同)。会话恢复不重新下发 Core 配置，也不重新签发 Agent Space 密钥。

本地配置 MCP 来自 `localMcp`，按连接的 `enabled`、凭据和 `agents` 绑定装配。可选 `localMcp[].tools` 使用服务器原始工具名作为精确白名单；省略时开放全部工具，空数组表示全部停用且不生成该连接的原生 toolset，重复名称拒绝。绑定和 `readonly` 继续约束最终可见工具，示例见 [本地 MCP 工具选择](configuration.md#本地-mcp-工具选择)。平台连接的本机选择保存在 `platformMcpTools`，只缩小平台授权范围，空数组同样停用全部工具。工具含义、参数、返回值和文件访问范围由该服务负责；认证令牌使用同一 JSON 的 `authToken` 字段并由文件权限保护，与企业 DAV、本地任务目录没有关联。动态服务清单不写死在本文。

## 对话交付：`tuck_deliver_result`

受管主助手默认启用，`workbench.assistants[].deliveryTool=false` 可关闭；子代理不获得此工具。不要求企业登录或 WebDAV 可用，但运行时使用官方请求头模板传递每轮交付令牌。设置 → 编排 →“启用交付要求”控制正常完成但未交付时是否自动续轮，默认关闭。

参数为必填 `summary` 和可选 `files`。摘要支持 Markdown，通常 1–3 句，最多 1,000 字符；只列出需要交付的关键文件，每项必填相对工作目录的 `path`，选填 `description`。无需文件时省略 `files`，`files: []` 或 `null` 同样表示无文件。不提供由模型填写的 request_id、版本号或会话身份。

一次最多 10 个关键文件；未变文件仅在使用本次成果必需时附带，不重复提交历史整套文件。单文件不超过 512 MiB，合计不超过 2 GiB。成功后保存独立副本并增加版本；普通对话和任务频道使用相同记录回看。源文件后续修改、删除或任务归档不会改变历史交付。路径、链接、复制期间的变化及文件大小均由服务验证，失败整次不建版。持久化与保留规则见 [交付存储合同](database-design.md#交付与清理)，实际模型描述与参数以 [工具定义](../internal/tuck/local_tools.go) 为准。

维护入口：唯一配置为 tuck.config.json；原生装配在 internal/tuck/models.go，会话协作在 app.go / tasks.go，本机 MCP 在 local_tools.go，DAV 在 workspace_tools.go，企业代理在 mcp.go，交付在 deliveries.go。更改名称、参数或执行规则时同步本文、配置合同和对应测试。

对应测试和真实服务验收要求见 [验证指南](verification.md#真实服务与已知限制)；本工具目录不声明当前部署已完成所有真实副作用验收。

### 工具设置中的启停

两张入口卡片分别进入原生工具同页列表和 Tuck 工具开关列表。`agents.<agent>.disabledTuckTools` 支持 `list_sessions`、`send_session_message`、`workspace_webdav_upload`、`workspace_webdav_download_files`，缺省全部启用，保存并应用后按 Agent 过滤原生运行时的工具目录。交付沿用 `workbench.assistants[].deliveryTool`。停用不会改变其他 Agent；原有主/子代理边界、只读筛选和企业文件权限保持有效。
