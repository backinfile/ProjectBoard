import { lazy, Suspense, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Navigate, NavLink, Route, Routes, useLocation, useNavigate, useParams } from 'react-router-dom'
import { Activity, Archive, Bell, Bot, BookOpen, Boxes, ChevronDown, ChevronsLeft, CircleGauge, Columns3, GitBranch, Inbox, LayoutDashboard, Menu, Search, Settings, ShieldCheck, Sparkles, Workflow, X } from 'lucide-react'
import { api } from './api'
import { CommandPalette, IconButton, LiveDot, RiskBanner } from './components'
import type { Project } from './types'

const Welcome=lazy(()=>import('./pages').then(m=>({default:m.WelcomePage})))
const Overview=lazy(()=>import('./pages').then(m=>({default:m.OverviewPage})))
const Board=lazy(()=>import('./pages').then(m=>({default:m.BoardPage})))
const TaskList=lazy(()=>import('./pages').then(m=>({default:m.TaskListPage})))
const TaskWorkspace=lazy(()=>import('./pages').then(m=>({default:m.TaskWorkspacePage})))
const Changes=lazy(()=>import('./pages').then(m=>({default:m.ChangesPage})))
const Knowledge=lazy(()=>import('./pages').then(m=>({default:m.KnowledgePage})))
const KnowledgeHistory=lazy(()=>import('./pages').then(m=>({default:m.KnowledgeHistoryPage})))
const Agents=lazy(()=>import('./pages').then(m=>({default:m.AgentsPage})))
const SearchPage=lazy(()=>import('./pages').then(m=>({default:m.SearchPage})))
const InboxPage=lazy(()=>import('./pages').then(m=>({default:m.InboxPage})))
const Automation=lazy(()=>import('./pages').then(m=>({default:m.AutomationPage})))
const Templates=lazy(()=>import('./pages').then(m=>({default:m.TemplatesPage})))
const Audit=lazy(()=>import('./pages').then(m=>({default:m.AuditPage})))
const SettingsPage=lazy(()=>import('./pages').then(m=>({default:m.SettingsPage})))

export function App(){
  const projects=useQuery({queryKey:['projects'],queryFn:api.projects})
  if(projects.isLoading)return <BootScreen/>
  return <Suspense fallback={<div className="boot"><span className="brand-mark"><Sparkles/></span><b>ProjectBoard</b><span>正在恢复本地工作区</span></div>}>
    <Routes>
      <Route path="/welcome" element={<Welcome/>}/>
      <Route path="/search" element={<Standalone><SearchPage/></Standalone>}/>
      <Route path="/inbox" element={<Standalone><InboxPage/></Standalone>}/>
      <Route path="/templates" element={<Standalone><Templates/></Standalone>}/>
      <Route path="/p/:pid/*" element={<ProjectShell projects={projects.data||[]}/>}/>
      <Route path="*" element={<Navigate to={projects.data?.[0]?`/p/${projects.data[0].id}/overview`:'/welcome'} replace/>}/>
    </Routes>
  </Suspense>
}

function BootScreen(){return <div className="boot"><span className="brand-mark"><Sparkles/></span><b>ProjectBoard</b><span>正在读取本地项目</span></div>}

function Standalone({children}:{children:React.ReactNode}){return <div className="standalone"><StandaloneTop/>{children}</div>}
function StandaloneTop(){const navigate=useNavigate();return <header className="topbar standalone-top"><button className="brand" onClick={()=>navigate('/welcome')}><span className="brand-mark"><Sparkles/></span><b>ProjectBoard</b></button><div className="top-spacer"/><button className="top-action" onClick={()=>navigate('/search')}><Search/>搜索</button><button className="top-action" onClick={()=>navigate('/inbox')}><Inbox/>收件箱</button></header>}

