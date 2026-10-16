# 工具参考

本文列出官方基线 Docker Agent 默认配置中的 21 个常用原生工具，以及 Tuck 提供的 7 个工具（会话协作 2 个、交付 1 个、工作区文件传输 2 个、文生图 1 个、附件识别 1 个）。设置 → Agent → 工具以“Docker Agent 原生工具”和“Tuck 工具”两张卡片组织；MCP 独立选择。原生参数以当前运行版本实际发给模型的工具目录为准，以下清单不是引擎准入白名单；企业和本地配置 MCP 的工具由连接动态发现，不属于固定清单。配置 engine.mode=external 时，以该运行实例提供的工具定义为准。

下文 `?` 表示可采用默认值。原生工具经过模型协议转换后，部分可选字段会出现在 `required` 中并允许 `null`；此时应按模型收到的 schema 填写 `null`，不能照抄省略字段的示意调用。七个 Tuck 工具的成功字段、英文错误类型及可选字段遵守[工具返回合同](tool-results.md)，外部和原生工具保持原合同。

Tuck 自实现工具统一不加来源或分组前缀：`list_sessions`、`send_session_message`、`deliver_result`、`upload_runcove_file`、`download_runcove_files`、`generate_images`、`inspect_attachments`。名称采用动词在前、对象在后，保留会话、Runcove 等必要限定。工具及参数描述统一由 [tool_descriptions.go](../internal/tuck/tool_descriptions.go) 定义并通过 ResolveToolText 返回；资源名和英文原文见本文末尾。

