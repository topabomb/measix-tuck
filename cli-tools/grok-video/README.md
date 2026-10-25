# grok-video 完整手册

独立的 xAI 视频创作 CLI，支持生成、编辑、续接、查询、等待与下载。可执行文件不依赖 Tuck、Node、Python、FFmpeg 或源代码；模型凭据与服务地址由调用者显式提供。


本 `README.md` 是工具唯一的完整 Markdown 文档源文件，包含使用说明、全部命令和参数、协议调研及验证边界。编译时通过 `go:embed README.md` 直接嵌入；`grok-video docs` 原样输出这些字节，不拼接其他文件、不生成参数章节、不添加版本或替换环境变量。修改本文后须重新构建，`version` 单独报告程序版本。

目录：

- [支持的模型与能力](#支持的模型与能力)
- [获取帮助和离线文档](#获取帮助和离线文档)
- [连接配置](#连接配置)
- [创作与任务操作](#创作与任务操作)
- [命令与参数参考](#命令与参数参考)
- [输出、取消和失败](#输出取消和失败)
- [协议调研](#协议调研)
- [构建和测试](#构建和测试)

## 支持的模型与能力

下表是工具支持的 xAI 协议能力，供应商/账户实际可用范围另见 [协议调研](#协议调研)。

| 模型 | 文字/单图生成 | 参考素材和固定帧 | 编辑/续接 |
| --- | --- | --- | --- |
| `grok-imagine-video-1.5` | 1～15 秒；480p/720p/原生 1080p | 最多 14 张参考图、3 个参考音色/音频、首尾帧、4 个中间关键帧；参考/固定帧模式最高 720p | 官方当前未支持 |
| `grok-imagine-video-1.5-lite` | 1～15 秒；480p/720p/上采样 1080p | 单图起始帧；不支持多参考素材、尾帧、中间关键帧 | 官方当前未支持 |
| `grok-imagine-video` | 1～15 秒；480p/720p | 最多 7 张参考图、参考模式最长 10 秒；不支持参考音色、尾帧、中间关键帧，也不能混合单图和参考图 | 编辑源片最长 8.7 秒；续接源片 2～15 秒，新增 2～10 秒 |

`grok-imagine-video-1.5-preview` 和 `grok-imagine-video-1.5-2026-05-30` 按官方已知别名采用 1.5 的参数校验，但请求中的模型名保持原样。其他模型名也原样提交，由服务端检查能力。

**匿名化实测（2026-10-25）：所测兼容服务的普通版和 1.5 生成、普通版 URL 编辑成功；Lite 返回 400 不支持，续接返回 405。** 1.5 参考素材、固定帧组合及静音也已完成生成和下载。这些是特定服务在该日期的结果，不能推广到其他服务；官方能力、工具参数支持与供应商实际可用性有各自边界，详见本文协议调研。

## 获取帮助和离线文档

```powershell
grok-video --help
grok-video help generate
grok-video edit --help
grok-video docs
grok-video docs > grok-video-manual.md
```

`docs` 将编译时嵌入的本 `README.md` 原样写入 stdout，逐字节一致。完整手册的源文件就是当前文件；导出的 `grok-video-manual.md` 是输出产物，不参与编译。无需源码、旁边的文档、认证或网络。`help` 提供即时命令帮助，修改参数时须同步维护本文。

## 连接配置

默认 API 前缀为 `https://api.x.ai/v1`，可用 `GROK_BASE_URL` 或 `--base-url` 覆盖；前缀必须包含服务要求的 `/v1`。默认读取 `GROK_API_KEY`，缺失时读取 `XAI_API_KEY`。也可用 `--api-key-env NAME` 指定另一个变量，或用 `--api-key-file PATH` 读取 UTF-8 密钥文件；这两个显式选项互斥。

工具不读取 Tuck 配置。密钥不使用命令行明文参数，避免出现在进程列表和命令历史中。以下例子假定已经通过环境或密钥文件配置认证：

```powershell
$env:GROK_BASE_URL = 'https://video-api.example.com/v1'
grok-video models --api-key-file C:\private\provider-key.txt
```

所有选项跟在子命令后；对于带任务 ID 的命令，选项可以位于 ID 前后。

## 创作与任务操作

```powershell
# 提交一次，立即输出 request_id；不自动等待。
grok-video generate --model grok-imagine-video-1.5 --prompt-file scene.txt --duration 6

# 一次完成创建、等待、下载；输出目录须已存在。
grok-video generate --prompt '红球在白色桌面上缓慢滚动，固定镜头。' --output clip.mp4

# Lite 的正确调用方式；要求所选服务实际开放该模型。
grok-video generate --model grok-imagine-video-1.5-lite --image first.png --prompt '镜头缓慢拉远' --output lite.mp4

# 1.5 的固定帧与中间分镜。
grok-video generate --image first.png --last-frame last.png --keyframe 2=middle.png --duration 4 --output transition.mp4

# 独立查询和恢复等待，沿用相同地址/凭据。
grok-video status REQUEST_ID
grok-video wait REQUEST_ID --wait-timeout 15m --output result.mp4
grok-video download REQUEST_ID --output another-copy.mp4

# 普通版编辑与续接；是否可用取决于供应商。
grok-video edit --video source.mp4 --prompt '把球改成蓝色，其他内容保持不变' --output edited.mp4
grok-video extend --video source.mp4 --prompt '镜头继续缓慢后退' --duration 3 --output extended.mp4

# 只检查并查看将要发送的 JSON，不需要凭据，也不提交任务。
grok-video generate --image first.png --last-frame last.png --duration 4 --dry-run
```

图片/视频输入可以是本地路径、HTTP(S) URL、Base64 data URL 或 `file-id:ID`；音频参考使用本地文件、HTTP(S) URL 或 Base64 data URL，不支持文件 ID。相对路径从当前工作目录解析。本地及 data URL 单素材上限 32 MiB，编码后的整次请求上限 128 MiB；更大的素材使用远端 URL 或已上传文件 ID。输入视频时长、编码和自带音频的账户权限由上游检查。

`--output` 是本地 MP4；`--upload-url` 和 `--store-*` 是可选的上游投递/存储能力，语义不同。现有本地文件默认保留，只有 `--overwrite` 明确允许替换。下载先写同目录临时文件、检查 MP4 标识并完成写入，再发布；失败不会留下半个正式文件。


## 命令与参数参考

### 调用语法

```text
grok-video COMMAND [OPTIONS] [REQUEST_ID]
grok-video help [COMMAND]
grok-video --help
grok-video --version
```

选项必须位于子命令之后；任务 ID 可以位于选项前后。值可使用 `--name VALUE` 或 `--name=VALUE`，布尔选项使用 `--flag` 或 `--flag=false`。可重复选项每出现一次追加一项；路径/提示词含空格时使用 shell 引号。`--` 结束选项解析。没有参数时输出总帮助。

| 子命令 | 用途 | 默认行为 |
| --- | --- | --- |
| `generate` | 文字、图片、参考素材或固定帧生成 | 提交一次，不等待 |
| `edit` | 修改现有视频 | 提交一次，不等待 |
| `extend` | 为现有视频追加片段 | 提交一次，不等待 |
| `status REQUEST_ID` | 查询任务一次 | 输出当前 JSON，不轮询/下载 |
| `wait REQUEST_ID` | 等待已有任务 | 轮询到终态，可下载 |
| `download REQUEST_ID` | 下载成品 | 查询一次，要求 `--output` |
| `models` | 查看模型目录 | 筛选名称前缀，不创建视频 |
| `docs` | 输出本文 | 离线原样输出 Markdown |
| `version` | 输出程序版本 | 离线文本输出 |
| `help [COMMAND]` | 输出总帮助或命令帮助 | 离线文本输出 |

以下参数按适用命令分组，每个命令同时使用注明的公共参数表。

### 连接与帮助参数

适用于 `generate`、`edit`、`extend`、`status`、`wait`、`download`、`models`。

| 参数 | 类型与默认值 | 语义和限制 |
| --- | --- | --- |
| `--base-url` | URL；`GROK_BASE_URL` 环境值，否则 `https://api.x.ai/v1` | API 前缀，包含服务要求的 `/v1`；允许 HTTP(S)，不允许用户名、密码、查询串或片段。API 请求不跟随重定向 |
| `--api-key-env` | 环境变量名；`GROK_API_KEY` | 默认变量为空时回退到 `XAI_API_KEY`；显式指定此选项后不回退。与 `--api-key-file` 互斥 |
| `--api-key-file` | UTF-8 文件路径；未设置 | 从文件读取密钥，允许 UTF-8 BOM，去除两端空白；最多 64 KiB，密钥非空且不能含空格、换行或制表符 |
| `--timeout` | Go 时长；`2m` | 每次 API 请求和每次成品下载的独立超时，必须大于零；例如 `500ms`、`30s`、`2m` |
| `--help`、`-h` | 布尔；`false` | 输出该命令完整帮助，不要求认证或网络 |

密钥只能通过环境或指定文件提供，没有明文密钥参数。`docs`、`version`、`help` 不接受这些连接参数；`docs --help`、`version --help` 及 `-h` 可用。

### 创建共用参数

适用于 `generate`、`edit`、`extend`，另有少量明确标出的差异。

| 参数 | 类型与默认值 | 语义和限制 |
| --- | --- | --- |
| `--model` | 字符串；`generate` 为 `grok-imagine-video-1.5`，`edit`/`extend` 为 `grok-imagine-video` | 上游模型完整 ID，不能为空；不改写名字 |
| `--prompt` | UTF-8 文本；未设置 | 与 `--prompt-file` 互斥；纯文字/纯参考素材生成、编辑、续接必填，单图或固定帧生成可以省略 |
| `--prompt-file` | 文件路径或 `-`；未设置 | 从 UTF-8 文件或 stdin 读取提示词，最大 1 MiB，允许 UTF-8 BOM；与 `--prompt` 互斥 |
| `--dry-run` | 布尔；`false` | 校验并输出准确的 `{method,path,body}`，会读取素材；不提交、不等待、不下载，不要求密钥。素材可能以 data URL 出现在输出中 |
| `--user` | 字符串；未设置 | 仅 `generate`/`edit` 接受，作为终端用户标识发送；不是 API key |
| `--upload-url` | HTTP(S) URL；未设置 | 上游向预签名地址 PUT 投递 MP4；发送 `output.upload_url`。不等于本地 `--output` |
| `--store-filename` | 字符串；未设置 | 请求上游保存到 xAI Files，发送 `storage_options.filename`；所有其他 `--store-*` 选项均要求它 |
| `--store-expires` | 整数秒；`0` | 文件 TTL；显式非零值须为 3600～2592000。0 不发送 TTL，按上游规则保留，不在客户端定时删除 |
| `--store-public` | 布尔；`false` | 请求公开的远端文件 URL，要求 `--store-filename`；发送 `storage_options.public_url` |
| `--store-public-expires` | 整数秒；`0` | 公开 URL TTL；要求 `--store-public`，非零值须为 3600～2592000；若指定文件 TTL，不得超过它。0 省略，由上游继承 |

使用远端存储/PUT 时仍可能没有可下载的 `video.url`；可直接查看任务结果的 `file_output`、`storage_error` 等字段。需要本地下载时，结果必须包含 `video.url`。

### 等待与下载共用参数

适用于 `generate`、`edit`、`extend`、`wait`、`download`；`status`、`models` 不接受。

| 参数 | 类型与默认值 | 语义和限制 |
| --- | --- | --- |
| `--wait` | 布尔；`false` | 创建命令和 `download` 可用；开启轮询。`wait` 命令始终轮询，没有此选项。创建命令指定 `--output` 也会开启等待 |
| `--poll-interval` | Go 时长；`5s` | 两次 pending 查询之间的等待，必须大于零 |
| `--wait-timeout` | Go 时长；`10m` | 总轮询时限，必须大于零；创建命令从提交成功后开始，`wait`/`download --wait` 从开始轮询时计，不包括随后的下载 |
| `--output` | 本地文件路径；未设置 | MP4 目的地；父目录须已存在，不接受 `-`。`download` 必填；创建命令自动等待；`wait` 可选 |
| `--overwrite` | 布尔；`false` | 允许替换现有普通文件，要求 `--output`；不允许目录、符号链接或特殊文件 |
| `--max-download-bytes` | 整数；`536870912`（512 MiB） | 成品下载上限，有效范围 12～2^62−1 字节；超限失败，不发布半成品 |

### 命令 generate

```text
grok-video generate [OPTIONS]
```

`POST /videos/generations`。使用连接、创建、等待/下载三组共用参数，以及以下专用参数。

| 参数 | 类型与默认值 | 语义和限制 |
| --- | --- | --- |
| `--duration` | 整数秒；`8` | 成品长度，1～15；普通版参考图模式最多 10 秒；发送 `duration` |
| `--aspect-ratio` | 枚举；未设置 | `1:1`、`16:9`、`9:16`、`4:3`、`3:4`、`3:2`、`2:3`、`21:9`、`5:2`；省略由上游默认 16:9；发送 `aspect_ratio`。图生视频按上游规则跟随源图比例 |
| `--resolution` | 枚举；未设置 | `480p`、`720p`、`1080p`；省略由上游默认 480p。普通版最高 720p；参考素材/固定帧模式最高 720p |
| `--audio` | 枚举；`auto` | `auto` 不发送字段，上游通常生成音轨；`on`/`off` 发送 `generate_audio:true/false` |
| `--image` | 图片 SOURCE；未设置 | 起始帧，发送 `image`；格式和大小见素材规则 |
| `--reference-image` | 可重复图片 SOURCE；空列表 | 发送 `reference_images`；普通版最多 7 张，1.5 最多 14 张；Lite 不支持 |
| `--reference-voice` | 可重复音色 ID；空列表 | 例如 `eve`，编码为 `reference_audios` 中的 `voice_id`；1.5 专属，与参考音频合计最多 3 项 |
| `--reference-audio` | 可重复音频 SOURCE；空列表 | 编码为 `reference_audios` 中的 `url`；1.5 专属，单段最长 15 秒由上游检查，自带音频需账户特别权限 |
| `--last-frame` | 图片 SOURCE；未设置 | 固定尾帧，发送 `last_frame`；1.5 专属 |
| `--keyframe` | 可重复 `SECONDS=SOURCE`；空列表 | 1.5 最多 4 个中间帧；时间可以是小数，严格在 0 与成品时长之间，占据不同的内部 1/3 秒时隙；发送 `keyframes[].{timestamp_s,image}` |

纯文字和纯参考素材请求需要提示词；提供 `--image`、`--last-frame` 或 `--keyframe` 时允许省略提示词，上游模型仍可施加自身规则。未知模型名原样提交，但客户端仍限制最多 14 张参考图、3 个参考声音/音频和 4 个中间帧。模型间的组合限制见能力表。

```powershell
# 参考图与预置音色。
grok-video generate --reference-image person.png --reference-voice eve --prompt '<IMAGE_0> 使用 <AUDIO_0> 说你好' --wait
# 静音。
grok-video generate --image first.png --prompt '红球缓慢滚动' --audio off --output silent.mp4
# 保存远端 Files；不请求本地下载。
grok-video generate --prompt-file scene.txt --store-filename scene.mp4 --store-expires 86400 --store-public --store-public-expires 3600 --wait
```

### 命令 edit

```text
grok-video edit --video SOURCE --prompt TEXT [OPTIONS]
```

`POST /videos/edits`。使用连接、创建、等待/下载参数；也可用 `--prompt-file`。

| 专用参数 | 类型与默认值 | 语义和限制 |
| --- | --- | --- |
| `--video` | MP4 SOURCE；未设置，必填 | 编辑源视频，最长 8.7 秒由上游检查；发送 `video` |

默认普通版模型，已知 1.5/Lite 模型会在本地拒绝编辑。保持源视频时长和比例，分辨率最高 720p。没有 `--duration`、`--resolution`、`--aspect-ratio` 或音轨开关。

```powershell
# 匿名化兼容服务实测通过的是远程 URL 输入的编辑路径。
grok-video edit --video https://example.com/source.mp4 --prompt-file edit.txt --wait
```

### 命令 extend

```text
grok-video extend --video SOURCE --prompt TEXT [OPTIONS]
```

`POST /videos/extensions`。使用连接、创建、等待/下载参数；不接受 `--user`，也可用 `--prompt-file`。

| 专用参数 | 类型与默认值 | 语义和限制 |
| --- | --- | --- |
| `--video` | MP4 SOURCE；未设置，必填 | 源片 2～15 秒，由上游检查；发送 `video` |
| `--duration` | 整数秒；`6` | 新增片段长度，2～10；成品还包含原视频，不是总时长 |

默认普通版模型，已知 1.5/Lite 模型会在本地拒绝续接。保持源比例和分辨率，最高 720p；没有比例、分辨率或音轨开关。下文实测的兼容服务返回 405，工具支持官方调用合同但未取得该供应商的续接成功验收。

### 命令 status

```text
grok-video status REQUEST_ID [OPTIONS]
```

仅使用连接/帮助参数。一次 `GET /videos/REQUEST_ID`，保留上游字段并补充 `request_id`。查询成功就退出 0，即使远端是 `pending`、`failed` 或 `expired`。不等待、不下载。

```powershell
grok-video status REQUEST_ID --base-url https://video-api.example.com/v1
```

### 命令 wait

```text
grok-video wait REQUEST_ID [OPTIONS]
```

使用连接和等待/下载参数，没有 `--wait` 选项。反复查询已有任务，pending 时等待，done 成功，failed/expired/审核过滤退出 4；未知状态作为协议错误。可选 `--output` 在完成后下载。

```powershell
grok-video wait REQUEST_ID --wait-timeout 15m --output result.mp4
```

### 命令 download

```text
grok-video download REQUEST_ID --output FILE [OPTIONS]
```

使用连接和等待/下载参数。默认查询一次，任务未完成、失败、过期或过滤时退出 4；`--wait` 可先轮询。完成后下载 `video.url`，MP4 须有 `ftyp` 标识。默认不覆盖文件；成功 JSON 增加 `download.path/bytes`。

```powershell
grok-video download REQUEST_ID --output another-copy.mp4
grok-video download REQUEST_ID --wait --output result.mp4 --overwrite
```

### 命令 models

```text
grok-video models [OPTIONS]
```

使用连接参数，另有以下参数。`GET /models`，保留目录顶层字段，用筛选后的数组替换 `data`。目录列出的模型不等于该模型各项视频能力可调用。

| 专用参数 | 类型与默认值 | 语义 |
| --- | --- | --- |
| `--prefix` | 字符串；`grok-imagine-video` | 仅保留 ID 以此开头的项；`--prefix=` 输出完整目录 |

```powershell
grok-video models
grok-video models --prefix= --api-key-file C:\private\provider-key.txt
```

### 命令 docs

```text
grok-video docs
grok-video docs > grok-video-manual.md
```

将编译时嵌入的本 `README.md` 作为 UTF-8 Markdown 字符串原样写入 stdout，逐字节一致。只接受 `--help`/`-h` 查看说明，不接受文件名、连接或其他参数。输出路径由 shell 重定向决定。无需源码、旁边的文件、凭据或网络，不生成任何附加章节。退出 0 表示输出成功，1 表示写入失败。

### 命令 version

```text
grok-video version
grok-video --version
```

输出 `grok-video VERSION` 和换行；没有连接参数，`version --help`/`version -h` 查看用法。离线执行，成功退出 0。版本由构建脚本的 `-Version` 注入，默认 `0.1.0`。

### 命令 help

```text
grok-video help
grok-video help generate
grok-video generate --help
```

无子命令时输出总帮助；指定子命令时与其 `--help` 等价。总帮助也可使用 `grok-video --help` 或 `grok-video -h`。无需认证/网络；错误命令或多余参数退出 2。

### 素材与分镜规则

图片支持 JPEG/PNG/WebP，视频为 MP4；本地音频按 MIME 识别，上游决定实际格式、最长 15 秒及账户权限。`file-id:` 必须是调用者事先取得的文件 ID，工具不会调用上传接口。远程 URL 交给上游读取，不由本机提前下载；URL 不允许用户名、密码或片段。本地/data URL 的大小及路径规则见前面的创作示例。

本地普通文件会转成 data URL；素材对象的 `url` 与 `file_id` 互斥。输入视频时长、完整编码兼容性由上游检查。`--dry-run` 也会读取并编码本地素材，输出可能很大；素材和签名 URL 应按运行数据保管。

参考图按出现顺序映射 `<IMAGE_0>`、`<IMAGE_1>` 等；若有起始帧 `--image`，它占 `<IMAGE_0>`。音频索引按“所有 `--reference-voice`，然后所有 `--reference-audio`”的稳定顺序映射 `<AUDIO_0>` 等，即使两类参数在命令中交错出现也是如此。

中间关键帧格式为 `--keyframe 2.5=middle.png`。时间须严格位于 `(0,duration)` 内，四舍五入到 1/3 秒后不能落在首尾或与其他关键帧重叠。1.5 最多 4 个中间帧。固定首尾/关键帧不等于对成品逐帧一致性的保证。

## 输出、取消和失败

成功操作输出一个 JSON 对象；帮助/版本输出文本，`docs` 输出 Markdown。进度和错误在 stderr。查询结果保留上游字段并补充 `request_id`；下载结果另外包含 `download.path` 和 `download.bytes`。

| 退出码 | 含义 |
| --- | --- |
| `0` | 操作成功；`status` 仅表示查询成功，远端失败状态仍在 JSON 中 |
| `1` | 本地 I/O 或输出失败 |
| `2` | 参数、配置或创建输入校验失败 |
| `3` | HTTP、网络或协议错误；未知任务状态、缺失必要字段也属于此类 |
| `4` | 等待/下载的远端任务失败、过期、审核过滤或尚未完成 |
| `5` | 总等待时限用尽；单次 HTTP 请求超时属于 3 |
| `130` | Ctrl+C 中断 |

创建请求不自动重试，模型不静默替换。提交后立即在 stderr 显示任务 ID；Ctrl+C 和本地超时只停止客户端工作，不代表服务端取消。等待超时可使用相同 ID 继续。若创建响应丢失，远端可能已经接受任务，应先核对供应商记录再决定是否重发。

API 请求不跟随重定向；下载使用独立的无认证连接，即使下载地址与 API 同域或发生重定向，也不附带 API key。


以下是下载成功结果的结构示例，不是某次真实任务记录：

```json
{
  "request_id": "example-job",
  "status": "done",
  "video": {
    "url": "https://example.com/result.mp4",
    "duration": 2,
    "respect_moderation": true
  },
  "download": {
    "path": "C:\\work\\result.mp4",
    "bytes": 123456
  }
}
```

只有请求过下载才有 `download`。pending 没有成品；failed/expired 可包含 `error`。`wait` 遇到远端失败时同时输出已取得的任务 JSON 和非零退出码；网络/本地失败时不能依赖 stdout 一定有 JSON。下载 URL 可能带签名，stdout 也应按运行数据保管。

任务 ID 为 1～256 个字母、数字、下划线或连字符，首位为字母或数字。客户端识别 pending/done/failed/expired，缺失或未知状态明确报协议错误。done 结果审核失败时不会下载。总等待超时退出 5，单次 HTTP 请求超时退出 3；创建输入校验失败退出 2。

下载限制重定向次数并拒绝 HTTPS 降级；不覆盖发布使用文件系统硬链接，覆盖发布使用重命名，若文件系统不支持对应操作则明确报错。工具不会创建输出目录。MP4 标识检查不代替完整媒体解码检查。

## 协议调研

核对日期：2026-10-25。下文区分官方协议、CLI 实现和某个兼容服务的实测。服务身份与地址不公开；`https://video-api.example.com/v1` 仅为占位示例，须替换为调用者自己的地址。未取得 xAI 直连验收，因此不能把代理结果写成官方直连结果。

### 官方依据

- [视频 REST 参考](https://docs.x.ai/developers/rest-api-reference/inference/videos)：路径、请求与返回结构；其 `.md` 版本可读取网页折叠的完整字段。
- [生成流程](https://docs.x.ai/developers/model-capabilities/video/generation)、[能力比较](https://docs.x.ai/developers/model-capabilities/video/overview)：时长、尺寸、声音、轮询与模型差异。
- [参考图、语音与固定帧](https://docs.x.ai/developers/model-capabilities/video/reference-to-video)：输入组合及限制。
- [编辑](https://docs.x.ai/developers/model-capabilities/video/editing)、[续接](https://docs.x.ai/developers/model-capabilities/video/extension)：源片限制与新增时长语义。
- [输出保存](https://docs.x.ai/developers/model-capabilities/imagine/files/outputs)：远端存储和公开链接的独立有效期。
- 模型页：[1.5](https://docs.x.ai/developers/models/grok-imagine-video-1.5)、[1.5-lite](https://docs.x.ai/developers/models/grok-imagine-video-1.5-lite)、[普通版](https://docs.x.ai/developers/models/grok-imagine-video)。

### 线协议和 CLI 映射

认证使用 `Authorization: Bearer ...`，提交 JSON；API 前缀包含 `/v1`。

| CLI | HTTP 路径 | 结果 |
| --- | --- | --- |
| `generate` | `POST /videos/generations` | `request_id` |
| `edit` | `POST /videos/edits` | `request_id` |
| `extend` | `POST /videos/extensions` | `request_id` |
| `status` / `wait` | `GET /videos/{request_id}` | 状态、进度、成品或错误 |
| `download` | 查询后对 `video.url` 发 GET | MP4；不传 API 凭据 |
| `models` | `GET /models` | 按名称前缀筛选目录，不能证明可执行能力 |

生成字段包括 `model/prompt/duration/aspect_ratio/resolution/image/reference_images/reference_audios/last_frame/keyframes/generate_audio/user`；可选输出使用 `output.upload_url`、`storage_options`。编辑使用 `model/prompt/video/user` 和输出选项；续接使用 `model/prompt/video/duration` 和输出选项。各字段的 CLI 类型、限制、缺省值、输入方式和示例见本文命令与参数参考；`help` 提供即时命令帮助，`docs` 原样输出本文。

素材对象的 `url` 与 `file_id` 互斥；音频使用 `voice_id` 或 `url`。创建与最终成功不是同一件事。官方生成指南的核心状态为 `pending/done/failed`，续接示例还处理 `expired`。完成结果包含 `video.url/duration/respect_moderation`，还可能包含 `progress/usage/file_output/storage_error`。客户端保留已取得字段，不猜测进度，不把未知状态当作完成。

SDK 的 `timeout/interval` 是本地等待参数，不是视频生成请求字段；CLI 对应 `--wait-timeout/--poll-interval`。公开视频资源未列出取消生成任务的接口，停止客户端不会宣称取消远端任务。

### 官方文档差异

生成指南提供 `generate_audio` 和 `last_frame` 的 REST 示例，但当前 REST Markdown 字段表没有列出这两个字段。CLI 按指南编码，并保留上游实际错误。`prompt` 的必填描述在参考指南和 REST 表中也有差异：CLI 允许单图或固定帧请求省略提示词，纯文字、纯参考素材、编辑和续接要求提示词。

REST 表说明 `duration` 可接受 OpenAI 风格 `seconds`，但本工具统一发送 xAI 的 `duration`。参数别名、共享认证方式和 `/v1` 前缀都不足以证明完整 OpenAI Videos 合同。

### 兼容服务实测（匿名）

以下是同一个兼容服务在核对日期的匿名化实测结果，来自带认证请求，不从首页或模型目录推测，也不代表所有兼容服务的能力。真实地址、凭据、账户与任务记录未写入公开手册；示例地址不能用来复现实测。

| 项目 | 取得的事实 | 结论 |
| --- | --- | --- |
| 模型目录 | 普通版、1.5、1.5-preview、1.5-lite 均列出 | 仅表示目录广告 |
| 普通版文生视频 | 构建的 CLI 提交 2 秒 480p，查询到 done，下载可解码的 H.264/AAC MP4 | 端到端通过 |
| 1.5 文生视频 | 同样生成、查询、下载成功 | 端到端通过 |
| 1.5-preview | 早期探测实际生成 1 秒视频，结果 model 为 1.5 | 别名可用；保留原请求名 |
| 1.5-lite | `/videos/generations` 和 `/videos` 均返回 HTTP 400，明确说明该模型不受支持 | 工具支持其官方参数，所测兼容服务接口不可用；不替换模型 |
| 非法时长 | `duration:0` 返回 400，要求 1～15 秒 | 服务端执行时长校验 |
| 普通版参考音色 | 返回 400，明确 `reference_audios` 不支持该模型 | 该字段到达能力校验层 |
| `/videos` 别名 | `seconds:"1"` 创建并完成 1 秒视频，返回 `request_id`；请求 `size:"1280x720"`，成品却是 848×480；`GET /videos/{id}/content` 返回 404 | 明确不是完整 OpenAI Videos 合同；统一使用 xAI 路径和字段 |
| 视频编辑 | 首次本地 MP4 输入返回 503 `auth_unavailable`，包含上游 405；随后用同一源片的远程 URL 独立提交，完成并下载约 2 秒、848×480 的 H.264/AAC MP4 | URL 编辑端到端通过；本地输入失败原因尚不能确定 |
| 视频续接 | URL 输入首次请求和独立复测均返回 HTTP 405 | 当前部署未完成续接；不能算通过 |
| 1.5 单图与静音 | CLI 将本地图转成 data URL，`--audio off` 提交成功，下载约 2 秒 MP4；`ffprobe` 确认没有音轨 | 单图调用和静音语义通过；实际输出 736×400，尺寸不能按分辨率标签机械推算 |
| 1.5 参考素材与固定帧 | 本地首帧、1 张参考图、预置 eve 音色、尾帧、1 个中间关键帧组合请求完成；下载约 3 秒、848×480 的 H.264/AAC MP4 | 组合请求、轮询与下载通过；未宣称逐帧完全一致或声纹一致 |

上述成功样例已经检查媒体编码、实际时长与音轨。未跑满 14 张参考图/3 个声音/4 个关键帧的边界组合，也未逐一验收所有比例、720p/1080p、审核过滤或真实取消等待；这些行为分别由离线约束/流程测试和明确的上游返回处理覆盖，不能称为真实服务全量通过。

### 实现边界与复核方式

同一套 xAI JSON 编码用于官方和兼容服务，只替换显式 `base URL`，不维护供应商猜测、协议降级、模型替换或后台任务服务。Lite 的 400、续接的 405 和编辑过程中出现的 503 都直接向调用者返回；一次失败不推断整个能力永久不可用。供应商修复后可直接复核，不需要改模型名。

本地文件会转成 data URL；远端 URL 和 `file-id:` 原样提交。自带参考音频、Files 存储、预签名 PUT 投递和所有模型的高分辨率组合尚未取得真实服务验收，只有参数编码/校验测试；模型时长/编码和账户权限最终由上游判定。

默认测试只使用本地 HTTP 夹具。真实验收使用小尺寸短片、真实可执行文件、独立任务查询和 `ffprobe` 检查时长/编码；“生成完成”不等于对参考素材一致性、声音身份和画面美感做出了评价。原始素材、请求 ID、下载地址、源码文档缓存与测试输出保存在已忽略的 `.local/grok-video-research/`，不提交个人运行数据。

## 构建和测试

在仓库根目录运行 `./cli-tools/build.ps1 -Tool grok-video`，本机执行文件位于 `cli-tools/dist/<os>-<arch>/`。支持构建 Windows/Linux/macOS 的 amd64/arm64 目标；可执行文件随身携带文档。修改本 README 后必须重新构建才能更新内嵌手册。交叉构建不代表目标系统运行验收。

在 `cli-tools/common/` 和本目录分别运行 `go test ./...`、`go vet ./...`。测试使用本地 HTTP 夹具，不访问收费服务；覆盖请求编码、三个模型及别名的能力约束、完整 help/docs、异步结果、超时/取消、不重试、认证边界与原子下载。真实服务证据和未验证边界见协议调研。


文档测试检查 `docs` 与本 README 的字节一致性、脱离源码目录后的离线输出、写入失败，以及手册是否包含所有实际子命令与参数。修改 CLI 参数或行为时，在同一变更中同步本文。默认测试不调用收费 API。

源码目录只维护本 README 一份 Markdown；导出的 `grok-video-manual.md` 是可丢弃输出，保存在调用者指定位置，不作为另一个文档源文件或编译输入。
