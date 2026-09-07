# 本地测试执行报告

执行时间：2026-09-07T01:40:28.020Z。环境：Windows x64，Node v24.14.0，Edge headless；独立本地数据目录和随机回环端口。

功能与全部条目见 [测试目录](test-catalog.md)。

| 测试层 | 通过/总项 | 说明 |
| --- | --- | --- |
| 接口黑盒 | 50/50 | 真实部署 HTTP/MCP |
| 浏览器黑盒 | 27/27 | Edge 真实页面 |
| 新增白盒子测试 | 80/80 | 独立命名子测试 |
| 原有及新增集成测试 | 14/14 | 不含 stdio 辅助进程入口 |
| 正式前端 VM | 7/7 | 状态/异步契约 |
| 冻结预览 VM | 41/41 | 设计基准 |
| 独立运行 | 7/7 | 须结合 standalone 步骤退出码 |
| 专项人工条目 | 0/10 | 未执行，详见目录 |

## 每条黑盒结果

| 编号 | 测试项 | 结果 | 耗时ms | 错误 |
| --- | --- | --- | --- | --- |
| B001 | 本地进程健康与内嵌资源 | PASS | 140 |  |
| B002 | 首次会话与登录保护 | PASS | 2 |  |
| B003 | 错误初始化凭据 | PASS | 4 |  |
| B004 | 管理员初始化与 Cookie | PASS | 164 |  |
| B005 | 初始化仅一次 | PASS | 157 |  |
| B006 | 请求来源和 CSRF | PASS | 6 |  |
| B007 | 创建项目与设置持久化 | PASS | 3 |  |
| B008 | 项目简称与状态约束 | PASS | 5 |  |
| B009 | 项目归档恢复与自定义状态 | PASS | 2 |  |
| B010 | 修订冲突与原子保存 | PASS | 3 |  |
| B011 | MCP 项目令牌 | PASS | 1 |  |
| B012 | MCP 协议初始化与工具发现 | PASS | 10 |  |
| B013 | MCP 读取修改项目及状态 | PASS | 14 |  |
| B014 | 任务默认字段 | PASS | 14 |  |
| B015 | 所有任务标量设置与清空 | PASS | 184 |  |
| B016 | 任务边界与拒绝后原子性 | PASS | 40 |  |
| B017 | 任务状态、排序与列表分页 | PASS | 34 |  |
| B018 | 过期分页游标 | PASS | 5 |  |
| B019 | 任务清单与三种任务关联 | PASS | 28 |  |
| B020 | 知识树与点分寻址 | PASS | 26 |  |
| B021 | 知识改名移动与稳定引用 | PASS | 59 |  |
| B022 | 知识非法名称与循环 | PASS | 25 |  |
| B023 | 项目标签创建、批量添加和 AND/OR | PASS | 35 |  |
| B024 | 标签移除、改名与合并 | PASS | 26 |  |
| B025 | 跨项目批量操作与搜索范围 | PASS | 21 |  |
| B026 | 评论发表、回复、编辑与列表 | PASS | 21 |  |
| B027 | 评论删除与恢复 | PASS | 19 |  |
| B028 | 空评论及无效回复 | PASS | 10 |  |
| B029 | MCP 附件上传、评论引用与下载 | PASS | 16 |  |
| B030 | 任务详细附件与下载 | PASS | 9 |  |
| B031 | 图片预览 | PASS | 3 |  |
| B032 | 文本编码与原样显示 | PASS | 8 |  |
| B033 | Markdown 排版和源码安全 | PASS | 2 |  |
| B034 | 长文本截断与完整下载 | PASS | 20 |  |
| B035 | 附件范围与非法引用 | PASS | 5 |  |
| B036 | 二进制预览回退 | PASS | 2 |  |
| B037 | 提醒一次投递与已读 | PASS | 9 |  |
| B038 | 每日/每周/月末重复任务 | PASS | 50 |  |
| B039 | 任务删除恢复及标签删除覆盖回收站 | PASS | 21 |  |
| B040 | 知识子树删除计划、提交和恢复 | PASS | 24 |  |
| B041 | 删除计划修订过期 | PASS | 20 |  |
| B042 | 完整 ZIP 备份与恢复 | PASS | 22 |  |
| B043 | 损坏备份与旧修订恢复 | PASS | 2 |  |
| B044 | 预览 JSON 导入 | PASS | 6 |  |
| B045 | MCP 37 工具均实际调用 | PASS | 5 |  |
| B046 | 令牌撤销与请求头项目范围 | PASS | 2 |  |
| B047 | 错误密码、改密及旧会话失效 | PASS | 632 |  |
| B048 | 退出登录与重新登录 | PASS | 164 |  |
| B049 | 非法 JSON、接口方法和深链接 | PASS | 3 |  |
| B050 | 重启持久化与正常停止 | PASS | 316 |  |
| U001 | 管理员首次设置页面 | PASS | 442 |  |
| U002 | 创建项目 | PASS | 111 |  |
| U003 | 创建任务和独立页 | PASS | 157 |  |
| U004 | 任务详细位置和唯一属性入口 | PASS | 20 |  |
| U005 | 详细编辑取消与保存 | PASS | 128 |  |
| U006 | 详细图片上传和缩放 | PASS | 177 |  |
| U007 | 发表评论、回复和删除恢复 | PASS | 231 |  |
| U008 | 评论文件上传和 Markdown/源码/文本预览 | PASS | 158 |  |
| U009 | 附件下载字节 | PASS | 246 |  |
| U010 | 浏览器多页面同步 | PASS | 5604 |  |
| U011 | 看板、列表、时间线和月份导航 | PASS | 205 |  |
| U012 | 知识创建、地址定位和内容自动保存 | PASS | 357 |  |
| U013 | 知识节点历史与置顶 | PASS | 185 |  |
| U014 | 任务和知识导出文件 | PASS | 281 |  |
| U015 | 全局搜索并打开任务 | PASS | 67 |  |
| U016 | 主题保存和移动布局 | PASS | 133 |  |
| U017 | 脚本异常检查 | PASS | 0 |  |
| U018 | 属性添加、清空与标签筛选 | PASS | 301 |  |
| U019 | 评论写入知识与手动历史恢复 | PASS | 360 |  |
| U020 | 我的工作筛选与提醒中心 | PASS | 72 |  |
| U021 | 任务回收站恢复 | PASS | 132 |  |
| U022 | 项目归档与恢复 | PASS | 147 |  |
| U023 | MCP 页面真实读取 | PASS | 71 |  |
| U024 | 看板拖动、自定义状态与删除迁移 | PASS | 319 |  |
| U025 | 复制独立任务链接 | PASS | 32 |  |
| U026 | 知识子树回收站恢复与永久删除 | PASS | 428 |  |
| U027 | 任务永久删除 | PASS | 271 |  |

