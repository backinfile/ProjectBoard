'use strict';

const paths = {
  layout: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18M9 9h12"/>',
  home: '<path d="m3 10 9-7 9 7v10a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1Z"/><path d="M9 21v-9h6v9"/>',
  search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  chevron: '<path d="m9 18 6-6-6-6"/>',
  down: '<path d="m6 9 6 6 6-6"/>',
  tree: '<rect x="3" y="3" width="6" height="6" rx="1"/><rect x="15" y="15" width="6" height="6" rx="1"/><path d="M6 9v9h9M6 12h12v3"/>',
  tag: '<path d="m20 13-7 7a2 2 0 0 1-3 0l-8-8V2h10l8 8a2 2 0 0 1 0 3Z"/><path d="M7 7h.01"/>',
  settings: '<path d="M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Z"/><path d="m9 3 1-1h4l1 3 3 1 3 1v4l-2 2-1 3 1 3-3 2-3-1-3 1-3-2 1-3-1-3-2-2V7l3-1Z"/>',
  trash: '<path d="M3 6h18M9 6V4h6v2M5 6l1 14h12l1-14M10 10v6M14 10v6"/>',
  close: '<path d="m18 6-12 12M6 6l12 12"/>',
  list: '<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>',
  board: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18M15 3v18"/>',
  link: '<path d="M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-2 2M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l2-2"/>',
  copy: '<rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/>',
  history: '<path d="M3 12a9 9 0 1 0 3-7L3 8M3 3v5h5M12 7v5l4 2"/>',
  pin: '<path d="m16 9 3-3-1-1-3 3-5-3-5 5 3 5-3 3 1 1 3-3 5 3 5-5ZM2 22l6-6"/>',
  arrow: '<path d="M12 19V5m-7 7 7-7 7 7"/>',
  download: '<path d="M21 15v5H3v-5M12 3v12m-5-5 5 5 5-5"/>',
  upload: '<path d="M21 15v5H3v-5M12 15V3m-5 5 5-5 5 5"/>',
  check: '<path d="m20 6-11 11-5-5"/>',
  folder: '<path d="M3 7V4h6l2 3h10v13H3Z"/>',
  logout: '<path d="M9 21H3V3h6M9 12h12m-4-4 4 4-4 4"/>',
  moon: '<path d="M20.9 13a9 9 0 1 1-9.9-9.9A7 7 0 0 0 20.9 13Z"/>',
  archive: '<rect x="3" y="3" width="18" height="4" rx="1"/><path d="M5 7v14h14V7M10 11h4"/>',
  edit: '<path d="m16 3 5 5-13 13H3v-5ZM14 5l5 5"/>',
  menu: '<path d="M3 6h18M3 12h18M3 18h18"/>',
  circle: '<circle cx="12" cy="12" r="9"/>',
  move: '<path d="M12 3v18M3 12h18M8 7l4-4 4 4M8 17l4 4 4-4M7 8l-4 4 4 4M17 8l4 4-4 4"/>'
};
const I = n => `<svg viewBox="0 0 24 24" aria-hidden="true">${paths[n] || paths.circle}</svg>`;
const E = v => String(v ?? '').replace(/[&<>"']/g, c => ({
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;'
})[c]);
const uid = () => typeof crypto.randomUUID==='function'?crypto.randomUUID():Array.from(crypto.getRandomValues(new Uint8Array(16)),b=>b.toString(16).padStart(2,'0')).join(''),
  now = () => new Date().toISOString(),
  today = () => new Date().toLocaleDateString('en-CA'),
  key = 'projectboard.local.v1';
const baseStatuses = () => [{
  id: 'created',
  name: '创建',
  kind: 'todo'
}, {
  id: 'doing',
  name: '进行中',
  kind: 'active'
}, {
  id: 'done',
  name: '已完成',
  kind: 'done'
}, {
  id: 'cancelled',
  name: '已取消',
  kind: 'cancelled'
}];
let db = {
  schema: 2,
  revision: 0,
  theme: 'light',
  projects: [],
  tasks: [],
  nodes: []
};
let ui = {
  page: 'project',
  tab: 'tasks',
  project: 'p1',
  node: 'n8',
  view: 'board',
  query: '',
  status: 'all',
  priority: 'all',
  sort: 'manual',
  tags: [],
  tagMode: 'all',
  selected: new Set(),
  collapsed: new Set(),
  task: null
};
try {
  Object.assign(ui, JSON.parse(sessionStorage.getItem(key + '.ui')) || {});
} catch {}
ui.selected = new Set();
ui.collapsed = new Set();
let logged = false;
let draftTimer,
  activeDraft = null,
  blocked = false;
// 持久化成功后再推进修订号，失败时保留可重试状态。

