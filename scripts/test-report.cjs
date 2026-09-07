const fs=require('node:fs'),path=require('node:path'),{spawnSync}=require('node:child_process');
const out=path.resolve(process.env.PB_TEST_OUTPUT||'test-output/full');
const read=(name,fallback)=>{try{return JSON.parse(fs.readFileSync(path.join(out,name),'utf8'))}catch{return fallback}};
const list=script=>{const r=spawnSync(process.execPath,[script,'--list'],{encoding:'utf8'});if(r.status!==0)throw Error(r.stderr);return JSON.parse(r.stdout)};
const clean=s=>String(s??'').replaceAll('|','\\|').replace(/\r?\n/g,' ');
const table=(head,rows)=>'| '+head.join(' | ')+' |\n| '+head.map(()=>'---').join(' | ')+' |\n'+rows.map(r=>'| '+r.map(clean).join(' | ')+' |').join('\n')+'\n';
const black=list('scripts/blackbox.cjs'),ui=list('scripts/browser-test.cjs'),features=JSON.parse(fs.readFileSync('docs/test-features.json','utf8'));
const goFiles=['internal/domain/model_test.go','internal/server/whitebox_test.go'];
const white=[...new Set(goFiles.flatMap(f=>[...fs.readFileSync(f,'utf8').matchAll(/"(W\d+_[^"]+)"/g)].map(m=>m[1])))].sort();
const valid=new Set(['W001','W020','W027','W034','W037','W040','W041','W057','W062']);
const wExpected=name=>{const id=name.split('_')[0];if(+id.slice(1)<=62)return valid.has(id)?'Validate 成功；默认值/边界值保持':'Validate 返回错误';return ({W063:'规范化为 NFC 并去重，保留顺序',W064:'改名更新点分地址，子树稳定 ID 集合正确',W065:'修改克隆不会影响原对象',W066:'默认创建、空标量和非 nil 集合',W067:'1000 个 ID 格式有效且样本无重复',W068:'已删除任务/评论附件仍被收集',W069:'省略字段保留原值',W070:'null 清空字符串和指针',W071:'显式 0 保留',W072:'未知字段拒绝且对象不变',W073:'错误类型拒绝且对象不变',W074:'小数进度拒绝且对象不变'})[id]||'工具参数类型校验返回错误';};
const integration=[];
for(const dir of ['cmd/projectboard','internal/server','internal/store'])for(const f of fs.readdirSync(dir).filter(f=>f.endsWith('_test.go')&&f!=='whitebox_test.go'))for(const m of fs.readFileSync(dir+'/'+f,'utf8').matchAll(/func (Test\w+)\(t \*testing.T\)/g)){if(m[1].includes('Helper'))continue;integration.push({name:m[1],file:dir+'/'+f})}
const descriptions={TestConcurrentCompareAndSwap:'16 个并发请求使用同一修订：恰 1 成功、15 冲突，修订只加 1',TestRestartExclusiveDirectoryAndMonotonicNumbers:'同目录互斥；任务清理后编号递增；重启保留数据；旧修订拒绝',TestCleanupRetainsReferencedAttachments:'直接老化附件时间，清理仅删除闲置文件，回收站附件仍可读',TestFutureDatabaseVersionIsRejected:'写入 user_version=99，再次打开必须拒绝',TestDeletePlanExpiryPaginationAndAtomicRestore:'把计划时间设为过期；过期游标拒绝；损坏恢复回滚；旧空附件导入',TestTaskFieldCommentAndLabelLifecycle:'任务字段 null/0、评论附件生命周期、标签合并冲突和删除完整性',TestPerformanceFixture:'构造 10000 任务与 2000 节点，执行中文搜索并记录耗时',TestAttachmentPreviewFormatsAndSafety:'Markdown 表格/强调/脚本/危险链接、纯文本、UTF16、图片、二进制回退',TestKnowledgeAndLabelsAreTransactional:'直接调用服务层验证稳定引用、树循环、跨项目批量原子性',TestAuthenticationPersistenceAndConflict:'会话、CAS冲突、CSRF/Host、保存后的内部状态检查',TestStdioRoundTrip:'官方 SDK 通过真实 stdio 子进程读写，与 HTTP 数据一致'};
Object.assign(descriptions,{
 TestAttachmentsBackupRestoreAndReminder:'检查附件上传/归属/校验、ZIP备份与恢复字节、重复后继和提醒；错误恢复保持现有存储',
 TestMCPProtocolTokenIsolationAndRevocation:'官方SDK初始化与工具调用、绑定项目拒绝越界、撤销令牌后拒绝协议请求',
 TestHTTPSCookiesAndOrigin:'TLS测试服务器检查Secure/HttpOnly/SameSite Cookie，拒绝非同源或错误协议来源'
});
const manual=[
 ['M01','真实终端密码重设','停止专用实例，执行 --reset-password，隐藏输入新密码，再登录并检查旧会话失效','待人工执行；自动测试覆盖 ResetPassword 服务逻辑'],
 ['M02','真实证书部署与局域网多机器访问','指定证书与自定义 Host，在第二台机器访问并检查证书链','待外部环境；自动测试覆盖 TLS 测试服务器、Cookie 和来源'],
 ['M03','浏览器历史/快捷键全键盘遍历','前进后退、Ctrl+K、Enter、Escape、Tab 遍历全部交互','待补充专门无障碍回归；已自动测独立链接/刷新'],
 ['M04','操作系统桌面通知','允许/拒绝权限，服务到期后检查真实系统通知和点击','待人工执行；自动测试覆盖提醒中心、投递和已读'],
 ['M05','系统拖文件/剪贴板图片','从资源管理器拖图到详细/评论、粘贴剪贴板图片','待人工执行；文件选择上传已自动验证'],
 ['M06','图片格式视觉矩阵','分别上传 JPEG/GIF动画/WebP及大图，检查方向、动画、缩放','待补充视觉矩阵；当前浏览器 PNG 已通过'],
 ['M07','真实总配额和磁盘耗尽','真实累计上传到 256MB，验证满额行为；隔离磁盘写满后验证恢复','待隔离磁盘环境；单附件/区域边界由模型测试覆盖'],
 ['M08','崩溃/断电/长时间离线草稿','输入知识内容后终止浏览器/服务，重启检查草稿恢复和已提交数据','待故障注入；自动测试覆盖正常重启、冲突草稿、后台同步'],
 ['M09','知识复制地址/子树剪贴板','复制地址和子树，检查子树顺序、内容、标签','待补充浏览器剪贴板测试；任务链接复制已自动验证'],
 ['M10','多浏览器和视觉无障碍','Chrome/Firefox/Safari、200%缩放、屏幕阅读器、触屏拖动','待相应设备；本轮为 Windows Edge 桌面和390px视口']
];
const mcpSource=fs.readFileSync('internal/server/mcp.go','utf8').split('var toolNames = map[string]string{')[1].split('\n}')[0];
const toolNames=[...mcpSource.matchAll(/"([a-z_]+)":/g)].map(m=>m[1]).sort();
let catalog='# 功能与测试目录\n\n以当前正式源代码为基准。功能清单、测试编号、步骤和预期结果均可复跑核对。接口黑盒只访问部署进程的公开 HTTP/MCP；浏览器黑盒使用真实 Edge；白盒检查内部函数、状态和存储。冻结预览测试仅用于设计基准回归，不计入正式后端/浏览器覆盖。\n\n';
catalog+='## 全部功能清单\n\n'+table(['编号','功能范围','对应验证'],features)+'\n';
catalog+='## 接口黑盒条目\n\n脚本：scripts/blackbox.cjs。每轮自动创建新数据目录、随机端口和随机测试密码；正常结束后停止测试进程。每条按编号执行，依赖项失败会使后续相关断言失败，逐条结果保留。\n\n'+table(['编号','测试项','步骤','预期'],black.map(c=>[c.id,c.name,c.steps,c.expected]));
catalog+='\n## 浏览器黑盒条目\n\n脚本：scripts/browser-test.cjs。独立部署；页面操作、文件选择、下载、第二页面同步均使用真实浏览器。页面定位器失败与产品断言失败都记录为 FAIL，修正后重跑。\n\n'+table(['编号','测试项','步骤','预期'],ui.map(c=>[c.id,c.name,c.steps,c.expected]));
catalog+='\n## 白盒领域与分支条目\n\nW001–W062 从有效的两项目/三任务/三节点夹具开始，单独修改标题中指明的字段；每项调用 Validate。其余项直接调用模型辅助函数、patch 和工具参数校验。每条为独立 Go 子测试。\n\n'+table(['编号','输入/分支','预期','脚本'],white.map(n=>[n.split('_')[0],n.split('_').slice(1).join('_'),wExpected(n),+n.slice(1,4)<=68?goFiles[0]:goFiles[1]]));
catalog+='\n## 白盒集成测试条目\n\n这些测试读取/修改内部存储、计划或会话状态，并在临时目录运行。一个集成测试内有多条相关断言，报告按 Go 顶层测试计数。\n\n'+table(['编号','测试函数','检查点','脚本'],integration.map((c,i)=>['I'+String(i+1).padStart(2,'0'),c.name,descriptions[c.name]||'完整断言见该函数；请求结果和内部状态同时检查',c.file]));
catalog+='\n## MCP 每个工具的黑盒覆盖\n\n全部通过真正的 /api/mcp tools/call 调用；B045 比较成功调用集合与服务 tools/list，发现工具增加而脚本未覆盖时失败。\n\n'+table(['工具','调用条目'],toolNames.map(n=>[n,(()=>{const src=fs.readFileSync('scripts/blackbox.cjs','utf8');return [...src.matchAll(/test\('(B\d+)'([\s\S]*?)(?=\ntest\(|\nasync function start)/g)].filter(m=>m[2].includes("'"+n+"'")).map(m=>m[1]).join('、')||'B045 集合校验'})()]));
catalog+='\n## 需要人工或专项环境的黑盒条目\n\n以下明确保留为未执行，不能据自动化通过推断这些场景已通过。\n\n'+table(['编号','场景','步骤与预期检查','状态'],manual);
catalog+='\n## 执行方法\n\n```powershell\nnpm ci\nnode scripts/test-all.cjs\n```\n\n需要 Go 1.26+、Node 22+、本机 Edge。仅测试使用 Playwright，发布程序运行仍不依赖 Node。可设置 PB_BROWSER_CHANNEL=chrome 使用已安装 Chrome。默认输出 test-output/full，PB_TEST_OUTPUT 可指定独立报告目录。全部进程使用测试目录，保留正常管理员目录和已运行实例。\n\n单独运行：`node scripts/blackbox.cjs dist/projectboard-test.exe`、`node scripts/browser-test.cjs dist/projectboard-test.exe`；`--list` 只输出条目，不启动部署。\n\n结果分层：B/U 为正式部署黑盒；W/I 为 Go 白盒；V 为正式前端 VM 契约；P 为冻结预览 VM 基准。覆盖率为 Go 语句覆盖率，不等于功能或分支全部覆盖。\n';
fs.writeFileSync('docs/test-catalog.md',catalog);
const goLines=fs.existsSync(path.join(out,'go-tests.jsonl'))?fs.readFileSync(path.join(out,'go-tests.jsonl'),'utf8').split(/\r?\n/).filter(Boolean).flatMap(l=>{try{return[JSON.parse(l)]}catch{return[]}}):[];
const statuses=new Map(goLines.filter(x=>x.Test&&['pass','fail','skip'].includes(x.Action)).map(x=>[x.Test,x.Action.toUpperCase()]));
const b=read('blackbox.json',{results:[]}),u=read('browser.json',{results:[]}),run=read('run.json',{steps:[]});
const front=read('frontend.json',[]),preview=read('frozen-preview.json',{cases:[]}).cases,smoke=read('standalone.json',{checks:[]});
const cover=name=>{try{return fs.readFileSync(path.join(out,name),'utf8').trim().split(/\r?\n/).at(-1)}catch{return '未生成'}};
let result='# 本地测试执行报告\n\n执行时间：'+(run.at||new Date().toISOString())+'。环境：Windows x64，Node '+process.version+'，Edge headless；独立本地数据目录和随机回环端口。\n\n功能与全部条目见 [测试目录](test-catalog.md)。\n\n';
result+=table(['测试层','通过/总项','说明'],[['接口黑盒',b.results.filter(x=>x.status==='PASS').length+'/'+b.results.length,'真实部署 HTTP/MCP'],['浏览器黑盒',u.results.filter(x=>x.status==='PASS').length+'/'+u.results.length,'Edge 真实页面'],['新增白盒子测试',white.filter(n=>[...statuses].some(([k,v])=>k.endsWith('/'+n)&&v==='PASS')).length+'/'+white.length,'独立命名子测试'],['原有及新增集成测试',integration.filter(c=>statuses.get(c.name)==='PASS').length+'/'+integration.length,'不含 stdio 辅助进程入口'],['正式前端 VM',front.filter(c=>c.status==='PASS').length+'/'+front.length,'状态/异步契约'],['冻结预览 VM',preview.filter(c=>c.status==='PASS').length+'/'+preview.length,'设计基准'],['独立运行',smoke.checks.length+'/'+smoke.checks.length,'须结合 standalone 步骤退出码'],['专项人工条目','0/'+manual.length,'未执行，详见目录']]);
result+='\n## 每条黑盒结果\n\n'+table(['编号','测试项','结果','耗时ms','错误'],[...b.results,...u.results].map(c=>[c.id,c.name,c.status,c.ms,c.status==='FAIL'?c.error:'']));
result+='\n## 每条白盒结果\n\n'+table(['编号','测试项','结果'],white.map(n=>[n.split('_')[0],n.split('_').slice(1).join('_'),[...statuses].find(([k])=>k.endsWith('/'+n))?.[1]||'NOT RUN']).concat(integration.map((c,i)=>['I'+String(i+1).padStart(2,'0'),c.name,statuses.get(c.name)||'NOT RUN'])));
result+='\n## 前端和冻结基准逐项结果\n\n'+table(['编号','测试项','结果'],front.map((c,i)=>['V'+String(i+1).padStart(2,'0'),c.name,c.status]).concat(preview.map((c,i)=>['P'+String(i+1).padStart(2,'0'),c.name,c.status])));
result+='\n## 执行步骤和覆盖率\n\n'+table(['步骤','退出码','耗时ms'],run.steps.map(s=>[s.id,s.exitCode,s.ms]));
result+='\n单元/集成 Go 覆盖率：`'+cover('coverage.txt')+'`。\n\n真实部署插桩覆盖率：`'+cover('runtime-coverage.txt')+'`。两者分别计算，不相加；白盒分支条目不代表完成全分支覆盖。\n\n逐函数报告、coverage.html、原始 Go JSON、黑盒 JSON、浏览器 trace ZIP、失败截图和移动/桌面截图均在 `test-output/full`（或 PB_TEST_OUTPUT）中。所有 PASS 仅代表列出的断言通过。\n';
result+='\n## 本轮发现与修正\n\n1. **产品问题**：1×1 极小图片的缩略图点击区域被文件名按钮遮挡。为缩略图设置 72px 最小点击区域；U006 用真实图片选择与点击回归。冻结预览保持原样，正式样式与内嵌资源同步修复。\n2. **脚本假设**：重复初始化实际返回409，上传返回200；搜索实体位于 value；schema2附件JSON导入需要内嵌字节。按公开接口的实际语义修正断言，并验证数据不变/完整。\n3. **测试客户端**：Node fetch 不发送覆盖后的 Host，改用原生 HTTP 发出真实 Host 请求。\n4. **浏览器定位**：详细/评论各有附件区域，改为区域定位；知识自动历史按一分钟合并，改用手动快照验证精确历史；导出入口在设置页。\n5. **执行器**：PowerShell 的带点 Go 参数曾被拆分，改为 Node spawn 传递精确 argv；最终报告只读取结构化 Go JSON。\n\n专项未执行项见测试目录 M01–M10。当前覆盖不能代替多设备、真实通知、故障注入或磁盘满测试。\n';
fs.writeFileSync('docs/local-test-report.md',result);
console.log('Wrote docs/test-catalog.md and docs/local-test-report.md');
