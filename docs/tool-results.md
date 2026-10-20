# Tuck 工具返回合同

本文只约束七个 Tuck 自实现工具，不改写外部 MCP 或 Docker Agent 内置工具。

模型可见字段的选择、结构及 Tuck 提供的错误和警告遵守 [统一内容入口约定](architecture.md#模型可见内容的唯一入口)；业务 owner 提供事实，网页 presentation 和内部诊断不因此进入模型结果。

## 基本结构

返回 JSON 对象，必填 `status: string`。completed 表示本工具操作完成；accepted 表示消息已接收，不代表接收方已完成消息要求的工作；partial 表示部分下载完成；failed 表示操作失败；unknown 表示远端写入或跨会话消息提交结果无法确认，须先核实目标再决定是否重试。不能把未知结果声称为失败并暗示远端已回滚。

JSON 通过 MCP 文本内容返回；failed、partial、unknown 同时标记 `isError`。生图额外返回标准 ImageContent，不能将图片数据 URL 当作普通文本替代图片内容块。

成功只加后续工作需要的业务字段，不另包 data/result，不增加 success/ok 布尔值。输入通常不回显；新产物路径、查询结果和错误定位例外。可选字段无实际信息时省略，不输出 null、空字符串或占位对象。会话查询无结果返回 conversations: []；无文件交付返回 file_count: 0。

Tuck 错误和警告文本统一英文。识别模型正文 text 按任务要求使用相应语言，不翻译、不机械截断。共享网页 API 的中文提示不因此改为英文。

## 成功字段

| 工具 | status | 必填业务字段 | 可选业务字段 |
| --- | --- | --- | --- |
| list_conversations | completed | conversations: array，每项 conversation_id、title、assistant_name、status 均为 string | 每项 pending_interaction: string，仅确有待处理交互时输出 |
| send_conversation_message | accepted | 无 | 无；不重复目标、消息或内部投递方式 |
| deliver_result | completed | version: integer、file_count: integer、view_url: string | 无 |
| upload_runcove_file | completed | overwritten: boolean，实际是否替换已有文件 | 无 |
| download_runcove_files | completed | files: array，每项仅 path: string，表示实际本地落点 | 无 |
| generate_images | completed | files: array，每项 path: string、prompt_path: string | warning: string，仅结果使用存在明确限制时输出 |
| inspect_attachments | completed | text: string | 无 |

生图继续通过 MCP ImageContent 返回实际图片；JSON 不重复图片字节、提示词正文、资源 ID、用量、MIME、大小或摘要。模型未声明图片输入时，warning 说明该限制，仅在当前许可且已配置识图模型时建议 inspect_attachments。识别不返回 modelId、输入附件列表或 workspace。交付不返回内部 ID、总字节数、清理状态，界面仍使用 version、file_count、view_url。必要内部事实保留在既有 owner。

跨会话消息在调用原生 `/steer` 前失败时保留对应参数、配置或交互错误；实际提交后响应丢失或本地记录失败时返回 `unknown / conversation_message_unconfirmed`，提示核查目标会话后再决定是否发送。已取得原生接收事件时仍可确认 accepted。Tuck 不自动重发。

会话工具将共享任务 owner 的目标状态错误投影为 `conversation_not_found`、`conversation_readonly`、`conversation_stopping`、`conversation_capacity`；其他工具及业务 HTTP API 保持各自原有错误合同。

## 失败字段

failed、partial、unknown 必填 `code: string` 和 `message: string`。code 是稳定的错误类型，不再重复 reason/type/category；message 简短说明原因，必要时给出一项纠正方式，不再重复 detail。不能把超时误导为模型图片能力问题。

| 可选字段 | 类型 | 何时提供 |
| --- | --- | --- |
| field | string | 能准确定位错误参数或数组项时，如 files[1].path |
| upstream_status | integer | 模型请求收到 HTTP 错误响应时保留实际状态码；DAV 和平台准入错误不提供此字段 |
| upstream_code | string | 模型服务提供非空机器错误码，经过长度、字符和秘密校验后提供 |
| files | array of {path: string} | 批量下载有已完成文件时提供 |
| errors | array of {field: string, code: string, message: string} | 批量下载逐项失败，按输入顺序排列；field 定位 remote_paths 原下标 |

不输出 retryable、堆栈、原始响应、凭据、内部目录、配置对象、重复 issues。普通失败不提供 errors 数组。批量请求预检失败只有普通失败字段；逐项全失败使用 download_failed，部分失败使用 partial_download；取消或授权失效后未执行项为 not_attempted。没有成功文件就省略 files。

```json
{"status":"completed","text":"The image shows a blue background and a gold square."}
```

```json
{"status":"failed","code":"upstream_timeout","message":"The model service timed out. Try again later.","upstream_status":504}
```

```json
{"status":"partial","code":"partial_download","message":"Some files could not be downloaded. Completed files were kept.","files":[{"path":"review/report.pdf"}],"errors":[{"field":"remote_paths[1]","code":"webdav_file_not_found","message":"The remote file does not exist."}]}
```

```json
{"status":"unknown","code":"outcome_unknown","message":"The remote write may have completed. Check the remote file before sending it again."}
```

## 错误类型

保留已有明确业务错误码，不为形式统一合并纠正方式不同的错误。统一出口提供英文消息和未知错误兜底。

| 范围 | 错误码及含义 |
| --- | --- |
| 参数与会话 | invalid_arguments / invalid_json：参数或 JSON 不符；invalid_message / invalid_target：消息或目标无效；interaction_pending：目标等待用户；configuration_changing：配置更新中 |
| 执行与权限 | tool_not_permitted：本轮权限失效；operation_canceled：取消；not_attempted：尚未执行 |
| 目录 | workspace_required / workspace_unavailable / invalid_workspace：目录缺失或不可用；workspace_changed / artifact_directory_changed / task_changed：目录或任务改变；path_outside_workspace：越界 |
| 本地文件 | file_not_found：源不存在；local_file_not_found / file_not_readable：源不可读；file_changed：读取期间源文件变化；local_file_exists：目标已存在；local_name_conflict：同名目标；file_too_large：大小超限 |
| 附件 | attachment_type_unsupported：类型不支持；attachment_invalid_image：无法解码；attachment_limit：大小限制；attachment_changed / attachment_unavailable：变化或不可用 |
| 生图 | image_resource_unavailable：资源未配置或失效；image_references_unsupported：平台参考图不支持；invalid_image_reference / image_reference_limit / image_reference_changed：参考图无效、超限或变化；invalid_image_response / invalid_image / image_limit：生成结果无效或超限 |
| 识别 | inspection_model_required / inspection_model_unavailable：未配置或失效；inspection_request_invalid：地址无效；inspection_response_invalid / inspection_incomplete：响应无效或未完整完成 |
| 模型 HTTP | upstream_authentication_failed：401；upstream_forbidden：403；upstream_rate_limited：429；upstream_timeout：408/504；upstream_rejected：其他 4xx；upstream_unavailable：其他服务错误 |
| 模型网络 | image_unreachable / inspection_unreachable：连接失败；网络等待超时为 upstream_timeout；不增加总期限、重试或模型回退 |
| 平台 | resource_changed / resource_unavailable、not_enrolled、managed_snapshot_required 等：身份、准入或配置失败，保留已识别平台类型 |
| 远端文件 | webdav_file_not_found / webdav_not_file / webdav_parent_not_directory：源或目标不符；webdav_file_exists：覆盖冲突；webdav_read_only / webdav_path_not_permitted：只读或受限路径；webdav_version_unavailable / file_version_unavailable / webdav_version_changed / remote_file_unavailable：版本无法确认或变化；webdav_unavailable / webdav_upload_failed / webdav_download_failed：传输失败 |
| 交付存储 | delivery_file_limit：超过 10 个文件，应筛选或归档；delivery_too_large：交付总大小超限；snapshot_failed：快照保存失败；insufficient_storage / persistence_failed：存储或提交失败；delivery_canceled：提交前取消 |
| 批量或不确定结果 | partial_download / download_failed / outcome_unknown：部分完成、全部失败、远端结果未知；conversation_message_unconfirmed：跨会话消息提交结果未确认 |
| 未分类内部异常 | internal_error：无法可靠归类的 IO、数据库或程序错误；固定英文，不透出原始错误 |

上游 message 不保证英文和安全，不直接作为工具 message。根据实际 HTTP 状态生成英文摘要；upstream_code 仅提供机器诊断。取消优先于网络错误；原始文件错误可明确分类时使用对应类型，否则 internal_error。code 不得为空。

## 前端呈现

附件识别成功时按安全 Markdown 展示正文；失败、部分完成和结果未确认时优先展示英文错误消息与类型，并保留完整返回详情。部分完成和结果未确认分别标示，不能显示为成功。此展示只解释上述七工具的返回合同，不改写模型收到的结果，也不将外部 MCP 输出套入此合同。
