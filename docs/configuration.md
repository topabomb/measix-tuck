# 配置合同

唯一用户配置是 `tuck.config.json`，由 `tuck init` 创建。无凭据模板在 [default-config.json](../internal/tuck/contract/default-config.json)，也可用 `tuck config-template` 输出；init 生成本机独立的 `security.credentialKey`，已有文件不覆盖。所有相对路径以配置目录解析，不读取 .env 或历史启动环境覆盖。字段及类型校验以 [config.go](../internal/tuck/config.go) 为准，安装步骤见 [README](../README.md#快速开始)。

## 结构与默认值

| 分组 | 内容 |
| --- | --- |
| schemaVersion | 当前 2；正式 Go v1 须显式离线迁移，拒绝未知格式与重复 JSON 键 |
| server | listenHost、port、frontendDir；默认 127.0.0.1:8090、frontend/dist |
| storage | dataDir、workspaceRoot；默认 data、data/workspaces |
| engine | mode=managed/external、version、binary、url、desktopProxy；默认官方 v1.150.0、回环 8091 |
| security | auth、settingsProtection、必填的 32 字节 credentialKey；不自动替换已有密钥 |
| providers | 自定义供应商 URL、协议、apiKey、模型目录和能力 |
| imageGenerators / defaultImageGenerator | 可选生图资源及默认来源/ID；复用 providers 连接，独立声明生图协议 |
| defaultModelId / modelDefaults | 默认模型及上下文/输出预算；空默认要求用户选择可用模型 |
| defaultAttachmentInspectionModelId | 可选全局识图模型；Agent 可用 attachmentInspectionModelId 独立覆盖 |
| agents | 原生提示、模型、工具集、allow_list、readonly、子代理、执行上限及 Tuck/企业工具选择 |
| permissions | 官方工具 allow/ask/deny 规则，生成引擎输入时保留 |
| workbench | 主助手目录、默认助手、每助手工作目录/任务并发、requireDelivery、deliveryRetentionCount |
| localMcp | 启用状态、HTTP(S) URL、直接凭据、助手范围、精确工具选择；不解析历史环境变量引用 |
| platformModelSettings / platformMcpTools | 当前平台资源预算、temperature/topP 和工具选择；不能替换平台身份/许可 |

## 模型、助手与工作目录

模型 ID、助手身份与目录选择明确保存，不根据名称猜来源。原生 max_iterations=0 保留官方无限语义，max_consecutive_tool_calls 等字段必须合法；新增原生字段先核对兼容版本。原生工具 allow_list:[] 仍采用官方“不限定目录”语义；shell 不继承文件工具路径限制。readonly 与工具名称选择同时生效。

工作目录有 auto / specified / none。auto 用工作根目录生成；specified 必须是存在、可访问的绝对目录，与交付存储隔离；none 不设置原生任务目录，也不承诺本地附件/交付文件能力。助手配置是默认值，创建时明确选择优先；已有对话和 fork 保留实际目录事实。

Tuck 自动管理的上传附件和生成图片统一进入工作目录下 `artifacts/` 的用途子目录；DAV 下载可指定任务根目录内的任意位置。`none` 明确禁止 Tuck 向引擎默认目录写入附件、编辑结果、生图或 DAV 下载。目录、命名、提示词配套保存及只读/交付例外见 [任务文件规范](task-files.md)。

交付保留数为整数 0–100，默认 10，助手可覆盖。0 表示不保留历史版本，当前最新版本始终保留。只有成功提交新版本才裁剪旧记录。

## 文生图资源

设置 → 提供商与模型 → 文生图工具中添加资源；先创建提供商连接，聊天模型目录可以为空。生图资源引用 `providerId`，共用该连接的 `baseUrl/apiKey/enabled`，不受 provider 的聊天 `protocol` 控制。供应商需要不同地址或凭据时建立另一连接，不按品牌合并。

以下为配置片段，`connection-a` 必须是已有本地提供商；`upstreamModelKey` 使用服务实际提供的模型名称：

```json
{
  "imageGenerators": [{
    "imageId": "poster",
    "providerId": "connection-a",
    "displayName": "海报生图",
    "enabled": true,
    "protocol": "OPENAI_IMAGES_GENERATIONS",
    "upstreamModelKey": "your-image-model",
    "responseFormat": ""
  }],
  "defaultImageGenerator": {"source": "local", "imageId": "poster"}
}
```

当前实现标准 OpenAI Images 非流式调用：无参考图时 `POST <baseUrl>/images/generations`，JSON 包含 model、prompt、n；有参考图时 `POST <baseUrl>/images/edits`，使用 multipart/form-data，按参数顺序发送 `image[]` 文件。工具默认生成 1 张，可选 1–4 张；参考图默认不传，可选 0–4 张。本地省略 size，平台固定发送 size="auto"；均不发送 resolution 或 aspect_ratio，尺寸由服务决定，不承诺精确比例。

本地配置不提供尺寸清单和数量上限。`responseFormat` 缺省或空字符串时不发送 response_format；兼容服务可明确选 `b64_json` 或 `url`。GPT 图片模型使用服务默认返回格式。参考图要求服务实现标准 OpenAI Images Edits 文件上传；不能因为支持文字生成或声称 OpenAI 兼容就推定支持参考图。具体差异见[参考图调研与实施边界](image-generation.md)。不按主机或模型名分支，不自动重试、丢弃参考图或切换接口。

平台生图资源来自快照 `imageGenerators`，保持只读，使用明确下发的 runtimePath、admit 和短期 access；数量仍受平台 maxImagesPerRequest 约束，工具上限为两者较小值。平台 allowedSizes 保留原合同校验，但不作为请求参数或本地设置。现行合同未声明参考图编辑路由，工具描述明确提示平台资源不接受参考图，调用时在读取文件和申请准入前拒绝；不把 generations 路径替换成 edits，不改写外部合同。其他线协议显示不可用。

默认选择包含 `source=local/platform` 和 `imageId`；空对象或省略时采用平台 defaultImageGenerationId（如有）。明确选择失效时保留不可用事实，不换资源；工具不接受 resource 参数，无可用的有效选择时不提供生图工具。

生图设置归入 providers 配置保护，使用相同保存/中断/延后应用流程。在编辑器内修改生图资源标识时，同步更新指向它的本地默认选择和 Agent 覆盖；Agent 配置受保护时需先解锁才能修改资源标识；修改尚未保存的提供商标识时，同步更新生图资源对该连接的引用。删除资源不会自动改选其他资源；删除仍被生图资源引用的连接时，须先调整或删除这些引用才能保存。新增字段可省略，缺省未配置；不改变旧聊天配置、数据库或文件布局的解释，配置版本仍为 2。使用这些新字段后，旧程序不能识别新能力；回退须恢复对应配置备份，不手改版本绕过校验。

聊天模型仍需独立配置；需要模型理解工具返回的图片时，选择声明 IMAGE 输入并实际支持视觉的聊天模型。生图工具许可在设置 → Agent → Tuck 工具中控制，详见 [工具参考](tools-reference.md#文生图1-个)。TTS、ASR 和其他线协议仅属于[目标架构规划](architecture.md#模型操作的目标架构与实施范围)。

Agent 可通过 `agents.<name>.imageGenerator` 覆盖生图资源，值与全局默认一样为 `{source: local|platform, imageId: ...}`。选择顺序为 Agent 覆盖 → 全局 defaultImageGenerator → 平台默认；字段省略或空对象表示继承，明确选择失效则报错，不回退。设置位于 Agent → Tuck 工具 → 生图资源（覆盖），随 agents 配置保护和保存；提供商设置锁定时仍提供不含连接详情的资源选项。覆盖只改变资源选择，不放宽工具许可、主代理、可写目录及平台准入限制。此字段由 Tuck 消费，不写入 Docker Agent YAML。

## 附件识别模型

附件识别的全局默认位于“提供商与模型”；各 Agent 的 Tuck 工具页可开启“识别附件”并覆盖识图模型。模型需声明 TEXT、IMAGE 输入，无需 TOOL；未指定时沿用平台明确下发的默认识图模型。明确选择失效时不回退。工具只读，支持任务目录任意位置的图片；参数、子代理及上下文边界见[附件识别](attachment-inspection.md)。新增字段可省略，不改变已有数据解释，不需要数据库迁移。

## 本地 MCP 工具选择

`localMcp` 中每个连接使用唯一 `name`、HTTP(S) `url`、`enabled` 和 `agents`。认证秘密直接写同一配置的 `authToken`，不引用环境变量。`tools` 使用服务器原始工具名；省略表示允许该连接的全部已授权工具，空数组表示全部停用且不生成原生 toolset。重复名称拒绝；助手绑定及只读规则仍共同约束可见性。

下面是配置中的一个无凭据连接示例，地址和工具名须替换为实际服务：

```json
{
  "localMcp": [{
    "name": "project-tools",
    "url": "http://127.0.0.1:9000/mcp",
    "enabled": true,
    "agents": ["root"],
    "tools": ["read_file"]
  }]
}
```

企业连接的本机选择使用 `platformMcpTools`，只缩小当前平台授权范围，不替代平台身份、许可或工具合同。注册配置、最近验证目录、当前连接和实际调用结果的含义见 [引擎边界](architecture.md#docker-agent-合同)。

## 保存与应用

设置页和维护命令更新同一配置。revision 是文件字节 SHA256；保存校验 revision 后原子替换，格式化后不能超过 4 MiB。秘密通过 hasApiKey 等标志表示；未提交新密钥时保留原值，不能修改资源 ID 复用旧凭据。未知字段和被保护分类注入明确拒绝。版本升级入口、密钥保留和中断恢复见 [持久化版本与迁移](persistence-migrations.md)。

普通运行配置可立即应用或保存待应用；活动任务要求明确 interrupt 或 defer。成功只标记实际进入运行时的那份配置，期间更新的外部编辑仍显示 pending。应用失败比较 revision 恢复，不覆盖更新的外部编辑。平台/模型/引擎共用变更门，新轮次和工作区请求在变更完整结束前拒绝。

server、storage、登录 auth、credentialKey，以及引擎 mode/version/binary/url 由启动时的资源 owner 持有，外部修改后必须重启；“应用运行配置”返回 restart_required，不停止任务，也不把新目录/密钥标为已生效。官方版本升级仍走独立引擎升级流程；desktopProxy 可由现有引擎配置重建。修改配置路径不等于搬移数据，替换密钥也不等于完成凭据重加密；必须先准备匹配资产和备份。

## 配置保护与密码

保护分类为 platform / providers / agents / mcp / orchestration / engine；未指定分类表示全部，外观不参与保护。保护密码至少 6 个字符，登录密码至少 12 个字符。保护变更立即生效，不中断工作、不顺带应用待生效配置。解锁绑定当前登录、固定一小时，锁定/重启/改密/分类变化撤销授权。

`tuck password login` 维护登录密码，`tuck password settings` 维护保护密码；空密码清除对应凭据，保护忘记密码用 `tuck password settings --reset`。维护命令在停服后执行，取得配置排他锁并验证当前版本及修改后的完整配置，再启动同步运行身份；外部监听时不能清除登录保护。密码从终端隐藏输入或标准输入读取，不进入参数、日志和提交。无凭据模板不能直接作为完整运行配置，实际 credentialKey 由 init 生成或从匹配备份保留。
