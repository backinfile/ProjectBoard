# 功能与测试目录

以当前正式源代码为基准。功能清单、测试编号、步骤和预期结果均可复跑核对。接口黑盒只访问部署进程的公开 HTTP/MCP；浏览器黑盒使用真实 Edge；白盒检查内部函数、状态和存储。冻结预览测试仅用于设计基准回归，不计入正式后端/浏览器覆盖。

## 全部功能清单

| 编号 | 功能范围 | 对应验证 |
| --- | --- | --- |
| F01 | 本地单进程启动、内嵌静态资源、随机/指定端口、中文数据目录、版本信息、重复启动、端口冲突、数据目录互斥、正常停止 | B001 B049 B050；独立运行脚本 7 项 |
| F02 | 首次令牌初始化、单管理员、密码长度、登录、登出、修改密码、会话失效、命令行重设密码 | B002–B006 B047 B048；I 集成测试；M01 |
| F03 | 来源/Host/CSRF 防护、Cookie、HTTPS、MCP 令牌隔离 | B006 B011 B046；I HTTPS 测试；M02 |
| F04 | 项目创建、名称、简称、简介、本地目录、项目切换、概览统计 | B007 B008 B013；U002；W009–W011 |
| F05 | 项目归档、恢复；归档项目从工作区和搜索隐藏 | B009；U022；P 冻结基准回归 |
| F06 | 我的工作、按项目筛选、进行中/今日到期/逾期统计 | U020；P 冻结基准回归 |
| F07 | 新任务标题、自动编号、默认创建状态、其余字段按需添加 | B014；U003 U018；W030–W035 W066 |
| F08 | 任务标题编辑、独立页面、直接链接、刷新、返回项目、复制链接 | B049；U003 U015 U025；M03 |
| F09 | 任务详细固定在评论上方；默认阅读、编辑、取消、保存 | B030；U004 U005；V 前端契约 |
| F10 | 任务看板、卡片摘要、拖拽状态、手动上移下移 | B017；U011 U024；P 冻结基准回归 |
| F11 | 任务列表、动态属性列、关键词/状态/优先级筛选、排序 | B017；U011；P 冻结基准回归 |
| F12 | 月度时间线、开始/结束条、截止标记、跨月、待安排、月份导航 | U011；P 冻结基准回归 |
| F13 | 固定创建状态、自定义状态、编辑/排序、删除并迁移任务 | B008 B009 B017；U024；W012–W016 |
| F14 | 优先级：低、中、高、紧急、立即处理；可清空 | B015 B016；W035；P 冻结基准回归 |
| F15 | 开始/截止/结束日期、结束不早于开始、日期清空 | B015 B016；W036–W038 |
| F16 | 进度 0–100、空值与 0 区分；颜色 #RRGGBB | B015 B016；U018；W039–W044 W069–W074 |
| F17 | 提醒时间、提醒中心、一次投递、已读、打开对应任务、桌面提醒授权 | B037；U020；W045；M04 |
| F18 | 每日/每周/每月重复；完成后继任务、月底处理、只生成一次 | B038；I 重复任务与提醒集成测试 |
| F19 | 检查清单新增/完成/删除/清空，完成数量显示 | B019；W055；P 冻结基准回归 |
| F20 | 任务关联：相关、阻塞、子任务；添加/移除、同项目约束 | B019 B025；W047–W049 |
| F21 | 任务与知识双向导航、添加/移除引用、从知识创建任务 | B021；U019；W046；P 冻结基准回归 |
| F22 | 评论发表、按时间排列、编辑、回复、取消回复、删除/恢复 | B026–B028；U007；W050–W054 |
| F23 | 评论写入知识，编辑写入内容并关联任务 | U019；P 冻结基准回归 |
| F24 | 详细和评论文件上传、待发送文件移除、仅附件评论、附件下载 | B029 B030 B035；U006–U009；W056–W062；M05 |
| F25 | PNG/JPEG/GIF/WebP 图片预览，缩略图、放大/缩小/适应窗口 | B031；U006；I 预览测试；M06 |
| F26 | 普通文本 UTF8/BOM/UTF16LE/BE 预览；HTML/SVG 安全回退 | B032 B036；U008；I 预览测试 |
| F27 | Markdown 排版、表格、代码、任务清单、源码切换、危险 HTML/链接抑制 | B033；U008；I 预览测试 |
| F28 | 文本 1MB 截断、中文边界、完整下载；单文件20MB/区域8个/总256MB限制 | B034 B035；W056–W062；M07 |
| F29 | 内部知识树：根与子节点均有内容，展开收起、点分地址定位 | B020；U012；W017–W024 W064 |
| F30 | 知识节点改名、移动、同级排序、置顶；稳定引用 | B021 B022；U013；W064；P 冻结基准回归 |
| F31 | 知识内容自动保存、草稿恢复、切页保存、手动快照、历史恢复 | U012 U013 U019；V 前端契约；P 冻结基准回归；M08 |
| F32 | 知识节点互相关联、解绑、复制完整地址和子树内容 | B021；W025 W026；P 冻结基准回归；M09 |
| F33 | 项目独立词表，任务和节点共享标签；名称规范化去重 | B023 B025；U018；W063；V 前端契约 |
| F34 | 标签 AND/OR、类型和关键词筛选、全选结果、批量增删标签 | B023–B025；U018；P 冻结基准回归 |
| F35 | 标签独立创建、改名、显式合并、删除并保留内容；覆盖回收站 | B024 B039；I 标签生命周期 |
| F36 | 按标签批量删除任务/知识，完整子树范围提示、去重 | B040 B041；P 冻结基准回归 |
| F37 | 任务回收站恢复、永久清理；编号不复用、引用清理 | B039；U021 U027；I 单调编号测试 |
| F38 | 知识子树删除、同批恢复、恢复位置/重名处理、永久清理及引用清理 | B040；U026；W020 W024；I 事务测试 |
| F39 | 全局/项目搜索：项目、任务标题/详细、知识地址/内容/标签，打开结果 | B025；U015；P 冻结基准回归 |
| F40 | 浅色/深色主题、侧栏/顶部导航、移动端、键盘快捷键 | B007；U016；M03 M10 |
| F41 | ZIP 完整备份、校验清单、附件字节、恢复前备份、恢复原子性 | B042 B043；I 备份及损坏恢复测试 |
| F42 | 预览 schema2 JSON 导入；旧内嵌附件和空附件兼容 | B044；I 旧格式导入测试 |
| F43 | 任务 CSV/JSON、知识树 JSON 导出 | U014；P 冻结基准回归 |
| F44 | 全局修订号、冲突回滚、草稿保留/导出/载入最新、后台同步 | B010 B018；U010；V 前端契约；I 并发CAS |
| F45 | SQLite 重启持久化、互斥、未来版本保护、附件闲置清理与回收站保留 | B050；W068；I 存储测试 |
| F46 | 运行信息、数据目录提示、管理员停止服务 | B001 B050；独立运行脚本；U 设置页 |
| F47 | MCP 项目令牌创建/列表/撤销，仅显示一次，摘要保存 | B011 B046；I MCP 协议测试 |
| F48 | MCP Streamable HTTP、stdio、初始化/发现/调用，Web共用存储 | B012 B013 B045；U023；I stdio 子进程测试 |
| F49 | MCP 37工具：项目、任务、评论附件、知识树、关联、标签、搜索、删除计划 | B013–B045；下方逐工具映射 |
| F50 | MCP 类型校验、项目范围、分页游标、修订冲突、5分钟删除计划、批量原子性 | B018 B025 B041 B046；W075–W080；I 计划过期测试 |
| F51 | 冻结预览 SHA 校验、内嵌资源构建、发布包、第三方许可证、可复跑测试 | 构建脚本；独立运行脚本；P 冻结基准回归 |

