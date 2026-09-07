# ProjectBoard

本地安装、本地使用的项目任务与树状知识管理应用。一个管理员，一个 Go 进程，一个 SQLite 数据目录。

[下载发布版](https://github.com/backinfile/ProjectBoard/releases/latest) · [完整功能与测试目录](docs/test-catalog.md) · [本地测试报告](docs/local-test-report.md) · [MCP 接入说明](docs/mcp-design.md)

把任务、讨论和项目知识放在同一个工作空间：通过看板、列表或时间线安排工作，在独立任务页记录详细要求和评论，用点分地址定位知识，用项目标签组织任务和知识节点。

## 开始使用

1. 从 Releases 下载 `ProjectBoard-<版本>-windows-amd64.zip`，解压到本地目录。
2. 双击 `projectboard.exe`，程序自动打开浏览器。
3. 首次运行时设置管理员 `admin` 的密码。
4. 创建项目，开始安排任务和整理知识。

日常使用直接运行程序。已运行时会打开已有工作空间。关闭浏览器后服务继续运行；在“设置 → 运行信息 → 停止服务”或控制台按 Ctrl+C 停止。

用户机器无需安装 Go、Node.js、Docker、Git 或独立数据库。界面、图标和 Markdown 渲染随程序提供。

当前发布包面向 Windows x64。默认访问地址为 `http://127.0.0.1:7331`，可将解压目录中的程序创建为桌面快捷方式。

## 功能

- 项目创建、设置、归档和恢复；我的工作与项目筛选。
- 任务看板、列表、月度时间线；独立任务链接、排序、自定义状态。
- 新任务默认“创建”；其他属性按需添加。任务详细默认阅读，位于评论上方。
- 评论、回复、检查清单、任务与知识关联；删除与回收站恢复。
- 任务详细和评论上传附件。图片可缩放；文本可直接阅读；Markdown 支持排版、表格、代码块与源码切换。
- 内部知识树：每个节点均可有内容和子节点，使用 `技术.开发.启动命令` 这样的地址定位。
- 知识移动、改名、置顶、关联与版本历史；稳定 ID 保持引用。
- 项目内共享任务和知识标签；筛选、批量标记、改名、合并、删除。
- 全局/项目搜索、浅色/深色主题、中文界面。
- 提醒中心与可选浏览器桌面提醒；完成重复任务后创建后继任务。
- ZIP 完整备份、恢复前自动备份、预览 JSON 导入、任务 CSV/JSON 与知识树 JSON 导出。
- 项目独立 MCP 令牌；Streamable HTTP 与 stdio，37 个工具。

### 任务字段

新任务只需填写标题，初始状态为“创建”。通过任务页右侧“属性”的“＋”设置优先级、开始/截止/结束日期、进度、提醒、重复周期、颜色和标签。字段清空后恢复按需添加；0% 进度作为已设置值保留。

任务详细固定显示在评论上方，点击“编辑”后修改正文或上传附件。评论支持回复、编辑、删除和恢复，也可将讨论内容写入知识节点。检查清单、关联任务和关联知识集中在“检查清单与关联”区域。

### 知识树与项目标签

每个知识节点都可以同时拥有内容和子节点。例如 `技术.开发.启动命令` 唯一定位当前项目中的一条知识。改名和移动会更新地址，任务关联仍通过稳定 ID 保持连接；内容支持自动保存、手动版本和历史恢复。

每个项目拥有自己的标签词表，任务和知识节点共享该词表。标签页支持匹配全部或任一所选标签，结合类型和关键词筛选，再对选中内容批量添加标签、移除标签或删除。标签改名、合并和删除会同步更新本项目内容。

## 数据和运行参数

默认地址：`http://127.0.0.1:7331`。

默认数据目录：Windows `%LOCALAPPDATA%\ProjectBoard`。目录中包含 `projectboard-v2.sqlite`、附件、备份和日志。知识节点由应用管理，无需维护文档文件。

```powershell
.\projectboard.exe --data-dir "D:\ProjectBoard数据"
.\projectboard.exe --listen 127.0.0.1:7332
.\projectboard.exe --no-browser
.\projectboard.exe --version
```

显式开启局域网或 HTTPS：

```powershell
.\projectboard.exe --listen 0.0.0.0:7331
.\projectboard.exe --listen 0.0.0.0:7331 --host board.local:7331 --tls-cert server.crt --tls-key server.key
```

管理员密码重设：先停止服务，再运行 `projectboard.exe --reset-password`；使用自定义目录时同时提供 `--data-dir`。密码在控制台隐藏输入。改密和重设都会使旧会话失效。

更新：导出备份 → 停止服务 → 替换程序 → 重新运行。升级时沿用原数据目录；应用会检查数据库格式版本。当前实现使用 `projectboard-v2.sqlite`，与此前旧实现的 `projectboard.db` 分开保存。

## 附件与备份

单文件 20 MB，每个任务详细或每条评论最多 8 个附件，工作空间附件总容量 256 MB。删除的任务和评论保留附件；永久删除后的闲置附件由后台清理。文本预览支持 UTF-8 / UTF-16，展示前 1 MB，完整文件可下载。PNG、JPEG、GIF、WebP 支持图片预览；SVG/HTML 作为源码或下载内容。

| 附件类型 | 阅读方式 |
| --- | --- |
| PNG、JPEG、GIF、WebP | 缩略图、独立预览、放大/缩小、适应窗口 |
| 普通文本及常见源码文件 | 保留换行和原始文本，支持 UTF-8、UTF-16 |
| `.md`、`.markdown` | Markdown 排版、表格、代码块、清单；可切换源码 |
| 其他文件 | 下载后通过本机应用打开 |

图片和文件可添加到任务详细及评论。Markdown 使用安全渲染，远程图片以文本占位；所有附件均提供完整文件下载。

备份包含事务一致的工作空间快照、附件字节、格式版本和 SHA-256 校验清单。管理员密码、会话和 MCP 令牌保留在当前安装中。恢复验证完整性后替换数据，并把当前工作空间备份到 `backups`。ZIP 导入上限 512 MB。

此前 HTML 预览的数据可在预览“设置”中导出 JSON，再在正式版“设置”中导入。验收数据和用户工作空间使用独立目录。

任务 CSV/JSON 和知识树 JSON 适合单独导出内容；迁移整个工作空间时使用“全部项目备份”的 ZIP 文件。

## MCP

项目 → MCP → 管理接入令牌 → 创建。令牌绑定该项目并仅显示一次。配置示例、工具字段和调用约定见 [MCP 文档](docs/mcp-design.md)。

```json
{
  "mcpServers": {
    "projectboard": {
      "url": "http://127.0.0.1:7331/api/mcp",
      "headers": {
        "Authorization": "Bearer <项目令牌>",
        "X-Project-ID": "<项目ID>"
      }
    }
  }
}
```

stdio 客户端设置环境变量 `PROJECTBOARD_TOKEN`，启动 `projectboard.exe mcp --project <项目ID>`。stdio 连接正在运行的服务，共用工作空间数据。

## 开发与验证

开发环境：Go 1.26+、Node.js 22+。前端是已确认设计对应的原生 HTML/CSS/JavaScript，零运行时 JavaScript 依赖。

```powershell
node scripts/build-web.cjs
go test ./...
node web/check.cjs
node preview/check.cjs
go build -trimpath -ldflags "-s -w" -o dist/projectboard.exe ./cmd/projectboard
```

发布：`powershell -ExecutionPolicy Bypass -File scripts/release.ps1`。

实现基准见 [实施记录](docs/implementation.md)，验收见 [正式版验收报告](docs/release-test-report.md)。冻结预览 `preview/frozen-20260905.html` 及 SHA-256 文件保留原方案。

## 完整本地测试

[功能和逐项黑盒/白盒测试目录](docs/test-catalog.md) · [本地测试执行报告](docs/local-test-report.md)

```powershell
npm ci
npm run test:all
```

需要开发环境及本机 Edge。流水线构建专用程序，在独立数据目录和随机端口执行 HTTP/MCP 与浏览器测试，生成 Go 语句覆盖率、逐条结果、截图和浏览器 trace。输出位于 `test-output/full`，测试结束自动停止专用实例。Playwright 仅用于开发测试，发布程序继续保持单文件运行。

最近一次完整本地回归覆盖：50 项 HTTP/MCP 黑盒、27 项真实浏览器黑盒、80 项白盒子测试、14 项集成测试，以及前端、冻结预览和独立运行检查。逐项结果、运行环境及尚需专项环境验证的场景见[测试报告](docs/local-test-report.md)。

## 项目结构

| 路径 | 内容 |
| --- | --- |
| `cmd/projectboard` | 程序启动、运行参数和 MCP stdio 入口 |
| `internal/domain` | 项目、任务、评论、知识节点及约束 |
| `internal/store` | SQLite、附件、备份恢复和提醒 |
| `internal/server` | HTTP 接口、管理员会话、MCP 和文件预览 |
| `web/src` | 正式前端源码 |
| `web/assets` | 随程序内嵌的静态资源 |
| `preview` | 确认过的界面方案和冻结校验文件 |
| `scripts` | 构建、发布、黑盒测试与报告生成 |
| `docs` | 产品设计、接入说明和测试记录 |
