# 前端架构与界面

本文集中维护渲染、界面布局、样式所有权和依赖补丁。产品与原生执行边界见 [架构](architecture.md)，开发命令见 [贡献指南](../CONTRIBUTING.md)，测试入口和验收门槛见 [验证指南](verification.md)。

## 分层与所有权

| 位置（相对 `frontend/src/`） | 职责 |
| --- | --- |
| `app/` | 登录、Hash 路由、全屏外壳、全局导航、主题与配置保护 |
| `modes/` | 对话、任务、设置和企业工作区的产品组合；目标/编排仍为占位页 |
| `shared/workspace/` | 产品运行投影、历史补页与缓存、附件草稿 |
| `shared/enterprise-workspace/` | 企业文件状态、标签、编辑与预览 |
| `shared/ui/` | 图标、摘要弹层、模态框等公共外壳 |
| `engine/`（不含 `vue/`） | 与框架无关的消息身份、会话语义、投影和阅读位置 |
| `engine/vue/` | Vue/Virtua 物理视口、测量和默认渲染组件 |
| `reference/` | `#/reference`、`#/architecture` 的合成场景、播放、压力测试和诊断 |

前端渲染 Engine 与 Docker Agent 是不同组件。渲染 Engine 不执行模型、工具、审批后的继续、子代理调度或网络持久化，也不决定重试和恢复策略。`engine/` 不导入 `reference/`；中立层不依赖 Vue、DOM、Virtua 或产品布局。公开入口分别为 [engine/index.ts](../frontend/src/engine/index.ts) 和可替换的 [engine/vue/index.ts](../frontend/src/engine/vue/index.ts)。

真实产品由 `modes/chat/` 等宿主消费后端事实；Reference 只演练渲染，不提供真实邮件、日历、办公文件生成或其他外部副作用。百万消息场景不等于生产数据库容量。

## 消息与会话语义

消息形状以 [model/conversation.ts](../frontend/src/engine/model/conversation.ts) 为准，当前会话事实以 [conversation/contracts.ts](../frontend/src/engine/conversation/contracts.ts) 和 [session-kernel.ts](../frontend/src/engine/conversation/session-kernel.ts) 为准。维护时须保留以下关系：

- Message、Turn、Step、Block、工具 `callId`、交互 `interactionId` 和子代理引用各有身份，不能用 DOM 顺序、相邻卡片或标题替代。
- 历史 `plan` 块是可重放快照；当前 `activePlan` 由生产者明确提供，`WorkPlan` 复用同一 Plan 形状，不扫描最新历史推测当前计划。Plan 也不代替实际执行 Step。
- `waiting` 当且仅当存在一个待处理交互。回答必须匹配当前交互的精确 ID 和类型，过期回答不能清除后来同类问题；阻断未解除时不能结束执行，已有执行也不能被重复启动重置。
- 审批可通过 `PendingApproval.callId` 关联拟议工具调用，但交互 ID 与调用 ID 仍不同。批准或拒绝只解除对应阻断，不合成工具执行结果；后续行为由外部运行时决定。
- 工具状态、进度、耗时、终端输出和结果采用生产者报告的事实。缺少执行状态时保留未知，不补为 running 或 success；调用和结果按 `callId` 关联。
- `ResourceRef` 表示文件、URL 或产物身份，打开方式、认证和权限由宿主负责。工具能力类别与呈现意图分开，引用同一资源时保持身份一致。
- delegation 保存稳定的子代理引用、状态和可选 `childSessionId`，子消息留在独立会话。foreground/background 是观察事实；子代理失败不自动决定父会话结果或后续策略。
- 终端增量更新稳定 RenderUnit；中断、失败、用量和轮次结局明确保留。idle 只表示当前没有执行，不等于成功；对外暴露的会话值不能泄漏可变内部引用。

这些是渲染一致性要求，不是第二套执行状态机。业务操作继续交给后端与官方运行时。

## 历史、投影与滚动

`ConversationHistorySource` 提供同步的本地热读取；异步加载、预取和缓存填充留在产品适配层。邻近窗口移动只投影新增片段，远跳重建一个有界窗口；Markdown、思考和终端增量保留稳定节点，常规工作量随变化量、热窗口及可见内容增长。

| 范围 | 当前预算及来源 |
| --- | --- |
| 产品历史 | 窗口 128 条、请求/移动 64 条、缓存 512 条且 8 MiB、热会话 3 个；[history-policy.ts](../frontend/src/shared/workspace/history-policy.ts) |
| 渲染运行时默认值 / Reference | 热窗口 2048 条、移动 512 条；[session-runtime.ts](../frontend/src/engine/runtime/session-runtime.ts) |
| 默认投影缓存 | 最多 4096 条；[projection-engine.ts](../frontend/src/engine/presentation/projection-engine.ts) |

预算是实现参数，不放进中立公共 API，也不约束原生模型上下文。产品覆盖默认预算时不得把 Reference 的窗口当成后端分页大小。