## 接口黑盒条目

脚本：scripts/blackbox.cjs。每轮自动创建新数据目录、随机端口和随机测试密码；正常结束后停止测试进程。每条按编号执行，依赖项失败会使后续相关断言失败，逐条结果保留。

| 编号 | 测试项 | 步骤 | 预期 |
| --- | --- | --- | --- |
| B001 | 本地进程健康与内嵌资源 | 随机端口启动独立程序，读取健康接口、HTML、JS、CSS | 健康版本与 package.json 一致；资源均可读取 |
| B002 | 首次会话与登录保护 | 初始化前读取会话和工作空间 | initialized=false，state 返回 401 |
| B003 | 错误初始化凭据 | 分别提交错误令牌和过短密码 | 请求均拒绝，仍可正确初始化 |
| B004 | 管理员初始化与 Cookie | 使用启动令牌设置密码，检查会话 | 设置成功；Cookie 为 HttpOnly、SameSite=Strict |
| B005 | 初始化仅一次 | 重复提交正确初始化令牌 | 409 拒绝重复初始化 |
| B006 | 请求来源和 CSRF | 错误 Origin、Host 和 CSRF 请求 | 分别返回 403 |
| B007 | 创建项目与设置持久化 | 保存两个不同项目、描述、目录、深色主题 | 重新读取字段完全保留 |
| B008 | 项目简称与状态约束 | 重复简称、删除创建状态、增加待处理状态 | 每次拒绝且整体数据保持 |
| B009 | 项目归档恢复与自定义状态 | 归档后恢复项目，添加自定义状态 | 归档标志与状态持久化 |
| B010 | 修订冲突与原子保存 | 对同一修订提交两次不同内容 | 第二次 409，第一次内容保留 |
| B011 | MCP 项目令牌 | 创建令牌，再读取令牌列表 | 令牌只在创建响应提供 |
| B012 | MCP 协议初始化与工具发现 | 发送 initialize 和 tools/list | 协议返回成功，工具恰为 37 个 |
| B013 | MCP 读取修改项目及状态 | get_project、update_project、list_project_statuses | 字段更改可从 HTTP state 读取 |
| B014 | 任务默认字段 | create_task 仅提供标题，然后 get_task | 创建状态、其他业务标量为空 |
| B015 | 所有任务标量设置与清空 | 逐个设置、读取、null 清空详细/优先级/日期/提醒/重复/颜色/进度 | 值一致，null 清空；0 进度保留 |
| B016 | 任务边界与拒绝后原子性 | 提交进度 -1/101、无效日期/颜色/枚举、反向日期 | 全部拒绝且修订不改变 |
| B017 | 任务状态、排序与列表分页 | move_task 设置自定义状态及排序，list_tasks 分页 | 状态/排序正确，两页对象不同 |
| B018 | 过期分页游标 | 写入后继续使用旧游标 | 拒绝旧游标 |
| B019 | 任务清单与三种任务关联 | 设置检查项及 related/blocks/subtask 关联，清空 | 结构和完成值保留，清空不删除关联任务 |
| B020 | 知识树与点分寻址 | 创建带内容根节点和子节点，按地址查找 | 父子均保留内容，地址等于 技术.开发 |
| B021 | 知识改名移动与稳定引用 | 关联任务和知识，改名根、移动子节点，再解绑 | 地址变化，稳定 ID 引用保持，解绑保留对象 |
| B022 | 知识非法名称与循环 | 点号名称、同级重复、根移到子下、ID/地址不一致 | 各请求拒绝，树完整 |
| B023 | 项目标签创建、批量添加和 AND/OR | 创建重点/技术，标记任务和节点，查询 all/any | AND 只匹配同时含两标签的任务；OR 同时匹配节点 |
| B024 | 标签移除、改名与合并 | 移除任务技术标签，重点改名关键后合并到技术 | 去重、原有对象内容保留 |
| B025 | 跨项目批量操作与搜索范围 | p1 请求含 p2 任务，随后按中文关键词搜索 | 跨项目整批失败，搜索只含 p1 |
| B026 | 评论发表、回复、编辑与列表 | 创建正文及回复，编辑首条评论 | 正文更新，回复 ID 保留，列表两条 |
| B027 | 评论删除与恢复 | 删除首条，再恢复 | 删除隐藏，恢复内容保持 |
| B028 | 空评论及无效回复 | 空正文无附件、自回复或不存在回复 | 各请求拒绝 |
| B029 | MCP 附件上传、评论引用与下载 | Base64 上传 Markdown，附加到评论并下载 | 下载字节和文件元数据一致 |
| B030 | 任务详细附件与下载 | HTTP 上传普通文本，附加到任务详细并读取 | 详细正文与附件持久化，下载字节原样 |
| B031 | 图片预览 | 上传 PNG，读取预览和 inline 字节 | kind=image，image/png，字节一致 |
| B032 | 文本编码与原样显示 | 上传 UTF8/BOM/UTF16LE/UTF16BE 文本 | 解码正文一致，尖括号保持文本 |
| B033 | Markdown 排版和源码安全 | 上传标题/表格/代码/脚本/危险链接 | 渲染包含排版，源码保留，HTML 不含可执行脚本 |
| B034 | 长文本截断与完整下载 | 上传超过 1MB 的中文文本 | 预览截断且 UTF8 完整；下载完整 |
| B035 | 附件范围与非法引用 | 从 p2 请求 p1 附件、伪造附件 ID、同区九附件 | 拒绝读取/保存 |
| B036 | 二进制预览回退 | 上传 ZIP 类型字节，请求预览后下载 | 预览拒绝，下载保持字节 |
| B037 | 提醒一次投递与已读 | 设置过去提醒，读取两次、标记已读 | 同一提醒只一条，已读消失 |
| B038 | 每日/每周/月末重复任务 | 分别完成三种周期任务，重复提交完成 | 日期正确推进，只生成一个后继，重置状态 |
| B039 | 任务删除恢复及标签删除覆盖回收站 | 删除任务、删除标签、恢复任务 | 标签删除覆盖回收站，评论和附件保留 |
| B040 | 知识子树删除计划、提交和恢复 | 预览重复选择的根和子，提交后恢复根 | 去重数量 2，整树删除恢复，内容保持 |
| B041 | 删除计划修订过期 | 生成计划后修改项目，再提交旧计划 | 拒绝，目标保留 |
| B042 | 完整 ZIP 备份与恢复 | 备份，修改正文，恢复 ZIP，再下载附件 | 内容恢复，附件字节保留，会话仍有效 |
| B043 | 损坏备份与旧修订恢复 | 提交损坏 ZIP 和旧修订 ZIP | 损坏 400，旧修订 409，数据保持 |
| B044 | 预览 JSON 导入 | 导入当前 schema2 JSON 的修改副本 | 内容更新，项目和附件保持 |
| B045 | MCP 37 工具均实际调用 | 对成功调用集合与 tools/list 比较 | 每一个已发现工具都存在成功调用 |
| B046 | 令牌撤销与请求头项目范围 | 错误 X-Project-ID，撤销令牌后调用 | 分别 403 和 401 |
| B047 | 错误密码、改密及旧会话失效 | 错误密码登录，正确改密后使用旧 Cookie | 错误登录 401，改密成功，旧会话 401 |
| B048 | 退出登录与重新登录 | 退出后读 state，再登录 | 退出后 401，登录恢复数据 |
| B049 | 非法 JSON、接口方法和深链接 | 发送串接 JSON、POST 静态页，访问独立任务 URL | 400、405，深链接可到应用 |
| B050 | 重启持久化与正常停止 | 保存状态，正常停止并从相同测试目录重启 | 重新登录后工作空间完整一致 |

