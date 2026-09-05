const fs=require('node:fs');
const vm=require('node:vm');
const assert=require('node:assert/strict');
const {webcrypto}=require('node:crypto');
const html=fs.readFileSync(__dirname+'/index.html','utf8');
const script=html.match(/<script>([\s\S]*)<\/script>/)[1];
const elements=new Map(),data=new Map(),session=new Map();
const element=id=>{if(!elements.has(id))elements.set(id,{innerHTML:'',textContent:'',value:'',classList:{toggle(){},remove(){},add(){}},focus(){},replaceChildren(x){this.textContent=x.textContent}});return elements.get(id)};
const storage=map=>({getItem:k=>map.get(k)||null,setItem:(k,v)=>map.set(k,v),removeItem:k=>map.delete(k)});
const ctx=vm.createContext({console,crypto:webcrypto,TextEncoder,Blob,URL,Date,Set,Map,localStorage:storage(data),sessionStorage:storage(session),setTimeout:()=>0,clearTimeout(){},FormData:class{constructor(target){this.values=target.values}get(k){return this.values[k]??''}},navigator:{clipboard:{writeText:async()=>{}}},document:{getElementById:element,body:{classList:{toggle(){}}},querySelector:()=>null,querySelectorAll:()=>[],addEventListener(){},createTextNode:text=>({textContent:text})},window:{addEventListener(){}}});
vm.runInContext(script,ctx);
const run=source=>vm.runInContext(source,ctx),button=(action,id)=>run(`actions[${JSON.stringify(action)}]({dataset:{id:${JSON.stringify(id)}}})`);
function submit(values){element('modal-form').onsubmit({preventDefault(){},target:{values}});const error=element('form-error').textContent;if(error){element('form-error').textContent='';throw Error(error)}}
const cases=[];
function test(name,fn){try{fn();cases.push({name,status:'PASS'})}catch(e){cases.push({name,status:'FAIL',detail:e.message});console.error(name,e);process.exitCode=1}}
test('脚本语法、初始数据及所有页面渲染',()=>{assert.equal(run('nodes().length'),11);for(const expr of ['projectView()','overview()','taskView()','workView()','tagsView()','trashView()','settingsView()'])assert.ok(run(expr).length>100)});
test('知识地址与祖先路径',()=>assert.equal(run("address(node('n8'))"),'技术.开发.启动命令'));
test('节点名称与同级唯一约束',()=>{assert.throws(()=>run("validName('含.点',null)"));assert.throws(()=>run("validName(' 产品 ',null)"));assert.equal(run("validName(' 架构 ','n7')"),'架构')});
test('创建带标签父节点',()=>{button('node-new');submit({name:'测试父节点',parent:'',tags:'验收, 验收, 技术',content:'父节点有自己的内容'});assert.equal(run("node(ui.node).tags.length"),2);run('globalThis.parentId=ui.node')});
test('创建子节点且不继承标签',()=>{run('nodeForm(null,parentId)');submit({name:'测试子节点',parent:run('parentId'),tags:'',content:'独立内容'});assert.equal(run('node(ui.node).tags.length'),0);assert.equal(run('address(node(ui.node))'),'测试父节点.测试子节点');run('globalThis.childId=ui.node')});
test('从知识节点创建任务并保留引用',()=>{run("taskForm(null,'todo',childId)");submit({title:'验收任务',description:'测试',status:'todo',priority:'高',due:'2026-09-05',tags:'验收'});assert.equal(run('db.tasks.at(-1).knowledge[0]'),run('childId'));run('ui.task=null')});
test('父节点移动与重命名，后代地址更新且任务引用保持',()=>{run('nodeForm(parentId)');submit({name:'已移动节点',parent:'n5',tags:'验收'});assert.equal(run('address(node(childId))'),'技术.已移动节点.测试子节点');assert.equal(run('db.tasks.at(-1).knowledge[0]'),run('childId'))});
test('移动目标排除本节点及其后代',()=>{run('nodeForm(parentId)');const modal=element('dialog').innerHTML;assert.ok(!modal.includes(`<option value="${run('childId')}"`));assert.ok(!modal.includes(`<option value="${run('parentId')}"`))});
test('标签 AND 与 OR 匹配',()=>{run("ui.tags=['技术','核心'];ui.tagMode='all';ui.query=''");assert.equal(run('matchingNodes().length'),1);run("ui.tagMode='any'");assert.equal(run('matchingNodes().length'),6)});
test('批量添加标签仅影响选择节点',()=>{run("tagEditor([parentId])");submit({tags:'新标签, 新标签'});assert.equal(run("node(parentId).tags.filter(t=>t==='新标签').length"),1);assert.equal(run("node(childId).tags.includes('新标签')"),false)});
test('批量移除标签保留内容与子节点',()=>{run("tagEditor([parentId],true)");submit({tags:'新标签'});assert.equal(run("node(parentId).tags.includes('新标签')"),false);assert.equal(run('node(childId).content'),'独立内容')});
test('标签改名与合并去重',()=>{run("actions['tag-rename']({dataset:{tag:'验收'}})");submit({tag:'技术'});assert.equal(run("node(parentId).tags.join(',')"),'技术')});
test('批量删除预告包含未匹配标签的后代并去重',()=>{run('deleteNodes([parentId,childId])');assert.ok(element('dialog').innerHTML.includes('共 <b>2</b>'));element('confirm-action').onclick();assert.equal(run('Boolean(node(parentId).deleted&&node(childId).deleted)'),true)});
test('子树恢复保留关系与内容',()=>{run("actions['node-restore']({dataset:{id:parentId}})");submit({name:'恢复节点',parent:'n5'});assert.equal(run('Boolean(node(parentId).deleted||node(childId).deleted)'),false);assert.equal(run('address(node(childId))'),'技术.恢复节点.测试子节点')});
test('节点内容自动保存与历史恢复',()=>{run("ui.node=childId;activeDraft={id:childId,content:'新的内容'};flushDraft()");assert.equal(run('node(childId).content'),'新的内容');assert.equal(run('node(childId).history.at(-1).content'),'独立内容');run("actions['history-restore']({dataset:{node:childId,id:node(childId).history.at(-1).id}})");element('confirm-action').onclick();assert.equal(run('node(childId).content'),'独立内容')});
test('节点双向关联与解绑',()=>{run("ui.node=childId;node(childId).links=['n8'];node('n8').links=[childId]");button('node-unlink','n8');assert.equal(run('node(childId).links.length'),0);assert.equal(run("node('n8').links.length"),0)});
test('任务状态变更与排序',()=>{run("ui.task='t2'");button('task-up');assert.equal(run("tasks().filter(t=>t.status==='created').sort((a,b)=>a.order-b.order)[0].id"),'t2')});
test('新增自定义状态与删除迁移',()=>{run('statusForm()');submit({name:'待验证',kind:'active'});run("globalThis.customId=p().statuses.at(-1).id;db.tasks[0].status=customId;actions['status-delete']({dataset:{id:customId}})");submit({target:'doing'});assert.equal(run('db.tasks[0].status'),'doing');assert.equal(run("p().statuses.some(s=>s.name==='待验证')"),false)});
test('工作记录写入知识的第二个编辑对话框保持打开',()=>{run("ui.task='t1';db.tasks[0].notes.push({id:'note-test',text:'验收结论',at:now()});actions['note-to-node']({dataset:{id:'note-test'}})");submit({node:'n8'});assert.ok(element('dialog').innerHTML.includes('编辑知识内容'));submit({content:'写入的结论'});assert.equal(run("node('n8').content"),'写入的结论')});
test('任务删除与恢复',()=>{button('task-delete','t2');element('confirm-action').onclick();assert.equal(run("Boolean(db.tasks.find(t=>t.id==='t2').deleted)"),true);button('task-restore','t2');assert.equal(run("Boolean(db.tasks.find(t=>t.id==='t2').deleted)"),false)});
test('备份格式、循环层级与重复 ID 校验',()=>{assert.equal(run('validateBackup(JSON.parse(JSON.stringify(db))).schema'),2);assert.throws(()=>run("(()=>{const x=JSON.parse(JSON.stringify(db));x.nodes[0].parent=x.nodes[0].id;return validateBackup(x)})()"));assert.throws(()=>run("(()=>{const x=JSON.parse(JSON.stringify(db));x.tasks[0].id=x.nodes[0].id;return validateBackup(x)})()"))});
test('知识内容与标签安全转义',()=>assert.equal(run("E('<script>\"&')"),'&lt;script&gt;&quot;&amp;'));