语义阅读位置、Latest、锚点和跟随由既有视口 owner 管理；DOM 测量属于 Vue/Virtua。消息间距放在可测量的 `.node-seat` 内部，虚拟包装层 block margin/padding 保持 0。宽度变化、展开、异步媒体、队列或计划出现时保持阅读锚点，Latest 到达实际末尾；不在业务组件中追加 scrollTop 循环、估高表、负 margin 或高度动画。侧栏收起不卸载会话，切换会话和窗口重排仍走原导航 owner。

## 样式与公共组件

以下路径均相对 `frontend/src/`；属性回到所属文件修改，组件 scoped 样式只管理内部布局。`deep` 限于明确的 Engine 适配或组件插槽，不通过提高选择器权重覆盖其他组件的内部结构。

| 所有者 | 管理内容 |
| --- | --- |
| `app/AppShell.vue` | 全屏/可视高度、一级模式栏与手机底栏、模式容器、全局入口 |
| `app/styles/base.css`、`typography.css`、`focus.css` | 文档根、主题滚动条、字号行高、焦点和强制色 |
| `app/themes.ts` | 主题变量和渲染 Engine 颜色映射 |
| `shared/ui/dialog-tokens.css`、`WorkbenchDialog.vue` | 模态框、焦点围栏、表单与预览外壳；业务组件负责保存和关闭判断 |
| `shared/ui/SummaryPopover.vue`、`summary-popover.css` | 摘要定位、关闭和正文内滚动，不负责持续工作面板或完整设置表单 |
| `modes/chat/styles/workbench.css` | ChatPage scoped 的列布局、列表抽屉、详情浮层和顶部入口 |
| `modes/chat/styles/conversation.css` | 产品消息密度、正文、Engine 插槽和 Latest |
| `engine/vue/engine.css` | 可复用视口、测量容器和默认控件 |
| `reference/styles/` | 隔离的参考演练，不供产品消息复用 |

字体基线为辅助 12/18、控件 13/20、正文 14/23、标题 16/24（字号/行高，CSS px）；手机聊天输入 16px。颜色来自主题，Teleported 内容保留字体和主题。纯图标入口有可访问名称，状态不只依赖颜色；支持减少动态效果，使用系统滚动条。

状态、展开、助手身份和侧栏入口分别复用 `ActivityStatus`、`DisclosureIcon`、`AssistantIcon`、`SidebarIcon`，加载提示复用 `TuckMark`。产品工具/思考条目使用自己的 `agent-activity` 外壳。全局“更多”和桌面设置共享 `SettingsMenuContent`；配置保护的横排由 `SettingsLockButton` 的 inline 变体管理。

## 响应式布局与输入

视口宽度控制全局导航和键盘行为，容器宽度控制内容排列。桌面对话列变窄不能触发手机全局导航。断点来自 [AppShell](../frontend/src/app/AppShell.vue)、[useMobileLayout](../frontend/src/shared/use-mobile-layout.ts) 及 [对话样式](../frontend/src/modes/chat/styles/workbench.css)，不按设备品牌或 User-Agent 分支。

| 范围 | 当前布局 |
| --- | --- |
| 视口 > 760px | 56px 纵向模式栏；桌面输入工具栏 |
| 视口 ≤ 760px | 对话、任务、目标、工作区、更多五项底栏；编排、配置保护、设置、主题在“更多”中 |
| 模式容器 ≤ 760px | 对话列表改抽屉，宽 `min(88cqw, 300px)`，限制在模式容器中 |
| 模式容器 ≤ 1050px | 运行详情改有界浮层，不持续挤占对话列 |