## 浏览器黑盒条目

脚本：scripts/browser-test.cjs。独立部署；页面操作、文件选择、下载、第二页面同步均使用真实浏览器。页面定位器失败与产品断言失败都记录为 FAIL，修正后重跑。

| 编号 | 测试项 | 步骤 | 预期 |
| --- | --- | --- | --- |
| U001 | 管理员首次设置页面 | 浏览器打开启动链接，设置并确认密码 | 进入项目页 |
| U002 | 创建项目 | 填写名称和简称，保存 | 项目导航出现对应名称 |
| U003 | 创建任务和独立页 | 新建任务仅填写标题，刷新独立 URL | 标题保留，详细阅读态，只显示创建属性 |
| U004 | 任务详细位置和唯一属性入口 | 检查详细和评论位置及属性加号 | 详细在评论上方，仅一个加号入口 |
| U005 | 详细编辑取消与保存 | 输入后取消，再编辑保存 | 取消回阅读态；保存内容持久化 |
| U006 | 详细图片上传和缩放 | 编辑详细，选择图片保存，打开预览放大再适应窗口 | 图片加载、缩放控制有效 |
| U007 | 发表评论、回复和删除恢复 | 发表正文，回复，再删除恢复第一条 | 回复引用可见，恢复后正文保留 |
| U008 | 评论文件上传和 Markdown/源码/文本预览 | 评论上传 md 和 txt，逐个打开并切换源码 | Markdown 渲染表格，源码显示标签，文本原样显示 |
| U009 | 附件下载字节 | 点击普通文本下载入口 | 本地下载内容与上传一致 |
| U010 | 浏览器多页面同步 | 第二页面打开同任务，第一页修改详细 | 第二页在轮询后显示新内容 |
| U011 | 看板、列表、时间线和月份导航 | 返回任务面板，切换三种视图，前后月和本月 | 任务可从三种视图打开，未排期任务在待安排 |
| U012 | 知识创建、地址定位和内容自动保存 | 创建根节点及子节点，输入内容后切换页面 | 点分地址定位子节点，重新打开内容保留 |
| U013 | 知识节点历史与置顶 | 修改节点内容，保存后查看历史，切换置顶 | 历史含旧内容，置顶可取消 |
| U014 | 任务和知识导出文件 | 分别下载 CSV、任务 JSON 和知识 JSON | 格式与当前项目数据一致 |
| U015 | 全局搜索并打开任务 | 搜索任务标题，点击结果 | 进入对应独立任务页面 |
| U016 | 主题保存和移动布局 | 设置深色后刷新，缩至 390px，打开任务 | 深色保留，移动视口可操作任务 |
| U017 | 脚本异常检查 | 汇总本轮页面未捕获异常 | 无未捕获异常 |
| U018 | 属性添加、清空与标签筛选 | 添加 0% 进度和标签，清空进度，按标签搜索 | 0% 可见；清空隐藏；标签结果含任务 |
| U019 | 评论写入知识与手动历史恢复 | 将评论写入节点，保存历史，再修改和恢复 | 评论出现在节点，恢复历史正文 |
| U020 | 我的工作筛选与提醒中心 | 打开我的工作，筛选项目，再打开提醒 | 任务保留在选定项目；提醒窗口可访问 |
| U021 | 任务回收站恢复 | 删除任务，到回收站恢复并打开 | 详细、评论、标签保持 |
| U022 | 项目归档与恢复 | 项目设置归档，再到设置恢复 | 归档后侧栏隐藏，恢复后出现 |
| U023 | MCP 页面真实读取 | 打开 MCP 页运行默认 list_tasks | 返回包含当前任务的 JSON |
| U024 | 看板拖动、自定义状态与删除迁移 | 新增验证状态，拖入任务，删除状态迁移到进行中 | 拖动和迁移后状态持久化 |
| U025 | 复制独立任务链接 | 点击复制任务链接，读取系统剪贴板 | 复制值与独立 URL 相同 |
| U026 | 知识子树回收站恢复与永久删除 | 创建临时根节点，删除恢复，再删除并永久清理 | 恢复保留节点；永久清理后从存储消失 |
| U027 | 任务永久删除 | 创建临时任务，删除后从回收站永久清理 | 任务消失，原任务保留 |