test('项目标签汇总任务与知识，跨项目同名标签隔离',()=>{
 run("ui.project='p1';ui.task=null;ui.tags=[];ui.query='';ui.tagType='all';db.tasks[0].tags.push('隔离验收');db.nodes[0].tags.push('隔离验收');db.nodes.find(n=>n.project==='p2').tags.push('隔离验收');");
 assert.equal(run("allTags().includes('隔离验收')"),true);
 run("actions['tag-rename']({dataset:{tag:'隔离验收'}})");submit({tag:'项目内标签'});
 assert.equal(run("db.tasks[0].tags.includes('项目内标签')&&db.nodes[0].tags.includes('项目内标签')"),true);
 assert.equal(run("db.nodes.find(n=>n.project==='p2').tags.includes('隔离验收')"),true);
 run("ui.tags=['项目内标签'];ui.tagType='task'");assert.equal(run("matchingEntities().length"),1);
 run("ui.tagType='node'");assert.equal(run("matchingEntities().length"),1);
 run("actions['tag-delete']({dataset:{tag:'项目内标签'}})");element('confirm-action').onclick();
 assert.equal(run("db.tasks[0].tags.includes('项目内标签')||db.nodes[0].tags.includes('项目内标签')"),false);
 assert.equal(run("db.nodes.find(n=>n.project==='p2').tags.includes('隔离验收')"),true);
});
test('混合选择批量标签更新及越界目标拒绝',()=>{
 run("tagEditor([db.tasks[0].id,db.nodes[0].id])");submit({tags:'混合验收'});
 assert.equal(run("db.tasks[0].tags.includes('混合验收')&&db.nodes[0].tags.includes('混合验收')"),true);
 run("tagEditor([db.tasks[0].id,db.nodes[0].id],true)");submit({tags:'混合验收'});
 assert.equal(run("db.tasks[0].tags.includes('混合验收')||db.nodes[0].tags.includes('混合验收')"),false);
 run("closeModal();tagEditor([db.nodes.find(n=>n.project==='p2').id])");
 assert.equal(element('dialog').innerHTML,'');
});
test('标签混合删除预告包含任务及未匹配的子节点',()=>{
 run("ui.tags=[];ui.tagType='all';ui.query='';ui.selected=new Set([db.tasks[0].id,parentId]);deleteTagged()");
 assert.ok(element('dialog').innerHTML.includes('1 个任务和 2 个知识节点'));
 assert.ok(element('dialog').innerHTML.includes('测试子节点'));
 run("closeModal();ui.selected.clear()");
});
test('MCP 演示范围校验、点分定位和标签读写',()=>{
 assert.throws(()=>run("mcpPreviewCall('list_tasks',{project_id:'p2'})"),/PROJECT_SCOPE/);
 assert.equal(run("mcpPreviewCall('get_knowledge_node',{project_id:'p1',address:'技术.开发.启动命令'}).id"),'n8');
 run("mcpPreviewCall('attach_labels',{project_id:'p1',target_type:'task',target_ids:['t1'],tags:['MCP验收']})");
 assert.equal(run("mcpPreviewCall('list_tasks',{project_id:'p1',tags:['MCP验收']}).length"),1);
 assert.throws(()=>run("mcpPreviewCall('attach_labels',{project_id:'p1',target_type:'node',target_ids:[db.nodes.find(n=>n.project==='p2').id],tags:['越界']})"),/NOT_FOUND/);
 run("mcpPreviewCall('detach_labels',{project_id:'p1',target_type:'task',target_ids:['t1'],tags:['MCP验收']})");
 assert.equal(run("db.tasks[0].tags.includes('MCP验收')"),false);
 assert.ok(run("mcpView()").includes('浏览器演示'));
});


