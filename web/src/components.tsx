import { type ButtonHTMLAttributes, type PropsWithChildren, useEffect, useState } from 'react'
import { AlertTriangle, Bot, Check, ChevronRight, Circle, Command, GitBranch, Inbox, LoaderCircle, Search, X } from 'lucide-react'
import type { RunStatus, TaskStatus } from './types'

export function Button({variant='default',className='',...props}:ButtonHTMLAttributes<HTMLButtonElement>&{variant?:'primary'|'default'|'ghost'|'danger'}){
  return <button className={`button ${variant} ${className}`} {...props}/>
}

export function IconButton({label,...props}:ButtonHTMLAttributes<HTMLButtonElement>&{label:string}){
  return <button className="icon-button" aria-label={label} title={label} {...props}/>
}

const taskLabels:Record<TaskStatus,string>={inbox:'收件箱',planning:'待规划',ready:'可执行',in_progress:'进行中',waiting_user:'等待用户',verifying:'验证中',completed:'已完成',cancelled:'已取消'}
const runLabels:Record<RunStatus,string>={not_started:'未启动',queued:'排队中',running:'运行中',waiting_approval:'等待授权',waiting_input:'等待输入',paused:'已暂停',succeeded:'成功',failed:'失败',cancelled:'已取消',interrupted:'异常中断'}

export function TaskStatusBadge({status}:{status:TaskStatus}){return <span className={`badge task-${status}`}><Circle/> {taskLabels[status]}</span>}
export function RunStatusBadge({status}:{status:RunStatus}){return <span className={`badge run-${status}`}>{status==='running'?<LoaderCircle className="spin"/>:<Bot/>}{runLabels[status]}</span>}
export function Tag({children}:{children:React.ReactNode}){return <span className="tag">{children}</span>}

export function PageTitle({eyebrow,title,description,actions}:{eyebrow?:string;title:string;description?:string;actions?:React.ReactNode}){
  return <header className="page-title"><div>{eyebrow&&<span className="eyebrow">{eyebrow}</span>}<h1>{title}</h1>{description&&<p>{description}</p>}</div>{actions&&<div className="page-actions">{actions}</div>}</header>
}

export function EmptyState({icon,title,body,action}:{icon?:React.ReactNode;title:string;body:string;action?:React.ReactNode}){
  return <div className="empty-state">{icon||<Command/>}<h2>{title}</h2><p>{body}</p>{action}</div>
}

export function Drawer({open,title,onClose,children}:PropsWithChildren<{open:boolean;title:string;onClose:()=>void}>){
  useEffect(()=>{const fn=(e:KeyboardEvent)=>{if(e.key==='Escape')onClose()};addEventListener('keydown',fn);return()=>removeEventListener('keydown',fn)},[onClose])
  if(!open)return null
  return <div className="overlay drawer-overlay" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><aside className="drawer"><header><div><span className="eyebrow">任务详情</span><h2>{title}</h2></div><IconButton label="关闭" onClick={onClose}><X/></IconButton></header>{children}</aside></div>
}

export function Modal({open,title,children,onClose}:PropsWithChildren<{open:boolean;title:string;onClose:()=>void}>){
  if(!open)return null
  return <div className="overlay modal-overlay" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><section className="modal" role="dialog" aria-modal="true"><header><h2>{title}</h2><IconButton label="关闭" onClick={onClose}><X/></IconButton></header>{children}</section></div>
}

export function CommandPalette({open,onClose}:{open:boolean;onClose:()=>void}){
  const [q,setQ]=useState('')
  if(!open)return null
  return <div className="overlay command-overlay" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><section className="command-palette"><div className="command-input"><Search/><input autoFocus value={q} onChange={e=>setQ(e.target.value)} placeholder="搜索任务、知识、运行或输入命令…"/><kbd>Esc</kbd></div><div className="command-results"><span className="eyebrow">快速前往</span>{['创建任务','查看等待授权的执行','打开全局搜索','检查项目 Git 状态'].filter(v=>v.includes(q)||!q).map(v=><button key={v}>{v}<ChevronRight/></button>)}</div></section></div>
}

export function RiskBanner(){return <div className="risk-banner"><AlertTriangle/> 当前服务监听了非回环地址且未启用鉴权。网络中的其他设备可能访问本地项目与执行接口。</div>}

export function LiveDot(){return <span className="live-dot" aria-label="实时连接正常"/>}
export function Verified(){return <span className="verified"><Check/> 已验证</span>}
export function GitPill({branch='main'}:{branch?:string}){return <span className="status-pill"><GitBranch/>{branch}</span>}
export function InboxPill({count=0}:{count?:number}){return <span className="status-pill"><Inbox/>收件箱{count>0&&<b>{count}</b>}</span>}