## 白盒领域与分支条目

W001–W062 从有效的两项目/三任务/三节点夹具开始，单独修改标题中指明的字段；每项调用 Validate。其余项直接调用模型辅助函数、patch 和工具参数校验。每条为独立 Go 子测试。

| 编号 | 输入/分支 | 预期 | 脚本 |
| --- | --- | --- | --- |
| W001 | 有效完整结构 | Validate 成功；默认值/边界值保持 | internal/domain/model_test.go |
| W002 | 结构版本拒绝 | Validate 返回错误 | internal/domain/model_test.go |
| W003 | 主题枚举拒绝 | Validate 返回错误 | internal/domain/model_test.go |
| W004 | 项目上限 | Validate 返回错误 | internal/domain/model_test.go |
| W005 | 任务上限 | Validate 返回错误 | internal/domain/model_test.go |
| W006 | 节点上限 | Validate 返回错误 | internal/domain/model_test.go |
| W007 | 跨实体重复ID | Validate 返回错误 | internal/domain/model_test.go |
| W008 | 非法ID | Validate 返回错误 | internal/domain/model_test.go |
| W009 | 空项目名 | Validate 返回错误 | internal/domain/model_test.go |
| W010 | 项目名长度 | Validate 返回错误 | internal/domain/model_test.go |
| W011 | 简称忽略大小写重复 | Validate 返回错误 | internal/domain/model_test.go |
| W012 | 状态ID重复 | Validate 返回错误 | internal/domain/model_test.go |
| W013 | 状态种类非法 | Validate 返回错误 | internal/domain/model_test.go |
| W014 | 待处理状态拒绝 | Validate 返回错误 | internal/domain/model_test.go |
| W015 | 创建状态名称保护 | Validate 返回错误 | internal/domain/model_test.go |
| W016 | 创建状态种类保护 | Validate 返回错误 | internal/domain/model_test.go |
| W017 | 节点名称点号 | Validate 返回错误 | internal/domain/model_test.go |
| W018 | 节点控制字符 | Validate 返回错误 | internal/domain/model_test.go |
| W019 | 同级大小写碰撞 | Validate 返回错误 | internal/domain/model_test.go |
| W020 | 回收站同名允许 | Validate 成功；默认值/边界值保持 | internal/domain/model_test.go |
| W021 | 跨项目父节点 | Validate 返回错误 | internal/domain/model_test.go |
| W022 | 缺失父节点 | Validate 返回错误 | internal/domain/model_test.go |
| W023 | 循环父节点 | Validate 返回错误 | internal/domain/model_test.go |
| W024 | 活动子节点已删父 | Validate 返回错误 | internal/domain/model_test.go |
| W025 | 节点自关联 | Validate 返回错误 | internal/domain/model_test.go |
| W026 | 节点跨项目关联 | Validate 返回错误 | internal/domain/model_test.go |
| W027 | 节点内容4MB边界 | Validate 成功；默认值/边界值保持 | internal/domain/model_test.go |
| W028 | 节点内容超4MB | Validate 返回错误 | internal/domain/model_test.go |
| W029 | 历史ID非法 | Validate 返回错误 | internal/domain/model_test.go |
| W030 | 空任务标题 | Validate 返回错误 | internal/domain/model_test.go |
| W031 | 任务标题长度 | Validate 返回错误 | internal/domain/model_test.go |
| W032 | 任务编号零 | Validate 返回错误 | internal/domain/model_test.go |
| W033 | 项目内编号重复 | Validate 返回错误 | internal/domain/model_test.go |
| W034 | 空状态默认创建 | Validate 成功；默认值/边界值保持 | internal/domain/model_test.go |
| W035 | 无效优先级 | Validate 返回错误 | internal/domain/model_test.go |
| W036 | 无效日期 | Validate 返回错误 | internal/domain/model_test.go |
| W037 | 闰年日期 | Validate 成功；默认值/边界值保持 | internal/domain/model_test.go |
| W038 | 结束早于开始 | Validate 返回错误 | internal/domain/model_test.go |
| W039 | 进度负数 | Validate 返回错误 | internal/domain/model_test.go |
| W040 | 进度零 | Validate 成功；默认值/边界值保持 | internal/domain/model_test.go |
| W041 | 进度100 | Validate 成功；默认值/边界值保持 | internal/domain/model_test.go |
| W042 | 进度101 | Validate 返回错误 | internal/domain/model_test.go |
| W043 | 重复周期非法 | Validate 返回错误 | internal/domain/model_test.go |
| W044 | 颜色非法 | Validate 返回错误 | internal/domain/model_test.go |
| W045 | 提醒格式非法 | Validate 返回错误 | internal/domain/model_test.go |
| W046 | 任务跨项目知识 | Validate 返回错误 | internal/domain/model_test.go |
| W047 | 任务自关联 | Validate 返回错误 | internal/domain/model_test.go |
| W048 | 任务跨项目关联 | Validate 返回错误 | internal/domain/model_test.go |
| W049 | 关联类型非法 | Validate 返回错误 | internal/domain/model_test.go |
| W050 | 空评论 | Validate 返回错误 | internal/domain/model_test.go |
| W051 | 重复评论ID | Validate 返回错误 | internal/domain/model_test.go |
| W052 | 评论自回复 | Validate 返回错误 | internal/domain/model_test.go |
| W053 | 回复目标缺失 | Validate 返回错误 | internal/domain/model_test.go |
| W054 | 评论内容超限 | Validate 返回错误 | internal/domain/model_test.go |
| W055 | 空检查项 | Validate 返回错误 | internal/domain/model_test.go |
| W056 | 附件负大小 | Validate 返回错误 | internal/domain/model_test.go |
| W057 | 附件20MB边界 | Validate 成功；默认值/边界值保持 | internal/domain/model_test.go |
| W058 | 附件超20MB | Validate 返回错误 | internal/domain/model_test.go |
| W059 | 附件重名ID | Validate 返回错误 | internal/domain/model_test.go |
| W060 | 区域九附件 | Validate 返回错误 | internal/domain/model_test.go |
| W061 | 附件文件名控制字符 | Validate 返回错误 | internal/domain/model_test.go |
| W062 | 仅附件评论允许 | Validate 成功；默认值/边界值保持 | internal/domain/model_test.go |
| W063 | NFC标签去重 | 规范化为 NFC 并去重，保留顺序 | internal/domain/model_test.go |
| W064 | 地址和子树稳定ID | 改名更新点分地址，子树稳定 ID 集合正确 | internal/domain/model_test.go |
| W065 | 克隆隔离 | 修改克隆不会影响原对象 | internal/domain/model_test.go |
| W066 | 默认字段空集合 | 默认创建、空标量和非 nil 集合 | internal/domain/model_test.go |
| W067 | ID生成格式及样本唯一 | 1000 个 ID 格式有效且样本无重复 | internal/domain/model_test.go |
| W068 | 回收站附件引用收集 | 已删除任务/评论附件仍被收集 | internal/domain/model_test.go |
| W069 | 补丁省略保留 | 省略字段保留原值 | internal/server/whitebox_test.go |
| W070 | 补丁null清空 | null 清空字符串和指针 | internal/server/whitebox_test.go |
| W071 | 补丁零值保留 | 显式 0 保留 | internal/server/whitebox_test.go |
| W072 | 补丁未知字段拒绝 | 未知字段拒绝且对象不变 | internal/server/whitebox_test.go |
| W073 | 补丁错误类型拒绝 | 错误类型拒绝且对象不变 | internal/server/whitebox_test.go |
| W074 | 补丁小数进度拒绝 | 小数进度拒绝且对象不变 | internal/server/whitebox_test.go |
| W075 | 工具未知参数 | 工具参数类型校验返回错误 | internal/server/whitebox_test.go |
| W076 | 工具标签类型 | 工具参数类型校验返回错误 | internal/server/whitebox_test.go |
| W077 | 工具字段对象类型 | 工具参数类型校验返回错误 | internal/server/whitebox_test.go |
| W078 | 工具版本类型 | 工具参数类型校验返回错误 | internal/server/whitebox_test.go |
| W079 | 工具合并布尔类型 | 工具参数类型校验返回错误 | internal/server/whitebox_test.go |
| W080 | 工具父节点类型 | 工具参数类型校验返回错误 | internal/server/whitebox_test.go |