test('新任务仅标题必填，默认创建状态、其他可选字段为空并进入独立页面',()=>{
 run("ui.project='p1';ui.page='project';ui.task=null;taskForm()");assert.ok(!element('dialog').innerHTML.includes('name="priority"'));submit({title:'可选字段验收'});
 run("globalThis.optionalId=ui.task");assert.equal(run("ui.page"),'task');assert.equal(run("taskFields().filter(f=>f.key!=='status').some(f=>taskHas(db.tasks.at(-1),f.key))"),false);
 assert.equal(run("status(db.tasks.at(-1)).name"),'创建');assert.ok(!run("taskView()").includes('未设置状态'));assert.ok(!run("taskView()").includes('待处理'));
});
test('逐项字段设置、清空，零进度与空值不同',()=>{
 for(const [key,value] of [['priority','立即处理'],['status','doing'],['start','2026-09-05'],['end','2026-09-09'],['due','2026-09-10'],['progress','0'],['color','#5269b8'],['reminder','2026-09-06T09:00'],['repeat','weekly'],['tags','本地, 验收'],['description','按需内容']]){run('taskFieldEditor('+JSON.stringify(key)+')');submit({value});}
 assert.equal(run("taskHas(db.tasks.at(-1),'progress')"),true);
 assert.equal(run("db.tasks.at(-1).progress"),0);
 assert.ok(run("taskTable([db.tasks.at(-1)])").includes('0%'));
 run("clearTaskField('priority');clearTaskField('progress')");
 assert.equal(run("taskHas(db.tasks.at(-1),'priority')||taskHas(db.tasks.at(-1),'progress')"),false);
 assert.equal(run("db.tasks.at(-1).status"),'doing');
});
test('所有任务使用统一字段校验，备份拒绝无效可选字段',()=>{
 for(const expr of ["t.progress=101","t.start='2026-09-11';t.end='2026-09-09'","t.color='url(bad)'","t.repeat='invalid'","t.due='2026-02-30'","t.relations=[{id:'t1',type:'invalid'}]"]){
 assert.throws(()=>run("(()=>{const x=JSON.parse(JSON.stringify(db)),t=x.tasks.at(-1);"+expr+";return validateBackup(x)})()"));
 }
 assert.equal(run("validateBackup(JSON.parse(JSON.stringify(db))).schema"),2);
});
test('任务可选检查清单、工作记录和关联任务',()=>{
 run("ui.task=optionalId;taskFieldEditor('checks')");submit({value:'验证链接刷新'});assert.equal(run("db.tasks.at(-1).checks.length"),1);
 run("taskFieldEditor('notes')");submit({value:'可选字段测试记录'});assert.equal(run("db.tasks.at(-1).notes.length"),1);
 run("taskFieldEditor('relations')");submit({value:'t1',relation:'related'});assert.equal(run("db.tasks.at(-1).relations[0].id"),'t1');
 run("renderTask()");assert.ok(element('task-page').innerHTML.includes('验证链接刷新'));
 run("clearTaskField('checks')");element('confirm-action').onclick();assert.equal(run("db.tasks.at(-1).checks.length"),0);
});
test('打开任务、切换全局导航、返回项目不会保留遮罩',()=>{
 button('task-open','t1');assert.equal(run('ui.page'),'task');assert.equal(element('root').inert,false);
 button('nav-settings');assert.equal(element('root').inert,false);
 button('task-open','t1');button('task-close');assert.equal(run('ui.page'),'project');assert.equal(run('ui.task'),null);
});