## 每条白盒结果

| 编号 | 测试项 | 结果 |
| --- | --- | --- |
| W001 | 有效完整结构 | PASS |
| W002 | 结构版本拒绝 | PASS |
| W003 | 主题枚举拒绝 | PASS |
| W004 | 项目上限 | PASS |
| W005 | 任务上限 | PASS |
| W006 | 节点上限 | PASS |
| W007 | 跨实体重复ID | PASS |
| W008 | 非法ID | PASS |
| W009 | 空项目名 | PASS |
| W010 | 项目名长度 | PASS |
| W011 | 简称忽略大小写重复 | PASS |
| W012 | 状态ID重复 | PASS |
| W013 | 状态种类非法 | PASS |
| W014 | 待处理状态拒绝 | PASS |
| W015 | 创建状态名称保护 | PASS |
| W016 | 创建状态种类保护 | PASS |
| W017 | 节点名称点号 | PASS |
| W018 | 节点控制字符 | PASS |
| W019 | 同级大小写碰撞 | PASS |
| W020 | 回收站同名允许 | PASS |
| W021 | 跨项目父节点 | PASS |
| W022 | 缺失父节点 | PASS |
| W023 | 循环父节点 | PASS |
| W024 | 活动子节点已删父 | PASS |
| W025 | 节点自关联 | PASS |
| W026 | 节点跨项目关联 | PASS |
| W027 | 节点内容4MB边界 | PASS |
| W028 | 节点内容超4MB | PASS |
| W029 | 历史ID非法 | PASS |
| W030 | 空任务标题 | PASS |
| W031 | 任务标题长度 | PASS |
| W032 | 任务编号零 | PASS |
| W033 | 项目内编号重复 | PASS |
| W034 | 空状态默认创建 | PASS |
| W035 | 无效优先级 | PASS |
| W036 | 无效日期 | PASS |
| W037 | 闰年日期 | PASS |
| W038 | 结束早于开始 | PASS |
| W039 | 进度负数 | PASS |
| W040 | 进度零 | PASS |
| W041 | 进度100 | PASS |
| W042 | 进度101 | PASS |
| W043 | 重复周期非法 | PASS |
| W044 | 颜色非法 | PASS |
| W045 | 提醒格式非法 | PASS |
| W046 | 任务跨项目知识 | PASS |
| W047 | 任务自关联 | PASS |
| W048 | 任务跨项目关联 | PASS |
| W049 | 关联类型非法 | PASS |
| W050 | 空评论 | PASS |
| W051 | 重复评论ID | PASS |
| W052 | 评论自回复 | PASS |
| W053 | 回复目标缺失 | PASS |
| W054 | 评论内容超限 | PASS |
| W055 | 空检查项 | PASS |
| W056 | 附件负大小 | PASS |
| W057 | 附件20MB边界 | PASS |
| W058 | 附件超20MB | PASS |
| W059 | 附件重名ID | PASS |
| W060 | 区域九附件 | PASS |
| W061 | 附件文件名控制字符 | PASS |
| W062 | 仅附件评论允许 | PASS |
| W063 | NFC标签去重 | PASS |
| W064 | 地址和子树稳定ID | PASS |
| W065 | 克隆隔离 | PASS |
| W066 | 默认字段空集合 | PASS |
| W067 | ID生成格式及样本唯一 | PASS |
| W068 | 回收站附件引用收集 | PASS |
| W069 | 补丁省略保留 | PASS |
| W070 | 补丁null清空 | PASS |
| W071 | 补丁零值保留 | PASS |
| W072 | 补丁未知字段拒绝 | PASS |
| W073 | 补丁错误类型拒绝 | PASS |
| W074 | 补丁小数进度拒绝 | PASS |
| W075 | 工具未知参数 | PASS |
| W076 | 工具标签类型 | PASS |
| W077 | 工具字段对象类型 | PASS |
| W078 | 工具版本类型 | PASS |
| W079 | 工具合并布尔类型 | PASS |
| W080 | 工具父节点类型 | PASS |
| I01 | TestStdioRoundTrip | PASS |
| I02 | TestTaskFieldCommentAndLabelLifecycle | PASS |
| I03 | TestDeletePlanExpiryPaginationAndAtomicRestore | PASS |
| I04 | TestPerformanceFixture | PASS |
| I05 | TestAttachmentPreviewFormatsAndSafety | PASS |
| I06 | TestAuthenticationPersistenceAndConflict | PASS |
| I07 | TestKnowledgeAndLabelsAreTransactional | PASS |
| I08 | TestAttachmentsBackupRestoreAndReminder | PASS |
| I09 | TestMCPProtocolTokenIsolationAndRevocation | PASS |
| I10 | TestHTTPSCookiesAndOrigin | PASS |
| I11 | TestConcurrentCompareAndSwap | PASS |
| I12 | TestRestartExclusiveDirectoryAndMonotonicNumbers | PASS |
| I13 | TestCleanupRetainsReferencedAttachments | PASS |
| I14 | TestFutureDatabaseVersionIsRejected | PASS |