## 白盒集成测试条目

这些测试读取/修改内部存储、计划或会话状态，并在临时目录运行。一个集成测试内有多条相关断言，报告按 Go 顶层测试计数。

| 编号 | 测试函数 | 检查点 | 脚本 |
| --- | --- | --- | --- |
| I01 | TestStdioRoundTrip | 官方 SDK 通过真实 stdio 子进程读写，与 HTTP 数据一致 | cmd/projectboard/main_test.go |
| I02 | TestTaskFieldCommentAndLabelLifecycle | 任务字段 null/0、评论附件生命周期、标签合并冲突和删除完整性 | internal/server/contracts_test.go |
| I03 | TestDeletePlanExpiryPaginationAndAtomicRestore | 把计划时间设为过期；过期游标拒绝；损坏恢复回滚；旧空附件导入 | internal/server/contracts_test.go |
| I04 | TestPerformanceFixture | 构造 10000 任务与 2000 节点，执行中文搜索并记录耗时 | internal/server/contracts_test.go |
| I05 | TestAttachmentPreviewFormatsAndSafety | Markdown 表格/强调/脚本/危险链接、纯文本、UTF16、图片、二进制回退 | internal/server/preview_test.go |
| I06 | TestAuthenticationPersistenceAndConflict | 会话、CAS冲突、CSRF/Host、保存后的内部状态检查 | internal/server/server_test.go |
| I07 | TestKnowledgeAndLabelsAreTransactional | 直接调用服务层验证稳定引用、树循环、跨项目批量原子性 | internal/server/server_test.go |
| I08 | TestAttachmentsBackupRestoreAndReminder | 检查附件上传/归属/校验、ZIP备份与恢复字节、重复后继和提醒；错误恢复保持现有存储 | internal/server/server_test.go |
| I09 | TestMCPProtocolTokenIsolationAndRevocation | 官方SDK初始化与工具调用、绑定项目拒绝越界、撤销令牌后拒绝协议请求 | internal/server/server_test.go |
| I10 | TestHTTPSCookiesAndOrigin | TLS测试服务器检查Secure/HttpOnly/SameSite Cookie，拒绝非同源或错误协议来源 | internal/server/server_test.go |
| I11 | TestConcurrentCompareAndSwap | 16 个并发请求使用同一修订：恰 1 成功、15 冲突，修订只加 1 | internal/store/store_test.go |
| I12 | TestRestartExclusiveDirectoryAndMonotonicNumbers | 同目录互斥；任务清理后编号递增；重启保留数据；旧修订拒绝 | internal/store/store_test.go |
| I13 | TestCleanupRetainsReferencedAttachments | 直接老化附件时间，清理仅删除闲置文件，回收站附件仍可读 | internal/store/store_test.go |
| I14 | TestFutureDatabaseVersionIsRejected | 写入 user_version=99，再次打开必须拒绝 | internal/store/store_test.go |