test('评论始终可见，旧工作记录显示为评论',()=>{
 run("ui.project='p1';ui.task='t1';ui.page='task';render()");
 const out=element('task-page').innerHTML;assert.ok(out.includes('任务评论'));assert.ok(out.indexOf('task-conversation')<out.indexOf('task-supplement'));
});
test('文字与附件评论发布、回复、编辑和删除恢复',()=>{
 run("globalThis.ct=db.tasks.find(t=>t.id==='t1');globalThis.beforeComments=ct.notes.length;commentDraft('t1').text='附件验收';commentDraft('t1').files=[{id:'file-test',name:'test.txt',type:'text/plain',size:2,data:'aGk='}];postComment()");
 assert.equal(run('ct.notes.length'),run('beforeComments+1'));assert.equal(run('ct.notes.at(-1).attachments[0].size'),2);
 run("globalThis.commentId=ct.notes.at(-1).id;commentDraft('t1').reply=commentId;commentDraft('t1').text='回复验收';postComment()");
 assert.equal(run('ct.notes.at(-1).replyTo'),run('commentId'));
 run("editComment(commentId)");submit({text:'编辑后的评论'});assert.equal(run("ct.notes.find(n=>n.id===commentId).text"),'编辑后的评论');
 run("actions['comment-delete']({dataset:{id:commentId}})");element('confirm-action').onclick();assert.equal(run("Boolean(ct.notes.find(n=>n.id===commentId).deleted)"),true);
 run("actions['comment-restore']({dataset:{id:commentId}})");assert.equal(run("Boolean(ct.notes.find(n=>n.id===commentId).deleted)"),false);
});
test('空评论不发布，文件内容校验与备份往返',()=>{
 run("globalThis.nCount=ct.notes.length;postComment()");assert.equal(run('ct.notes.length'),run('nCount'));
 assert.equal(run("attachmentOK({id:'a',name:'a.txt',type:'text/plain',size:2,data:'aGk='})"),true);
 assert.equal(run("attachmentOK({id:'a',name:'a.txt',type:'text/plain',size:3,data:'aGk='})"),false);
 assert.throws(()=>run("(()=>{const x=JSON.parse(JSON.stringify(db));x.tasks[0].notes[0].attachments=[{id:'bad',data:'javascript:alert(1)'}];return validateBackup(x)})()"));
 assert.equal(run("validateBackup(JSON.parse(JSON.stringify(db))).schema"),2);
});
test('时间线展示区间、截止标记和未安排，不写入缺失日期',()=>{
 run("globalThis.month=today().slice(0,7);globalThis.lineTasks=[{id:'a',title:'排期任务',start:month+'-02',end:month+'-08',due:month+'-09'},{id:'b',title:'未安排验收',start:'',end:'',due:''}];ui.timelineOffset=0");
 const out=run('timelineView(lineTasks)');assert.ok(out.includes('timeline-bar'));assert.ok(out.includes('timeline-marker due'));assert.ok(out.includes('未安排验收'));
 assert.equal(run("lineTasks[1].start+lineTasks[1].end+lineTasks[1].due"),'');
 run("ui.timelineOffset=1");assert.ok(run("timelineView(lineTasks)").includes('为任务设置日期，安排本月计划'));
 run("ui.timelineOffset=0");
});