function ProjectShell({projects}:{projects:Project[]}){
  const {pid}=useParams();const project=projects.find(p=>p.id===pid);const navigate=useNavigate();const location=useLocation()
  const [collapsed,setCollapsed]=useState(false);const [mobile,setMobile]=useState(false);const [contextOpen,setContextOpen]=useState(true);const [palette,setPalette]=useState(false)
  const runs=useQuery({queryKey:['runs'],queryFn:()=>api.runs(),refetchInterval:5000})
  const taskCount=useQuery({queryKey:['tasks',pid],queryFn:()=>api.tasks(pid!),enabled:!!pid})
  const health=useQuery({queryKey:['health'],queryFn:api.health})
  useEffect(()=>{const onKey=(e:KeyboardEvent)=>{if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='k'){e.preventDefault();setPalette(v=>!v)}if(e.key==='Escape')setPalette(false)};addEventListener('keydown',onKey);return()=>removeEventListener('keydown',onKey)},[])
  useEffect(()=>{const source=new EventSource('/api/v1/events');source.onmessage=()=>{};return()=>source.close()},[])
  if(!project&&projects.length)return <Navigate to={`/p/${projects[0].id}/overview`} replace/>
  if(!project)return <Navigate to="/welcome" replace/>
  const activeRuns=(runs.data||[]).filter(r=>['queued','running','waiting_approval','waiting_input'].includes(r.status))
  const nav=[
    ['overview','总览',LayoutDashboard],['tasks/board','任务',Columns3],['knowledge','知识库',BookOpen],['agents','AI 配置',Bot],['runs','执行队列',Activity],['git','Git',GitBranch],['automation','自动化',Workflow],['audit','审计',ShieldCheck],['settings','设置',Settings],
  ] as const
  return <div className={`app-shell ${collapsed?'nav-collapsed':''} ${contextOpen?'':'context-collapsed'}`}>
    <header className="topbar">
      <IconButton label="菜单" className="mobile-menu" onClick={()=>setMobile(v=>!v)}><Menu/></IconButton>
      <button className="brand" onClick={()=>navigate(`/p/${pid}/overview`)}><span className="brand-mark"><Sparkles/></span><b>ProjectBoard</b></button>
      <div className="project-switch"><span className="project-color" style={{background:project.color}}/><span>{project.name}</span><ChevronDown/></div>
      <button className="global-search" onClick={()=>setPalette(true)}><Search/><span>搜索任务、知识、运行…</span><kbd>Ctrl K</kbd></button>
      <div className="top-spacer"/><div className="run-indicator"><LiveDot/><span>{activeRuns.length?`${activeRuns.length} 个 Agent 活动中`:'Agent 就绪'}</span></div>
      <IconButton label="收件箱" onClick={()=>navigate('/inbox')}><Bell/></IconButton>
      <button className="avatar">本</button>
    </header>
    <aside className={`sidenav ${mobile?'mobile-open':''}`}><div className="nav-scroll">{nav.map(([path,label,Icon])=><NavLink key={path} to={`/p/${pid}/${path}`} className={({isActive})=>isActive||(path==='tasks/board'&&location.pathname.includes('/tasks/'))?'active':''} onClick={()=>setMobile(false)}><Icon/><span>{label}</span>{path==='tasks/board'&&<b className="nav-count">{taskCount.data?.length||0}</b>}</NavLink>)}</div><button className="collapse-nav" onClick={()=>setCollapsed(v=>!v)}><ChevronsLeft/><span>收起导航</span></button></aside>
    {mobile&&<button className="mobile-scrim" aria-label="关闭菜单" onClick={()=>setMobile(false)}><X/></button>}
    <main className="workspace"><Routes>
      <Route path="overview" element={<Overview/>}/><Route path="tasks/board" element={<Board/>}/><Route path="tasks/list" element={<TaskList/>}/><Route path="tasks/:tid" element={<TaskWorkspace/>}/><Route path="tasks/:tid/chat/:cid" element={<TaskWorkspace/>}/>
      <Route path="runs/:rid/changes" element={<Changes/>}/><Route path="runs" element={<Changes queueMode/>}/><Route path="git" element={<Changes gitMode/>}/>
      <Route path="knowledge" element={<Knowledge/>}/><Route path="knowledge/:kid/history" element={<KnowledgeHistory/>}/><Route path="agents" element={<Agents/>}/><Route path="automation" element={<Automation/>}/><Route path="audit" element={<Audit/>}/><Route path="settings" element={<SettingsPage/>}/><Route path="*" element={<Navigate to="overview" replace/>}/>
    </Routes></main>
    <aside className="context-panel"><button className="context-handle" onClick={()=>setContextOpen(v=>!v)}><ChevronsLeft/></button><div className="context-content"><span className="eyebrow">活动现场</span><h3>Agent 与确认</h3>{activeRuns.length?activeRuns.slice(0,4).map(run=><button className="run-context" key={run.id}><span className={`run-orb ${run.status}`}/><span><b>{run.prompt.slice(0,42)}</b><small>{run.status} · {run.id.slice(-6)}</small></span></button>):<div className="quiet"><Bot/><span>当前没有运行中的 Agent</span></div>}<div className="context-divider"/><span className="eyebrow">项目脉搏</span><div className="health-row"><CircleGauge/><span>目录与存储健康</span><b>正常</b></div><div className="health-row"><Archive/><span>待审核修改</span><b>0</b></div></div></aside>
    <footer className="statusbar"><span><GitBranch/>{project.defaultBranch}</span><span><Boxes/>SQLite 已保存</span><span><Activity/>{activeRuns.length} 个执行活动中</span><div/><span>{project.path}</span></footer>
    <CommandPalette open={palette} onClose={()=>setPalette(false)}/>
    {health.data?.externalListen&&<RiskBanner/>}
  </div>
}