## MCP 每个工具的黑盒覆盖

全部通过真正的 /api/mcp tools/call 调用；B045 比较成功调用集合与服务 tools/list，发现工具增加而脚本未覆盖时失败。

| 工具 | 调用条目 |
| --- | --- |
| attach_labels | B023、B025 |
| commit_delete | B040、B041 |
| create_knowledge_node | B020、B022 |
| create_label | B023 |
| create_task | B014、B038 |
| create_task_comment | B026、B028、B029 |
| delete_label | B039 |
| delete_task | B039 |
| delete_task_comment | B027 |
| detach_labels | B024 |
| get_comment_attachment | B029 |
| get_knowledge_node | B020、B021、B022 |
| get_knowledge_subtree | B020、B022、B040 |
| get_project | B013、B025 |
| get_task | B014、B015、B017、B019、B021、B024、B025、B039、B041、B042 |
| link_knowledge_nodes | B021 |
| link_task_knowledge | B021 |
| list_knowledge_nodes | B020、B040 |
| list_labels | B023 |
| list_project_statuses | B013 |
| list_task_comments | B026、B027 |
| list_tasks | B017、B018 |
| move_knowledge_node | B021、B022 |
| move_task | B017、B038 |
| preview_delete | B040、B041 |
| rename_label | B024 |
| restore_knowledge_node | B040 |
| restore_task | B039 |
| restore_task_comment | B027 |
| search | B023、B025 |
| unlink_knowledge_nodes | B021 |
| unlink_task_knowledge | B021 |
| update_knowledge_node | B021 |
| update_project | B013、B041 |
| update_task | B015、B016、B017、B019、B030、B037、B042 |
| update_task_comment | B026 |
| upload_comment_attachment | B029 |