模式、列表、正文和面板各自负责滚动，网格子项保持 `min-width:0`、`min-height:0`。长名称省略、路径和文本折行；代码、表格在自己的容器横向滚动，页面不整体溢出。抽屉不遮住一级导航，有遮罩、关闭及 Escape 行为。任务和设置沿用各自布局与返回位置，频道地址合同见 [架构](architecture.md#api与文件)。

三处对话栏头为 48px，主要手机操作保留 44px 触达区域；停止为 32px，既有紧凑文件工具栏须单独验证。辅助折叠行以 `--chat-auxiliary-row-height:26px` 统一，正文、媒体和展开内容按实际高度测量。消息链接位于内容边界内，不遮住标题或展开按钮；代码/差异标题为 32px，折叠时显示前 8 行，展开后正文独立滚动。Latest 可见高度 30px，距消息区底边 4px，不进入输入区。

手机主输入行保留附件、对话设置和发送。模型、推理、审批复用原 select 和状态；执行中默认排队，用户明确选择后才立即补充，待审批/回答时仍以当前交互优先。失败保留草稿和队列，停止保留待发送消息；归档或子会话不能因窄屏恢复发送。

对话菜单、归档列表和任务详情展示服务端计算的保留状态：归档日期、预计日期、保留标记、阻止原因及最近删除失败。设置中的保留策略独立热应用，确认前预览、取消不保存；通过全局 retention 事件或重连刷新使其他窗口重新取得到期状态。预计日期不是执行成功事实，文件未回收与对话已删除分别说明。策略定义见[配置](configuration.md#归档与保留)。

桌面 Enter 发送/排队、Shift+Enter 换行，Ctrl/⌘+Enter 沿用现有行为；手机 Enter 换行，输入法 composing 不触发发送。输入高度上限 144px。输入上下文顺序为待处理交互、队列、错误、运行反馈/停止、计划、输入；上下文内部滚动，上限为可视高度的 34% 且不超过 280px，展开计划自身最多 160px。布局变化仍交给现有视口 owner。

AppShell 常规高度为 `100dvh`。手机且 `visualViewport.scale === 1` 时采用实际可视高度，`innerHeight - visualViewport.height > 120px` 时收起底栏；这是视口收缩事实，不按输入焦点猜键盘。缩放不被视为键盘，监听随卸载清理。桌面模拟不能代替 Android/iOS 真机软键盘、输入法和安全区验收。

## 摘要、预览与阅读状态

摘要统一使用原生 `popover="auto"`：桌面锚定触发入口，最大宽 360px、高 520px，与边界留 12px；手机距底部和两侧 12px，高度受实际可视视口限制。标题和关闭入口保留、正文内滚动，Escape/关闭返回可见触发入口。它是非模态查看入口；新建、分支、配置保护和未保存确认继续使用 dialog。

聊天通过 `data-summary-boundary` 声明摘要边界，公共组件不查找聊天类名；边界变化只触发弹层定位，不参与消息测量。原生 select 的 picker 与回退均须实际打开并点击验证，不能只检查 z-index 或调用 `selectOption()`。

助手摘要使用当前对话和已生效配置，不能以助手默认值覆盖历史参数或把待应用值当作已生效。原生工具计数、MCP 登记/最近验证/当前连接、Core `filesAvailable` 分别展示；未知计数不能填 0，读取失败不沿用旧成功结论。分类保护在后端返回前再次核对，锁定或权限未知时隐藏受保护内容，变化后清空；摘要不暴露密码、Token、上游 URL、完整指令或原始错误。关闭摘要只取消它自己的请求。

图片和文件预览复用 `WorkbenchDialog`，图片完整按比例展示，文本区内部滚动，标题、关闭与未保存确认始终可达。基础尺寸来自 `dialog-tokens.css`，不为普通表单叠加预览样式。

企业文件由 [state.ts](../frontend/src/shared/enterprise-workspace/state.ts) 统一管理，最多 12 个标签和 24 MiB 文本预算，未保存草稿不淘汰。只驻留当前和最近一个查看器，其他标签保存轻量阅读状态：源码/换行/选择/滚动，HTML/Markdown 内部滚动，CSV 筛选/分页，PDF 页码/缩放/查询，图片缩放/平移及媒体播放位置。

阅读状态只在当前页面内存保留，关闭/淘汰标签、退出账号或更换空间时释放，刷新不恢复。后台文件停止图片读取、PDF 渲染/查找和媒体连接，返回后由用户继续播放；未修改的正文、预览准备和图片缓存可复用，文件版本变化后失效。专注模式保留退出入口；手机仅在实际打开文件且专注时隐藏底栏，退出或回到列表后恢复。

## 来源与依赖补丁

前端来源为 [topabomb/demo1 的固定提交](https://github.com/topabomb/demo1/tree/34d103e496ddddeb0826130d6ef9f8a93d21a099)，后续产品修改由本仓库维护，来源项目的发布流程不属于 Tuck 的发布合同。

[virtua@0.50.3.patch](../frontend/patches/virtua@0.50.3.patch) 修复 Vue Virtualizer 测量补偿后的过期滚动偏移：写入 DOM 后立即把浏览器实际钳制的偏移通过 `ACTION_SCROLL` 同步到 store，防止同一原生 scroll 事件前多轮 ResizeObserver/Vue post-flush 覆盖补偿。补丁作用于本产品导入的 `lib/vue/index.js`；上游对应 `src/core/driver.ts` 的 `createElementDriver` 补偿回调。

pnpm 在 [pnpm-workspace.yaml](../frontend/pnpm-workspace.yaml) 登记补丁、在 lockfile 锁定哈希，Docker 在冻结安装前复制它，`.gitattributes` 固定补丁为 LF。升级 Virtua 时核对上游是否已同步相同事实，补齐后移除补丁，不追加定时器或 UI 滚轮补偿。[scroll-ownership.spec.ts](../frontend/e2e/scroll-ownership.spec.ts) 验证测量期间真实可见行的滚轮位移、恢复点、切换、宽度和缩放变化，不能只核对最终滚动条位置。
