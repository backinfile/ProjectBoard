import type {
  AgentProfile,
  Conversation,
  Dashboard,
  Message,
  Project,
  Run,
  Task,
  TaskStatus,
} from "./types";

const jsonHeaders = { "Content-Type": "application/json" };

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/v1${path}`, init);
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new Error(body?.error?.message || `请求失败（${response.status}）`);
  }
  return response.json();
}

export const api = {
  health: () => request<{ status: string; externalListen: boolean }>("/health"),
  drives: () => request<string[]>("/filesystem/drives"),
  directories: (path: string) =>
    request<{ name: string; path: string }[]>(
      `/filesystem/directories?path=${encodeURIComponent(path)}`,
    ),
  projects: () => request<Project[]>("/projects"),
  project: (id: string) => request<Project>(`/projects/${id}`),
  createProject: (body: Partial<Project>) =>
    request<Project>("/projects", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    }),
  tasks: (projectId: string) => request<Task[]>(`/projects/${projectId}/tasks`),
  task: (id: string) => request<Task>(`/tasks/${id}`),
  createTask: (projectId: string, body: Partial<Task>) =>
    request<Task>(`/projects/${projectId}/tasks`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    }),
  updateTaskStatus: (id: string, status: TaskStatus, version: number) =>
    request<Task>(`/tasks/${id}/status`, {
      method: "PATCH",
      headers: jsonHeaders,
      body: JSON.stringify({ status, version }),
    }),
  dashboard: (projectId: string) =>
    request<Dashboard>(`/projects/${projectId}/dashboard`),
  agents: (projectId: string) =>
    request<AgentProfile[]>(`/projects/${projectId}/agents`),
  conversations: (taskId: string) =>
    request<Conversation[]>(`/tasks/${taskId}/conversations`),
  conversation: (taskId: string, agentId: string) =>
    request<Conversation>(`/tasks/${taskId}/conversations`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ agentId }),
    }),
  messages: (conversationId: string) =>
    request<Message[]>(`/conversations/${conversationId}/messages`),
  sendMessage: (
    conversationId: string,
    body: { taskID: string; agentID: string; content: string },
  ) =>
    request<{ message: Message; run: Run }>(
      `/conversations/${conversationId}/messages`,
      { method: "POST", headers: jsonHeaders, body: JSON.stringify(body) },
    ),
  runs: (taskId?: string) =>
    request<Run[]>(`/runs${taskId ? `?taskId=${taskId}` : ""}`),
  search: (query: string, projectId?: string) =>
    request<Record<string, string>[]>(
      `/search?q=${encodeURIComponent(query)}${projectId ? `&projectId=${projectId}` : ""}`,
    ),
  templates: () => request<Record<string, unknown>[]>("/templates"),
  notifications: () => request<Record<string, unknown>[]>("/notifications"),
  markAllNotificationsRead: () =>
    request("/notifications/read-all", {
      method: "POST",
      headers: jsonHeaders,
      body: "{}",
    }),
  audit: () => request<Record<string, unknown>[]>("/audit"),
  settings: () => request<Record<string, unknown>[]>("/settings"),
  knowledge: (projectId: string) =>
    request<Record<string, unknown>[]>(`/projects/${projectId}/knowledge`),
  automations: (projectId: string) =>
    request<Record<string, unknown>[]>(`/projects/${projectId}/automations`),
  proposeParallelPlan: (
    taskId: string,
    body: {
      mainAgentId: string;
      prompt: string;
      agents: { agentId: string; instruction: string }[];
    },
  ) =>
    request<{ id: string }>(`/tasks/${taskId}/parallel-plans`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    }),
  approveParallelPlan: (planId: string) =>
    request<{ runs: Run[] }>(`/parallel-plans/${planId}/approve`, {
      method: "POST",
      headers: jsonHeaders,
      body: "{}",
    }),
  decideApproval: (
    approvalId: string,
    decision: "accept" | "acceptForSession" | "decline" | "cancel",
  ) =>
    request(`/approvals/${approvalId}/decide`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ decision }),
    }),
};
