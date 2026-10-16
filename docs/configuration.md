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
| defaultModelId / modelDefaults | 默认模型及上下文/输出预算；空默认要求用户选择可用模型 |
| agents | 原生提示、模型、工具集、allow_list、readonly、子代理、执行上限及 Tuck/企业工具选择 |
| permissions | 官方工具 allow/ask/deny 规则，生成引擎输入时保留 |
| workbench | 主助手目录、默认助手、每助手工作目录/任务并发、requireDelivery、deliveryRetentionCount |
| localMcp | 启用状态、HTTP(S) URL、直接凭据、助手范围、精确工具选择；不解析历史环境变量引用 |
| platformModelSettings / platformMcpTools | 当前平台资源预算、temperature/topP 和工具选择；不能替换平台身份/许可 |

## 模型、助手与工作目录

模型 ID、助手身份与目录选择明确保存，不根据名称猜来源。原生 max_iterations=0 保留官方无限语义，max_consecutive_tool_calls 等字段必须合法；新增原生字段先核对兼容版本。原生工具 allow_list:[] 仍采用官方“不限定目录”语义；shell 不继承文件工具路径限制。readonly 与工具名称选择同时生效。

工作目录有 auto / specified / none。auto 用工作根目录生成；specified 必须是存在、可访问的绝对目录，与交付存储隔离；none 不设置原生任务目录，也不承诺本地附件/交付文件能力。助手配置是默认值，创建时明确选择优先；已有对话和 fork 保留实际目录事实。

交付保留数为整数 0–100，默认 10，助手可覆盖。0 表示不保留历史版本，当前最新版本始终保留。只有成功提交新版本才裁剪旧记录。

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
