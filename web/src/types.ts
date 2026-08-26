export type TaskStatus = 'inbox' | 'planning' | 'ready' | 'in_progress' | 'waiting_user' | 'verifying' | 'completed' | 'cancelled'
export type RunStatus = 'not_started' | 'queued' | 'running' | 'waiting_approval' | 'waiting_input' | 'paused' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted'

export interface Project { id:string; name:string; description:string; path:string; color:string; defaultBranch:string; defaultAgentId:string; archived:boolean; createdAt:string; updatedAt:string }
export interface AcceptanceCriterion { id:string; text:string; done:boolean }
export interface Task { id:string; projectId:string; key:string; title:string; description:string; status:TaskStatus; runStatus:RunStatus; priority:string; labels:string[]; acceptance:AcceptanceCriterion[]; dueAt?:string; version:number; createdAt:string; updatedAt:string }
export interface AgentProfile { id:string; projectId?:string; name:string; model:string; reasoning:string; systemPrompt:string; sandbox:string; network:boolean; isDefault:boolean; createdAt:string }
export interface Conversation { id:string; taskId:string; agentId:string; title:string; summary:string; codexThreadId?:string; createdAt:string; updatedAt:string }
export interface Message { id:string; conversationId:string; role:'user'|'assistant'|'system'; kind:string; content:string; createdAt:string }
export interface Run { id:string; taskId:string; conversationId:string; agentId:string; workspaceId?:string; status:RunStatus; prompt:string; startedAt?:string; finishedAt?:string; createdAt:string }
export interface Dashboard { taskCount:number; statusCounts:Record<string,number>; activeRuns:number; unreviewedFiles:number; recentTasks:Task[] }
export interface ApiError { error:{ code:string; message:string } }