test('旧状态迁移包含回收站且幂等，创建状态不可删除',()=>{
 const result=run("(()=>{const x=seed();x.projects[0].statuses.push({id:'legacy-todo',name:'待处理',kind:'todo'});x.tasks[0].status='legacy-todo';x.tasks[0].deleted=now();x.tasks[1].status='';ensureCreatedStatus(x);return {s:x.tasks.slice(0,2).map(t=>t.status),removed:!x.projects[0].statuses.some(s=>s.name==='待处理'),again:ensureCreatedStatus(x)}})()");
 assert.deepEqual(Array.from(result.s),['created','created']);assert.equal(result.removed,true);assert.equal(result.again,false);
 run("ui.project='p1';ui.task='t1';clearTaskField('status')");assert.equal(run("db.tasks[0].status"),'created');
 run("closeModal();actions['status-delete']({dataset:{id:p().initialStatusId}})");assert.equal(element('dialog').innerHTML,'');
});


test('任务详细固定在评论前，文字与附件独立保存',()=>{
 run("ui.project='p1';ui.task='t1';ui.page='task';render();globalThis.detailNoteCount=db.tasks[0].notes.length");
 const out=element('task-page').innerHTML;assert.ok(out.indexOf('task-fixed-detail')<out.indexOf('task-conversation'));
 run("detailDraft(db.tasks[0]).text='固定详细内容';detailDraft(db.tasks[0]).files=[{id:'detail-file',name:'detail.txt',type:'text/plain',size:2,data:'aGk='}];saveTaskDetail()");
 assert.equal(run("db.tasks[0].description"),'固定详细内容');assert.equal(run("db.tasks[0].detailAttachments.length"),1);assert.equal(run("db.tasks[0].notes.length"),run('detailNoteCount'));
 assert.equal(run("validateBackup(JSON.parse(JSON.stringify(db))).tasks[0].detailAttachments[0].data"),'aGk=');
 run("detailDraft(db.tasks[0]).text='';saveTaskDetail()");assert.ok(element('task-page').innerHTML.includes('任务详细'));assert.equal(run("db.tasks[0].detailAttachments.length"),1);
});
test('任务详细附件导入校验',()=>{
 assert.throws(()=>run("(()=>{const x=JSON.parse(JSON.stringify(db));x.tasks[0].detailAttachments=[{id:'bad',name:'bad.txt',size:9,data:'aGk='}];return validateBackup(x)})()"));
});