本机 MCP 使用官方公开配置省略 `name`，因此引擎直接暴露工具原名；内部端点及目录 Cookie 仍分别管理职责，没有合并权限或生命周期。不添加旧工具别名，不改写已有原生历史。第三方/平台 MCP 的命名空间规则不变。依据：[官方 MCP 配置](https://docs.docker.com/ai/docker-agent/tools/mcp/)。

## 装配与工作目录

| 默认 Agent | 原生工具 | Tuck 工具 |
| --- | --- | --- |
| root / video / image / publishing | 本地文件、shell、待办、用户提问、后台子代理、任务委派 | 附件识别、会话协作与交付；生图资源可用时提供文生图；企业 DAV 可用时提供上传和下载 |
| worker | 本地文件、shell、待办 | 附件识别；企业 DAV 可用时提供上传和下载 |
| reviewer | 目录树、列目录、单文件读取、多文件读取、内容搜索 | 只读附件识别；不提供会话协作和 DAV 传输 |

附件识别要求 Agent 未停用该工具；本地附件需要已知工作目录，远程附件需要可用的 Runcove 文件授权；只读不禁止识别。未配置可用识图模型时，调用返回明确配置错误。

任务工作目录支持自动生成、指定已有目录和 `none`，详见 [工作目录配置](configuration.md#模型助手与工作目录)。自动模式按任务独立生成，指定模式使用用户选择的目录；`none` 不向原生会话设置工作目录，禁止 Tuck 本地写入；远程识别和远程文件交付无需本地目录，本地只读能力仍要求已知目录。

在有明确工作目录时，原生文件工具默认以该目录为基准，配置 `allow_list: ["."]`，用户附件、工具产物和 DAV 下载文件可在其中使用；这里不采用 Android 的受管 `/upload/<file>` 路径约定。`shell` 在后端所在环境运行，拥有该进程的权限，不继承文件工具的 `allow_list` 路径限制；`none` 也不关闭原生工具或构成 Docker 沙箱。

企业文件管理界面通过 Core Files API 管理远端工作区；两个 DAV 工具通过该空间明确关联的 DAV 上传单文件、下载一批文件。两条链路指向同一企业空间，本地工作目录仍由 Tuck 管理。MCP 连接的远端文件系统和凭据属于各自服务，不能凭同名路径或工具名推断它们就是企业工作区。

工具调用沿用原生审批模式。实际可用工具还取决于 Agent 配置、企业授权和连接状态；助手名称不会自动赋予视频生成、浏览器操作或发布能力。配置入口见 [助手配置](configuration.md) 和 [原生对接](architecture.md)。

## 附件识别：1 个

附件识别另提供只读工具 `inspect_attachments`，接收 `attachments`（1–4 个按序文件引用，每项为 path 和可选 source）与 `request`（识别要求）。source 默认为 local，读取任务目录内绝对或相对路径；source=runcove 时读取已连接工作区根目录下的相对路径。支持 PNG/JPEG/GIF/WebP，暂不支持 PDF、任意 URL 或 base64。专用识图模型返回文字，纯文本主模型也能接收；各 Agent 可覆盖全局默认，显式配置失效不回退。具体合同见[附件识别](attachment-inspection.md)。

## 文生图：1 个

`generate_images` 由现有本机 workspace MCP 提供；当前提供给工作台主助手，要求有效 turn、可写本地任务目录和助手工具许可。不依赖企业 DAV，可在 Agent 的 Tuck 工具设置中停用；原生审批仍由 Docker Agent 负责。

原图与提示词遵守 [任务文件规范](task-files.md)，写入任务目录下 `artifacts/images/`。`none` 模式不提供此工具，也不调用生图供应商。助手工作目录配置只决定新任务默认值，已有任务继续使用创建时确定的实际目录。

| 参数 | 含义 |
| --- | --- |
| `prompt` | 必填，1–32000 字符的文字提示 |
| `count?` | 默认 1，可选整数 1–4；平台资源还受下发的数量许可约束；null 等同省略 |
| `reference_images?` | 默认不传；0–4 个任务目录内 PNG/JPEG/WebP 相对文件路径，使用 `/` 分隔；仅保留主体/风格或编辑已有图片确有必要时才传，否则省略、null 或 `[]`；顺序对应提示词中的图片角色 |

使用设置中的明确默认资源，不接受 resource、size、resolution 或比例参数。尺寸由供应商决定。参考图不接受 URL、base64、绝对路径、越界路径、重复路径或指向同一文件的别名；实际解码，单张最多 16 MiB、合计最多 48 MiB，保留原字节和顺序，不创建副本。上传文件、已有工作文件和前次生成的图片都可引用。

本地资源有参考图时使用标准 OpenAI Images Edits 文件上传，无参考图时使用 Images Generations。描述明确非必要不用参考图；不自动附带会话中的图片。现行平台合同没有编辑路由，工具描述明确平台资源不接受参考图；若传入非空参考图，在读取图片、平台准入和模型请求前直接报错。不支持遮罩、厂商专有参考图格式、异步生成或流式图片。协议和设置见 [文生图资源](configuration.md#文生图资源)，供应商差异见[调研记录](image-generation.md)。

成功返回 `status=completed` 与 `files` 中的 path、prompt_path，并通过标准 MCP ImageContent 返回可供模型理解的图片表示。每张原图使用 `img_<16位Base62ID>` 文件名，配套同名 `.prompt.txt`，不覆盖旧文件；产物列表和交付沿用既有 Files/Deliveries。从对话或任务产物点击生成图片，可预览并查看、复制提示词。

单张原图最多 16 MiB，一次原图合计最多 48 MiB；响应 JSON 上限 72 MiB，模型表示合计最多 16 MiB。校验实际图片解码及像素限制，模型表示按现有 PrepareModelImage 规则准备，不能用它覆盖原图。URL 结果仅接受公开 HTTPS、无跳转的图片地址，下载不携带供应商或平台凭据，不访问本地网络；不符合要求的兼容服务应提供 base64。

取消沿当前 turn 和 HTTP 上下文停止，未发布的本次文件清理，成功发布的文件正常保留。供应商调用失败或结果不符不自动重试、不改用其他资源；取消不能证明供应商未计费。图片进入浏览器不等于聊天模型理解图片；模型能力及 Chat/Responses 图片映射由 Docker Agent 处理。独立生图用量不混入原生聊天 token 累计。

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

可选整数 `limit` 默认 10、范围 1–100。按已有创建时间和最近一轮开始/结束时间中较新值排序，不新增持久化活动状态。返回 `{status: "completed", sessions: [...]}`，每项包含 `task_id`、`title`、`assistant_name` 和 `status`，仅确有待处理交互时提供 `pending_interaction`。只列出当前未归档主会话，不包含原生子会话。后续发送使用这里的精确 `task_id`，不能使用标题或后台任务 ID 代替。 Tuck 会话 ID 为服务端生成的 16 位随机 base62；原生工具调用、后台任务和问题的 `elicitation_id` 保持原生值，不能按 Tuck ID 格式改写或推断。

工具通过 POST 调用既有 `/api/tools/list-sessions`，避免原生 GET API 工具只接受字符串参数的限制；HTTP GET 入口继续可用，使用默认数量。POST 拒绝无效 limit 和未知字段。

### `send_session_message`

| 参数 | 必填 | 含义 |
| --- | --- | --- |
| `target_task_id: string` | 是 | `list_sessions` 返回的目标主会话 ID |
| `message: string` | 是 | 去除首尾空白后非空的消息；输入长度最多 100,000 字符 |

目标必须是未归档主会话。执行中的会话收到立即补充；空闲会话启动新轮；等待审批或回答等交互的会话拒绝投递。消息带协作来源标记，成功结果如下：

```json
{"status":"accepted"}
```

`accepted` 只表示已接收，不表示目标任务完成；不回显目标、消息或内部投递方式。仅在用户明确要求联系该会话时发送，可按任务需要在消息中要求回复并明确所需反馈。工具不负责跨会话复制文件。

目标 ID 去除首尾空白后查找；目标 ID 和消息仍必须是字符串。额外字段忽略，不改变目标、投递方式或等待交互时的拒绝规则。

## 企业 DAV 文件交换：2 个

原生模型目录中的名称是 `upload_runcove_file` 和 `download_runcove_files`；本地 MCP 定义为 `upload_runcove_file` / `download_runcove_files`。连接由 Core `WorkspaceProjection.fileExchangeWebdavResourceId` 和 Snapshot v5 的 `webdav[]` 明确确定，不接收连接 ID。没有确定可用目标时两者一起隐藏；只读 DAV 的上传返回 `webdav_read_only`。`none` 模式隐藏并拒绝下载，上传仍可读取已知任务目录，不限制源文件位于 artifacts 下。reviewer 默认不装配这两个写入工具。

每轮捕获企业身份、空间、DAV 授权和当前任务目录；调用及下载发布前重新核对授权、配置与登录状态。本轮中途切换目标不会使调用转向新空间；条件变化时拒绝执行。没有目录同步、差异比较、后台同步或自动重试。协议与实现见 [企业工作区](architecture.md#api与文件)。

### `upload_runcove_file`

| 参数 | 必填 | 含义 |
| --- | --- | --- |
| `file_path: string` | 是 | 当前任务目录内现有普通文件的精确相对路径 |
| `remote_path: string` | 是 | Runcove 工作区根目录内的相对目标文件路径，包含文件名 |
| `overwrite: boolean` | 否 | 默认 `false`；明确允许替换已有远端文件 |

上传不改变本地文件，自动创建缺失的远端父目录；父路径是文件或目标是目录时失败。新建使用 `If-None-Match: *` 防止并发覆盖；显式替换使用服务器强 ETag 和 `If-Match`。XML 属性缺少有效强 ETag 时通过 HEAD 获取实际 HTTP ETag，仍不可用就失败。

```json
{"file_path":"reports/report.pdf","remote_path":"reports/report.pdf","overwrite":false}
```

成功结果中 `overwritten` 表示实际是否替换了已有文件：

```json
{"status":"completed","overwritten":false}
```

### `download_runcove_files`

| 参数 | 必填 | 含义 |
| --- | --- | --- |
| `remote_paths: string[]` | 是 | 1–20 个Runcove 工作区根目录内的相对源文件路径，不接受目录、通配符展开或重复目标文件名 |
| `local_directory: string` | 是 | 相对任务工作目录：`.` 为根目录，`reports` 为其下 reports 子目录；不得越出工作目录 |
| `overwrite: boolean` | 否 | 默认 `false`；一个顶层参数统一作用于整个 `remote_paths` 数组，明确允许替换已有本地普通文件 |

自动创建下载范围内缺失的子目录，按源文件 basename 原样保存。未传 `overwrite` 或为 `false` 时，已有目标返回 `local_file_exists`；`true` 允许整批替换已有本地普通文件，不允许替换目录或符号链接。所有路径、目录边界、已有目标和批内同名冲突都在下载 GET 前检查；Windows 同名检查不区分大小写。批内同名仍返回请求级失败，本批不写入文件。每个文件流式下载到临时文件，完成后再次核对授权和目标目录的真实路径；真实路径改变时拒绝发布。默认以不可覆盖的原子操作发布，明确覆盖时原子替换目标。失败或取消保留该文件的原有内容，清理本次临时文件；已经完成的替换保留。

```json
{"remote_paths":["reports/report.pdf","tables/data.csv"],"local_directory":"review","overwrite":false}
```

`files` 只列出已成功下载的本地路径。全部成功为 `completed`，部分成功为 `partial`，全部失败为 `failed`；后两者标记 MCP 工具错误并提供 code/message，逐项错误放入 errors，field 指向 remote_paths 的原下标。请求级预检失败不输出 files/errors；无成功文件时省略 files。取消或授权失效停止后续下载，未开始项为 not_attempted；已成功文件保留，修复原因后只处理失败项。

```json
{"status":"partial","code":"partial_download","message":"Some files could not be downloaded. Completed files were kept.","files":[{"path":"review/report.pdf"}],"errors":[{"field":"remote_paths[1]","code":"webdav_file_not_found","message":"The remote file does not exist."}]}
```

`files[].path` 是后续本地工具或上传使用的路径。下载图片也不自动将其像素加入上下文；需要识别时显式调用识别工具。原始落盘字节不改变。传输不套用聊天附件的 16 MiB 限制，继续受 Core 授权、存储和服务限制约束；没有新增目录同步、总容量配额或自动重试。

### 路径、容错与失败

- 远端路径必须是 DAV 根目录内相对文件路径，拒绝绝对路径、空段、`.`、`..`、反斜杠和控制字符。
- 本地路径必须留在当前任务真实工作目录内，符号链接不能越界；Windows 本地路径兼容反斜杠，拒绝无效 Windows 文件名。
- 可选参数为 `null` 时使用默认值；批量下载的 `remote_paths` 和 `local_directory` 必填。上传与下载的 `overwrite` 均默认为 `false`，兼容字符串 `"false"` / `"true"`，忽略其大小写和首尾空白；其他类型或含糊值仍返回 `invalid_arguments`。
- 调用只使用当前工具定义的参数，其他字段忽略，不新增其暗示的功能；例如 `create_parents`、`timeout_secs`、`connection_id` 不改变既有父目录创建、超时或固定企业目标。上传只有明确的 `overwrite=true` 才允许远端条件覆盖；下载只有明确的 `overwrite=true` 才允许替换整批的已有本地普通文件。
- 缺少必需参数、已定义参数的错误类型及非法路径仍返回 `invalid_arguments`；跨任务路径、保护目录与访问授权规则不因参数兼容而放宽。

普通失败返回 JSON，并标记 MCP 工具错误：

```json
{"status":"failed","code":"webdav_file_exists","message":"The remote file already exists. Set overwrite=true only if replacement is intended."}
```

| `code` | 含义与处理 |
| --- | --- |
| `invalid_arguments` | 已定义参数的类型或路径无效；按 schema 修正，额外字段按上述容错规则忽略 |
| `local_file_not_found` | 本地源不存在、不是普通文件或无法读取 |
| `local_file_exists` | 本地下载目标已存在；换用其他目录，或从重试清单移除已成功文件 |
| `local_name_conflict` | 批内多个源文件映射到同一本地文件名；拆分到不同目录下载 |
| `operation_canceled` / `not_attempted` | 当前文件已取消或停止后未开始，前面成功的文件保留 |
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
| `webdav_upload_failed` / `webdav_download_failed` | 服务未确认该操作成功；依据 message 核实目标 |
| `outcome_unknown` | 远端写入可能已经完成；返回 `status: "unknown"`，先通过企业文件管理器核实，不自动重试 |

取消沿原生取消流程处理；已经发起的远端写入无法确认时返回未知结果，不承诺远端回滚。

## 动态 MCP 与维护

企业 MCP 从当前 Snapshot 和服务器工具目录发现；`ALL` / `ALLOWLIST` 按平台下发规则过滤，实际调用再次检查企业授权。原始工具名可能带服务命名空间和映射后缀，应使用模型当前收到的精确名称与 schema。默认只读 Agent 仅获得符合只读规则的工具。

访问令牌刷新与资源更新见 [资源更新与续聊](architecture.md#平台与安全)；MCP 会话恢复及调用重试由官方引擎管理，见 [MCP 生命周期](architecture.md#docker-agent-合同)。会话恢复不重新下发 Core 配置，也不重新签发 Agent Space 密钥。

本地配置 MCP 来自 `localMcp`，按连接的 `enabled`、凭据和 `agents` 绑定装配。可选 `localMcp[].tools` 使用服务器原始工具名作为精确白名单；省略时开放全部工具，空数组表示全部停用且不生成该连接的原生 toolset，重复名称拒绝。绑定和 `readonly` 继续约束最终可见工具，示例见 [本地 MCP 工具选择](configuration.md#本地-mcp-工具选择)。平台连接的本机选择保存在 `platformMcpTools`，只缩小平台授权范围，空数组同样停用全部工具。工具含义、参数、返回值和文件访问范围由该服务负责；认证令牌使用同一 JSON 的 `authToken` 字段并由文件权限保护，与企业 DAV、本地任务目录没有关联。动态服务清单不写死在本文。

## 对话交付：`deliver_result`

受管主助手默认启用，`workbench.assistants[].deliveryTool=false` 可关闭；子代理不获得此工具。不要求企业登录或 WebDAV 可用，但运行时使用官方请求头模板传递每轮交付令牌。设置 → 编排 →“启用交付要求”默认关闭；在主 Agent 一轮明确正常结束后，检查当时的开关、交付工具许可和该轮交付记录；运行中开启或关闭也按结束时的配置判断。普通输入正常结束但未交付时最多补充一次自动请求。自动补交付不能再触发自动补交付；仍未交付就按原生完成事实结束，界面保留无本轮交付的事实。后续用户输入独立判断；依据持久化 source.kind 而非提示词文字区分，沿用既有队列和取消行为。

参数为必填 `summary` 和可选 `files`。摘要支持 Markdown，通常 1–3 句，最多 1,000 字符；只列出需要交付的关键文件，每项必填 `path`，选填 `source`（local/runcove，默认 local）和可省略的 `description`；path 相对所选来源根目录。Runcove 文件必须来自本轮已授权的工作区，直接流入现有交付暂存并形成快照，不先写任务目录；无本地目录仍可交付远程文件。文件读取失败不提交新版本。无需文件时省略 `files`，`files: []` 或 `null` 同样表示无文件。不提供由模型填写的 request_id、版本号或会话身份。

一次最多 10 个关键文件；未变文件仅在使用本次成果必需时附带，不重复提交历史整套文件。单文件不超过 512 MiB，合计不超过 2 GiB。成功后保存独立副本并增加版本；普通对话和任务频道使用相同记录回看。源文件后续修改、删除或任务归档不会改变历史交付。路径、链接、复制期间的变化及文件大小均由服务验证，失败整次不建版。持久化与保留规则见 [交付存储合同](database-design.md#交付与清理)，实际模型描述与参数以 [工具定义](../internal/tuck/local_tools.go) 为准。

维护入口：唯一配置为 tuck.config.json；原生装配在 internal/tuck/engine_config.go，会话协作在 app.go / tasks.go，本机 MCP 在 local_tools.go，生图协议在 image_generation.go，DAV 在 workspace_tools.go，企业代理在 mcp.go，交付在 deliveries.go。更改名称、参数或执行规则时同步本文、配置合同和对应测试。

对应测试和真实服务验收要求见 [验证指南](verification.md#真实服务与已知限制)；本工具目录不声明当前部署已完成所有真实副作用验收。

### 工具设置中的启停

两张入口卡片分别进入原生工具同页列表和 Tuck 工具开关列表。`agents.<agent>.disabledTuckTools` 支持 `list_sessions`、`send_session_message`、`upload_runcove_file`、`download_runcove_files`、`generate_images`、`inspect_attachments`，缺省全部启用，保存并应用后按 Agent 过滤原生运行时的工具目录。附件识别允许只读 Agent 使用。交付沿用 `workbench.assistants[].deliveryTool`。停用不会改变其他 Agent；原有主/子代理边界和企业文件权限保持有效。

## 描述资源与编排边界

已实现：Tuck 的七个工具和全部参数描述集中在 `tool_descriptions.go`，以 `tuck.<工具名>.description` 和 `tuck.<工具名>.param.<参数路径>` 标识；动态模型/提供商文字也有独立资源名。调用处不散落描述正文，缺失资源或必要占位值属于明确错误。参数 schema、权限与执行仍归既有 owner。

规划中：未来编排可引用 Tuck、外部 MCP、Docker Agent 内置工具的来源身份和选择配置；本期不实现编排运行器或描述覆盖系统。外部 MCP 原始合同和哈希不得被本地文案改写破坏；引擎内置工具仅按实际版本的公开能力配置，不读取或改写引擎内部实现。模型和提供商显示值不能代替稳定工具身份。

<!-- tool-text-resources:start -->

| 资源名 | 英文描述原文 |
| --- | --- |
| `tuck.list_sessions.description` | List Tuck conversations and their task IDs to find a conversation to contact. |
| `tuck.list_sessions.param.limit` | Number of conversations to return, from 1 to 100; default 10. |
| `tuck.send_session_message.description` | Send a message to a Tuck conversation as requested by the user. When needed, ask for a reply in the message and explain what feedback is needed. Acceptance means the message was received, not that the task is complete. |
| `tuck.send_session_message.param.target_task_id` | Target conversation task ID, available from list_sessions. |
| `tuck.send_session_message.param.message` | Message to send; when a reply is needed, explain what feedback you want. |
| `tuck.deliver_result.description` | Submit the results as a new delivery version with a brief summary and, when needed, key files from the task working directory or the connected Runcove workspace. |
| `tuck.deliver_result.param.summary` | Briefly describe the completed work. |
| `tuck.deliver_result.param.files` | Up to 10 key files to deliver. Omit when no files are needed. |
| `tuck.deliver_result.param.files[].source` | File source: local for the task working directory, or runcove for the connected Runcove workspace. Default local. |
| `tuck.deliver_result.param.files[].path` | File path relative to the selected source root, including the filename. |
| `tuck.deliver_result.param.files[].description` | Brief description of the file's content or purpose. May be omitted. |
| `tuck.upload_runcove_file.description` | Upload one file from the task working directory to the connected Runcove workspace. Existing files are not overwritten by default. |
| `tuck.upload_runcove_file.param.file_path` | Source file path relative to the task working directory, such as reports/result.pdf. |
| `tuck.upload_runcove_file.param.remote_path` | Destination file path relative to the Runcove workspace root, including the filename. Do not start with / or /workspace/. |
| `tuck.upload_runcove_file.param.overwrite` | Whether to overwrite an existing remote file at the same path. Default false. |
| `tuck.download_runcove_files.description` | Download 1–20 files from the connected Runcove workspace to the task working directory, preserving filenames. Existing files are not overwritten by default. |
| `tuck.download_runcove_files.param.remote_paths` | 1–20 source file paths relative to the Runcove workspace root. Do not start with / or /workspace/. |
| `tuck.download_runcove_files.param.local_directory` | Destination directory relative to the task working directory. Use . for its root or specify a subdirectory; stay inside the working directory. |
| `tuck.download_runcove_files.param.overwrite` | Whether to overwrite existing files with the same names in the destination directory. Default false. |
| `tuck.generate_images.description` | Generate images from a prompt, one image by default. Provide references only when needed to preserve a subject or style, or edit existing images. Generated images and their prompts are saved in artifacts/images; returns images and file paths. |
| `tuck.generate_images.model` | Model: {model}. |
| `tuck.generate_images.provider` | Provider: {provider}. |
| `tuck.generate_images.platform_references` | This platform image resource does not accept reference images; omit reference_images. |
| `tuck.generate_images.param.prompt` | Describe the desired image. With references, explain each image's role in order and what to preserve or change. |
| `tuck.generate_images.param.count` | Number of images to generate, from 1 to 4; default 1, subject to resource limits. |
| `tuck.generate_images.param.reference_images` | Optional 0–4 PNG/JPEG/WebP reference images. Use file paths relative to the task working directory; omit unless needed. |
| `tuck.inspect_attachments.description` | Use when you need to understand attachment content, extract information, or compare differences. Reads files from the task working directory or the connected Runcove workspace and returns text. Supports images; PDF and other attachment types are not yet supported. Provide relevant background and specific questions. |
| `tuck.inspect_attachments.param.attachments` | 1–4 attachments to inspect, in order. Supports PNG/JPEG/GIF/WebP images. |
| `tuck.inspect_attachments.param.attachments[].source` | Attachment source: local for the task working directory, or runcove for the connected Runcove workspace. Default local. |
| `tuck.inspect_attachments.param.attachments[].path` | For local files, a relative or absolute path inside the task working directory. For Runcove files, a path relative to the workspace root, without /workspace/. |
| `tuck.inspect_attachments.param.request` | What to identify, extract, compare, or verify. Refer to attachments by array order and include relevant background and the desired answer format when needed. |

<!-- tool-text-resources:end -->