## 前端和冻结基准逐项结果

| 编号 | 测试项 | 结果 |
| --- | --- | --- |
| V01 | 任务详细默认阅读，位于评论上方 | PASS |
| V02 | 阅读状态的详细正文允许后台同步 | PASS |
| V03 | 评论等待服务端确认后才报告发表 | PASS |
| V04 | 冲突回滚并保留评论草稿 | PASS |
| V05 | 附件提供预览与独立下载入口 | PASS |
| V06 | 项目独立标签与实体标签共同显示 | PASS |
| V07 | 局域网 HTTP 使用安全随机 ID | PASS |
| P01 | 脚本语法、初始数据及所有页面渲染 | PASS |
| P02 | 知识地址与祖先路径 | PASS |
| P03 | 节点名称与同级唯一约束 | PASS |
| P04 | 创建带标签父节点 | PASS |
| P05 | 创建子节点且不继承标签 | PASS |
| P06 | 从知识节点创建任务并保留引用 | PASS |
| P07 | 父节点移动与重命名，后代地址更新且任务引用保持 | PASS |
| P08 | 移动目标排除本节点及其后代 | PASS |
| P09 | 标签 AND 与 OR 匹配 | PASS |
| P10 | 批量添加标签仅影响选择节点 | PASS |
| P11 | 批量移除标签保留内容与子节点 | PASS |
| P12 | 标签改名与合并去重 | PASS |
| P13 | 批量删除预告包含未匹配标签的后代并去重 | PASS |
| P14 | 子树恢复保留关系与内容 | PASS |
| P15 | 节点内容自动保存与历史恢复 | PASS |
| P16 | 节点双向关联与解绑 | PASS |
| P17 | 任务状态变更与排序 | PASS |
| P18 | 新增自定义状态与删除迁移 | PASS |
| P19 | 工作记录写入知识的第二个编辑对话框保持打开 | PASS |
| P20 | 任务删除与恢复 | PASS |
| P21 | 备份格式、循环层级与重复 ID 校验 | PASS |
| P22 | 知识内容与标签安全转义 | PASS |
| P23 | 项目标签汇总任务与知识，跨项目同名标签隔离 | PASS |
| P24 | 混合选择批量标签更新及越界目标拒绝 | PASS |
| P25 | 标签混合删除预告包含任务及未匹配的子节点 | PASS |
| P26 | MCP 演示范围校验、点分定位和标签读写 | PASS |
| P27 | 新任务仅标题必填，默认创建状态、其他可选字段为空并进入独立页面 | PASS |
| P28 | 逐项字段设置、清空，零进度与空值不同 | PASS |
| P29 | 所有任务使用统一字段校验，备份拒绝无效可选字段 | PASS |
| P30 | 任务可选检查清单、工作记录和关联任务 | PASS |
| P31 | 打开任务、切换全局导航、返回项目不会保留遮罩 | PASS |
| P32 | 评论始终可见，旧工作记录显示为评论 | PASS |
| P33 | 文字与附件评论发布、回复、编辑和删除恢复 | PASS |
| P34 | 空评论不发布，文件内容校验与备份往返 | PASS |
| P35 | 时间线展示区间、截止标记和未安排，不写入缺失日期 | PASS |
| P36 | 旧状态迁移包含回收站且幂等，创建状态不可删除 | PASS |
| P37 | 任务详细固定在评论前，文字与附件独立保存 | PASS |
| P38 | 任务详细附件导入校验 | PASS |
| P39 | 任务详细默认阅读，编辑后可取消，保存返回阅读 | PASS |
| P40 | 多标签页并发修改不会覆盖较新数据 | PASS |
| P41 | HTML 无 CDN、外部脚本与远程字体 | PASS |