test('任务详细默认阅读，编辑后可取消，保存返回阅读',()=>{
 run("ui.task='t1';ui.page='task';commentDraft.items.delete('detail:t1');render()");
 assert.ok(!element('task-page').innerHTML.includes('id="task-detail-form"'));
 button('detail-edit');assert.ok(element('task-page').innerHTML.includes('id="task-detail-form"'));
 run("detailDraft(db.tasks[0]).text='取消的编辑'");button('detail-cancel');
 assert.notEqual(run("db.tasks[0].description"),'取消的编辑');assert.ok(!element('task-page').innerHTML.includes('id="task-detail-form"'));
 button('detail-edit');run("detailDraft(db.tasks[0]).text='阅读模式验收';saveTaskDetail()");
 assert.equal(run("db.tasks[0].description"),'阅读模式验收');assert.ok(!element('task-page').innerHTML.includes('id="task-detail-form"'));
});

test('多标签页并发修改不会覆盖较新数据',()=>{const stored=JSON.parse(data.get('projectboard.preview.v2'));stored.revision+=5;data.set('projectboard.preview.v2',JSON.stringify(stored));assert.equal(run('save()'),false);assert.equal(JSON.parse(data.get('projectboard.preview.v2')).revision,stored.revision)});
test('HTML 无 CDN、外部脚本与远程字体',()=>{assert.ok(!/<script[^>]+src=|<link[^>]+href=|@import|url\(https?:/i.test(html))});
fs.writeFileSync(__dirname+'/test-results.json',JSON.stringify({at:new Date().toISOString(),mode:'Node VM functional checks, separate from browser acceptance',cases},null,2));
console.log(`${cases.filter(t=>t.status==='PASS').length}/${cases.length} checks passed`);