function persistUI() {
  const {
    selected,
    collapsed,
    ...s
  } = ui;
  sessionStorage.setItem(key + '.ui', JSON.stringify(s));
}
function toast(msg, duration = 2600) {
  const t = document.getElementById('toast');
  t.className = 'toast';
  t.textContent = msg;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => {
    t.className = '';
    t.textContent = '';
  }, duration);
}
function nodes(project = ui.project) {
  return db.nodes.filter(n => n.project === project && !n.deleted);
}
function tasks(project = ui.project) {
  return db.tasks.filter(t => t.project === project && !t.deleted);
}
function node(id) {
  return db.nodes.find(n => n.id === id);
}
function address(n) {
  let parts = [],
    seen = new Set();
  while (n) {
    if (seen.has(n.id)) return '[循环]';
    seen.add(n.id);
    parts.unshift(n.name);
    n = node(n.parent);
  }
  return parts.join('.');
}
function subtree(id) {
  let ids = new Set([id]),
    changed = true;
  while (changed) {
    changed = false;
    db.nodes.forEach(n => {
      if (ids.has(n.parent) && !ids.has(n.id)) {
        ids.add(n.id);
        changed = true;
      }
    });
  }
  return [...ids];
}
function status(t) {
  return db.projects.find(p => p.id === t.project)?.statuses.find(s => s.id === t.status) || {
    name: '创建',
    kind: 'todo'
  };
}
function button(action, text, icon, cls = '', data = '') {
  return `<button type="button" data-action="${action}" class="${cls}" ${data}>${icon ? I(icon) : ''}${text}</button>`;
}
function iconBtn(a, i, title, data = '') {
  return `<button type="button" data-action="${a}" title="${title}" aria-label="${title}" ${data}>${I(i)}</button>`;
}
function chips(tags) {
  return tags.map(t => `<span class="pill">${I('tag')}${E(t)}</span>`).join('');
}
function empty(msg) {
  return `<div class="empty">${I('tree')}<p>${msg}</p></div>`;
}
function render() {
  if (ui.status !== 'all' && !p().statuses.some(s => s.id === ui.status)) ui.status = 'all';
  syncTaskRoute();
  if (ui.page === 'tags') {
    ui.page = 'project';
    ui.tab = 'tags';
  }
  if (!db.projects.find(x => x.id === ui.project)) ui.project = db.projects[0]?.id;
  document.body.classList.toggle('dark', db.theme === 'dark');
  persistUI();
  if (!logged) {
    renderLogin();
    return;
  }
  const project = p(),
    isProject = ui.page === 'project' || ui.page === 'task',
    pageName = {
      work: '我的工作',
      tags: '知识标签',
      trash: '回收站',
      settings: '设置'
    }[ui.page] || '';
  document.getElementById('root').innerHTML = `<div class="app"><aside class="sidebar" id="sidebar"><div class="workspace-selector"><button class="workspace-button" data-action="nav-work" aria-label="ProjectBoard 工作空间">${I('layout')}<span>ProjectBoard</span>${I('down')}</button><button class="avatar" data-action="nav-settings" title="管理员设置" aria-label="管理员设置">AD</button></div>${button('search', '搜索 <span class="kbd">Ctrl K</span>', 'search', 'sidebar-search')}<div class="side-section">工作空间${I('down')}</div>${button('nav-work', '我的工作', 'home', 'nav ' + (ui.page === 'work' ? 'active' : ''))}${button('nav-projects', '所有项目', 'folder', 'nav ' + (ui.page === 'projects' ? 'active' : ''))}<div class="project-group"><div class="side-section">项目${iconBtn('project-new', 'plus', '新建项目')}</div>${db.projects.filter(p => !p.archived).map(pr => `<button class="nav project-nav ${isProject && ui.project === pr.id ? 'active' : ''}" data-action="project-switch" data-id="${pr.id}"><span class="project-icon">${I('folder')}</span>${E(pr.name)}<span class="count">${tasks(pr.id).filter(t => ['unset', 'todo', 'active'].includes(status(t).kind)).length}</span></button>`).join('')}</div><div class="side-bottom">${button('nav-trash', '回收站', 'trash', 'nav ' + (ui.page === 'trash' ? 'active' : ''))}${button('nav-settings', '设置', 'settings', 'nav ' + (ui.page === 'settings' ? 'active' : ''))}<div class="sidebar-foot"><span><span class="online"></span>本地运行</span>${iconBtn('logout', 'logout', '退出登录')}</div></div></aside><main class="main"><header class="topbar">${iconBtn('menu', 'menu', '展开导航', 'class="mobile-menu"')}<div class="breadcrumbs"><span class="muted">工作空间</span>${I('chevron')}<span>${E(isProject ? project.name : ui.page === 'projects' ? '所有项目' : pageName)}</span></div>${isProject ? `<nav class="tabs" aria-label="项目视图">${[['overview', '概览', 'home'], ['tasks', '任务', 'board'], ['knowledge', '知识库', 'tree'], ['tags', '标签', 'tag'], ['mcp', 'MCP', 'link']].map(([id, name, icon]) => button('tab', name, icon, (ui.page === 'task' ? id === 'tasks' : ui.tab === id) ? 'active' : '', `data-id="${id}" aria-pressed="${ui.page === 'task' ? id === 'tasks' : ui.tab === id}"`)).join('')}</nav>` : ''}<div class="right">${isProject ? iconBtn('project-edit', 'settings', '项目设置') : ''}${notificationButton()}${iconBtn('search', 'search', '搜索工作空间')}<span class="preview-label">本地保存</span></div></header><div class="content ${ui.page === 'task' ? 'task-content' : isProject ? 'project-content' : ''}">${ui.page === 'task' ? '<div id="task-page"></div>' : isProject ? projectView() : ui.page === 'projects' ? projectsView() : ui.page === 'work' ? workView() : ui.page === 'tags' ? tagsView() : ui.page === 'trash' ? trashView() : settingsView()}</div></main></div>`;
  document.getElementById('overlay').innerHTML = '';
  if (ui.page === 'task') renderTask();
  document.getElementById('root').inert = !!document.getElementById('dialog').innerHTML;
  document.getElementById('overlay').inert = !!document.getElementById('dialog').innerHTML;
}
function projectsView() {
  return `<div class="heading"><div><h1>所有项目</h1><p>管理本地工作空间中的项目。</p></div>${button('project-new', '新建项目', 'plus', 'primary')}</div><div class="scroll-table"><table class="table"><thead><tr><th>项目</th><th>简介</th><th>进行中</th><th>知识节点</th><th></th></tr></thead><tbody>${db.projects.filter(pr => !pr.archived).map(pr => `<tr><td>${button('project-switch', E(pr.name), 'folder', '', `data-id="${pr.id}"`)}</td><td class="muted">${E(pr.description)}</td><td>${tasks(pr.id).filter(t => status(t).kind === 'active').length}</td><td>${nodes(pr.id).length}</td><td>${button('project-switch', '打开', 'chevron', '', `data-id="${pr.id}"`)}</td></tr>`).join('')}</tbody></table></div>`;
}
function projectView() {
  return `<div class="project-surface ${ui.tab}-surface">${ui.tab === 'overview' ? overview() : ui.tab === 'tasks' ? taskView() : ui.tab === 'tags' ? tagsView() : ui.tab === 'mcp' ? mcpView() : knowledgeView()}</div>`;
}
function overview() {
  const ts = tasks(),
    done = ts.filter(t => status(t).kind === 'done').length;
  return `<div class="overview"><section><div class="stats"><div><small>全部任务</small><strong>${ts.length}</strong></div><div><small>进行中</small><strong>${ts.filter(t => status(t).kind === 'active').length}</strong></div><div><small>知识节点</small><strong>${nodes().length}</strong></div></div><div class="heading"><h2>正在进行</h2>${button('tab', '全部任务', 'chevron', '', `data-id="tasks"`)}</div>${ts.filter(t => status(t).kind === 'active').map(t => `<div class="plain-row"><span class="dot active"></span><span>${E(t.title)}</span>${button('task-open', '查看', 'chevron', '', `data-id="${t.id}"`)}</div>`).join('') || empty('选择任务，开始推进')}<div class="node-section"><h3>项目进度</h3><p class="muted">${done} / ${ts.filter(t => status(t).kind !== 'cancelled').length} 个任务已完成</p><div class="progress"><span style="width:${done / Math.max(1, ts.filter(t => status(t).kind !== 'cancelled').length) * 100}%"></span></div></div></section><aside><div class="caption">置顶知识</div>${nodes().filter(n => n.pinned).map(n => `<button class="child-row" data-action="node-open" data-id="${n.id}">${I('pin')}${E(n.name)}${I('chevron')}</button>`).join('') || empty('置顶常用知识，便于快速打开')}<div class="node-section"><div class="caption">项目目录</div><code>${E(p().directory || '设置项目目录')}</code></div></aside></div>`;
}
function taskView(all = false) {
  const ts = filteredTasks(all);
  return `<div class="toolbar"><input class="filter-input" id="task-filter" aria-label="筛选任务" placeholder="筛选任务…" value="${E(ui.query)}"><select id="status-filter" aria-label="状态筛选"><option value="all">所有状态</option>${taskColumns().map(s => `<option value="${s.id}" ${ui.status === s.id ? 'selected' : ''}>${E(s.name)}</option>`).join('')}</select><select id="priority-filter" aria-label="优先级筛选"><option value="all">所有优先级</option><option value="" ${ui.priority === '' ? 'selected' : ''}>待设优先级</option>${priorityValues().map(x => `<option ${ui.priority === x ? 'selected' : ''}>${x}</option>`).join('')}</select><select id="task-sort" aria-label="任务排序">${[['manual', '手动排序'], ['priority', '优先级'], ['due', '截止日期']].map(([v, l]) => `<option value="${v}" ${ui.sort === v ? 'selected' : ''}>${l}</option>`).join('')}</select><span class="spacer"></span><small>${ts.length} 个任务</small>${!all ? `<div class="segmented">${button('view', '看板', 'board', ui.view === 'board' ? 'active' : '', 'data-id="board"')}${button('view', '列表', 'list', ui.view === 'list' ? 'active' : '', 'data-id="list"')}${button('view', '时间线', 'history', ui.view === 'timeline' ? 'active' : '', 'data-id="timeline"')}</div>` : ''}${!all ? iconBtn('status-manage', 'settings', '管理状态') : ''}${button('task-new', '新建任务', 'plus', 'primary')}</div>${ui.view === 'timeline' && !all ? timelineView(ts) : ui.view === 'board' && !all ? `<div class="board">${taskColumns().map(s => `<section class="column" data-drop="${s.id}"><div class="column-head"><span class="dot ${s.kind}"></span>${E(s.name)} <small>${ts.filter(t => t.status === s.id).length}</small>${iconBtn('task-new', 'plus', '新增任务', `data-status="${s.id}"`)}</div><div class="column-body">${ts.filter(t => t.status === s.id).map(taskCard).join('')}${button('task-new', '添加任务', 'plus', 'add-card', `data-status="${s.id}"`)}</div></section>`).join('')}</div>` : taskTable(ts)}`;
}
function taskCard(t) {
  return `<button draggable="true" data-drag="${t.id}" data-action="task-open" data-id="${t.id}" class="task-card"><div class="task-meta"><span>${E(p().code)}-${t.num}</span>${/^#[0-9a-f]{6}$/i.test(t.color || '') ? `<span class="task-color" style="background:${t.color}"></span>` : ''}</div><h3>${E(t.title)}</h3>${t.tags.length ? `<div class="task-meta">${chips(t.tags)}</div>` : ''}<div class="task-card-footer">${t.priority ? `<span class="priority">${E(t.priority)}</span>` : ''}${t.due ? `<span>${E(t.due)}</span>` : ''}${taskHas(t, 'progress') ? `<span>${t.progress}%</span>` : ''}<span class="spacer"></span>${t.knowledge.length ? I('link') + ' ' + t.knowledge.length : ''}${t.checks.length ? `<span>${t.checks.filter(c => c.done).length}/${t.checks.length}</span>` : ''}</div></button>`;
}
function taskTable(ts) {
  const fields = taskFields().filter(f => !['description', 'checks', 'notes', 'knowledge', 'relations'].includes(f.key) && ts.some(t => taskHas(t, f.key)));
  return ts.length ? `<div class="scroll-table"><table class="table"><thead><tr><th>编号</th><th>任务</th>${fields.map(f => `<th>${f.label}</th>`).join('')}</tr></thead><tbody>${ts.map(t => `<tr tabindex="0" data-task="${t.id}" data-action="task-open" data-id="${t.id}"><td class="muted">${E(db.projects.find(p => p.id === t.project)?.code)}-${t.num}</td><td class="title">${E(t.title)}</td>${fields.map(f => `<td>${taskHas(t, f.key) ? f.key === 'tags' ? chips(t.tags) : E(taskFieldText(t, f)) : '—'}</td>`).join('')}</tr>`).join('')}</tbody></table></div>` : empty('调整筛选，查看任务');
}
function treeRows(parent = null, depth = 0) {
  return nodes().filter(n => n.parent === parent).sort((a, b) => a.order - b.order).map(n => {
    const has = nodes().some(x => x.parent === n.id),
      closed = ui.collapsed.has(n.id);
    return `<div class="tree-row ${ui.node === n.id ? 'active' : ''}" style="padding-left:${depth * 15}px"><button class="toggle" data-action="tree-toggle" data-id="${n.id}" aria-label="${closed ? '展开' : '折叠'} ${E(n.name)}">${has ? I(closed ? 'chevron' : 'down') : ''}</button><button class="node-name" data-action="node-open" data-id="${n.id}">${I('tree')}<span>${E(n.name)}</span></button>${n.pinned ? '<small class="pin">' + I('pin') + '</small>' : ''}</div>${has && !closed ? treeRows(n.id, depth + 1) : ''}`;
  }).join('');
}
function knowledgeView() {
  let n = node(ui.node);
  if (!n || n.deleted || n.project !== ui.project) {
    n = nodes()[0];
    ui.node = n?.id;
  }
  return `<div class="toolbar knowledge-toolbar">${iconBtn('tree-mobile', 'tree', '展开知识树', 'class="tree-mobile"')}<div class="address-bar">${I('tree')}<input id="node-address" aria-label="知识节点地址" value="${E(n ? address(n) : '')}" placeholder="输入点分地址，回车定位">${iconBtn('address-go', 'chevron', '按地址定位')}${n ? iconBtn('copy-address', 'copy', '复制完整地址') : ''}</div><span class="spacer"></span><small class="toolbar-count">${nodes().length} 个节点</small>${button('nav-tags', '标签管理', 'tag', 'border')}${button('node-new', '新建节点', 'plus', 'primary')}</div><div class="knowledge"><aside class="tree-panel"><div class="tree-tools"><span>知识结构</span>${iconBtn('node-new', 'plus', '新增根节点')}</div><div class="tree-scroll">${treeRows() || empty('创建第一个知识节点')}</div><div class="tree-foot">${nodes().length} 个节点 · ${allTags().length} 个标签</div></aside><section class="node-panel">${n ? nodeView(n) : empty('从左侧开始构建你的知识树')}</section></div>`;
}
function nodeView(n) {
  const children = nodes().filter(x => x.parent === n.id).sort((a, b) => a.order - b.order),
    linked = tasks().filter(t => t.knowledge.includes(n.id));
  return `<div class="node-layout"><div class="node-main"><div class="node-title"><h2>${E(n.name)}</h2><div class="actions">${iconBtn('node-pin', 'pin', n.pinned ? '取消置顶' : '置顶', `data-id="${n.id}"`)}${iconBtn('node-edit', 'edit', '重命名与移动', `data-id="${n.id}"`)}${iconBtn('node-history', 'history', '版本历史', `data-id="${n.id}"`)}${iconBtn('node-delete', 'trash', '删除节点与子节点', `data-id="${n.id}"`)}</div></div><textarea id="node-content" class="node-editor" aria-label="节点内容" placeholder="记录这条知识的内容…">${E(n.content)}</textarea><div class="node-footer"><span id="save-state">已保存</span>${button('node-snapshot', '保存版本', 'check', '', `data-id="${n.id}"`)}</div><div class="node-section"><div class="heading" style="margin-bottom:8px"><span class="caption" style="margin:0">子节点 <small>${children.length}</small></span>${button('node-new', '添加子节点', 'plus', '', `data-parent="${n.id}"`)}</div>${children.map(c => `<button class="child-row" data-action="node-open" data-id="${c.id}">${I('tree')}${E(c.name)}<code>${E(address(c))}</code>${I('chevron')}</button>`).join('') || '<small>添加子节点，细分知识</small>'}</div></div><aside class="node-inspector"><div class="inspector-heading">节点属性</div><section class="inspector-block"><div class="inspector-label">${I('tag')}标签</div><div class="node-tags">${n.tags.map(t => `<button class="pill blue" data-action="tag-focus" data-tag="${E(t)}">${E(t)}</button>`).join('')}${button('node-tags', '添加标签', 'plus', '', `data-id="${n.id}"`)}</div></section><section class="inspector-block"><div class="inspector-label">${I('board')}关联任务 <small>${linked.length}</small>${iconBtn('task-from-node', 'plus', '从知识创建任务', `data-id="${n.id}"`)}</div>${linked.map(t => `<button class="child-row" data-action="task-open" data-id="${t.id}"><span class="dot ${status(t).kind}"></span>${E(t.title)}</button>`).join('') || '<small>点击＋创建关联任务</small>'}</section><section class="inspector-block"><div class="inspector-label">${I('link')}关联知识${iconBtn('node-link', 'plus', '添加关联', `data-id="${n.id}"`)}</div>${nodes().filter(x => n.links.includes(x.id) || x.links.includes(n.id)).map(x => `<div class="plain-row"><button data-action="node-open" data-id="${x.id}"><code>${E(address(x))}</code></button>${iconBtn('node-unlink', 'close', '移除关联', `data-id="${x.id}"`)}</div>`).join('') || '<small>连接相关知识节点</small>'}</section><section class="inspector-block"><div class="inspector-label">${I('folder')}位置</div><div class="node-location">${n.parent ? E(address(node(n.parent))) : '项目根节点'}</div><div class="inspector-actions">${button('node-edit', '移动', 'move', 'border', `data-id="${n.id}"`)}${button('node-up', '上移', 'arrow', 'border', `data-id="${n.id}"`)}${button('node-down', '下移', 'down', 'border', `data-id="${n.id}"`)}</div></section><section class="inspector-block"><div class="inspector-label">${I('history')}最近更新</div><small>${new Date(n.updated).toLocaleString('zh-CN')}</small></section>${button('copy-subtree', '复制子树内容', 'copy', 'border', `data-id="${n.id}"`)}</aside></div>`;
}
function taggedEntities() {
  return [...tasks().map(value => ({
    kind: 'task',
    value
  })), ...nodes().map(value => ({
    kind: 'node',
    value
  }))];
}
function matchingEntities() {
  return taggedEntities().filter(({
    kind,
    value: n
  }) => (!ui.tagType || ui.tagType === 'all' || ui.tagType === kind) && (!ui.tags.length || (ui.tagMode === 'all' ? ui.tags.every(t => n.tags.includes(t)) : ui.tags.some(t => n.tags.includes(t)))) && `${kind === 'node' ? address(n) : n.title} ${n.content || n.description || ''} ${n.tags.join(' ')}`.toLowerCase().includes(ui.query.toLowerCase()));
}
function deleteTagged() {
  const selected = matchingEntities().filter(x => ui.selected.has(x.value.id)),
    ts = selected.filter(x => x.kind === 'task').map(x => x.value),
    ids = [...new Set(selected.filter(x => x.kind === 'node').flatMap(x => subtree(x.value.id)))],
    ns = nodes().filter(x => ids.includes(x.id));
  if (!ts.length && !ns.length) return;
  confirmModal('删除所选内容', `<p>将 ${ts.length} 个任务和 ${ns.length} 个知识节点移入回收站。包含所选知识节点的全部子节点。</p><pre>${E([...ts.map(t => '任务：' + t.title), ...ns.map(n => '知识：' + address(n))].join('\n'))}</pre>`, async () => await applyMutation(() => {
    const batch = uid();
    ts.forEach(t => t.deleted = now());
    ns.forEach(n => {
      n.deleted = now();
      n.deleteBatch = batch;
    });
    ui.selected.clear();
  }), '移入回收站');
}
function taskFields() {
  return [{
    key: 'description',
    label: '任务详细',
    type: 'textarea'
  }, {
    key: 'status',
    label: '状态',
    type: 'status'
  }, {
    key: 'priority',
    label: '优先级',
    type: 'priority'
  }, {
    key: 'start',
    label: '开始日期',
    type: 'date'
  }, {
    key: 'due',
    label: '截止日期',
    type: 'date'
  }, {
    key: 'end',
    label: '结束日期',
    type: 'date'
  }, {
    key: 'progress',
    label: '进度',
    type: 'number'
  }, {
    key: 'reminder',
    label: '提醒时间',
    type: 'datetime-local'
  }, {
    key: 'repeat',
    label: '重复周期',
    type: 'repeat'
  }, {
    key: 'color',
    label: '颜色',
    type: 'color'
  }, {
    key: 'tags',
    label: '标签',
    type: 'tags'
  }, {
    key: 'knowledge',
    label: '关联知识',
    type: 'knowledge'
  }, {
    key: 'relations',
    label: '关联任务',
    type: 'relations'
  }, {
    key: 'checks',
    label: '检查清单',
    type: 'checks'
  }, {
    key: 'notes',
    label: '评论',
    type: 'notes'
  }];
}
function taskHas(t, k) {
  return Array.isArray(t[k]) ? t[k].length > 0 : t[k] !== undefined && t[k] !== null && t[k] !== '';
}
function priorityValues() {
  return ['低', '中', '高', '紧急', '立即处理'];
}
function taskFieldText(t, f) {
  const v = t[f.key];
  if (f.key === 'status') return status(t).name;
  if (f.key === 'progress') return v + '%';
  if (f.key === 'repeat') return {
    daily: '每天',
    weekly: '每周',
    monthly: '每月'
  }[v] || v;
  if (f.key === 'reminder') return String(v).replace('T', ' ');
  if (Array.isArray(v)) return f.key === 'tags' ? v.join('、') : v.length + ' 项';
  return v;
}
function taskFieldEditor(key) {
  const t = db.tasks.find(x => x.id === ui.task && !x.deleted),
    f = taskFields().find(x => x.key === key);
  if (!t || !f) return;
  const val = t[key] ?? '',
    pr = db.projects.find(x => x.id === t.project);
  if (key === 'knowledge') {
    actions['task-link']();
    return;
  }
  if (['checks', 'notes'].includes(key)) {
    formModal('添加' + f.label, `<label>内容<textarea name="value" required rows="4"></textarea></label>`, async form => {
      const text = form.get('value').trim();
      if (!text) throw Error('请输入内容');
      if (await applyMutation(() => t[key].push(key === 'checks' ? {
        id: uid(),
        text,
        done: false
      } : {
        id: uid(),
        text,
        at: now()
      }))) closeModal();
    });
    return;
  }
  if (key === 'relations') {
    formModal('关联任务', `<label>关系<select name="relation"><option value="related">相关任务</option><option value="blocks">阻塞此任务</option><option value="subtask">子任务</option></select></label><label style="margin-top:16px">任务<select name="value" required><option value="">选择当前项目任务</option>${tasks(t.project).filter(x => x.id !== t.id && !(t.relations || []).some(r => r.id === x.id)).map(x => `<option value="${x.id}">${E(pr.code + '-' + x.num + ' ' + x.title)}</option>`).join('')}</select></label>`, async form => {
      const id = form.get('value');
      if (!tasks(t.project).some(x => x.id === id && id !== t.id)) throw Error('请选择当前项目中的其他任务');
      if (await applyMutation(() => {
        t.relations ??= [];
        t.relations.push({
          id,
          type: form.get('relation')
        });
      })) closeModal();
    });
    return;
  }
  let input;
  if (['status', 'priority', 'repeat'].includes(f.type)) {
    const options = f.type === 'status' ? pr.statuses.map(s => [s.id, s.name]) : f.type === 'priority' ? priorityValues().map(v => [v, v]) : [['daily', '每天'], ['weekly', '每周'], ['monthly', '每月']];
    input = `<select name="value" required><option value="">请选择</option>${options.map(([v, l]) => `<option value="${E(v)}" ${v === val ? 'selected' : ''}>${E(l)}</option>`).join('')}</select>`;
  } else if (f.type === 'textarea') input = `<textarea name="value" rows="7" required>${E(val)}</textarea>`;else input = `<input name="value" type="${['tags', 'color'].includes(f.type) ? 'text' : f.type}" ${key === 'progress' ? 'min="0" max="100" step="1"' : ''} value="${E(Array.isArray(val) ? val.join(', ') : val)}" ${key === 'tags' ? 'list="field-tags" placeholder="逗号分隔"' : key === 'color' ? 'placeholder="#5269b8" pattern="#[0-9a-fA-F]{6}"' : ''} required>`;
  formModal('设置' + f.label, `<label>${f.label}${input}</label><datalist id="field-tags">${allTags().map(t => `<option value="${E(t)}">`).join('')}</datalist>${['reminder', 'repeat'].includes(key) ? '<div class="note"></div>' : ''}`, async form => {
    let value = form.get('value');
    if (key === 'tags') value = parseTags(value);else if (key === 'progress') {
      if (value === '') throw Error('请填写进度');
      value = Number(value);
      if (!Number.isInteger(value) || value < 0 || value > 100) throw Error('请填写 0–100 的整数');
    } else value = value.trim();
    if (!value && value !== 0) throw Error('请填写字段值');
    if (key === 'color' && !/^#[0-9a-f]{6}$/i.test(value)) throw Error('颜色格式应为 #RRGGBB');
    const candidate = {
      ...t,
      [key]: value
    };
    validateTaskFields(candidate);
    if (candidate.start && candidate.end && candidate.start > candidate.end) throw Error('请选择开始日期当天或之后的结束日期');
    if (await applyMutation(() => {
      t[key] = value;
      t.updated = now();
    })) closeModal();
  });
}
function taskFieldPicker() {
  const t = db.tasks.find(x => x.id === ui.task);
  modal('添加字段', `<div class="field-picker">${taskFields().filter(f => !['notes', 'description'].includes(f.key) && !taskHas(t, f.key)).map(f => button('task-field-edit', f.label, 'plus', 'border', `data-field="${f.key}"`)).join('') || empty('所有字段均已设置')}</div>`);
}
function clearTaskField(key) {
  const t = db.tasks.find(x => x.id === ui.task),
    f = taskFields().find(x => x.key === key);
  if (!t || !f) return;
  const clear = async () => await applyMutation(() => {
    t[key] = key === 'status' ? p().initialStatusId || 'created' : Array.isArray(t[key]) ? [] : key === 'progress' ? null : '';
    t.updated = now();
  });
  if (['description', 'checks', 'notes', 'relations', 'knowledge'].includes(key)) confirmModal(f.key === 'status' ? '重置为创建' : '清空' + f.label, `<p>清空此任务的${E(f.label)}。</p>`, clear, '清空');else clear();
}
function syncTaskRoute() {
  if (typeof location === 'undefined') return;
  const hash = ui.page === 'task' ? `#/projects/${ui.project}/tasks/${ui.task}` : ui.page === 'project' ? `#/projects/${ui.project}/${ui.tab}` : `#/${ui.page}`;
  if (location.hash !== hash) history.pushState(null, '', hash);
}
function readTaskRoute() {
  for (const d of commentDraft.items?.values() || []) d.editing = false;
  if (typeof location === 'undefined' || !location.hash) return;
  const parts = location.hash.slice(2).split('/');
  if (parts[0] === 'projects' && parts[1]) {
    ui.project = parts[1];
    if (parts[2] === 'tasks' && parts[3]) {
      ui.page = 'task';
      ui.task = parts[3];
    } else {
      ui.page = 'project';
      ui.tab = ['overview', 'tasks', 'knowledge', 'tags', 'mcp'].includes(parts[2]) ? parts[2] : 'tasks';
      ui.task = null;
    }
  } else if (['work', 'projects', 'settings', 'trash'].includes(parts[0])) {
    ui.page = parts[0];
    ui.task = null;
  }
}
function validateTaskFields(t, projects = db.projects, allTasks = db.tasks) {
  validateComments(t);
  if (t.detailAttachments !== undefined && (!Array.isArray(t.detailAttachments) || t.detailAttachments.length > 8 || !t.detailAttachments.every(attachmentOK) || new Set(t.detailAttachments.map(a => a.id)).size !== t.detailAttachments.length)) throw Error('请检查任务详细附件');
  for (const k of ['start', 'end', 'reminder', 'repeat', 'color']) if (t[k] !== undefined && typeof t[k] !== 'string') throw Error('请检查任务属性格式');
  if (t.progress != null && (!Number.isInteger(t.progress) || t.progress < 0 || t.progress > 100)) throw Error('请填写 0–100 的整数');
  for (const k of ['start', 'end', 'due']) if (t[k] && (!/^\d{4}-\d{2}-\d{2}$/.test(t[k]) || !Number.isFinite(Date.parse(t[k])) || new Date(t[k]).toISOString().slice(0, 10) !== t[k])) throw Error('请选择有效日期');
  if (t.start && t.end && t.start > t.end) throw Error('请选择开始日期当天或之后的结束日期');
  if (t.reminder && (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(t.reminder) || !Number.isFinite(Date.parse(t.reminder)))) throw Error('请选择有效的提醒时间');
  if (t.repeat && !['daily', 'weekly', 'monthly'].includes(t.repeat)) throw Error('请选择每天、每周或每月');
  if (t.color && !/^#[0-9a-f]{6}$/i.test(t.color)) throw Error('请填写六位十六进制颜色，如 #2563eb');
  if (t.relations !== undefined && (!Array.isArray(t.relations) || t.relations.some(r => !r || !['related', 'blocks', 'subtask'].includes(r.type) || r.id === t.id || !allTasks.some(other => other.id === r.id && other.project === t.project)) || new Set(t.relations.map(r => r.id)).size !== t.relations.length)) throw Error('请检查任务关联');
}
function exportTaskCSV() {
  const safe = v => {
    let s = String(v ?? '');
    if (/^[=+@-]/.test(s)) s = "'" + s;
    return '"' + s.replaceAll('"', '""') + '"';
  };
  const fs = taskFields(),
    rows = [['编号', '标题', ...fs.map(f => f.label)], ...tasks().map(t => [p().code + '-' + t.num, t.title, ...fs.map(f => !taskHas(t, f.key) ? '' : Array.isArray(t[f.key]) && f.key !== 'tags' ? JSON.stringify(t[f.key]) : taskFieldText(t, f))])];
  download('projectboard-tasks.csv', '\ufeff' + rows.map(r => r.map(safe).join(',')).join('\r\n'), 'text/csv;charset=utf-8');
  toast('当前项目任务已导出');
}
function commentDraft(id) {
  commentDraft.items ??= new Map();
  if (!commentDraft.items.has(id)) commentDraft.items.set(id, {
    text: sessionStorage.getItem(key + '.comment.' + id) || '',
    files: [],
    reply: null,
    loading: false
  });
  return commentDraft.items.get(id);
}
function validateComments(t) {
  const ids = new Set();
  for (const n of t.notes || []) {
    if (ids.has(n.id)) throw Error('请为每条评论设置唯一 ID');
    ids.add(n.id);
    if (n.attachments !== undefined && (!Array.isArray(n.attachments) || n.attachments.length > 8 || new Set(n.attachments.map(a => a.id)).size !== n.attachments.length || !n.attachments.every(attachmentOK))) throw Error('请检查评论附件');
    if (n.replyTo !== undefined && n.replyTo !== null && !t.notes.some(x => x.id === n.replyTo && x.id !== n.id)) throw Error('请检查评论回复对象');
  }
}
function commentView(t) {
  const d = commentDraft(t.id),
    reply = t.notes.find(n => n.id === d.reply);
  return `<section class="task-conversation" aria-label="任务评论"><div class="heading"><h2>评论 <small>${t.notes.filter(n => !n.deleted).length}</small></h2><small>按时间顺序</small></div><div class="comment-stream">${t.notes.map(n => {
    if (n.deleted) return `<div class="comment-deleted">评论已删除 ${button('comment-restore', '恢复', 'history', '', `data-id="${n.id}"`)}</div>`;
    const parent = t.notes.find(x => x.id === n.replyTo);
    return `<article class="comment-item"><span class="comment-avatar">AD</span><div class="comment-body"><div class="comment-meta"><strong>管理员</strong><time>${new Date(n.at).toLocaleString('zh-CN')}</time>${n.edited ? '<small>已编辑</small>' : ''}<span class="spacer"></span>${iconBtn('comment-edit', 'edit', '编辑评论', `data-id="${n.id}"`)}${iconBtn('comment-delete', 'trash', '删除评论', `data-id="${n.id}"`)}</div>${parent ? `<blockquote>回复：${E(parent.deleted ? '评论已删除' : parent.text || '附件')}</blockquote>` : ''}${n.text ? `<p class="comment-text">${E(n.text)}</p>` : ''}<div class="comment-attachments">${(n.attachments || []).map(a => attachmentHTML(a, n.id)).join('')}</div><div class="comment-actions">${button('comment-reply', '回复', null, '', `data-id="${n.id}"`)}${n.text ? button('note-to-node', '写入知识节点', 'tree', '', `data-id="${n.id}"`) : ''}</div></div></article>`;
  }).join('') || '<div class="comment-empty">写下第一条评论</div>'}</div><form id="comment-form" class="comment-composer">${reply ? `<div class="comment-reply-to">回复：${E(reply.deleted ? '评论已删除' : reply.text || '附件')}${iconBtn('comment-cancel-reply', 'close', '取消回复')}</div>` : ''}<textarea id="comment-text" aria-label="评论内容" placeholder="写下评论，或拖入文件…" rows="4">${E(d.text)}</textarea><div class="comment-pending">${d.files.map(a => `<span class="pill">${E(a.name)}${iconBtn('comment-file-remove', 'close', '移除待发送附件 ' + E(a.name), `data-id="${a.id}"`)}</span>`).join('')}</div><div class="comment-composer-footer">${button('comment-attach', d.loading ? '正在读取…' : '添加附件', 'upload', '', d.loading ? 'disabled' : '')}<span class="spacer"></span><button type="submit" class="primary" ${d.loading ? 'disabled' : ''}>发表评论</button></div><small class="comment-limit">附件：20 MB / 个 · 每条最多 8 个</small></form></section>`;
}
async function postComment() {
  const t = db.tasks.find(t => t.id === ui.task && !t.deleted),
    d = commentDraft(ui.task);
  if (!t || d.loading) return;
  if (!d.files.every(attachmentOK)) {
    toast('请重新选择附件');
    return;
  }
  const text = d.text.trim();
  if (!text && !d.files.length) {
    toast('请输入评论或添加附件');
    return;
  }
  if (d.reply && !t.notes.some(n => n.id === d.reply)) {
    toast('请重新选择回复对象');
    return;
  }
  if (await applyMutation(() => {
    t.notes.push({
      id: uid(),
      text,
      at: now(),
      replyTo: d.reply,
      attachments: d.files.map(a => ({
        ...a
      }))
    });
    t.updated = now();
  })) {
    commentDraft.items.delete(t.id);
    sessionStorage.removeItem(key + '.comment.' + t.id);
    render();
    toast('评论已发表');
  }
}
function bindComments(t) {
  const form = document.getElementById('comment-form'),
    input = document.getElementById('comment-text');
  if (!form || !input) return;
  input.oninput = () => {
    commentDraft(t.id).text = input.value;
    try {
      sessionStorage.setItem(key + '.comment.' + t.id, input.value);
    } catch {}
  };
  form.onsubmit = async e => {
    e.preventDefault();
    commentDraft(t.id).text = input.value;
    await postComment();
  };
  input.onkeydown = async e => {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault();
      commentDraft(t.id).text = input.value;
      await postComment();
    }
  };
  form.ondragover = e => {
    if (Array.from(e.dataTransfer.types).includes('Files')) {
      e.preventDefault();
      form.classList.add('drag-over');
    }
  };
  form.ondragleave = () => form.classList.remove('drag-over');
  form.ondrop = async e => {
    if (e.dataTransfer.files.length) {
      e.preventDefault();
      e.stopPropagation();
      form.classList.remove('drag-over');
      await addCommentFiles(t.id, e.dataTransfer.files);
    }
  };
  input.onpaste = async e => {
    if (e.clipboardData.files.length) {
      e.preventDefault();
      await addCommentFiles(t.id, e.clipboardData.files);
    }
  };
}
function editComment(id) {
  const t = db.tasks.find(t => t.id === ui.task),
    n = t.notes.find(n => n.id === id && !n.deleted);
  if (!n) return;
  formModal('编辑评论', `<label>评论内容<textarea name="text" rows="6">${E(n.text)}</textarea></label>`, async f => {
    const text = f.get('text').trim();
    if (!text && !n.attachments?.length) throw Error('请填写评论或添加附件');
    if (await applyMutation(() => {
      n.text = text;
      n.edited = now();
    })) closeModal();
  });
}
function timelineView(ts) {
  const todayDate = new Date(),
    first = new Date(todayDate.getFullYear(), todayDate.getMonth() + (ui.timelineOffset || 0), 1),
    last = new Date(first.getFullYear(), first.getMonth() + 1, 0),
    days = last.getDate(),
    month = first.getFullYear() + '-' + String(first.getMonth() + 1).padStart(2, '0'),
    start = month + '-01',
    end = month + '-' + days,
    dated = ts.filter(t => t.start || t.end || t.due),
    none = ts.filter(t => !t.start && !t.end && !t.due),
    visible = dated.filter(t => t.start && t.end && t.start <= end && t.end >= start || [t.start, t.end, t.due].some(d => d && d >= start && d <= end)),
    dayPosition = d => Number(d.slice(8, 10)) - 1;
  return `<div class="toolbar">${iconBtn('timeline-prev', 'chevron', '上个月')}${button('timeline-today', '本月', null, 'border')}<strong>${first.getFullYear()} 年 ${first.getMonth() + 1} 月</strong>${iconBtn('timeline-next', 'chevron', '下个月')}<span class="spacer"></span></div><div class="timeline-scroll"><div class="timeline-grid" style="--days:${days}"><div class="timeline-row timeline-header"><div class="timeline-name">任务</div><div class="timeline-days">${Array.from({
    length: days
  }, (_, i) => `<span>${i + 1}</span>`).join('')}</div></div>${visible.map(t => {
    const a = t.start && t.end ? t.start < start ? 0 : dayPosition(t.start) : 0,
      b = t.start && t.end ? t.end > end ? days : dayPosition(t.end) + 1 : 0;
    return `<div class="timeline-row"><button class="timeline-name" data-action="task-open" data-id="${t.id}">${E(t.title)}</button><div class="timeline-track">${t.start && t.end ? `<button class="timeline-bar" style="left:calc(${a} * var(--day));width:calc(${b - a} * var(--day))" data-action="task-open" data-id="${t.id}" aria-label="${E(t.title)} ${t.start} 至 ${t.end}">${E(t.start)} → ${E(t.end)}</button>` : ''}${[['start', '始'], ['end', '终'], ['due', '截']].filter(([k]) => t[k] && t[k] >= start && t[k] <= end).map(([k, l]) => `<button class="timeline-marker ${k}" style="left:calc(${dayPosition(t[k])} * var(--day))" data-action="task-open" data-id="${t.id}" title="${E(t.title)} · ${k === 'due' ? '截止' : k === 'start' ? '开始' : '结束'} ${t[k]}">${l}</button>`).join('')}</div></div>`;
  }).join('')}</div>${!visible.length ? empty('为任务设置日期，安排本月计划') : ''}</div><div class="timeline-unscheduled"><div class="heading"><h3>待安排 <small>${none.length}</small></h3><small>${dated.length - visible.length} 项安排在其他月份</small></div>${none.map(t => button('task-open', E(t.title), 'circle', 'border', `data-id="${t.id}"`)).join('') || '<small>任务日期已安排</small>'}</div>`;
}
function detailDraft(t) {
  const d = commentDraft('detail:' + t.id);
  if (!d.detailsInit) {
    if (sessionStorage.getItem(key + '.comment.detail:' + t.id) === null) d.text = t.description || '';
    d.detailsInit = true;
  }
  return d;
}
function taskDetailView(t) {
  const d = detailDraft(t);
  if (!d.editing) return `<section class="task-fixed-detail" aria-label="任务详细"><div class="heading"><h2>任务详细</h2>${button('detail-edit', '编辑', 'edit', '', `aria-label="编辑任务详细"`)}</div>${t.description ? `<p class="task-detail-reading">${E(t.description)}</p>` : '<p class="muted">点击编辑，补充任务详细</p>'}<div class="comment-attachments">${(t.detailAttachments || []).map(a => attachmentHTML(a, 'task-detail').replace('comment-download', 'detail-download')).join('')}</div></section>`;
  return `<section class="task-fixed-detail" aria-label="任务详细"><div class="heading"><h2>任务详细</h2></div><form id="task-detail-form" class="comment-composer"><textarea id="task-detail-text" aria-label="任务详细内容" placeholder="填写任务目标与要求…" rows="4">${E(d.text)}</textarea><div class="comment-attachments">${(t.detailAttachments || []).map(a => attachmentHTML(a, 'task-detail').replace('comment-download', 'detail-download')).join('')}</div><div class="comment-pending">${d.files.map(a => `<span class="pill">${E(a.name)}${iconBtn('detail-pending-remove', 'close', '移除待保存附件 ' + E(a.name), `data-id="${a.id}"`)}</span>`).join('')}</div><div class="comment-composer-footer">${button('detail-attach', d.loading ? '正在读取…' : '添加附件', 'upload', '', d.loading ? 'disabled' : '')}<span class="spacer"></span>${button('detail-cancel', '取消', null, 'border', d.loading ? 'disabled' : '')}<button type="submit" class="primary" ${d.loading ? 'disabled' : ''}>保存任务详细</button></div><small class="comment-limit">附件：20 MB / 个 · 最多 8 个</small></form></section>`;
}
async function saveTaskDetail() {
  const t = db.tasks.find(t => t.id === ui.task && !t.deleted),
    d = t && detailDraft(t);
  if (!t || d.loading) return;
  if (!d.files.every(attachmentOK)) return toast('请重新选择附件');
  if (await applyMutation(() => {
    t.description = d.text;
    t.detailAttachments = [...(t.detailAttachments || []), ...d.files.map(a => ({
      ...a
    }))];
    t.updated = now();
  })) {
    commentDraft.items.delete('detail:' + t.id);
    sessionStorage.removeItem(key + '.comment.detail:' + t.id);
    render();
    toast('任务详细已保存');
  }
}
function bindTaskDetail(t) {
  const form = document.getElementById('task-detail-form'),
    input = document.getElementById('task-detail-text');
  if (!form || !input) return;
  input.oninput = () => {
    detailDraft(t).text = input.value;
    try {
      sessionStorage.setItem(key + '.comment.detail:' + t.id, input.value);
    } catch {}
  };
  form.onsubmit = async e => {
    e.preventDefault();
    detailDraft(t).text = input.value;
    await saveTaskDetail();
  };
  form.ondragover = e => {
    if (Array.from(e.dataTransfer.types).includes('Files')) {
      e.preventDefault();
      form.classList.add('drag-over');
    }
  };
  form.ondragleave = () => form.classList.remove('drag-over');
  form.ondrop = async e => {
    if (e.dataTransfer.files.length) {
      e.preventDefault();
      e.stopPropagation();
      form.classList.remove('drag-over');
      await addCommentFiles('detail:' + t.id, e.dataTransfer.files);
    }
  };
  input.onpaste = async e => {
    if (e.clipboardData.files.length) {
      e.preventDefault();
      await addCommentFiles('detail:' + t.id, e.clipboardData.files);
    }
  };
}
function matchingNodes() {
  return nodes().filter(n => (!ui.tags.length || (ui.tagMode === 'all' ? ui.tags.every(t => n.tags.includes(t)) : ui.tags.some(t => n.tags.includes(t)))) && `${address(n)} ${n.content}`.toLowerCase().includes(ui.query.toLowerCase()));
}
function projectSelect() {
  return `<select id="project-scope" aria-label="选择项目">${db.projects.map(pr => `<option value="${pr.id}" ${pr.id === ui.project ? 'selected' : ''}>${E(pr.name)}${pr.archived ? '（已归档）' : ''}</option>`).join('')}</select>`;
}
function tagsView() {
  const entries = matchingEntities();
  return `<div class="toolbar"><input id="tag-query" value="${E(ui.query)}" placeholder="搜索任务、地址或内容" aria-label="搜索标签结果"><select id="tag-type" aria-label="内容类型">${[['all', '全部类型'], ['task', '任务'], ['node', '知识节点']].map(([v, l]) => `<option value="${v}" ${(ui.tagType || 'all') === v ? 'selected' : ''}>${l}</option>`).join('')}</select><select id="tag-mode" aria-label="标签匹配方式"><option value="all" ${ui.tagMode === 'all' ? 'selected' : ''}>匹配所有所选标签</option><option value="any" ${ui.tagMode === 'any' ? 'selected' : ''}>匹配任一所选标签</option></select><span class="spacer"></span>${button('tags-manage', '管理标签', 'settings', 'border')}</div><div class="tag-layout"><aside><div class="caption">项目标签</div>${button('tags-clear', '全部内容 <small>' + taggedEntities().length + '</small>', 'folder', 'tag-filter ' + (!ui.tags.length ? 'active' : ''))}${allTags().map(t => `<button class="tag-filter ${ui.tags.includes(t) ? 'active' : ''}" data-action="tag-toggle" data-tag="${E(t)}">${I('tag')}${E(t)}<small>${taggedEntities().filter(x => x.value.tags.includes(t)).length}</small></button>`).join('') || '<small>为任务和知识添加标签</small>'}</aside><section><div class="bulk"><label style="flex-direction:row;align-items:center;color:var(--text)"><input id="select-all-nodes" aria-label="全选当前结果" type="checkbox" ${entries.length && entries.every(x => ui.selected.has(x.value.id)) ? 'checked' : ''}> 已选 ${ui.selected.size} / ${entries.length}</label><span class="spacer"></span>${button('bulk-tag-add', '添加标签', 'plus', '', ui.selected.size ? '' : 'disabled')}${button('bulk-tag-remove', '移除标签', 'tag', '', ui.selected.size ? '' : 'disabled')}${button('bulk-delete', '删除内容', 'trash', 'danger', ui.selected.size ? '' : 'disabled')}</div>${entries.length ? `<div class="scroll-table"><table class="table"><thead><tr><th></th><th>类型</th><th>任务 / 节点地址</th><th>标签</th></tr></thead><tbody>${entries.map(({
    kind,
    value: n
  }) => `<tr><td><input type="checkbox" data-select-node="${n.id}" aria-label="选择 ${E(kind === 'node' ? address(n) : n.title)}" ${ui.selected.has(n.id) ? 'checked' : ''}></td><td class="muted">${kind === 'node' ? '知识节点' : '任务'}</td><td>${button(kind === 'node' ? 'node-open' : 'task-open', kind === 'node' ? `<code>${E(address(n))}</code>` : E(p().code + '-' + n.num + ' ' + n.title), null, '', `data-id="${n.id}"`)}</td><td>${chips(n.tags)}</td></tr>`).join('')}</tbody></table></div>` : empty('调整类型或标签，查找内容')}</section></div>`;
}
function trashView() {
  const ns = db.nodes.filter(n => n.deleted && n.project === ui.project),
    ts = db.tasks.filter(t => t.deleted && t.project === ui.project),
    roots = ns.filter(n => !ns.some(x => x.id === n.parent));
  return `<div class="heading"><div><h1>回收站</h1></div>${projectSelect()}</div><div class="caption">知识子树 · ${ns.length} 个节点</div>${roots.map(n => `<div class="plain-row">${I('tree')}<div><h3>${E(address(n))}</h3><small>${subtree(n.id).filter(id => node(id)?.deleted).length} 个节点</small></div><span class="spacer"></span>${button('node-restore', '恢复', 'history', 'border', `data-id="${n.id}"`)}${button('node-purge', '永久删除', 'trash', 'danger', `data-id="${n.id}"`)}</div>`).join('') || empty('在这里恢复知识节点')}<div class="node-section"><div class="caption">任务 · ${ts.length}</div>${ts.map(t => `<div class="plain-row">${I('board')}<span>${E(t.title)}</span><span class="spacer"></span>${button('task-restore', '恢复', 'history', 'border', `data-id="${t.id}"`)}${button('task-purge', '永久删除', 'trash', 'danger', `data-id="${t.id}"`)}</div>`).join('') || empty('在这里恢复任务')}</div>`;
}
function settingsView() {
  return `<div class="heading"><div><h1>设置</h1><span class="pill">本地工作空间</span></div></div><div class="settings"><section class="settings-section"><h2>外观</h2><div class="setting-row"><div><h3>界面主题</h3></div><div class="segmented">${button('theme', '浅色', null, db.theme === 'light' ? 'active' : '', 'data-id="light"')}${button('theme', '深色', 'moon', db.theme === 'dark' ? 'active' : '', 'data-id="dark"')}</div></div></section><section class="settings-section"><h2>管理员</h2><div class="setting-row"><div><h3>admin</h3></div>${button('password', db.auth ? '修改密码' : '修改密码', 'edit', 'border')}</div></section><section class="settings-section"><h2>数据管理</h2><div class="setting-row"><div><h3>全部项目备份</h3></div><div class="actions">${button('backup', '导出备份', 'download', 'border')}${button('restore', '导入备份', 'upload', 'border')}</div></div><div class="setting-row"><div><h3>当前项目任务</h3></div><div class="actions">${button('export-tasks', '导出 CSV', 'download', 'border')}${button('export-task-json', '导出 JSON', 'download', 'border')}</div></div><div class="setting-row"><div><h3>工作空间数据</h3><p>${db.projects.length} 个项目 · ${db.tasks.length} 个任务 · ${db.nodes.length} 个节点 · ${(new Blob([JSON.stringify(db)]).size / 1024).toFixed(1)} KB</p></div>${button('runtime-info', '运行信息', 'settings', 'border')}</div></section><section class="settings-section"><h2>项目知识</h2>${button('export-knowledge', '导出知识树', 'download', 'border')}</section><section class="settings-section"><h2>已归档项目</h2>${db.projects.filter(pr => pr.archived).map(pr => `<div class="plain-row"><span>${E(pr.name)}</span>${button('project-unarchive', '恢复项目', 'history', 'border', `data-id="${pr.id}"`)}</div>`).join('') || '<p class="muted">归档项目可在这里恢复</p>'}</section></div>`;
}
function modal(title, body, footer = '', wide = false) {
  document.getElementById('root').inert = true;
  document.getElementById('overlay').inert = true;
  document.getElementById('dialog').innerHTML = `<div class="modal-layer"><section role="dialog" aria-modal="true" aria-label="${E(title)}" class="modal ${wide ? 'wide' : ''}"><div class="modal-head"><h2>${E(title)}</h2>${iconBtn('modal-close', 'close', '关闭')}</div>${body}${footer ? `<div class="modal-footer">${footer}</div>` : ''}</section></div>`;
  setTimeout(() => document.querySelector('#dialog input:not([type=checkbox]),#dialog textarea,#dialog select,#dialog button')?.focus(), 0);
}
function closeModal() {
  document.getElementById('dialog').innerHTML = '';
  document.getElementById('root').inert = false;
  document.getElementById('overlay').inert = false;
  if (ui.page === 'task') document.querySelector('[data-action=task-fields]')?.focus();
}
function formModal(title, fields, onSubmit, wide = false, submit = '保存') {
  modal(title, `<form id="modal-form">${fields}<p id="form-error" class="error" role="alert"></p><div class="modal-footer">${button('modal-close', '取消', null, 'border')}<button class="primary" type="submit">${submit}</button></div></form>`, '', wide);
  document.getElementById('modal-form').onsubmit = async e => {
    e.preventDefault();
    try {
      await onSubmit(new FormData(e.target));
    } catch (error) {
      document.getElementById('form-error').textContent = error.message;
    }
  };
}
function confirmModal(title, body, fn, label = '确认') {
  modal(title, body, button('modal-close', '取消', null, 'border') + `<button id="confirm-action" class="primary">${label}</button>`);
  document.getElementById('confirm-action').onclick = async () => {
    closeModal();
    await fn();
  };
}
function parseTags(s) {
  return [...new Set(String(s).split(/[,，\n]/).map(x => x.trim().normalize('NFC')).filter(Boolean))];
}
function validName(name, parent, except) {
  name = name.trim().normalize('NFC');
  if (!name || /[.\x00-\x1f\x7f]/.test(name)) throw Error('请填写节点名称，使用文字、数字或短横线。');
  if (nodes().some(n => n.id !== except && n.parent === parent && n.name.toLocaleLowerCase() === name.toLocaleLowerCase())) throw Error('请换一个同级节点名称。');
  return name;
}
function nodeForm(id, parent = null) {
  const n = id ? node(id) : null,
    exclude = n ? subtree(id) : [];
  formModal(n ? '编辑节点' : '新建知识节点', `<div class="form-grid"><label class="full">节点名称<input name="name" required maxlength="80" value="${E(n?.name || '')}" placeholder="例如：启动命令"></label><label class="full">父节点<select name="parent"><option value="">根节点</option>${nodes().filter(x => !exclude.includes(x.id)).map(x => `<option value="${x.id}" ${x.id === (n?.parent || parent) ? 'selected' : ''}>${E(address(x))}</option>`).join('')}</select></label><label class="full">标签 · 逗号分隔<input name="tags" value="${E(n?.tags.join(', ') || '')}" placeholder="技术, 命令"></label>${!n ? '<label class="full">节点内容<textarea name="content" rows="4" placeholder="记录知识内容"></textarea></label>' : ''}</div>`, async f => {
    const parent = f.get('parent') || null,
      name = validName(f.get('name'), parent, id);
    const created = id || uid();
    if (await applyMutation(() => {
      if (n) {
        n.name = name;
        n.parent = parent;
        n.tags = parseTags(f.get('tags'));
        n.updated = now();
      } else db.nodes.push({
        id: created,
        project: ui.project,
        name,
        parent,
        content: f.get('content'),
        tags: parseTags(f.get('tags')),
        order: Date.now(),
        pinned: false,
        updated: now(),
        history: [],
        links: []
      });
      ui.node = created;
      ui.page = 'project';
      ui.tab = 'knowledge';
    })) {
      closeModal();
      toast(n ? '节点已更新' : '知识节点已创建');
    }
  });
}
function deleteNodes(ids) {
  const affected = [...new Set(ids.flatMap(subtree))].filter(id => !node(id)?.deleted);
  confirmModal('删除知识节点', `<p>将把所选 ${ids.length} 个节点及其子节点移入回收站，共 <b>${affected.length}</b> 个节点。</p><pre>${affected.map(id => E(address(node(id)))).join('\n')}</pre>`, async () => {
    if (await applyMutation(() => {
      const batch = uid();
      affected.forEach(id => {
        node(id).deleted = now();
        node(id).deleteBatch = batch;
      });
      ui.selected.clear();
    })) {
      toast('已移入回收站');
    }
  }, '移入回收站');
}
function tagEditor(ids, remove = false) {
  const project = ui.project,
    items = [...nodes(), ...tasks()].filter(x => ids.includes(x.id));
  if (items.length !== ids.length) {
    toast('请重新选择当前项目内容');
    return;
  }
  formModal(remove ? '批量移除标签' : '添加标签', `<label>标签 · 逗号分隔<input name="tags" required list="known-tags" placeholder="例如：核心, 待完善"></label><datalist id="known-tags">${allTags().map(t => `<option value="${E(t)}">`).join('')}</datalist><div class="note">当前项目 · 已选 ${items.length} 项</div>`, async f => {
    const tags = parseTags(f.get('tags'));
    if (!tags.length) throw Error('请输入标签');
    if (project !== ui.project) throw Error('项目已切换，请重新选择');
    if (await applyMutation(() => {
      items.forEach(n => {
        n.tags = remove ? n.tags.filter(t => !tags.includes(t)) : [...new Set([...n.tags, ...tags])];
        n.updated = now();
      });
      ui.selected.clear();
    })) {
      closeModal();
      toast('标签已更新');
    }
  });
}
function manageTags() {
  modal('管理项目标签', `<p class="muted">${E(p().name)}</p>${allTags().map(t => `<div class="tag-manage">${I('tag')}<span>${E(t)}</span><small>${tasks().filter(n => n.tags.includes(t)).length} 任务 · ${nodes().filter(n => n.tags.includes(t)).length} 节点</small>${button('tag-rename', '改名 / 合并', null, 'border', `data-tag="${E(t)}"`)}${iconBtn('tag-delete', 'trash', '删除标签', `data-tag="${E(t)}"`)}</div>`).join('') || empty('添加标签，整理项目内容')}`, '', true);
}
function taskForm(id, initialStatus = '', knowledge) {
  const t = id ? db.tasks.find(t => t.id === id) : null;
  formModal(t ? '编辑任务标题' : '新建任务', `<label>标题<input name="title" required value="${E(t?.title || '')}" placeholder="下一步要完成什么？"></label>`, async f => {
    const title = f.get('title').trim();
    if (!title) throw Error('请输入任务标题');
    const idNew = id || uid();
    if (await applyMutation(() => {
      if (t) {
        t.title = title;
        t.updated = now();
      } else db.tasks.push({
        id: idNew,
        num: Math.max(0, ...db.tasks.filter(x => x.project === ui.project).map(x => x.num)) + 1,
        project: ui.project,
        title,
        description: '',
        status: p().initialStatusId || 'created',
        priority: '',
        due: '',
        start: '',
        end: '',
        progress: null,
        reminder: '',
        repeat: '',
        color: '',
        tags: [],
        order: Date.now(),
        knowledge: knowledge ? [knowledge] : [],
        checks: [],
        notes: [],
        relations: [],
        created: now(),
        updated: now()
      });
      ui.task = idNew;
      ui.page = 'task';
    })) {
      closeModal();
      toast(t ? '标题已更新' : '任务已创建');
    }
  });
}
function renderTask() {
  const host = document.getElementById('task-page');
  if (!host) return;
  const t = db.tasks.find(t => t.id === ui.task && !t.deleted && t.project === ui.project);
  if (!t) {
    host.innerHTML = empty('返回项目，选择任务') + button('task-close', '返回项目', 'chevron', 'border');
    return;
  }
  const pr = db.projects.find(p => p.id === t.project),
    fields = taskFields().filter(f => !['description', 'checks', 'notes', 'knowledge', 'relations'].includes(f.key) && taskHas(t, f.key));
  const blockHead = (key, label) => `<div class="heading"><h3>${label}</h3><div class="actions">${button('task-field-edit', '添加', 'plus', '', `data-field="${key}"`)}${iconBtn('task-field-clear', 'close', '清空' + label, `data-field="${key}"`)}</div></div>`;
  host.innerHTML = `<div class="task-page-toolbar">${button('task-close', '返回项目', 'chevron')}<span class="muted">${E(pr.code)}-${t.num}</span><span class="spacer"></span>${button('task-copy-link', '复制任务链接', 'link')}${iconBtn('task-delete', 'trash', '删除任务', `data-id="${t.id}"`)}</div><article class="task-document"><div class="task-document-main"><button class="task-title-button" data-action="task-edit" data-id="${t.id}" title="编辑任务标题"><h1>${E(t.title)}</h1>${I('edit')}</button>${taskDetailView(t)}${commentView(t)}<details class="task-supplement"><summary>检查清单与关联</summary>${taskHas(t, 'checks') ? `<section class="node-section">${blockHead('checks', '检查清单')}<small>${t.checks.filter(c => c.done).length} / ${t.checks.length} 已完成</small>${t.checks.map(c => `<div class="check-row ${c.done ? 'done' : ''}"><input type="checkbox" data-check="${c.id}" aria-label="${E(c.text)}" ${c.done ? 'checked' : ''}><span>${E(c.text)}</span>${iconBtn('check-delete', 'close', '移除清单项', `data-id="${c.id}"`)}</div>`).join('')}<form id="check-form" class="inline-fields"><input name="text" required placeholder="添加检查项" aria-label="检查项"><button type="submit">${I('plus')}</button></form></section>` : ''}${taskHas(t, 'knowledge') ? `<section class="node-section">${blockHead('knowledge', '关联知识')}${t.knowledge.map(id => {
    const n = node(id);
    return `<div class="plain-row"><button ${!n || n.deleted ? 'disabled' : ''} data-action="node-open" data-id="${id}"><code>${E(n ? address(n) : '节点已永久删除')}${n?.deleted ? '（已删除）' : ''}</code></button>${iconBtn('task-unlink', 'close', '移除关联', `data-id="${id}"`)}</div>`;
  }).join('')}</section>` : ''}${taskHas(t, 'relations') ? `<section class="node-section">${blockHead('relations', '关联任务')}${t.relations.map(r => {
    const other = db.tasks.find(x => x.id === r.id);
    return `<div class="plain-row"><small>${{
      related: '相关任务',
      blocks: '阻塞此任务',
      subtask: '子任务'
    }[r.type]}</small>${button('task-open', E(other?.title || '任务已删除'), 'board', '', `data-id="${r.id}" ${other && !other.deleted ? '' : 'disabled'}`)}${iconBtn('task-relation-remove', 'close', '移除任务关联', `data-id="${r.id}"`)}</div>`;
  }).join('')}</section>` : ''}</details></div><aside class="task-properties"><div class="heading"><h3>属性</h3>${iconBtn('task-fields', 'plus', '添加任务字段')}</div>${fields.map(f => `<div class="task-property"><span class="muted">${f.label}</span><button data-action="task-field-edit" data-field="${f.key}">${f.key === 'color' ? `<span class="task-color" style="background:${/^#[0-9a-f]{6}$/i.test(t.color) ? t.color : 'transparent'}"></span>` : ''}${E(taskFieldText(t, f))}</button>${iconBtn('task-field-clear', 'close', f.key === 'status' ? '重置为创建' : '清空' + f.label, `data-field="${f.key}"`)}</div>`).join('') || '<p class="muted">点击＋添加属性</p>'}<div class="task-order-actions">${button('task-up', '排序上移', 'arrow')}${button('task-down', '排序下移', 'down')}</div>${t.created ? `<small class="task-timestamps">创建于 ${new Date(t.created).toLocaleString('zh-CN')}</small>` : ''}</aside></article>`;
  bindTaskDetail(t);
  bindComments(t);
  const check = document.getElementById('check-form'),
    note = document.getElementById('note-form');
  if (check) check.onsubmit = async e => {
    e.preventDefault();
    const text = new FormData(e.target).get('text').trim();
    if (text) await applyMutation(() => t.checks.push({
      id: uid(),
      text,
      done: false
    }));
  };
  if (note) note.onsubmit = async e => {
    e.preventDefault();
    const text = new FormData(e.target).get('text').trim();
    if (text) await applyMutation(() => t.notes.push({
      id: uid(),
      text,
      at: now()
    }));
  };
}
function selectNodeModal(title, fn, exclude = []) {
  formModal(title, `<label>知识节点<select name="node" required><option value="">选择节点</option>${nodes().filter(n => !exclude.includes(n.id)).map(n => `<option value="${n.id}">${E(address(n))}</option>`).join('')}</select></label>`, async f => {
    const id = f.get('node');
    closeModal();
    await fn(id);
  });
}
function projectForm(edit = false) {
  const pr = edit ? p() : null;
  formModal(edit ? '项目设置' : '新建项目', `<div class="form-grid"><label>项目名称<input name="name" required value="${E(pr?.name || '')}"></label><label>项目简称<input name="code" required maxlength="10" pattern="[A-Za-z0-9_-]+" value="${E(pr?.code || '')}" placeholder="例如 PB"></label><label class="full">简介<textarea name="description" rows="3">${E(pr?.description || '')}</textarea></label><label class="full">本地目录（可选）<input name="directory" value="${E(pr?.directory || '')}" placeholder="例如 D:\\Projects\\MyApp"></label></div>${edit ? `<div class="note">${button('project-archive', '归档项目', 'archive', 'danger', `data-id="${pr.id}"`)}</div>` : ''}`, async f => {
    const name = f.get('name').trim();
    if (!name) throw Error('请输入项目名称');
    if (db.projects.some(x => x.id !== pr?.id && x.code.toLowerCase() === f.get('code').toLowerCase())) throw Error('请换一个项目简称');
    if (await applyMutation(() => {
      const fields = {
        name,
        code: f.get('code'),
        description: f.get('description'),
        directory: f.get('directory')
      };
      if (pr) Object.assign(pr, fields);else {
        const id = uid();
        db.projects.push({
          id,
          ...fields,
          statuses: baseStatuses(),
          initialStatusId: 'created'
        });
        ui.project = id;
        ui.page = 'project';
        ui.tab = 'overview';
        ui.node = null;
      }
    })) {
      closeModal();
      toast('项目已保存');
    }
  });
}
function statusManage() {
  modal('管理任务状态', `${p().statuses.map((s, i) => `<div class="tag-manage"><span>${E(s.name)} <small>${{
    todo: '准备中',
    active: '进行中',
    done: '已完成',
    cancelled: '已取消'
  }[s.kind]}</small></span>${iconBtn('status-up', 'arrow', '状态上移', `data-id="${s.id}"`)}${s.id === p().initialStatusId ? '<small>初始状态</small>' : button('status-edit', '编辑', null, 'border', `data-id="${s.id}"`) + iconBtn('status-delete', 'trash', '删除并迁移任务', `data-id="${s.id}"`)}</div>`).join('')}${button('status-new', '新增状态', 'plus', 'border', 'style="margin-top:20px"')}`);
}
function statusForm(id) {
  if (id && id === p().initialStatusId) {
    toast('“创建”是固定初始状态');
    return;
  }
  const s = p().statuses.find(s => s.id === id);
  formModal(s ? '编辑状态' : '新增状态', `<div class="form-grid"><label>名称<input name="name" required value="${E(s?.name || '')}"></label><label>状态分类<select name="kind">${[['todo', '准备中'], ['active', '进行中'], ['done', '已完成'], ['cancelled', '已取消']].map(([v, l]) => `<option value="${v}" ${v === s?.kind ? 'selected' : ''}>${l}</option>`).join('')}</select></label></div>`, async f => {
    const name = f.get('name').trim();
    if (!name) throw Error('请输入名称');
    if (p().statuses.some(x => x.id !== id && x.name === name)) throw Error('请换一个状态名称');
    if (await applyMutation(() => {
      if (s) {
        s.name = name;
        s.kind = f.get('kind');
      } else p().statuses.push({
        id: uid(),
        name,
        kind: f.get('kind')
      });
    })) statusManage();
  });
}
function download(name, content, type = 'application/json') {
  const url = URL.createObjectURL(new Blob([content], {
      type
    })),
    a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 3000);
}
async function copy(text) {
  try {
    await navigator.clipboard.writeText(text);
    toast('已复制');
  } catch {
    modal('复制内容', `<textarea id="copy-area" rows="10" style="width:100%">${E(text)}</textarea><small>请选中内容后复制。</small>`);
    document.getElementById('copy-area').select();
  }
}
const actions = {
  'nav-projects': async () => await navigate(() => ui.page = 'projects'),
  'tree-mobile': () => document.querySelector('.knowledge')?.classList.toggle('tree-open'),
  'menu': () => document.getElementById('sidebar').classList.toggle('open'),
  'nav-work': async () => await navigate(() => {
    ui.page = 'work';
    ui.task = null;
    ui.query = '';
    ui.status = 'all';
  }),
  'nav-tags': async () => await navigate(() => {
    ui.page = 'project';
    ui.tab = 'tags';
    ui.query = '';
    ui.tags = [];
    ui.tagType = 'all';
  }),
  'nav-trash': async () => await navigate(() => ui.page = 'trash'),
  'nav-settings': async () => await navigate(() => ui.page = 'settings'),
  'project-switch': async b => await navigate(() => {
    ui.project = b.dataset.id;
    ui.page = 'project';
    ui.task = null;
    ui.query = '';
    ui.status = 'all';
    ui.node = null;
    ui.tags = [];
    ui.tagType = 'all';
  }),
  'tab': async b => await navigate(() => {
    ui.page = 'project';
    ui.task = null;
    ui.tab = b.dataset.id;
  }),
  'view': async b => await navigate(() => ui.view = b.dataset.id),
  'project-new': () => projectForm(),
  'project-edit': () => projectForm(true),
  'project-archive': b => confirmModal('归档项目', '<p>项目将从侧栏收起，任务和知识保持完整，可在设置中恢复。</p>', async () => await applyMutation(() => {
    p().archived = true;
    ui.page = 'settings';
  }), '归档'),
  'project-unarchive': async b => await applyMutation(() => db.projects.find(p => p.id === b.dataset.id).archived = false),
  'node-new': b => nodeForm(null, b.dataset.parent || null),
  'node-edit': b => nodeForm(b.dataset.id),
  'node-open': async b => {
    closeModal();
    await navigate(() => {
      const n = node(b.dataset.id);
      ui.node = n.id;
      ui.project = n.project;
      ui.page = 'project';
      ui.tab = 'knowledge';
      ui.task = null;
      let parent = n.parent;
      while (parent) {
        ui.collapsed.delete(parent);
        parent = node(parent)?.parent;
      }
    });
  },
  'tree-toggle': b => {
    const id = b.dataset.id;
    ui.collapsed.has(id) ? ui.collapsed.delete(id) : ui.collapsed.add(id);
    render();
  },
  'node-pin': async b => await applyMutation(() => node(b.dataset.id).pinned = !node(b.dataset.id).pinned),
  'node-tags': b => tagEditor([b.dataset.id]),
  'node-delete': b => deleteNodes([b.dataset.id]),
  'bulk-delete': deleteTagged,
  'address-go': () => {
    const value = document.getElementById('node-address').value.trim(),
      n = nodes().find(n => address(n).toLowerCase() === value.normalize('NFC').toLowerCase());
    if (n) actions['node-open']({
      dataset: {
        id: n.id
      }
    });else toast('请输入完整的节点地址，如 技术.开发.启动命令');
  },
  'copy-address': () => copy(address(node(ui.node))),
  'copy-subtree': async b => {
    await flushDraft();
    copy(subtree(b.dataset.id).map(node).filter(n => !n.deleted).map(n => address(n) + '\n标签：' + n.tags.join(', ') + '\n' + n.content).join('\n\n'));
  },
  'node-snapshot': async b => {
    await flushDraft();
    if (await applyMutation(() => {
      const n = node(b.dataset.id);
      n.history.push({
        id: uid(),
        content: n.content,
        tags: [...n.tags],
        at: now(),
        reason: '手动版本'
      });
    })) {
      toast('已保存版本');
    }
  },
  'node-history': async b => {
    await flushDraft();
    const n = node(b.dataset.id);
    modal('节点版本历史', `<small>${E(address(n))}</small>${n.history.slice().reverse().map(h => `<div class="node-section"><div class="heading"><small>${E(h.reason)} · ${new Date(h.at).toLocaleString('zh-CN')}</small>${button('history-restore', '恢复此版本', 'history', 'border', `data-node="${n.id}" data-id="${h.id}"`)}</div><pre>${E(h.content)}</pre>${chips(h.tags)}</div>`).join('') || empty('保存版本，记录知识进展')}`, '', true);
  },
  'history-restore': b => {
    const n = node(b.dataset.node),
      h = n.history.find(h => h.id === b.dataset.id);
    confirmModal('恢复历史版本', '<p>当前内容会保存为一个新快照，再恢复所选内容与标签。</p>', async () => {
      if (await applyMutation(() => {
        n.history.push({
          id: uid(),
          content: n.content,
          tags: [...n.tags],
          at: now(),
          reason: '恢复前快照'
        });
        n.content = h.content;
        n.tags = [...h.tags];
        n.updated = now();
      })) {
        toast('已恢复版本');
      }
    });
  },
  'node-link': b => selectNodeModal('关联知识节点', async id => await applyMutation(() => node(b.dataset.id).links.push(id)), [b.dataset.id, ...node(b.dataset.id).links]),
  'node-unlink': async b => await applyMutation(() => {
    const n = node(ui.node),
      other = node(b.dataset.id);
    n.links = n.links.filter(id => id !== other.id);
    other.links = other.links.filter(id => id !== n.id);
  }),
  'node-up': async b => await reorderNode(b.dataset.id, -1),
  'node-down': async b => await reorderNode(b.dataset.id, 1),
  'tag-focus': async b => await navigate(() => {
    ui.page = 'project';
    ui.tab = 'tags';
    ui.tags = [b.dataset.tag];
    ui.query = '';
    ui.tagType = 'all';
  }),
  'tag-toggle': async b => await navigate(() => ui.tags = ui.tags.includes(b.dataset.tag) ? ui.tags.filter(t => t !== b.dataset.tag) : [...ui.tags, b.dataset.tag]),
  'tags-clear': async () => await navigate(() => ui.tags = []),
  'bulk-tag-add': () => tagEditor([...ui.selected]),
  'bulk-tag-remove': () => tagEditor([...ui.selected], true),
  'tags-manage': manageTags,
  'tag-rename': b => {
    const old = b.dataset.tag;
    formModal('标签改名或合并', `<label>新标签名称<input name="tag" value="${E(old)}" required></label><div class="note">当前项目 · 含回收站 · 同名合并</div>`, async f => {
      const name = f.get('tag').trim().normalize('NFC');
      if (!name || /[,，\n]/.test(name)) throw Error('请输入一个有效标签名称');
      if (await applyMutation(() => {
        [...db.nodes, ...db.tasks].filter(n => n.project === ui.project).forEach(n => n.tags = [...new Set(n.tags.map(t => t === old ? name : t))]);
        p().labels = (p().labels || []).map(t => t === old ? name : t);
        ui.tags = ui.tags.map(t => t === old ? name : t);
      })) {
        manageTags();
        toast('标签已更新');
      }
    });
  },
  'tag-delete': b => confirmModal('移除标签', `<p>从当前项目所有任务与知识节点（包括回收站）移除标签“${E(b.dataset.tag)}”。任务、节点与子节点内容保持完整。</p>`, async () => {
    if (await applyMutation(() => {
      [...db.nodes, ...db.tasks].filter(n => n.project === ui.project).forEach(n => n.tags = n.tags.filter(t => t !== b.dataset.tag));
      p().labels = (p().labels || []).filter(t => t !== b.dataset.tag);
      ui.tags = ui.tags.filter(t => t !== b.dataset.tag);
    })) {
      toast('标签已移除');
    }
  }, '移除标签'),
  'task-new': () => taskForm(),
  'task-from-node': b => taskForm(null, '', b.dataset.id),
  'task-edit': b => taskForm(b.dataset.id),
  'task-open': async b => await navigate(() => {
    ui.task = b.dataset.id;
    ui.project = db.tasks.find(t => t.id === ui.task).project;
    ui.page = 'task';
  }),
  'task-close': async () => await navigate(() => {
    ui.task = null;
    ui.page = 'project';
    ui.tab = 'tasks';
  }),
  'task-delete': b => confirmModal('删除任务', '<p>任务将移入回收站，可随时恢复。</p>', async () => await applyMutation(() => {
    db.tasks.find(t => t.id === b.dataset.id).deleted = now();
    ui.task = null;
    ui.page = 'project';
    ui.tab = 'tasks';
  }), '移入回收站'),
  'task-link': () => {
    const t = db.tasks.find(t => t.id === ui.task);
    selectNodeModal('关联知识节点', async id => await applyMutation(() => t.knowledge.push(id)), t.knowledge);
  },
  'task-unlink': async b => await applyMutation(() => {
    const t = db.tasks.find(t => t.id === ui.task);
    t.knowledge = t.knowledge.filter(id => id !== b.dataset.id);
  }),
  'check-delete': async b => await applyMutation(() => {
    const t = db.tasks.find(t => t.id === ui.task);
    t.checks = t.checks.filter(c => c.id !== b.dataset.id);
  }),
  'note-to-node': b => {
    const t = db.tasks.find(t => t.id === ui.task),
      note = t.notes.find(n => n.id === b.dataset.id);
    selectNodeModal('将评论写入知识', id => {
      const n = node(id);
      formModal('编辑知识内容', `<label>${E(address(n))}<textarea name="content" rows="10">${E(n.content + '\n\n' + note.text)}</textarea></label>`, async f => {
        if (await applyMutation(() => {
          n.history.push({
            id: uid(),
            content: n.content,
            tags: [...n.tags],
            at: now(),
            reason: '任务记录写入前'
          });
          n.content = f.get('content');
          n.updated = now();
          if (!t.knowledge.includes(id)) t.knowledge.push(id);
        })) {
          closeModal();
          toast('评论已写入知识');
        }
      });
    }, []);
  },
  'task-up': async () => await reorderTask(-1),
  'task-down': async () => await reorderTask(1),
  'status-manage': statusManage,
  'status-edit': b => statusForm(b.dataset.id),
  'status-new': () => statusForm(),
  'status-up': async b => {
    await applyMutation(() => {
      const s = p().statuses,
        i = s.findIndex(x => x.id === b.dataset.id);
      if (i > 0) [s[i - 1], s[i]] = [s[i], s[i - 1]];
    });
    statusManage();
  },
  'status-delete': b => {
    if (b.dataset.id === p().initialStatusId) {
      toast('请选择其他状态进行管理');
      return;
    }
    if (p().statuses.length === 1) {
      toast('至少保留一个任务状态');
      return;
    }
    formModal('删除状态并迁移任务', `<label>将此状态的所有任务迁移到<select name="target">${p().statuses.filter(s => s.id !== b.dataset.id).map(s => `<option value="${s.id}">${E(s.name)}</option>`).join('')}</select></label>`, async f => {
      if (await applyMutation(() => {
        db.tasks.filter(t => t.project === ui.project && t.status === b.dataset.id).forEach(t => t.status = f.get('target'));
        p().statuses = p().statuses.filter(s => s.id !== b.dataset.id);
        ui.status = 'all';
      })) statusManage();
    });
  },
  'node-restore': b => {
    const n = node(b.dataset.id),
      available = nodes();
    formModal('恢复知识子树', `<label>根节点名称<input name="name" value="${E(n.name)}" required></label><label style="margin-top:15px">恢复到<select name="parent"><option value="">项目根节点</option>${available.map(x => `<option value="${x.id}" ${x.id === n.parent ? 'selected' : ''}>${E(address(x))}</option>`).join('')}</select></label>`, async f => {
      const parent = f.get('parent') || null,
        name = validName(f.get('name'), parent, n.id);
      if (await applyMutation(() => {
        n.name = name;
        n.parent = parent;
        subtree(n.id).map(node).filter(x => x.deleteBatch === n.deleteBatch).forEach(x => {
          delete x.deleted;
          delete x.deleteBatch;
        });
      })) {
        closeModal();
        toast('子树已恢复');
      }
    });
  },
  'node-purge': b => confirmModal('永久删除知识子树', '<p>永久删除这棵知识子树及相关引用。</p>', async () => await applyMutation(() => {
    const ids = subtree(b.dataset.id);
    db.nodes = db.nodes.filter(n => !ids.includes(n.id));
    db.tasks.forEach(t => t.knowledge = t.knowledge.filter(id => !ids.includes(id)));
    db.nodes.forEach(n => n.links = n.links.filter(id => !ids.includes(id)));
  }), '永久删除'),
  'task-restore': async b => await applyMutation(() => delete db.tasks.find(t => t.id === b.dataset.id).deleted),
  'task-purge': b => confirmModal('永久删除任务', '<p>永久删除此任务及其评论、附件和关联。</p>', async () => await applyMutation(() => {
    db.tasks = db.tasks.filter(t => t.id !== b.dataset.id);
    db.tasks.forEach(t => {
      if (t.relations) t.relations = t.relations.filter(r => r.id !== b.dataset.id);
    });
  }), '永久删除'),
  'theme': async b => await applyMutation(() => db.theme = b.dataset.id),
  'export-tasks': exportTaskCSV,
  'detail-edit': () => {
    detailDraft(db.tasks.find(t => t.id === ui.task)).editing = true;
    render();
    document.getElementById('task-detail-text')?.focus();
  },
  'detail-cancel': () => {
    const id = 'detail:' + ui.task;
    if (commentDraft(id).loading) return;
    commentDraft.items.delete(id);
    sessionStorage.removeItem(key + '.comment.' + id);
    render();
  },
  'detail-attach': () => {
    const t = db.tasks.find(t => t.id === ui.task);
    detailDraft(t);
    const id = 'detail:' + t.id,
      input = document.getElementById('comment-files');
    input.value = '';
    input.onchange = async () => await addCommentFiles(id, input.files);
    input.click();
  },
  'detail-pending-remove': b => {
    const d = detailDraft(db.tasks.find(t => t.id === ui.task));
    d.files = d.files.filter(a => a.id !== b.dataset.id);
    render();
  },
  'detail-download': downloadDetailFile,
  'comment-attach': () => {
    const id = ui.task,
      input = document.getElementById('comment-files');
    input.value = '';
    input.onchange = async () => await addCommentFiles(id, input.files);
    input.click();
  },
  'comment-file-remove': b => {
    const d = commentDraft(ui.task);
    d.files = d.files.filter(a => a.id !== b.dataset.id);
    render();
  },
  'comment-reply': b => {
    commentDraft(ui.task).reply = b.dataset.id;
    render();
    document.getElementById('comment-text')?.focus();
  },
  'comment-cancel-reply': () => {
    commentDraft(ui.task).reply = null;
    render();
  },
  'comment-edit': b => editComment(b.dataset.id),
  'comment-delete': b => confirmModal('删除评论', '<p>评论与附件将一起隐藏，可以恢复。</p>', async () => await applyMutation(() => db.tasks.find(t => t.id === ui.task).notes.find(n => n.id === b.dataset.id).deleted = now()), '删除评论'),
  'comment-restore': async b => await applyMutation(() => delete db.tasks.find(t => t.id === ui.task).notes.find(n => n.id === b.dataset.id).deleted),
  'comment-download': downloadCommentFile,
  'timeline-prev': async () => await navigate(() => ui.timelineOffset = (ui.timelineOffset || 0) - 1),
  'timeline-next': async () => await navigate(() => ui.timelineOffset = (ui.timelineOffset || 0) + 1),
  'timeline-today': async () => await navigate(() => ui.timelineOffset = 0),
  'task-fields': taskFieldPicker,
  'task-field-edit': b => taskFieldEditor(b.dataset.field),
  'task-field-clear': b => clearTaskField(b.dataset.field),
  'task-copy-link': () => copy(location.href),
  'task-relation-remove': async b => await applyMutation(() => {
    const t = db.tasks.find(x => x.id === ui.task);
    t.relations = t.relations.filter(r => r.id !== b.dataset.id);
  }),
  'search': openSearch,
  'search-open': b => {
    closeModal();
    if (b.dataset.type === 'node') actions['node-open'](b);else if (b.dataset.type === 'task') actions['task-open'](b);else actions['project-switch'](b);
  },
  'modal-close': closeModal
};
async function reorderNode(id, direction) {
  const n = node(id),
    siblings = nodes().filter(x => x.parent === n.parent).sort((a, b) => a.order - b.order),
    i = siblings.findIndex(x => x.id === id);
  if (!siblings[i + direction]) return toast('已到首尾位置');
  await applyMutation(() => {
    [siblings[i], siblings[i + direction]] = [siblings[i + direction], siblings[i]];
    siblings.forEach((x, i) => x.order = i);
  });
}
async function reorderTask(direction) {
  const t = db.tasks.find(t => t.id === ui.task),
    siblings = tasks().filter(x => x.status === t.status).sort((a, b) => a.order - b.order),
    i = siblings.indexOf(t);
  if (!siblings[i + direction]) return toast('已到首尾位置');
  await applyMutation(() => {
    [siblings[i], siblings[i + direction]] = [siblings[i + direction], siblings[i]];
    siblings.forEach((x, i) => x.order = i);
    ui.sort = 'manual';
  });
}
document.addEventListener('click', e => {
  const b = e.target.closest('[data-action]');
  if (b && !b.disabled) {
    try {
      actions[b.dataset.action]?.(b);
    } catch (error) {
      toast(error.message, 5000);
    }
  } else if (e.target.classList.contains('drawer-layer')) actions['task-close']();else if (e.target.classList.contains('modal-layer')) closeModal();
});
// 输入后同步筛选，并恢复光标位置，连续输入保持流畅。
let filterTimer;
document.addEventListener('input', e => {
  const el = e.target;
  if (el.id === 'node-content') {
    activeDraft = {
      id: ui.node,
      content: el.value
    };
    document.getElementById('save-state').textContent = '保存中…';
    clearTimeout(draftTimer);
    draftTimer = setTimeout(flushDraft, 550);
  } else if (el.id === 'global-search') searchResults(el.value);else if (['tag-query', 'task-filter'].includes(el.id)) {
    clearTimeout(filterTimer);
    const value = el.value,
      field = el.id;
    filterTimer = setTimeout(() => {
      if (!document.getElementById(field)) return;
      ui.query = value;
      ui.selected.clear();
      render();
      const input = document.getElementById(field);
      input?.focus();
      input?.setSelectionRange(value.length, value.length);
    }, 180);
  }
});
document.addEventListener('change', async e => {
  const el = e.target;
  if (el.dataset.selectNode) {
    el.checked ? ui.selected.add(el.dataset.selectNode) : ui.selected.delete(el.dataset.selectNode);
    render();
  } else if (el.id === 'select-all-nodes') {
    ui.selected = el.checked ? new Set(matchingEntities().map(x => x.value.id)) : new Set();
    render();
  } else if (el.id === 'project-scope') await navigate(() => {
    ui.project = el.value;
    ui.tags = [];
    ui.query = '';
  });else if (el.id === 'mcp-tool') await navigate(() => ui.mcpTool = el.value);else if (el.id === 'tag-type') await navigate(() => ui.tagType = el.value);else if (el.id === 'tag-mode') await navigate(() => ui.tagMode = el.value);else if (['tag-query', 'task-filter'].includes(el.id)) await navigate(() => ui.query = el.value);else if (el.id === 'status-filter') await navigate(() => ui.status = el.value);else if (el.id === 'priority-filter') await navigate(() => ui.priority = el.value);else if (el.id === 'task-sort') await navigate(() => ui.sort = el.value);else if (['task-status', 'task-priority', 'task-due'].includes(el.id)) {
    const field = el.id.slice(5);
    await applyMutation(() => db.tasks.find(t => t.id === ui.task)[field] = el.value);
  } else if (el.dataset.check) await applyMutation(() => db.tasks.find(t => t.id === ui.task).checks.find(c => c.id === el.dataset.check).done = el.checked);
});
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') {
    if (document.getElementById('dialog').innerHTML) closeModal();else if (ui.task) actions['task-close']();else document.getElementById('sidebar')?.classList.remove('open');
    return;
  }
  if (!logged) return;
  if (e.key === 'Enter' && e.target.id === 'node-address') {
    e.preventDefault();
    actions['address-go']();
    return;
  }
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault();
    openSearch();
    return;
  }
  if (!['INPUT', 'TEXTAREA', 'SELECT'].includes(e.target.tagName)) {
    if (e.key.toLowerCase() === 'n' && !e.ctrlKey && !e.metaKey) {
      e.preventDefault();
      taskForm();
    }
    if (e.key === 'Enter' && e.target.dataset.task) actions['task-open'](e.target);
  }
  if (e.key === 'Tab') {
    const container = document.querySelector('#dialog .modal') || document.querySelector('#overlay .drawer');
    if (container) {
      const focusable = [...container.querySelectorAll('button:not(:disabled),input:not(:disabled),select,textarea,[tabindex="0"]')];
      const first = focusable[0],
        last = focusable.at(-1);
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }
});
let dragging = null;
document.addEventListener('dragstart', e => {
  const t = e.target.closest('[data-drag]');
  if (t) {
    dragging = t.dataset.drag;
    e.dataTransfer.setData('text/plain', dragging);
    e.dataTransfer.effectAllowed = 'move';
  }
});
document.addEventListener('dragover', e => {
  const col = e.target.closest('[data-drop]');
  if (col && dragging) {
    e.preventDefault();
    col.classList.add('over');
  }
});
document.addEventListener('dragleave', e => {
  const col = e.target.closest('[data-drop]');
  if (col && !col.contains(e.relatedTarget)) col.classList.remove('over');
});
document.addEventListener('drop', async e => {
  const col = e.target.closest('[data-drop]');
  if (col && dragging) {
    e.preventDefault();
    const id = dragging,
      target = e.target.closest('[data-drag]')?.dataset.drag;
    if (await applyMutation(() => {
      const t = db.tasks.find(t => t.id === id);
      t.status = col.dataset.drop;
      let siblings = tasks().filter(x => x.status === t.status && x.id !== id).sort((a, b) => a.order - b.order);
      const i = siblings.findIndex(x => x.id === target);
      siblings.splice(i < 0 ? siblings.length : i, 0, t);
      siblings.forEach((x, i) => x.order = i);
      ui.sort = 'manual';
    })) {
      toast('任务位置已更新');
    }
  }
  dragging = null;
  document.querySelectorAll('.over').forEach(x => x.classList.remove('over'));
});
document.addEventListener('dragend', () => {
  dragging = null;
  document.querySelectorAll('.over').forEach(x => x.classList.remove('over'));
});
window.addEventListener('beforeunload', async e => {
  await flushDraft();
  if (activeDraft) {
    e.preventDefault();
    e.returnValue = '请先保存草稿';
  }
});
readTaskRoute();
window.addEventListener('popstate', () => {
  closeModal();
  readTaskRoute();
  render();
});
window.addEventListener('hashchange', () => {
  closeModal();
  readTaskRoute();
  render();
});
// Local service, persistence, authentication and attachment preview.
let csrf = '',
  initialized = false,
  saving = false,
  setupToken = '',
  remoteRevision = null;