## 执行步骤和覆盖率

| 步骤 | 退出码 | 耗时ms |
| --- | --- | --- |
| build-web | 0 | 57 |
| vet | 0 | 882 |
| whitebox | 0 | 4785 |
| coverage | 0 | 885 |
| coverage-html | 0 | 834 |
| frontend | 0 | 88 |
| frozen-preview | 0 | 97 |
| build | 0 | 905 |
| blackbox | 0 | 2654 |
| browser | 0 | 12148 |
| standalone | 0 | 658 |
| runtime-covdata | 0 | 333 |
| runtime-coverage | 0 | 752 |

单元/集成 Go 覆盖率：`total:						(statements)		54.7%`。

真实部署插桩覆盖率：`total:						(statements)		81.0%`。两者分别计算，不相加；白盒分支条目不代表完成全分支覆盖。

逐函数报告、coverage.html、原始 Go JSON、黑盒 JSON、浏览器 trace ZIP、失败截图和移动/桌面截图均在 `test-output/full`（或 PB_TEST_OUTPUT）中。所有 PASS 仅代表列出的断言通过。

## 本轮发现与修正

1. **产品问题**：1×1 极小图片的缩略图点击区域被文件名按钮遮挡。为缩略图设置 72px 最小点击区域；U006 用真实图片选择与点击回归。冻结预览保持原样，正式样式与内嵌资源同步修复。
2. **脚本假设**：重复初始化实际返回409，上传返回200；搜索实体位于 value；schema2附件JSON导入需要内嵌字节。按公开接口的实际语义修正断言，并验证数据不变/完整。
3. **测试客户端**：Node fetch 不发送覆盖后的 Host，改用原生 HTTP 发出真实 Host 请求。
4. **浏览器定位**：详细/评论各有附件区域，改为区域定位；知识自动历史按一分钟合并，改用手动快照验证精确历史；导出入口在设置页。
5. **执行器**：PowerShell 的带点 Go 参数曾被拆分，改为 Node spawn 传递精确 argv；最终报告只读取结构化 Go JSON。

专项未执行项见测试目录 M01–M10。当前覆盖不能代替多设备、真实通知、故障注入或磁盘满测试。
