# 附件识别与模型上下文

## 当前契约

`inspect_attachments` 是 Tuck 本机 MCP 的 `inspect_attachments`，仅有两个必填参数：`attachments` 为按序的 1–4 个文件引用（每项含 path 和可选 source，source 为 local/runcove，默认 local），`request` 说明要提取、比较或核实什么。模型、协议、像素尺寸、识别模式不作为 Agent 参数。

本期支持本地及 Runcove 附件的 PNG、JPEG、GIF、WebP，PDF 等其他类型暂不支持。对于本地文件，按实际字节解码，不以文件扩展名推断能力。整个实际任务工作目录均可读取，包括项目原有文件、任意子目录及 `artifacts/uploads`、`images`、`downloads`；不采用 Android 的 `/upload` 专用目录约束。接受相对路径和工作目录内的绝对路径，拒绝外部路径、URL、base64、目录、重复文件身份及逃出根目录的链接。`os.Root` 限定读取，打开时核对文件身份，读取后核对根目录身份。

单件上限 16 MiB，原始文件合计 48 MiB。复用现有图片准备规则：实际解码、40M 像素上限、方向修正、静态图片最长边 2048px；动画保留原字节；模型表示合计不超过 16 MiB。只有内存快照，不新增缓存或落盘文件。`auto`、`specified` 都可读取；`none` 仅在托管引擎默认目录已知时允许读取，不增加任何写权限。外接引擎目录未知时拒绝本地读取；source=runcove 的附件不依赖本地目录。远程读取复用本轮捕获的企业身份、空间及 DAV 文件授权，使用根目录相对路径，不接受 `/workspace/` 前缀，不允许自动切换空间或猜测其他连接。读取前后重新核对授权，并校验文件版本及实际字节数。

展开工具输入可按参数顺序查看图片缩略图，点击使用现有图片预览。本地输入即时预览；远程输入在该调用状态为成功或错误时显示。浏览器投影只附加受保护的文件地址，不复制图片到历史或模型上下文；预览读取复用相同的路径、解码和大小校验，支持目录内绝对路径及没有扩展名的图片。远程预览从原调用的 Tuck tool-call presentation 取得本轮捕获的企业身份、空间和绑定版本，并与现有授权比对，切换账号或工作区后不能读取同名新文件；身份不来自工具结果，没有单独缓存或预览状态表。显示的是当前文件，不是历史快照；文件已移除或不可读取时明确提示。

## 配置与选择

识图直接引用现有本地/平台模型，不新建一套提供商或资源列表。支持现有 OpenAI Chat Completions 和 Responses 线协议；模型必须可用且声明 TEXT、IMAGE 输入，专用识图不要求 TOOL 能力。聊天模型仍要求 TOOL。

按以下顺序选择，空值表示继承：

1. `agents.<name>.attachmentInspectionModelId`。
2. `defaultAttachmentInspectionModelId`。
3. 平台已下发的 `policy.defaultAttachmentInspectionModelId`。

不继承主 Agent 的覆盖值，不使用聊天模型或目录首项兜底。明确指定但失效时报告 `inspection_model_unavailable`；未配置时报告 `inspection_model_required`。设置页提供全局默认和各 Agent 的覆盖下拉框；配置保护继续按 providers / agents 分类。禁用项为 `agents.<name>.disabledTuckTools` 中的 `inspect_attachments`；只读 Agent 可使用。

原生子代理也可独立选择模型。各 Agent 通过同一个 Tuck MCP 入口携带明确调用者身份；工具执行仍要求活跃主任务 turn header，并核对该 Agent 属于任务根 Agent 或其配置的子代理树。目录、说明和资源选择依据该 Agent 的许可，不继承主助手全部工具；SDK 连接及目录身份按调用者隔离，不为附件识别另建 MCP 服务，也不从原生内部状态猜当前 Agent 或建立新的子代理调度。

## 执行与上下文边界