## 需要人工或专项环境的黑盒条目

以下明确保留为未执行，不能据自动化通过推断这些场景已通过。

| 编号 | 场景 | 步骤与预期检查 | 状态 |
| --- | --- | --- | --- |
| M01 | 真实终端密码重设 | 停止专用实例，执行 --reset-password，隐藏输入新密码，再登录并检查旧会话失效 | 待人工执行；自动测试覆盖 ResetPassword 服务逻辑 |
| M02 | 真实证书部署与局域网多机器访问 | 指定证书与自定义 Host，在第二台机器访问并检查证书链 | 待外部环境；自动测试覆盖 TLS 测试服务器、Cookie 和来源 |
| M03 | 浏览器历史/快捷键全键盘遍历 | 前进后退、Ctrl+K、Enter、Escape、Tab 遍历全部交互 | 待补充专门无障碍回归；已自动测独立链接/刷新 |
| M04 | 操作系统桌面通知 | 允许/拒绝权限，服务到期后检查真实系统通知和点击 | 待人工执行；自动测试覆盖提醒中心、投递和已读 |
| M05 | 系统拖文件/剪贴板图片 | 从资源管理器拖图到详细/评论、粘贴剪贴板图片 | 待人工执行；文件选择上传已自动验证 |
| M06 | 图片格式视觉矩阵 | 分别上传 JPEG/GIF动画/WebP及大图，检查方向、动画、缩放 | 待补充视觉矩阵；当前浏览器 PNG 已通过 |
| M07 | 真实总配额和磁盘耗尽 | 真实累计上传到 256MB，验证满额行为；隔离磁盘写满后验证恢复 | 待隔离磁盘环境；单附件/区域边界由模型测试覆盖 |
| M08 | 崩溃/断电/长时间离线草稿 | 输入知识内容后终止浏览器/服务，重启检查草稿恢复和已提交数据 | 待故障注入；自动测试覆盖正常重启、冲突草稿、后台同步 |
| M09 | 知识复制地址/子树剪贴板 | 复制地址和子树，检查子树顺序、内容、标签 | 待补充浏览器剪贴板测试；任务链接复制已自动验证 |
| M10 | 多浏览器和视觉无障碍 | Chrome/Firefox/Safari、200%缩放、屏幕阅读器、触屏拖动 | 待相应设备；本轮为 Windows Edge 桌面和390px视口 |

## 执行方法

```powershell
npm ci
node scripts/test-all.cjs
```

需要 Go 1.26+、Node 22+、本机 Edge。仅测试使用 Playwright，发布程序运行仍不依赖 Node。可设置 PB_BROWSER_CHANNEL=chrome 使用已安装 Chrome。默认输出 test-output/full，PB_TEST_OUTPUT 可指定独立报告目录。全部进程使用测试目录，保留正常管理员目录和已运行实例。

单独运行：`node scripts/blackbox.cjs dist/projectboard-test.exe`、`node scripts/browser-test.cjs dist/projectboard-test.exe`；`--list` 只输出条目，不启动部署。

结果分层：B/U 为正式部署黑盒；W/I 为 Go 白盒；V 为正式前端 VM 契约；P 为冻结预览 VM 基准。覆盖率为 Go 语句覆盖率，不等于功能或分支全部覆盖。
