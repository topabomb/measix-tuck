# 生图协议调研与实施边界

## 当前工具契约

`generate_images` 只接收必填 prompt、默认 1 的 count（1–4）和默认不传的 reference_images（0–4 个任务内图片路径）。仅在需要保留主体、风格或编辑已有图片时传参考图，顺序对应提示词描述。生图资源按 Agent 的 imageGenerator 覆盖、全局 defaultImageGenerator、平台默认的顺序选择；省略覆盖或空对象表示继承，明确选择失效不回退。设置页面中的覆盖与工具描述、实际调用使用同一选择。不让 Agent 选择供应商、像素尺寸、分辨率档位或质量。本地请求省略 size，平台请求明确发送 size="auto"；所有请求都不传 resolution、aspect_ratio，不向 Agent 暴露尺寸参数，也不保证精确比例。

无参考图采用 OpenAI Images Generations JSON；有参考图采用 OpenAI Images Edits multipart/form-data 的 image[] 文件上传。一个工具根据是否有参考图选择公开操作，不按提供商域名或模型 ID 建立适配表，不尝试多种线协议，不失败后丢弃参考图再生成。模型不支持参考图或输出数量时保留明确错误，不自动重试或改数量。

参考图只通过 Files 读取任务内普通文件，保留原字节，验证解码和现有 40M 像素限制；格式限 PNG/JPEG/WebP，单张最多 16 MiB、合计最多 48 MiB。不自动读取会话所有图片，不接受远程 URL、base64 或绝对路径。引用前次产物可直接使用工具返回路径，不创建参考图缓存或上传到独立文件服务。输出文件和提示词沿用[任务文件规范](task-files.md)。

本地配置仍使用 `OPENAI_IMAGES_GENERATIONS` 表示当前 OpenAI Images 资源，支持上述两项操作；用户须确认服务实现标准 Edits 文件上传。平台现行资源合同只有一个生成 runtimePath，未声明编辑路由；Tuck 不改写这个路径，也不新增平台字段。平台资源只允许无参考图，数量上限为 min(4, maxImagesPerRequest)，保留 admit、access、身份及工具许可校验。平台 allowedSizes 仍按外部合同验证，不选择其中某个具体尺寸；固定请求 size="auto"，平台若拒绝则原样报告失败，不取首项兜底。平台开放参考图须先演进权威合同，再实施对应协议，不能由客户端猜测。

平台生图资源继续可用。传入任何参考图时，Tuck 在读取参考文件、申请模型调用准入或发送模型请求之前返回 image_references_unsupported，不丢弃参考图继续生成。待 Core 明确参考图合同后再适配，不猜编辑路由。工具简要提示该限制。提供商/模型文字由统一描述资源生成：本地显示已配置提供商显示名和 upstreamModelKey；平台只显示下发模型标识，因为现有生图合同没有提供商关联，不能伪造提供商名。

同日使用已配置 Weero 提供商直接调用 `/v1/images/generations`，分别验证 `grok-imagine-image-2.0` 和 `gpt-image-2.5`：请求只有 model、prompt、n=1，两次均 HTTP 200，返回一个含 b64_json 的图片条目。此证据仅覆盖真实供应商的无参考图单张生成，不等于真实平台链路、参考图或浏览器上下文验收。没有据此自动更改默认资源。

## 官方支持情况（2026-10-16 核对）

| 模型/接口 | 参考图与线协议 | 本期范围 |
| --- | --- | --- |
| OpenAI GPT Image 系列，包括 2.5 | Images Edits 支持多张图片，官方文件上传示例使用 image[]；当前参考文档列出最多 16 张输入，并提供 JSON 图片引用形式 | 实现官方 multipart 形式，产品上限 4 张；不另加 JSON 形式 |
| Grok Imagine | 最多 5 张参考图；Images Edits 使用 JSON images 数组，元素为图片引用对象 | 证实模型能力；其专有编码未实现，不能宣称标准 multipart 可直连 |
| 百炼 Qwen Image | 兼容模式在 Images Generations 用扩展 image 字符串/数组；2.1 支持 1–10 张，3.0 支持 1–3 张；明确不支持 Edits/multipart | 证实模型能力；这类专有扩展未实现，不能宣称所有模型都接受 4 张 |

来源：[OpenAI Images Edits](https://developers.openai.com/api/reference/resources/images/methods/edit)、[Grok 多图编辑](https://docs.x.ai/developers/model-capabilities/images/multi-image-editing)、[百炼千问生成与编辑](https://help.aliyun.com/zh/model-studio/qwen-image-generation-and-editing-api-reference)。这些资料证明能力和协议差异，不代替真实账号的模型可用性验收。

Android 的现有工具仅暴露提示词与设为背景，固定一张，内部优先 auto，否则取允许尺寸首项；其 OpenAI 适配省略 auto/空尺寸及 api.x.ai 的 size。这说明减少 Agent 参数可降低错误，但不能保证比例，也不等于已经实现跨厂商参考图。Tuck 借鉴工具简化，不复制尺寸首项兜底或域名特判。

## 后续规划与验收

其他线协议、平台编辑合同、遮罩、尺寸控制、TTS、ASR 均未实施。未来确有需求时增加明确的协议编码与合同，不建立通用任意参数模板或猜测模型能力。Docker Agent 始终只通过公开 MCP 调用并接收图片，不改引擎、不读原生数据库。

当前采用未修改官方引擎和隔离合成服务验证：无参考图默认一张；1/4 张参考图的 multipart 路径、顺序和原字节；四张生成结果的展示与 Chat/Responses 后续上下文；参数、路径、大小、取消、平台拒绝及失败不回退。浏览器检查宽屏、390px、320px 的多图预览、提示词及刷新后的文件事实。真实供应商验收按用户决定，待配置真实资源后另行执行。具体入口见[验证指南](verification.md#生图链路)。