`Files.ReadInspectionAttachments` 读取本地图片；`LocalTools.readToolAttachments` 根据显式来源复用本地读取或既有远程传输，并统一校验合计大小；`LocalTools` 校验参数、Agent 许可、任务/轮次、目录及平台身份；`model_content` 统一组织识图指令、按序图片标签和本次 request 的模型可见结构；`Models.InspectAttachments` 负责协议编码并执行一次无状态模型请求。识图模型不接收主聊天历史、主助手系统提示或任何工具。Responses 使用完整 input 与 `store=false`，没有 previous_response_id。唯一入口及内部职责见 [架构约定](architecture.md#模型可见内容的唯一入口)。

沿用共享模型客户端的 180 秒响应头等待上限，不设置附件识别总期限或独立计时器。远端服务可以提前返回自己的超时错误；用户取消仍沿原调用上下文传播。

本地复用 provider 地址和凭据；平台复用目录中的原始 runtimePath、admit、短期 access、managed generation 及 interaction ID。没有厂商域名特判、调用重试、工具递归、图片丢弃或模型回退。输出预算、显式配置的 temperature/topP 复用原模型参数，推理档采用供应商默认。响应最多 4 MiB，可用文字最多 256 KiB；拒绝空结果、异常结束和超限。取消传递到请求及响应体读取，结果发布前再次核对轮次/目录/身份。

成功只返回 `status=completed` 和 `text`，不重复附件参数、模型 ID 或界面身份，不返回图片字节。Docker Agent 将文字结果放进当前上下文、后续轮次和分支历史。失败通过 MCP isError 和英文 status/code/message 返回；实际收到上游错误时才附带 upstream_status 和安全的 upstream_code，不直传上游 message。完整规则见[工具返回合同](tool-results.md)。

远程预览身份来自本轮捕获的 workspace，保存在 Tuck tool-call presentation 中，不进入模型上下文。预览按消息和具体调用块定位并重新检查账号、空间、绑定及授权。分支从父对话的工具名、callId、完整输入匹配继承；身份缺失或不一致时明确不可预览，不使用当前登录身份猜测历史归属。

## 不支持多模态时的实际行为

官方 v1.150.0（`5277e7842058fc3fbb9943a6e88bc05a1c8e6fe2`）已通过隔离测试核对：公开模型配置的 `capabilities.image=false` 时，MCP 图片仍保存在公开 session 历史，但后续聊天请求不包含图片像素，且不会因此产生错误。不能从工具成功或界面有图推断模型已看见图片。生图工具在此情况下明确说明模型理解限制；只有附件识别确已获许可并可用时才建议调用它，避免提示使用已禁用工具。

用户上传图片时：聊天模型声明 IMAGE 则继续原有原生多模态输入；纯文本模型只有在附件识别已配置且获许可时，才允许提交图片，发送给聊天模型的只是文件路径与调用识别工具的明确指引，浏览器也显示此区别。不会自动调用识图或自动替换用户选择的聊天模型。未配置时前后端均给出可操作错误。

若识图模型声明 IMAGE 但上游实际不支持，服务错误作为工具结果交给 Agent，可在同轮解释并提出修正。若错误来自主聊天模型本身的请求，原生执行会报告模型失败；没有成功的模型续步可接收工具结果。Chat Completions 与 Responses 均已通过“主模型声明 IMAGE、真实上传图片、合成供应商返回 400”验证错误进入任务失败信息。Tuck 展示脱敏错误，不伪造工具消息、不改写原生历史、不接管原生重试。外部 MCP 的任意媒体结果仍由官方引擎处理，本工具不拦截其他服务。

## 规划与验收

PDF、音视频、Office 等附件类型尚未实施；名称和参数不限定图片，未来根据明确的媒体读取和线协议合同增加类型，不将 IMAGE 声明扩大解释为通用附件支持。TTS、ASR 不属于本期。

协议依据：[OpenAI 图片输入](https://developers.openai.com/api/docs/guides/images-vision)、[Docker Agent 模型配置](https://docker.github.io/docker-agent/concepts/models/)、[Docker Agent 工具](https://docker.github.io/docker-agent/concepts/tools/)。当前行为以官方二进制隔离测试为准，不依赖引擎源码内部实现或 SQLite。

`attachment_inspection_test.go` 覆盖 Chat/Responses 交叉协议、四张图片的顺序与实际模型表示、原生子代理覆盖、只读许可、上游失败进入上下文、后续轮次/分支、路径边界、平台准入及取消。`attachment-inspection.spec.ts` 覆盖实际设置、纯文本聊天上传与识别、模型请求证据、历史刷新、失败提示和停用，并检查 1365/390/320px 页面。真实服务验收须分别核对供应商实际调用、识别结果、上下文和界面展示；合成供应商不能证明识别质量。

`tool_revision_test.go` 补充远程识别的 Chat/Responses、纯文本主模型续步、无本地目录和绑定变更拒绝；`runcove-tools.spec.ts` 验收远程输入预览、刷新及远程交付快照。隔离服务验证不等于真实远程部署验收。