let notificationCount = 0;
function hasUserDraft() {
  return !!document.getElementById('dialog').innerHTML || ['INPUT', 'TEXTAREA', 'SELECT'].includes(document.activeElement?.tagName) || [...(commentDraft.items?.entries() || [])].some(([id, d]) => d.editing || d.files.length || !id.startsWith('detail:') && d.text.trim());
}
const draftKey = key + '.node-draft';
async function api(path, options = {}) {
  const headers = {
    'X-Requested-With': 'ProjectBoard',
    'X-CSRF-Token': csrf,
    ...options.headers
  };
  if (options.body && typeof options.body === 'string') headers['Content-Type'] = 'application/json';
  const response = await fetch('/api' + path, {
    ...options,
    headers,
    credentials: 'same-origin'
  });
  if (!response.ok) {
    let error;
    try {
      error = await response.json();
    } catch {}
    ;
    const e = new Error(error?.error || '请检查本地服务连接');
    e.status = response.status;
    throw e;
  }
  return response.headers.get('Content-Type')?.includes('application/json') ? response.json() : response.blob();
}
function reconcile(next) {
  for (const key of ['projects', 'tasks', 'nodes']) {
    const current = new Map(db[key].map(x => [x.id, x]));
    next[key] = next[key].map(x => {
      const old = current.get(x.id);
      if (!old) return x;
      for (const k of Object.keys(old)) if (!(k in x)) delete old[k];
      Object.assign(old, x);
      return old;
    });
  }
  Object.assign(db, next);
}
async function save() {
  if (saving) {
    toast('正在保存，请稍候');
    return false;
  }
  saving = true;
  try {
    const next = await api('/state', {
      method: 'PUT',
      body: JSON.stringify(db)
    });
    reconcile(next);
    blocked = false;
    return true;
  } catch (e) {
    if (e.status === 409) {
      blocked = true;
      showConflict();
    } else {
      toast(e.message, 7000);
    }
    ;
    return false;
  } finally {
    saving = false;
  }
}
function showConflict() {
  modal('数据已更新', `<p>保留当前草稿后，载入最新内容。</p><div class="actions">${button('export-draft', '导出当前副本', 'download', 'border')}${button('reload-state', '载入最新内容', 'history', 'primary')}</div>`);
}
async function applyMutation(fn) {
  if (saving) {
    toast('正在保存，请稍候');
    return false;
  }
  if (blocked) {
    showConflict();
    return false;
  }
  await flushDraft();
  if (activeDraft) return false;
  const before = structuredClone(db);
  try {
    await fn();
    if (!(await save())) {
      reconcile(before);
      return false;
    }
    ;
    render();
    return true;
  } catch (e) {
    reconcile(before);
    toast(e.message, 6000);
    return false;
  }
}
async function flushDraft() {
  clearTimeout(draftTimer);
  if (!activeDraft || saving) return;
  const draft = {
      ...activeDraft
    },
    n = node(draft.id);
  if (!n) return;
  if (n.content === draft.content) {
    activeDraft = null;
    localStorage.removeItem(draftKey);
    return;
  }
  const old = structuredClone(n),
    last = n.history.at(-1);
  if (!last || last.reason !== '自动保存' || Date.now() - Date.parse(last.at) > 60000) n.history.push({
    id: uid(),
    content: n.content,
    tags: [...n.tags],
    at: now(),
    reason: '自动保存'
  });
  n.content = draft.content;
  n.updated = now();
  try {
    localStorage.setItem(draftKey, JSON.stringify(draft));
  } catch {}
  if (!(await save())) {
    Object.assign(n, old);
    return;
  }
  if (activeDraft?.content === draft.content && activeDraft?.id === draft.id) {
    activeDraft = null;
    localStorage.removeItem(draftKey);
  }
  document.getElementById('save-state')?.replaceChildren(document.createTextNode(activeDraft ? '保存中…' : '已保存'));
  if (activeDraft) draftTimer = setTimeout(flushDraft, 200);
}
async function navigate(fn) {
  if (saving) {
    toast('正在保存，请稍候');
    return;
  }
  clearTimeout(filterTimer);
  await flushDraft();
  if (activeDraft) {
    toast('请先保存或复制草稿');
    return;
  }
  for (const d of commentDraft.items?.values() || []) d.editing = false;
  await fn();
  if (ui.page !== 'task') ui.task = null;
  ui.selected.clear();
  render();
}
function p() {
  return db.projects.find(p => p.id === ui.project) || db.projects[0] || {
    id: '',
    name: '工作空间',
    code: '',
    statuses: [],
    directory: '',
    labels: []
  };
}
function renderLogin() {
  const setup = !initialized;
  document.getElementById('overlay').innerHTML = '';
  document.getElementById('root').innerHTML = `<div class="login"><form id="login-form" class="login-inner"><div class="brand">${I('layout')}ProjectBoard</div><h2>${setup ? '创建你的工作空间' : '回到你的工作空间'}</h2><label>管理员<input value="admin" disabled autocomplete="username"></label>${setup && !setupToken ? '<label>初始化令牌<input name="token" required autocomplete="off" placeholder="粘贴启动窗口链接中的令牌"></label>' : ''}<label>${setup ? '设置密码' : '密码'}<input name="password" type="password" required ${setup ? 'minlength="8"' : ''} maxlength="72" autocomplete="${setup ? 'new-password' : 'current-password'}"></label>${setup ? '<label>确认密码<input name="confirm" type="password" required minlength="8" autocomplete="new-password"></label>' : ''}<button type="submit" class="primary" style="width:100%">${setup ? '创建工作空间' : '进入工作空间'}</button><p class="error" id="login-error" role="alert"></p></form></div>`;
  document.getElementById('login-form').onsubmit = async e => {
    e.preventDefault();
    const f = new FormData(e.target),
      password = f.get('password');
    if (setup && password !== f.get('confirm')) {
      document.getElementById('login-error').textContent = '请再次输入相同密码';
      return;
    }
    ;
    const submit = e.target.querySelector('[type=submit]');
    submit.disabled = true;
    try {
      const result = await api(setup ? '/setup' : '/login', {
        method: 'POST',
        body: JSON.stringify({
          password,
          token: setupToken || f.get('token')
        })
      });
      csrf = result.csrf;
      initialized = true;
      logged = true;
      setupToken = '';
      await loadState();
    } catch (err) {
      document.getElementById('login-error').textContent = err.message;
    } finally {
      submit.disabled = false;
    }
  };
}
async function loadState() {
  reconcile(await api('/state'));
  blocked = false;
  remoteRevision = null;
  if (!db.projects.length) {
    ui.page = 'projects';
    ui.task = null;
    ui.project = null;
  } else if (!db.projects.some(p => p.id === ui.project)) {
    ui.project = db.projects[0].id;
    ui.page = 'project';
    ui.tab = 'tasks';
    ui.task = null;
  }
  render();
  let draft;
  try {
    draft = JSON.parse(localStorage.getItem(draftKey));
  } catch {}
  if (draft && node(draft.id) && node(draft.id).content !== draft.content) {
    activeDraft = draft;
    modal('继续编辑草稿', `<textarea id="recovered-draft" rows="8" readonly>${E(draft.content)}</textarea><div class="actions">${button('draft-resume', '继续编辑', 'edit', 'primary')}${button('draft-discard', '使用已保存内容', 'history', 'border')}</div>`);
  }
}
async function bootstrap() {
  if (location.hash.startsWith('#setup/')) {
    setupToken = location.hash.slice(7);
    history.replaceState(null, '', '#/projects');
    ui.page = 'projects';
  }
  const session = await api('/session');
  csrf = session.csrf;
  initialized = session.initialized;
  logged = session.authenticated;
  document.getElementById('backup-file').accept = '.zip,.json,application/zip,application/json';
  if (logged) await loadState();else renderLogin();
  await pollNotifications();
  setInterval(pollNotifications, 15000);
  setInterval(async () => {
    if (!logged || saving || activeDraft || document.hidden) return;
    try {
      const version = await api('/revision');
      if (version.revision === db.revision) return;
      if (hasUserDraft()) {
        if (remoteRevision !== version.revision) {
          remoteRevision = version.revision;
          toast('工作空间已更新，保存时可保留当前草稿', 5000);
        }
        ;
        return;
      }
      ;
      const next = await api('/state');
      if (saving || activeDraft || hasUserDraft() || next.revision < db.revision) return;
      reconcile(next);
      if (ui.task && !db.tasks.some(t => t.id === ui.task && !t.deleted)) {
        ui.task = null;
        ui.page = 'project';
      }
      ;
      render();
    } catch (e) {
      if (e.status === 401) {
        logged = false;
        renderLogin();
      }
    }
  }, 5000);
}
function attachmentOK(a) {
  return a && /^[\w-]+$/.test(a.id) && typeof a.name === 'string' && a.name.length <= 255 && Number.isInteger(a.size) && a.size >= 0 && a.size <= 20 * 1024 * 1024 && (typeof a.sha256 === 'string' || typeof a.data === 'string');
}
async function readCommentFile(file, project=ui.project) {
  const form = new FormData();
  form.append('file', file);
  return await api('/attachments?project=' + encodeURIComponent(project), {
    method: 'POST',
    body: form
  });
}
async function addCommentFiles(id, files) {
	const project=db.tasks.find(t=>t.id===id||'detail:'+t.id===id)?.project;
	if(!project)return;
  const d = commentDraft(id),
    list = Array.from(files || []);
  if (!list.length || d.loading) return;
  try {
    if (list.some(f => f.size > 20 * 1024 * 1024)) throw Error('请选择 20 MB 以内的文件');
    if (d.files.length + list.length + (id.startsWith('detail:') ? (db.tasks.find(t => 'detail:' + t.id === id)?.detailAttachments || []).length : 0) > 8) throw Error('每个附件区域最多 8 个文件');
    d.loading = true;
    render();
    for (const f of list) d.files.push(await readCommentFile(f,project));
  } catch (e) {
    toast(e.message, 5000);
  } finally {
    d.loading = false;
    if ((ui.task === id || 'detail:' + ui.task === id) && ui.page === 'task') render();
  }
}
function attachmentURL(a, inline = false) {
  return '/api/attachments?project=' + encodeURIComponent(ui.project) + '&id=' + encodeURIComponent(a.id) + (inline ? '&inline=1' : '');
}
function attachmentHTML(a, noteId) {
  const image = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(a.type);
  return `<div class="comment-attachment">${image ? `<button class="attachment-thumbnail" data-action="attachment-preview" data-file="${a.id}" aria-label="预览 ${E(a.name)}"><img src="${E(attachmentURL(a, true))}" alt="${E(a.name)}" loading="lazy"></button>` : I('folder')}<button data-action="attachment-preview" data-file="${a.id}"><span>${E(a.name)}</span><small>${(a.size / 1024).toFixed(1)} KB</small></button>${iconBtn('comment-download', 'download', '下载 ' + E(a.name), `data-note="${noteId}" data-file="${a.id}"`)}</div>`;
}
function downloadCommentFile(b) {
  const t = db.tasks.find(t => t.id === ui.task),
    a = t?.notes.find(n => n.id === b.dataset.note && !n.deleted)?.attachments?.find(a => a.id === b.dataset.file);
  if (a) downloadAttachment(a);
}
function downloadDetailFile(b) {
  const a = db.tasks.find(t => t.id === ui.task)?.detailAttachments?.find(a => a.id === b.dataset.file);
  if (a) downloadAttachment(a);
}
function downloadAttachment(a) {
  const link = document.createElement('a');
  link.href = attachmentURL(a);
  link.download = a.name;
  link.click();
}
function allTags() {
  return [...new Set([...(p().labels || []), ...nodes().flatMap(n => n.tags), ...tasks().flatMap(t => t.tags)])].sort((a, b) => a.localeCompare(b, 'zh'));
}
function filteredTasks(all = false) {
  let ts = all ? db.tasks.filter(t => !t.deleted && !db.projects.find(p => p.id === t.project)?.archived && (!ui.workProject || ui.workProject === 'all' || t.project === ui.workProject)) : tasks();
  ts = ts.filter(t => (ui.status === 'all' || t.status === ui.status) && (ui.priority === 'all' || t.priority === ui.priority) && `${t.title} ${t.tags.join(' ')}`.toLowerCase().includes(ui.query.toLowerCase()));
  return ts.sort((a, b) => ui.sort === 'priority' ? (a.priority ? priorityValues().length - priorityValues().indexOf(a.priority) : 99) - (b.priority ? priorityValues().length - priorityValues().indexOf(b.priority) : 99) : ui.sort === 'due' ? (a.due || '9999').localeCompare(b.due || '9999') : a.order - b.order);
}
function taskColumns() {
  if (ui.page === 'work' && (!ui.workProject || ui.workProject === 'all')) return [...new Map(db.projects.filter(p => !p.archived).flatMap(p => p.statuses).map(s => [s.id, s])).values()];
  return p().statuses;
}
function workView() {
  const ts = db.tasks.filter(t => !t.deleted && !db.projects.find(p => p.id === t.project)?.archived && (!ui.workProject || ui.workProject === 'all' || t.project === ui.workProject)),
    open = ts.filter(t => !['done', 'cancelled'].includes(status(t).kind));
  return `<div class="heading"><h1>我的工作</h1><select id="work-project" aria-label="工作项目筛选"><option value="all">所有项目</option>${db.projects.filter(p => !p.archived).map(p => `<option value="${p.id}" ${ui.workProject === p.id ? 'selected' : ''}>${E(p.name)}</option>`).join('')}</select></div><div class="stats"><div><small>进行中</small><strong>${ts.filter(t => status(t).kind === 'active').length}</strong></div><div><small>今日到期</small><strong>${open.filter(t => t.due === today()).length}</strong></div><div><small>已逾期</small><strong>${open.filter(t => t.due && t.due < today()).length}</strong></div></div>${taskView(true)}`;
}
function openSearch() {
  modal('搜索工作空间', `<div class="inline-fields"><input id="global-search" aria-label="全局搜索" placeholder="搜索任务、知识内容或完整地址…"><select id="search-scope" aria-label="搜索范围"><option value="all">所有项目</option>${db.projects.filter(p => !p.archived).map(p => `<option value="${p.id}" ${ui.searchScope === p.id ? 'selected' : ''}>${E(p.name)}</option>`).join('')}</select></div><div id="search-results"></div>`, '', true);
  searchResults('');
}
function searchResults(query) {
  query = query.trim().toLowerCase();
  const results = [],
    projectVisible = id => (!ui.searchScope || ui.searchScope === 'all' || ui.searchScope === id) && !db.projects.find(p => p.id === id)?.archived;
  db.projects.filter(p => projectVisible(p.id) && p.name.toLowerCase().includes(query)).forEach(p => results.push({
    type: 'project',
    id: p.id,
    title: p.name,
    sub: '项目',
    icon: 'folder'
  }));
  db.nodes.filter(n => !n.deleted && projectVisible(n.project) && `${address(n)} ${n.content} ${n.tags.join(' ')}`.toLowerCase().includes(query)).forEach(n => results.push({
    type: 'node',
    id: n.id,
    title: address(n),
    sub: db.projects.find(p => p.id === n.project).name + ' · ' + n.content.slice(0, 70),
    icon: 'tree'
  }));
  db.tasks.filter(t => !t.deleted && projectVisible(t.project) && `${t.title} ${t.description} ${t.tags.join(' ')}`.toLowerCase().includes(query)).forEach(t => results.push({
    type: 'task',
    id: t.id,
    title: t.title,
    sub: '任务 · ' + status(t).name,
    icon: 'board'
  }));
  document.getElementById('search-results').innerHTML = results.slice(0, 40).map(r => `<button class="search-result" data-action="search-open" data-type="${r.type}" data-id="${r.id}">${I(r.icon)}<span>${E(r.title)}<small>${E(r.sub)}</small></span></button>`).join('') || empty('换个关键词试试');
}
function mcpView() {
  return `<div class="mcp-preview"><div class="heading"><h2>MCP</h2>${button('mcp-tokens', '管理接入令牌', 'settings', 'border')}</div><div class="node-section"><h3>连接当前项目</h3><pre>${E(JSON.stringify({
    mcpServers: {
      projectboard: {
        url: location.origin + '/api/mcp',
        headers: {
          Authorization: 'Bearer <项目令牌>',
          'X-Project-ID': ui.project
        }
      }
    }
  }, null, 2))}</pre></div><div class="node-section"><h3>工具调用</h3><div class="setting-row"><label>工具<select id="mcp-tool">${Object.keys(mcpExamples()).map(name => `<option ${(ui.mcpTool || 'list_tasks') === name ? 'selected' : ''}>${name}</option>`).join('')}</select></label>${button('mcp-run', '运行', 'chevron', 'primary')}</div><label>JSON 参数<textarea id="mcp-args" rows="9">${E(JSON.stringify(mcpExamples()[ui.mcpTool || 'list_tasks'], null, 2))}</textarea></label><p class="error" id="mcp-error" role="alert"></p><div class="caption">调用结果</div><pre id="mcp-result">运行后查看结果</pre></div></div>`;
}
function mcpExamples() {
  return {
    list_tasks: {
      project_id: ui.project,
      limit: 50
    },
    get_knowledge_node: {
      project_id: ui.project,
      address: nodes()[0] ? address(nodes()[0]) : '技术.开发'
    },
    list_labels: {
      project_id: ui.project
    },
    search: {
      project_id: ui.project,
      query: '',
      type: 'all'
    },
    attach_labels: {
      project_id: ui.project,
      expected_revision: db.revision,
      target_type: 'task',
      target_ids: tasks()[0] ? [tasks()[0].id] : [],
      tags: ['重点']
    },
    detach_labels: {
      project_id: ui.project,
      expected_revision: db.revision,
      target_type: 'task',
      target_ids: tasks()[0] ? [tasks()[0].id] : [],
      tags: ['重点']
    }
  };
}
async function tokenManager() {
  const tokens = await api('/tokens?project=' + encodeURIComponent(ui.project));
  modal('项目接入令牌', `${tokens.map(t => `<div class="plain-row"><span>${E(t.name)}</span><small>${E(t.created)}</small>${button('token-revoke', '撤销', null, 'danger', `data-id="${t.id}"`)}</div>`).join('')}<form id="token-form"><label>名称<input name="name" required placeholder="例如：桌面 AI 客户端"></label><button class="primary" type="submit">创建令牌</button></form>`);
  document.getElementById('token-form').onsubmit = async e => {
    e.preventDefault();
    try {
      const value = await api('/tokens?project=' + ui.project, {
        method: 'POST',
        body: JSON.stringify({
          name: new FormData(e.target).get('name')
        })
      });
      modal('保存接入令牌', `<p>复制并保存，关闭后通过新建令牌获取新的凭据。</p><textarea id="new-token" rows="3" readonly>${E(value.token)}</textarea>${button('token-copy', '复制令牌', 'copy', 'primary')}`);
    } catch (e) {
      toast(e.message);
    }
  };
}
actions['mcp-tokens'] = tokenManager;
actions['token-copy'] = () => copy(document.getElementById('new-token').value);
actions['token-revoke'] = b => confirmModal('撤销接入令牌', '<p>使用此令牌的客户端将结束项目访问。</p>', async () => {
  await api('/tokens?project=' + ui.project + '&id=' + b.dataset.id, {
    method: 'DELETE'
  });
  await tokenManager();
}, '撤销');
actions['mcp-run'] = async () => {
  try {
    const args = JSON.parse(document.getElementById('mcp-args').value),
      result = await api('/call', {
        method: 'POST',
        body: JSON.stringify({
          tool: ui.mcpTool || 'list_tasks',
          args
        })
      });
    document.getElementById('mcp-error').textContent = '';
    document.getElementById('mcp-result').textContent = JSON.stringify(result, null, 2);
    reconcile(await api('/state'));
  } catch (e) {
    document.getElementById('mcp-error').textContent = e.message;
  }
};
actions['logout'] = async () => {
  await flushDraft();
  if (activeDraft) return;
  await api('/logout', {
    method: 'POST'
  });
  logged = false;
  csrf = '';
  renderLogin();
};
actions['password'] = () => formModal('修改密码', `<label>当前密码<input type="password" name="old" required autocomplete="current-password"></label><label>新密码<input type="password" name="password" minlength="8" maxlength="72" required autocomplete="new-password"></label><label>确认密码<input type="password" name="confirm" required autocomplete="new-password"></label>`, async f => {
  if (f.get('password') !== f.get('confirm')) throw Error('请再次输入相同的新密码');
  const result = await api('/password', {
    method: 'POST',
    body: JSON.stringify({
      old: f.get('old'),
      password: f.get('password')
    })
  });
  csrf = result.csrf;
  closeModal();
  toast('密码已更新');
});
actions['backup'] = async () => {
  await flushDraft();
  const blob = await api('/backup');
  download('projectboard-' + today() + '.zip', blob, 'application/zip');
  toast('备份已导出');
};
actions['restore'] = () => {
  const input = document.getElementById('backup-file');
  input.value = '';
  input.onchange = () => {
    const file = input.files[0];
    if (!file) return;
    confirmModal('恢复备份', `<p>先备份当前数据，再使用 ${E(file.name)} 替换工作空间。</p>`, async () => {
      try {
        const st = await api('/restore', {
          method: 'POST',
          headers: {
            'X-Revision': String(db.revision)
          },
          body: file
        });
        reconcile(st);
        commentDraft.items?.clear();
        ui.page = 'projects';
        ui.task = null;
        ui.project = db.projects[0]?.id;
        ui.node = null;
        activeDraft = null;
        localStorage.removeItem(draftKey);
        render();
        toast('备份已恢复');
      } catch (e) {
        toast(e.message, 7000);
      }
    }, '备份并恢复');
  };
  input.click();
};
delete actions.reset;
actions['runtime-info'] = async () => {
  const info = await api('/info');
  modal('运行信息', `<p>ProjectBoard ${E(info.version)}</p><label>数据目录<input readonly value="${E(info.dataDir)}"></label><p>SQLite · 附件本地存储 · 附件总容量 256 MB</p>${button('shutdown', '停止服务', null, 'danger')}`);
};
actions['shutdown'] = () => confirmModal('停止本地服务', '<p>保存工作内容后停止。下次运行 ProjectBoard 即可继续。</p>', async () => {
  await flushDraft();
  if (activeDraft) return;
  await api('/shutdown', {
    method: 'POST'
  });
  logged = false;
  document.getElementById('root').innerHTML = '<div class="empty"><h2>服务已停止</h2><p>运行 ProjectBoard，继续你的工作</p></div>';
}, '停止服务');
actions['export-draft'] = () => download('projectboard-draft.json', JSON.stringify({
  ...db,
  nodeDraft: activeDraft,
  commentDrafts: [...(commentDraft.items || [])]
}, null, 2));
actions['reload-state'] = async () => {
  closeModal();
  await loadState();
};
actions['draft-resume'] = () => {
  const n = node(activeDraft.id);
  ui.project = n.project;
  ui.node = n.id;
  ui.page = 'project';
  ui.tab = 'knowledge';
  closeModal();
  render();
  document.getElementById('node-content').value = activeDraft.content;
};
actions['draft-discard'] = () => {
  activeDraft = null;
  localStorage.removeItem(draftKey);
  closeModal();
  render();
};
actions['notifications'] = async () => {
  const items = await api('/notifications');
  modal('提醒', `<div class="actions">${button('enable-notifications', '开启桌面提醒', 'history', 'border')}</div>` + (items.map(n => `<div class="plain-row"><button data-action="notification-open" data-task="${E(n.task)}" data-id="${E(n.id)}">${E(n.text)}</button><small>${E(n.at)}</small>${button('notification-read', '完成', null, 'border', `data-id="${E(n.id)}"`)}</div>`).join('') || '<p>提醒已处理</p>'));
};
function notificationButton() {
  return button('notifications', '提醒' + (notificationCount ? ' ' + notificationCount : ''), 'history', '', `id="notification-button" aria-label="提醒${notificationCount ? ' ' + notificationCount : ''}"`);
}
async function pollNotifications() {
  if (!logged) return;
  try {
    const items = await api('/notifications');
    notificationCount = items.length;
    const el = document.getElementById('notification-button');
    if (el) {
      el.innerHTML = I('history') + '提醒' + (notificationCount ? ' ' + notificationCount : '');
      el.setAttribute('aria-label', '提醒' + (notificationCount ? ' ' + notificationCount : ''));
    }
    ;
    let seen = [];
    try {
      seen = JSON.parse(sessionStorage.getItem(key + '.reminders')) || [];
    } catch {}
    ;
    for (const n of items) {
      if (seen.includes(n.id)) continue;
      toast('任务提醒：' + n.text, 6000);
      if ('Notification' in window && Notification.permission === 'granted') new Notification('ProjectBoard', {
        body: n.text,
        tag: n.id
      });
      seen.push(n.id);
    }
    ;
    sessionStorage.setItem(key + '.reminders', JSON.stringify(seen.slice(-500)));
  } catch {}
}
actions['enable-notifications'] = async () => {
  if (!('Notification' in window)) {
    toast('在提醒中心查看任务提醒');
    return;
  }
  ;
  const result = await Notification.requestPermission();
  toast(result === 'granted' ? '桌面提醒已开启' : '在提醒中心查看任务提醒');
};
actions['reconnect'] = () => location.reload();
actions['export-task-json'] = async () => {
  await flushDraft();
  download(p().code + '-tasks.json', JSON.stringify({
    format: 'projectboard.tasks.v1',
    project: p(),
    tasks: tasks()
  }, null, 2));
  toast('项目任务已导出');
};
actions['export-knowledge'] = async () => {
  await flushDraft();
  download(p().code + '-knowledge.json', JSON.stringify({
    format: 'projectboard.knowledge.v1',
    project: p(),
    nodes: nodes()
  }, null, 2));
  toast('知识树已导出');
};
let filePreview = null;
actions['attachment-preview'] = async b => {
  const t = db.tasks.find(t => t.id === ui.task),
    a = [...(t?.detailAttachments || []), ...(t?.notes || []).filter(c => !c.deleted).flatMap(c => c.attachments || [])].find(a => a.id === b.dataset.file);
  if (!a) return;
  filePreview = {
    id: a.id,
    a,
    zoom: 100,
    source: false
  };
  modal(a.name, '<div id="attachment-preview-body">正在打开…</div>', button('file-download', '下载文件', 'download', 'border'), true);
  try {
    const content = await api('/attachments?project=' + encodeURIComponent(ui.project) + '&id=' + encodeURIComponent(a.id) + '&preview=1');
    if (filePreview?.id !== a.id || !document.getElementById('attachment-preview-body')) return;
    filePreview.content = content;
    renderFilePreview();
  } catch (e) {
    const host = document.getElementById('attachment-preview-body');
    if (host) host.textContent = e.message;
  }
};
function renderFilePreview() {
  const f = filePreview,
    host = document.getElementById('attachment-preview-body');
  if (!f?.content || !host) return;
  const c = f.content;
  if (c.kind === 'image') {
    host.innerHTML = `<div class="file-preview-tools">${button('image-zoom-out', '缩小', null, 'border')}<span>${f.zoom}%</span>${button('image-zoom-in', '放大', null, 'border')}${button('image-fit', '适应窗口', null, 'border')}</div><div class="attachment-preview-image"><img src="${E(attachmentURL(f.a, true))}" alt="${E(f.a.name)}" style="--preview-scale:${f.zoom}%"></div>`;
    return;
  }
  ;
  host.innerHTML = `${c.kind === 'markdown' ? `<div class="file-preview-tools">${button('file-rendered', '预览', null, f.source ? 'border' : 'primary')}${button('file-source', '源码', null, f.source ? 'primary' : 'border')}</div>` : ''}${c.truncated ? '<p class="muted">已展示前 1 MB · 下载查看完整内容</p>' : ''}${c.kind === 'markdown' && !f.source ? '<article class="markdown-preview">' + c.html + '</article>' : '<pre class="text-preview">' + E(c.text) + '</pre>'}`;
  host.querySelectorAll('.markdown-preview a').forEach(a => {
    if (/^https?:\/\//i.test(a.getAttribute('href') || '')) {
      a.target = '_blank';
      a.rel = 'noopener noreferrer';
    } else {
      a.removeAttribute('href');
    }
  });
  host.querySelectorAll('.markdown-preview img').forEach(img => {
    const source = img.getAttribute('src') || '';
    if (!/^data:image\/(png|jpeg|gif|webp);base64,/i.test(source)) {
      const alt = document.createElement('span');
      alt.textContent = img.alt || '图片';
      img.replaceWith(alt);
    }
  });
}
actions['file-download'] = () => filePreview && downloadAttachment(filePreview.a);
actions['file-source'] = () => {
  filePreview.source = true;
  renderFilePreview();
};
actions['file-rendered'] = () => {
  filePreview.source = false;
  renderFilePreview();
};
actions['image-zoom-in'] = () => {
  filePreview.zoom = Math.min(400, filePreview.zoom + 25);
  renderFilePreview();
};
actions['image-zoom-out'] = () => {
  filePreview.zoom = Math.max(25, filePreview.zoom - 25);
  renderFilePreview();
};
actions['image-fit'] = () => {
  filePreview.zoom = 100;
  renderFilePreview();
};
actions['notification-read'] = async b => {
  await api('/notifications', {
    method: 'POST',
    body: JSON.stringify({
      id: b.dataset.id
    })
  });
  await actions.notifications();
};
actions['notification-open'] = async b => {
  await api('/notifications', {
    method: 'POST',
    body: JSON.stringify({
      id: b.dataset.id
    })
  });
  closeModal();
  const t = db.tasks.find(t => t.id === b.dataset.task && !t.deleted);
  if (t) await navigate(() => {
    ui.project = t.project;
    ui.task = t.id;
    ui.page = 'task';
  });
};
// Catch errors from every asynchronous event callback at the interface boundary.
window.addEventListener('unhandledrejection', e => {
  e.preventDefault();
  toast(e.reason?.message || '请重试当前操作', 6000);
});
document.addEventListener('input', e => {
  if (e.target.id === 'node-content') {
    try {
      localStorage.setItem(draftKey, JSON.stringify({
        id: ui.node,
        content: e.target.value
      }));
    } catch {}
  }
});
document.addEventListener('change', async e => {
  if (e.target.id === 'search-scope') {
    ui.searchScope = e.target.value;
    searchResults(document.getElementById('global-search').value);
  } else if (e.target.id === 'work-project') {
    await navigate(() => {
      ui.workProject = e.target.value;
      ui.status = 'all';
      if (ui.workProject !== 'all') ui.project = ui.workProject;
    });
  }
});
window.addEventListener('beforeunload', e => {
  if (saving || activeDraft) {
    e.preventDefault();
    e.returnValue = '请先保存草稿';
  }
});
bootstrap().catch(error => { document.getElementById("root").innerHTML = `<div class="empty"><h2>连接工作空间</h2><p>${E(error.message)}</p><button data-action="reconnect">重新连接</button></div>`; });
