import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  ArrowUpRight,
  Bell,
  Bot,
  BookOpen,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  CircleDot,
  ClipboardCheck,
  Columns3,
  Command,
  Copy,
  Database,
  FileCode2,
  FileDiff,
  FilePlus2,
  FileText,
  Filter,
  Folder,
  FolderGit2,
  GitBranch,
  GitCommitHorizontal,
  History,
  Inbox,
  KeyRound,
  LayoutList,
  ListFilter,
  Lock,
  MessageSquare,
  MoreHorizontal,
  PanelRightOpen,
  Pause,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  Send,
  Settings2,
  Shield,
  ShieldAlert,
  SlidersHorizontal,
  Sparkles,
  Square,
  Terminal,
  TestTube2,
  Unlock,
  UserRound,
  Zap,
} from "lucide-react";
import { api } from "./api";
import { DirectoryPicker } from "./DirectoryPicker";
import {
  Button,
  Drawer,
  EmptyState,
  IconButton,
  Modal,
  PageTitle,
  RunStatusBadge,
  Tag,
  TaskStatusBadge,
  Verified,
} from "./components";
import { parseAgentMentions } from "./mentions";
import type { AgentProfile, Message, Task, TaskStatus } from "./types";

const statusColumns: { id: TaskStatus; label: string; accent: string }[] = [
  { id: "inbox", label: "收件箱", accent: "gray" },
  { id: "planning", label: "待规划", accent: "cyan" },
  { id: "ready", label: "可执行", accent: "lime" },
  { id: "in_progress", label: "进行中", accent: "blue" },
  { id: "waiting_user", label: "等待用户", accent: "amber" },
  { id: "verifying", label: "验证中", accent: "violet" },
  { id: "completed", label: "已完成", accent: "green" },
];

export function WelcomePage() {
  const query = useQuery({ queryKey: ["projects"], queryFn: api.projects });
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [form, setForm] = useState({ name: "", path: "", description: "" });
  const [error, setError] = useState("");
  const create = useMutation({
    mutationFn: api.createProject,
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: ["projects"] });
      navigate(`/p/${p.id}/overview`);
    },
    onError: (e) => setError(e.message),
  });
  return (
    <div className="welcome-page">
      <div className="welcome-ambient">
        <div className="ambient-grid" />
        <div className="orbit orbit-one" />
        <div className="orbit orbit-two" />
        <span className="ambient-label">LOCAL EXECUTION FIELD · WINDOWS</span>
      </div>
      <header className="welcome-top">
        <div className="brand">
          <span className="brand-mark">
            <Sparkles />
          </span>
          <b>ProjectBoard</b>
        </div>
        <span>本地 AI 项目执行中心</span>
      </header>
      <main className="welcome-content">
        <span className="eyebrow">任务 · Agent · 文件 · 验证 · 知识</span>
        <h1>
          把 AI 的每一次
          <br />
          <em>行动变成证据。</em>
        </h1>
        <p>
          从本地项目目录开始，在一个可控、可恢复、可审计的工作空间里组织任务与
          Codex。
        </p>
        <div className="welcome-actions">
          <Button variant="primary" onClick={() => setOpen(true)}>
            <Folder />
            添加本地项目
          </Button>
          {query.data?.[0] && (
            <Button onClick={() => navigate(`/p/${query.data[0].id}/overview`)}>
              继续最近项目
              <ArrowRight />
            </Button>
          )}
        </div>
      </main>
      <section className="recent-projects">
        <header>
          <span className="eyebrow">最近项目</span>
          <span>{query.data?.length || 0} 个本地目录</span>
        </header>
        {query.isLoading ? (
          <p className="muted">正在读取本地数据库…</p>
        ) : query.data?.length ? (
          <div className="project-lines">
            {query.data.map((p, i) => (
              <button
                key={p.id}
                onClick={() => navigate(`/p/${p.id}/overview`)}
              >
                <span className="project-index">0{i + 1}</span>
                <span className="project-glyph" style={{ color: p.color }}>
                  <FolderGit2 />
                </span>
                <span>
                  <b>{p.name}</b>
                  <small>{p.path}</small>
                </span>
                <span className="project-meta">
                  <GitBranch />
                  {p.defaultBranch}
                </span>
                <ArrowUpRight />
              </button>
            ))}
          </div>
        ) : (
          <div className="first-project">
            <Database />
            <span>
              <b>还没有项目</b>
              <small>项目数据会保存在本机，不要求账户。</small>
            </span>
          </div>
        )}
      </section>
      <Modal open={open} onClose={() => setOpen(false)} title="添加本地项目">
        <form
          className="form-stack"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(form);
          }}
        >
          <label>
            项目名称
            <input
              autoFocus
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="例如 ProjectBoard"
            />
          </label>
          <label>
            本地目录
            <div className="input-action">
              <input
                value={form.path}
                onChange={(e) => setForm({ ...form, path: e.target.value })}
                placeholder="D:\Github\ProjectBoard"
              />
              <Button type="button" onClick={() => setPickerOpen(true)}>
                <Folder />
                浏览
              </Button>
            </div>
          </label>
          <label>
            一句描述
            <textarea
              value={form.description}
              onChange={(e) =>
                setForm({ ...form, description: e.target.value })
              }
              placeholder="这个项目要解决什么问题？"
            />
          </label>
          {error && (
            <p className="form-error">
              <AlertTriangle />
              {error}
            </p>
          )}
          <div className="modal-actions">
            <Button type="button" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button variant="primary" type="submit" disabled={create.isPending}>
              检查目录并创建
            </Button>
          </div>
        </form>
      </Modal>
      <DirectoryPicker
        open={pickerOpen}
        initialPath={form.path}
        onClose={() => setPickerOpen(false)}
        onSelect={(path) => setForm((current) => ({ ...current, path }))}
      />
    </div>
  );
}

function useProjectData() {
  const { pid } = useParams();
  const tasks = useQuery({
    queryKey: ["tasks", pid],
    queryFn: () => api.tasks(pid!),
    enabled: !!pid,
  });
  return { pid: pid!, tasks };
}

export function OverviewPage() {
  const { pid } = useParams();
  const dash = useQuery({
    queryKey: ["dashboard", pid],
    queryFn: () => api.dashboard(pid!),
    enabled: !!pid,
  });
  const d = dash.data;
  const counts = d?.statusCounts || {};
  const metrics = [
    ["任务总数", d?.taskCount || 0, "本地项目范围", ClipboardCheck],
    ["进行中的 Agent", d?.activeRuns || 0, "包含排队与等待", Bot],
    ["等待用户", counts.waiting_user || 0, "输入或权限确认", KeyRound],
    ["待审核文件", d?.unreviewedFiles || 0, "来自 Agent 修改", FileDiff],
  ] as const;
  return (
    <div className="page canvas-scroll">
      <PageTitle
        eyebrow="PROJECT SIGNAL"
        title="项目总览"
        description="任务、Agent 与本地目录此刻的真实状态。"
        actions={
          <Button variant="primary">
            <Plus />
            创建任务
          </Button>
        }
      />
      <section className="metric-strip">
        {metrics.map(([label, value, note, Icon], i) => (
          <div className="metric" key={label}>
            <span className={`metric-icon tone-${i}`}>
              <Icon />
            </span>
            <span>
              <small>{label}</small>
              <strong>{value}</strong>
              <em>{note}</em>
            </span>
          </div>
        ))}
      </section>
      <div className="overview-grid">
        <section className="flow-panel">
          <header>
            <div>
              <span className="eyebrow">FLOW</span>
              <h2>任务流动</h2>
            </div>
            <span>当前周期</span>
          </header>
          <div className="donut-wrap">
            <div
              className="donut"
              style={
                {
                  "--done": `${Math.min(82, (counts.completed || 0) * 12 + 18)}%`,
                } as React.CSSProperties
              }
            >
              <div>
                <strong>{counts.completed || 0}</strong>
                <span>已完成</span>
              </div>
            </div>
            <div className="legend">
              {statusColumns.slice(0, 6).map((s) => (
                <div key={s.id}>
                  <i className={`dot ${s.accent}`} />
                  <span>{s.label}</span>
                  <b>{counts[s.id] || 0}</b>
                </div>
              ))}
            </div>
          </div>
        </section>
        <section className="trend-panel">
          <header>
            <div>
              <span className="eyebrow">7 DAYS</span>
              <h2>执行与完成趋势</h2>
            </div>
            <Tag>本地时间</Tag>
          </header>
          <div className="bars">
            {[32, 46, 28, 65, 54, 84, 72].map((v, i) => (
              <div key={i}>
                <span style={{ height: `${v}%` }} />
                <i style={{ height: `${Math.max(12, v - 22)}%` }} />
                <small>{["三", "四", "五", "六", "日", "一", "今"][i]}</small>
              </div>
            ))}
          </div>
        </section>
        <section className="risk-list">
          <header>
            <div>
              <span className="eyebrow">ATTENTION</span>
              <h2>需要你决定</h2>
            </div>
            <b>{(counts.waiting_user || 0) + 1}</b>
          </header>
          <button>
            <span className="risk-icon amber">
              <KeyRound />
            </span>
            <span>
              <b>Agent 等待权限确认</b>
              <small>删除测试生成的临时目录</small>
            </span>
            <ChevronRight />
          </button>
          <button>
            <span className="risk-icon red">
              <AlertTriangle />
            </span>
            <span>
              <b>验证命令失败</b>
              <small>前端类型检查返回退出码 2</small>
            </span>
            <ChevronRight />
          </button>
          <button>
            <span className="risk-icon cyan">
              <BookOpen />
            </span>
            <span>
              <b>知识提案待审核</b>
              <small>Agent 建议更新开发命令</small>
            </span>
            <ChevronRight />
          </button>
        </section>
        <section className="activity-list">
          <header>
            <div>
              <span className="eyebrow">STREAM</span>
              <h2>近期动态</h2>
            </div>
            <button>查看全部</button>
          </header>
          {(d?.recentTasks || []).slice(0, 5).map((t, i) => (
            <div key={t.id}>
              <span className={`activity-line tone-${i % 4}`} />
              <span>
                <b>{t.title}</b>
                <small>
                  {t.key} · 状态变为{" "}
                  {statusColumns.find((s) => s.id === t.status)?.label}
                </small>
              </span>
              <time>{relative(t.updatedAt)}</time>
            </div>
          ))}
        </section>
      </div>
    </div>
  );
}

export function BoardPage() {
  const { pid, tasks } = useProjectData();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [selected, setSelected] = useState<Task | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [title, setTitle] = useState("");
  const create = useMutation({
    mutationFn: () => api.createTask(pid, { title, status: "inbox" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["tasks", pid] });
      setCreateOpen(false);
      setTitle("");
    },
  });
  return (
    <div className="page board-page">
      <PageTitle
        eyebrow="TASK FIELD"
        title="任务看板"
        description="任务状态与 Agent 执行状态始终分开。"
        actions={
          <>
            <div className="segmented">
              <button className="active">
                <Columns3 />
                看板
              </button>
              <button onClick={() => navigate(`/p/${pid}/tasks/list`)}>
                <LayoutList />
                列表
              </button>
            </div>
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              <Plus />
              创建任务
            </Button>
          </>
        }
      />
      <div className="filterbar">
        <button>
          <Filter />
          所有任务
          <ChevronDown />
        </button>
        <button>负责人：我</button>
        <button>有未审核修改</button>
        <span />
        <button>
          <SlidersHorizontal />
          视图设置
        </button>
      </div>
      <div className="kanban">
        {statusColumns.map((col) => {
          const items = (tasks.data || []).filter((t) => t.status === col.id);
          return (
            <section className="kanban-column" key={col.id}>
              <header>
                <i className={`dot ${col.accent}`} />
                <b>{col.label}</b>
                <span>{items.length}</span>
                <IconButton
                  label="新建任务"
                  onClick={() => setCreateOpen(true)}
                >
                  <Plus />
                </IconButton>
              </header>
              <div className="kanban-stack">
                {items.map((task) => (
                  <article
                    className="task-card"
                    key={task.id}
                    onClick={() => setSelected(task)}
                  >
                    <div className="task-top">
                      <span className={`priority ${task.priority}`}>
                        {task.priority === "high"
                          ? "高"
                          : task.priority === "low"
                            ? "低"
                            : "中"}
                      </span>
                      <span>{task.key}</span>
                      <MoreHorizontal />
                    </div>
                    <h3>{task.title}</h3>
                    {task.description && <p>{task.description}</p>}
                    <div className="task-tags">
                      {task.labels?.map((x) => (
                        <Tag key={x}>{x}</Tag>
                      ))}
                    </div>
                    <footer>
                      <RunStatusBadge status={task.runStatus} />
                      <span />
                      <MessageSquare />0
                    </footer>
                  </article>
                ))}
                <button
                  className="add-task"
                  onClick={() => setCreateOpen(true)}
                >
                  <Plus />
                  添加任务
                </button>
              </div>
            </section>
          );
        })}
      </div>
      <Drawer
        open={!!selected}
        title={selected?.title || ""}
        onClose={() => setSelected(null)}
      >
        {selected && (
          <div className="drawer-task">
            <div className="drawer-badges">
              <TaskStatusBadge status={selected.status} />
              <RunStatusBadge status={selected.runStatus} />
            </div>
            <p>
              {selected.description ||
                "还没有详细描述。进入任务对话后可以让 Agent 帮你补全计划。"}
            </p>
            <dl>
              <dt>任务编号</dt>
              <dd>{selected.key}</dd>
              <dt>优先级</dt>
              <dd>{selected.priority}</dd>
              <dt>验收标准</dt>
              <dd>{selected.acceptance?.length || 0} 项</dd>
              <dt>关联文件</dt>
              <dd>尚未产生修改</dd>
            </dl>
            <div className="drawer-section">
              <span className="eyebrow">下一步</span>
              <button
                className="conversation-entry"
                onClick={() => navigate(`/p/${pid}/tasks/${selected.id}`)}
              >
                <span>
                  <Bot />
                  <b>进入任务对话</b>
                </span>
                <small>与当前 Agent 对话，或使用 @ 召唤其他 Agent</small>
                <ArrowRight />
              </button>
            </div>
          </div>
        )}
      </Drawer>
      <Modal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        title="创建任务"
      >
        <form
          className="form-stack"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <label>
            任务标题
            <input
              autoFocus
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="要完成什么？"
            />
          </label>
          <div className="modal-actions">
            <Button type="button" onClick={() => setCreateOpen(false)}>
              取消
            </Button>
            <Button variant="primary" disabled={!title.trim()}>
              创建并打开
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}

export function TaskListPage() {
  const { pid, tasks } = useProjectData();
  const navigate = useNavigate();
  return (
    <div className="page canvas-scroll">
      <PageTitle
        eyebrow="TASK INDEX"
        title="任务列表"
        description="在一个可扫描的表面里筛选、排序和批量处理。"
        actions={
          <div className="segmented">
            <button onClick={() => navigate(`/p/${pid}/tasks/board`)}>
              <Columns3 />
              看板
            </button>
            <button className="active">
              <LayoutList />
              列表
            </button>
          </div>
        }
      />
      <div className="filterbar">
        <button>
          <ListFilter />
          全部状态
        </button>
        <button>优先级</button>
        <button>标签</button>
        <span />
        <div className="inline-search">
          <Search />
          <input placeholder="筛选当前列表" />
        </div>
      </div>
      <div className="data-table task-table">
        <div className="table-head">
          <span>任务</span>
          <span>任务状态</span>
          <span>Agent 状态</span>
          <span>优先级</span>
          <span>更新</span>
          <span />
        </div>
        {(tasks.data || []).map((t) => (
          <button
            className="table-row"
            key={t.id}
            onClick={() => navigate(`/p/${pid}/tasks/${t.id}`)}
          >
            <span>
              <b>{t.title}</b>
              <small>
                {t.key} · {t.labels?.join(" · ") || "无标签"}
              </small>
            </span>
            <TaskStatusBadge status={t.status} />
            <RunStatusBadge status={t.runStatus} />
            <span className={`priority ${t.priority}`}>{t.priority}</span>
            <time>{relative(t.updatedAt)}</time>
            <ChevronRight />
          </button>
        ))}
      </div>
    </div>
  );
}

export function TaskWorkspacePage() {
  const { pid, tid, cid } = useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const task = useQuery({
    queryKey: ["task", tid],
    queryFn: () => api.task(tid!),
    enabled: !!tid,
  });
  const agents = useQuery({
    queryKey: ["agents", pid],
    queryFn: () => api.agents(pid!),
    enabled: !!pid,
  });
  const conversations = useQuery({
    queryKey: ["conversations", tid],
    queryFn: () => api.conversations(tid!),
    enabled: !!tid,
  });
  const [active, setActive] = useState<string | undefined>(cid);
  const [input, setInput] = useState("");
  const [parallel, setParallel] = useState<AgentProfile[]>([]);
  const [contextOpen, setContextOpen] = useState(false);
  const currentConversation =
    conversations.data?.find((c) => c.id === (active || cid)) ||
    conversations.data?.[0];
  const currentAgent =
    agents.data?.find((a) => a.id === currentConversation?.agentId) ||
    agents.data?.find((a) => a.isDefault) ||
    agents.data?.[0];
  const messages = useQuery({
    queryKey: ["messages", currentConversation?.id],
    queryFn: () => api.messages(currentConversation!.id),
    enabled: !!currentConversation,
  });
  const approveParallel = useMutation({
    mutationFn: async () => {
      if (!task.data || parallel.length < 2) return;
      const parsed = parseAgentMentions(input, agents.data || []);
      const plan = await api.proposeParallelPlan(task.data.id, {
        mainAgentId: parallel[0].id,
        prompt: parsed.prompt || input,
        agents: parallel.map((a) => ({
          agentId: a.id,
          instruction: parsed.prompt || input,
        })),
      });
      return api.approveParallelPlan(plan.id);
    },
    onSuccess: () => {
      setInput("");
      setParallel([]);
      qc.invalidateQueries({ queryKey: ["conversations", tid] });
      qc.invalidateQueries({ queryKey: ["runs"] });
    },
  });
  const send = useMutation({
    mutationFn: async () => {
      if (!task.data || !currentAgent) return;
      const parsed = parseAgentMentions(input, agents.data || []);
      if (parsed.agents.length > 1) {
        setParallel(parsed.agents);
        throw new Error("PARALLEL_CONFIRM");
      }
      const target = parsed.agents[0] || currentAgent;
      const conversation = await api.conversation(task.data.id, target.id);
      setActive(conversation.id);
      navigate(`/p/${pid}/tasks/${tid}/chat/${conversation.id}`, {
        replace: true,
      });
      return api.sendMessage(conversation.id, {
        taskID: task.data.id,
        agentID: target.id,
        content: parsed.prompt || input,
      });
    },
    onSuccess: () => {
      setInput("");
      qc.invalidateQueries({ queryKey: ["conversations", tid] });
      qc.invalidateQueries({ queryKey: ["messages"] });
      qc.invalidateQueries({ queryKey: ["runs"] });
    },
    onError: (e) => {
      if (e.message !== "PARALLEL_CONFIRM") console.error(e);
    },
  });
  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (input.trim()) send.mutate();
  };
  return (
    <div className="task-workspace">
      <header className="task-chat-head">
        <div>
          <Link to={`/p/${pid}/tasks/board`}>任务</Link>
          <ChevronRight />
          <b>{task.data?.key}</b>
          <span>{task.data?.title}</span>
        </div>
        <div>
          {task.data && (
            <>
              <TaskStatusBadge status={task.data.status} />
              <RunStatusBadge status={task.data.runStatus} />
            </>
          )}
          <Button>
            <PanelRightOpen />
            任务信息
          </Button>
        </div>
      </header>
      <div className="authorization-bar">
        <Shield />
        <span>
          <b>本次对话权限</b> · 任务 worktree 可读写 · 知识按需读取 ·
          危险命令需确认
        </span>
        <button onClick={() => setContextOpen(true)}>
          预览发送上下文
          <ChevronRight />
        </button>
      </div>
      <div className="conversation-layout">
        <aside className="conversation-list">
          <header>
            <span className="eyebrow">AGENTS</span>
            <IconButton label="新建对话">
              <Plus />
            </IconButton>
          </header>
          {(conversations.data || []).map((c) => {
            const a = agents.data?.find((x) => x.id === c.agentId);
            return (
              <button
                key={c.id}
                className={c.id === currentConversation?.id ? "active" : ""}
                onClick={() => {
                  setActive(c.id);
                  navigate(`/p/${pid}/tasks/${tid}/chat/${c.id}`);
                }}
              >
                <span className="agent-avatar">
                  <Bot />
                </span>
                <span>
                  <b>{a?.name || "Codex"}</b>
                  <small>{c.summary || "任务对话 · 可恢复"}</small>
                </span>
                <CircleDot />
              </button>
            );
          })}
          <div className="conversation-hint">
            <Command />
            <span>
              在输入框键入 <b>@Agent</b> 可切换或并行召唤。
            </span>
          </div>
        </aside>
        <main className="message-flow">
          {!currentConversation ? (
            <div className="conversation-empty">
              <span className="agent-orbit">
                <Bot />
              </span>
              <span className="eyebrow">TASK CONVERSATION</span>
              <h2>{task.data?.title}</h2>
              <p>
                当前选择 <b>{currentAgent?.name || "Codex · 自动实现"}</b>
                。输入第一条消息，或用 @ 召唤其他 Agent。
              </p>
              <div className="starter-prompts">
                <button
                  onClick={() =>
                    setInput("阅读任务与验收标准，先给出实现计划。")
                  }
                >
                  先制定实现计划
                  <ArrowRight />
                </button>
                <button
                  onClick={() =>
                    setInput("检查当前项目状态，指出开始前的风险。")
                  }
                >
                  检查项目风险
                  <ArrowRight />
                </button>
              </div>
            </div>
          ) : (
            <div className="message-column">
              <div className="thread-intro">
                <span className="agent-avatar large">
                  <Bot />
                </span>
                <div>
                  <span className="eyebrow">CURRENT AGENT</span>
                  <h2>{currentAgent?.name}</h2>
                  <p>
                    {currentAgent?.model || "使用 Codex 默认模型"} · 思考强度{" "}
                    {currentAgent?.reasoning}
                  </p>
                </div>
                <Button>
                  <Settings2 />
                  档案
                </Button>
              </div>
              {(messages.data || []).map((m) => (
                <ChatMessage key={m.id} message={m} />
              ))}
              {send.isPending && (
                <div className="streaming">
                  <span />
                  <span />
                  <span />
                  正在启动 Codex app-server
                </div>
              )}
            </div>
          )}
          <form className="composer" onSubmit={submit}>
            <div className="composer-context">
              <button type="button">
                <BookOpen />
                知识按需读取
              </button>
              <button type="button">
                <FolderGit2 />
                任务 worktree
              </button>
              <button type="button">
                <Terminal />
                危险命令确认
              </button>
            </div>
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={`发送给 ${currentAgent?.name || "当前 Agent"}，或输入 @ 召唤其他 Agent…`}
              rows={3}
            />
            <footer>
              <button type="button">
                <Plus />
              </button>
              <span>Shift Enter 换行</span>
              <Button
                variant="primary"
                disabled={!input.trim() || send.isPending}
              >
                <Send />
                发送
              </Button>
            </footer>
          </form>
        </main>
        <aside className="task-inspector">
          <span className="eyebrow">TASK CONTEXT</span>
          <h3>验收与现场</h3>
          <section>
            <label>任务状态</label>
            {task.data && <TaskStatusBadge status={task.data.status} />}
          </section>
          <section>
            <label>验收标准</label>
            {task.data?.acceptance?.length ? (
              task.data.acceptance.map((a) => (
                <div className="checkline" key={a.id}>
                  <span className={a.done ? "done" : ""}>
                    {a.done ? <Check /> : <Square />}
                  </span>
                  {a.text}
                </div>
              ))
            ) : (
              <p className="muted">还没有验收标准。</p>
            )}
          </section>
          <section>
            <label>任务工作区</label>
            <div className="worktree-line">
              <GitBranch />
              <span>
                <b>projectboard/task-{task.data?.key.toLowerCase()}</b>
                <small>主 worktree · 无未审核修改</small>
              </span>
            </div>
          </section>
          <section>
            <label>其他对话</label>
            <p className="muted">Agent 可通过 MCP 按需读取同任务对话摘要。</p>
          </section>
        </aside>
      </div>
      <Modal
        open={parallel.length > 1}
        onClose={() => setParallel([])}
        title="确认并行 Agent 计划"
      >
        <div className="parallel-plan">
          <p>
            这条消息提及了 {parallel.length} 个 Agent。首个 Agent 使用任务主
            worktree，其余 Agent 将从当前内部快照创建子 worktree。
          </p>
          {parallel.map((a, i) => (
            <div key={a.id}>
              <span>{i === 0 ? "主" : "子"}</span>
              <b>{a.name}</b>
              <small>
                {i === 0 ? "任务主工作区" : "独立候选分支 · 完成后自动清理"}
              </small>
            </div>
          ))}
          <div className="permission-summary">
            <ShieldAlert />
            <span>
              将创建 {parallel.length - 1} 个子 worktree，并同时占用{" "}
              {parallel.length} 个全局 Agent 槽位。写入范围仅限各自工作区。
            </span>
          </div>
          {approveParallel.error && (
            <p className="form-error">
              <AlertTriangle />
              {approveParallel.error.message}
            </p>
          )}
          <div className="modal-actions">
            <Button onClick={() => setParallel([])}>取消</Button>
            <Button
              variant="primary"
              disabled={approveParallel.isPending}
              onClick={() => approveParallel.mutate()}
            >
              {approveParallel.isPending ? "正在创建工作区…" : "确认并开始"}
            </Button>
          </div>
        </div>
      </Modal>
      <Modal
        open={contextOpen}
        onClose={() => setContextOpen(false)}
        title="实际发送给 Agent 的上下文"
      >
        <div className="context-preview">
          <section>
            <b>任务</b>
            <p>{task.data?.title}</p>
            <small>{task.data?.description}</small>
          </section>
          <section>
            <b>授权目录</b>
            <code>任务主 worktree（可读写）</code>
          </section>
          <section>
            <b>知识能力</b>
            <p>只发送知识目录与摘要；正文由 Agent 通过受权 MCP 按需读取。</p>
          </section>
          <section>
            <b>禁止事项</b>
            <p>项目外访问、危险 Git 命令、删除与未授权网络需要人工确认。</p>
          </section>
        </div>
      </Modal>
    </div>
  );
}

function ChatMessage({ message }: { message: Message }) {
  return (
    <article className={`chat-message ${message.role}`}>
      <span className="message-avatar">
        {message.role === "user" ? <UserRound /> : <Bot />}
      </span>
      <div>
        <header>
          <b>{message.role === "user" ? "本地用户" : "Codex"}</b>
          <time>
            {new Date(message.createdAt).toLocaleTimeString("zh-CN", {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </time>
        </header>
        <div className="message-body">{message.content}</div>
      </div>
    </article>
  );
}

export function ChangesPage({
  queueMode = false,
  gitMode = false,
}: {
  queueMode?: boolean;
  gitMode?: boolean;
}) {
  const { pid, rid } = useParams();
  const runs = useQuery({ queryKey: ["runs"], queryFn: () => api.runs() });
  if (queueMode)
    return (
      <div className="page canvas-scroll">
        <PageTitle
          eyebrow="RUN QUEUE"
          title="执行队列"
          description="主工作区与子 worktree 的实时调度现场。"
        />
        <div className="run-queue">
          {(runs.data || []).map((r, i) => (
            <article key={r.id}>
              <span className={`queue-index ${r.status}`}>
                {String(i + 1).padStart(2, "0")}
              </span>
              <div>
                <b>{r.prompt}</b>
                <small>
                  {r.id} · {r.workspaceId || "任务主 worktree"}
                </small>
              </div>
              <RunStatusBadge status={r.status} />
              <Button>
                {r.status === "running" ? (
                  <>
                    <Pause />
                    暂停
                  </>
                ) : (
                  <>
                    <Play />
                    查看
                  </>
                )}
              </Button>
            </article>
          ))}
        </div>
      </div>
    );
  if (gitMode)
    return (
      <div className="page canvas-scroll">
        <PageTitle
          eyebrow="REPOSITORY"
          title="Git 现场"
          description="默认分支、任务分支与 worktree 的统一视图。"
          actions={
            <Button>
              <RefreshCw />
              刷新状态
            </Button>
          }
        />
        <div className="git-stage">
          <section className="branch-graph">
            <div className="graph-line" />
            <div className="commit main">
              <i />
              <span>
                <b>main</b>
                <small>项目默认分支 · 工作区干净</small>
              </span>
              <code>a81f29c</code>
            </div>
            <div className="commit task">
              <i />
              <span>
                <b>projectboard/task-pb-104</b>
                <small>任务主分支 · 4 个修改</small>
              </span>
              <code>snapshot</code>
            </div>
            <div className="commit child">
              <i />
              <span>
                <b>projectboard/task-pb-104/review</b>
                <small>子 worktree · 候选结果待审核</small>
              </span>
              <code>c91e712</code>
            </div>
          </section>
          <section className="repo-health">
            <span className="eyebrow">WORKTREES</span>
            <h2>隔离工作区</h2>
            {["任务主工作区", "安全审查候选", "性能方案候选"].map((x, i) => (
              <div key={x}>
                <FolderGit2 />
                <span>
                  <b>{x}</b>
                  <small>
                    {i
                      ? "子 worktree · 完成后自动清理"
                      : "主 worktree · 保留至任务完成"}
                  </small>
                </span>
                <Tag>{i ? "候选" : "活动"}</Tag>
              </div>
            ))}
          </section>
        </div>
      </div>
    );
  return (
    <div className="page changes-page">
      <PageTitle
        eyebrow="CHANGE REVIEW"
        title="修改审核"
        description={`执行 ${rid || "#R-1042"} · 逐文件、逐区块检查 Agent 的实际修改。`}
        actions={
          <>
            <Button>
              <RotateCcw />
              撤销本次
            </Button>
            <Button variant="primary">
              <GitCommitHorizontal />
              接受并验证
            </Button>
          </>
        }
      />
      <div className="changes-layout">
        <aside className="file-changes">
          <header>
            <b>7 个文件</b>
            <span>+284 −61</span>
          </header>
          {[
            "web/src/pages.tsx",
            "internal/agent/appserver.go",
            "internal/workspace/git.go",
            "internal/store/store.go",
            "README.md",
          ].map((f, i) => (
            <button className={i === 0 ? "active" : ""} key={f}>
              <span className={`file-origin ${i === 4 ? "human" : "ai"}`}>
                {i === 4 ? <UserRound /> : <Bot />}
              </span>
              <span>
                <b>{f.split("/").pop()}</b>
                <small>
                  {f.includes("/") ? f.slice(0, f.lastIndexOf("/")) : "/"}
                </small>
              </span>
              <em>
                +{[84, 62, 49, 38, 12][i]} −{[14, 8, 17, 22, 0][i]}
              </em>
            </button>
          ))}
        </aside>
        <main className="diff-view">
          <header>
            <span>
              <FileCode2 />
              <b>pages.tsx</b>
              <small>Agent 修改 · Run #R-1042</small>
            </span>
            <div>
              <Button>拒绝文件</Button>
              <Button variant="primary">接受文件</Button>
            </div>
          </header>
          <div className="diff-hunk">
            <div className="hunk-head">
              <code>@@ -142,8 +142,18 @@ TaskWorkspace</code>
              <span />
              <button>拒绝区块</button>
              <button>接受区块</button>
            </div>
            {[
              "  const currentAgent = agents.find(...)",
              "+ const mentions = parseAgentMentions(input, agents)",
              "+ if (mentions.agents.length > 1) {",
              "+   openParallelPlan(mentions.agents)",
              "+   return",
              "+ }",
              "  await sendMessage(currentAgent, input)",
              "- setDraft(input)",
              "+ setDraft(mentions.prompt)",
            ].map((line, i) => (
              <div
                className={
                  line.startsWith("+")
                    ? "added"
                    : line.startsWith("-")
                      ? "removed"
                      : ""
                }
                key={i}
              >
                <span>{142 + i}</span>
                <span>
                  {line.startsWith("+")
                    ? 143 + i
                    : line.startsWith("-")
                      ? ""
                      : 142 + i}
                </span>
                <code>{line}</code>
              </div>
            ))}
          </div>
        </main>
        <aside className="verification-panel">
          <span className="eyebrow">VERIFICATION</span>
          <h3>验证证据</h3>
          <div className="verify-score">
            <strong>3/4</strong>
            <span>检查通过</span>
          </div>
          {[
            ["TypeScript", "通过"],
            ["Go tests", "通过"],
            ["Playwright", "运行中"],
            ["Agent 自检", "通过"],
          ].map(([x, y], i) => (
            <div className="verify-line" key={x}>
              {i === 2 ? <RefreshCw className="spin" /> : <CheckCircle2 />}
              <span>
                <b>{x}</b>
                <small>{y}</small>
              </span>
            </div>
          ))}
          <Button>
            <TestTube2 />
            重新运行全部
          </Button>
        </aside>
      </div>
    </div>
  );
}

export function KnowledgePage() {
  const { pid } = useParams();
  const [category, setCategory] = useState("all");
  const [knowledgeQuery, setKnowledgeQuery] = useState("");
  const knowledge = useQuery({
    queryKey: ["knowledge", pid],
    queryFn: () => api.knowledge(pid!),
    enabled: !!pid,
  });
  const categories = [
    ["all", "全部知识"],
    ["product", "产品需求"],
    ["architecture", "架构说明"],
    ["specification", "技术规范"],
    ["command", "开发命令"],
    ["decision", "决策记录"],
    ["agent_rule", "AI 工作规则"],
  ] as const;
  const visibleKnowledge = (knowledge.data || []).filter(
    (item) =>
      (category === "all" || String(item.category) === category) &&
      (knowledgeQuery === "" ||
        `${item.title} ${item.summary}`
          .toLowerCase()
          .includes(knowledgeQuery.toLowerCase())),
  );
  return (
    <div className="page knowledge-page">
      <PageTitle
        eyebrow="PROJECT MEMORY"
        title="项目知识库"
        description="中央 SQLite 中可追踪、可授权、可按需提供给 Agent 的项目记忆。"
        actions={
          <Button variant="primary">
            <FilePlus2 />
            新建知识
          </Button>
        }
      />
      <div className="knowledge-layout">
        <aside className="knowledge-nav">
          <div className="inline-search">
            <Search />
            <input
              value={knowledgeQuery}
              onChange={(event) => setKnowledgeQuery(event.target.value)}
              placeholder="搜索知识"
            />
          </div>
          {categories.map(([value, label]) => (
            <button
              className={category === value ? "active" : ""}
              aria-current={category === value ? "page" : undefined}
              onClick={() => setCategory(value)}
              key={value}
            >
              <BookOpen />
              {label}
              <span>
                {value === "all"
                  ? (knowledge.data || []).length
                  : (knowledge.data || []).filter(
                      (item) => String(item.category) === value,
                    ).length}
              </span>
            </button>
          ))}
        </aside>
        <main className="knowledge-grid">
          {visibleKnowledge.length ? (
            visibleKnowledge.map((k, i) => (
              <article key={String(k.id)}>
                <header>
                  <span className={`knowledge-type tone-${i % 4}`}>
                    <BookOpen />
                  </span>
                  <span>
                    <Tag>{String(k.permission)}</Tag>
                    <IconButton label="更多">
                      <MoreHorizontal />
                    </IconButton>
                  </span>
                </header>
                <h2>{String(k.title)}</h2>
                <p>{String(k.summary || "暂无摘要")}</p>
                <footer>
                  <span>v{String(k.version)}</span>
                  <span />
                  <History />
                  {relative(String(k.updated_at || k.updatedAt))}
                </footer>
              </article>
            ))
          ) : (
            <EmptyState
              title="没有匹配的知识"
              body="调整分类或搜索词，或者创建第一条项目知识。"
            />
          )}
        </main>
        <aside className="knowledge-inspector">
          <span className="eyebrow">ACCESS MAP</span>
          <h3>Agent 读取边界</h3>
          <div className="access-meter">
            <div style={{ "--value": "72%" } as React.CSSProperties} />
            <strong>72%</strong>
            <span>知识可被默认 Agent 发现</span>
          </div>
          <div className="access-row">
            <Unlock />
            <span>可自动读取</span>
            <b>8</b>
          </div>
          <div className="access-row">
            <Shield />
            <span>建议修改</span>
            <b>3</b>
          </div>
          <div className="access-row">
            <Lock />
            <span>完全隐藏</span>
            <b>1</b>
          </div>
          <p>可读知识不会自动塞入上下文。Agent 通过短期能力令牌按需检索。</p>
        </aside>
      </div>
    </div>
  );
}

export function KnowledgeHistoryPage() {
  return (
    <div className="page canvas-scroll">
      <PageTitle
        eyebrow="KNOWLEDGE AUDIT"
        title="知识版本与审计"
        description="项目架构与模块边界 · 当前 v12"
        actions={
          <>
            <Button>
              <RotateCcw />
              回滚到此版本
            </Button>
            <Button variant="primary">批准修改</Button>
          </>
        }
      />
      <div className="history-layout">
        <aside className="version-list">
          {[12, 11, 10, 9, 8].map((v, i) => (
            <button className={i === 0 ? "active" : ""} key={v}>
              <span>v{v}</span>
              <div>
                <b>{i === 0 ? "Codex 更新提案" : "本地用户修改"}</b>
                <small>
                  {i === 0 ? "来源任务 PB-104 · 待审核" : `${i + 1} 天前`}
                </small>
              </div>
            </button>
          ))}
        </aside>
        <main className="knowledge-diff">
          <header>
            <b>v11 → v12</b>
            <span>
              <Tag>Codex · 自动实现</Tag>
              <Tag>Run #R-1042</Tag>
            </span>
          </header>
          {[
            "## 模块边界",
            "Store 负责持久化与迁移。",
            "+ RunManager 负责队列、取消和恢复语义。",
            "+ WorkspaceManager 隐藏 Git worktree 生命周期。",
            "- Server 直接创建 Codex 进程。",
            "+ Server 只依赖 RunManager 接口。",
          ].map((x, i) => (
            <div
              className={
                x.startsWith("+") ? "added" : x.startsWith("-") ? "removed" : ""
              }
              key={i}
            >
              <code>{x}</code>
            </div>
          ))}
        </main>
        <aside className="audit-side">
          <span className="eyebrow">PROVENANCE</span>
          <h3>变更来源</h3>
          <dl>
            <dt>任务</dt>
            <dd>PB-104</dd>
            <dt>执行</dt>
            <dd>#R-1042</dd>
            <dt>Agent</dt>
            <dd>Codex · 自动实现</dd>
            <dt>权限</dt>
            <dd>建议修改</dd>
            <dt>人工锁定</dt>
            <dd>否</dd>
          </dl>
          <div className="audit-callout">
            <Shield />
            <span>批准后生成新版本；历史版本不会被覆盖。</span>
          </div>
        </aside>
      </div>
    </div>
  );
}

export function AgentsPage() {
  const { pid } = useParams();
  const agents = useQuery({
    queryKey: ["agents", pid],
    queryFn: () => api.agents(pid!),
    enabled: !!pid,
  });
  return (
    <div className="page canvas-scroll">
      <PageTitle
        eyebrow="AGENT PROFILES"
        title="AI CLI 配置"
        description="复用本机 Codex 登录，为不同任务定义模型、思考强度和权限边界。"
        actions={
          <Button variant="primary">
            <Plus />
            新建档案
          </Button>
        }
      />
      <section className="codex-health">
        <span className="codex-symbol">
          <Bot />
        </span>
        <div>
          <span className="eyebrow">CODEX CLI</span>
          <h2>已连接 · app-server 可用</h2>
          <p>codex-cli 0.142.4 · 登录状态来自用户 Codex Home</p>
        </div>
        <Verified />
        <Button>
          <RefreshCw />
          重新检测
        </Button>
      </section>
      <div className="agent-profiles">
        {(agents.data || []).map((a, i) => (
          <article key={a.id}>
            <header>
              <span className={`agent-avatar tone-${i}`}>
                <Bot />
              </span>
              <div>
                <h2>{a.name}</h2>
                <p>{a.isDefault ? "项目默认档案" : "项目 Agent 档案"}</p>
              </div>
              <IconButton label="编辑">
                <Settings2 />
              </IconButton>
            </header>
            <dl>
              <dt>模型</dt>
              <dd>{a.model || "Codex 默认"}</dd>
              <dt>思考强度</dt>
              <dd>{a.reasoning}</dd>
              <dt>沙箱</dt>
              <dd>{a.sandbox}</dd>
              <dt>网络</dt>
              <dd>{a.network ? "允许" : "禁止"}</dd>
            </dl>
            <footer>
              <Tag>任务 worktree 可写</Tag>
              <Tag>危险命令确认</Tag>
              <Tag>知识按需读取</Tag>
            </footer>
          </article>
        ))}
      </div>
    </div>
  );
}

export function SearchPage() {
  const params = new URLSearchParams(location.search);
  const [query, setQuery] = useState(params.get("q") || "");
  const [scope, setScope] = useState("all");
  const results = useQuery({
    queryKey: ["search", query],
    queryFn: () => api.search(query),
    enabled: query.trim().length > 1,
  });
  const scopes = [
    ["all", "全部结果"],
    ["task", "任务"],
    ["conversation", "AI 对话"],
    ["knowledge", "知识库"],
    ["file", "文件与产出物"],
    ["log", "执行日志"],
  ] as const;
  const visibleResults = (results.data || []).filter(
    (result) =>
      scope === "all" ||
      result.type === scope ||
      (scope === "file" && result.type === "artifact") ||
      (scope === "log" && result.type === "run"),
  );
  return (
    <div className="page search-page">
      <PageTitle
        eyebrow="GLOBAL INDEX"
        title="全局搜索"
        description="任务、对话、知识、文件、日志与产出物。"
      />
      <div className="search-hero">
        <Search />
        <input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="输入两个以上字符开始搜索"
        />
        <kbd>Enter</kbd>
      </div>
      <div className="search-layout">
        <aside>
          <span className="eyebrow">范围与筛选</span>
          {scopes.map(([value, label]) => (
            <button
              className={scope === value ? "active" : ""}
              aria-current={scope === value ? "page" : undefined}
              onClick={() => setScope(value)}
              key={value}
            >
              {label}
              <span>
                {value === "all"
                  ? results.data?.length || 0
                  : (results.data || []).filter(
                      (result) => result.type === value,
                    ).length}
              </span>
            </button>
          ))}
          <div className="context-divider" />
          <label>
            项目
            <select>
              <option>全部项目</option>
            </select>
          </label>
          <label>
            更新时间
            <select>
              <option>任何时间</option>
            </select>
          </label>
        </aside>
        <main>
          {query.length < 2 ? (
            <EmptyState
              icon={<Search />}
              title="从整个本地工作空间中查找"
              body="搜索索引由 SQLite FTS 在后台维护，不会发送到网络。"
            />
          ) : results.isLoading ? (
            <p className="muted">正在搜索本地索引…</p>
          ) : visibleResults.length ? (
            visibleResults.map((r, i) => (
              <article className="search-result" key={r.id}>
                <span className={`result-type tone-${i % 4}`}>
                  {r.type === "task" ? <ClipboardCheck /> : <FileText />}
                </span>
                <div>
                  <span className="eyebrow">{r.type}</span>
                  <h2>{r.title}</h2>
                  <p dangerouslySetInnerHTML={{ __html: r.snippet }} />
                </div>
                <ArrowUpRight />
              </article>
            ))
          ) : (
            <EmptyState
              title="没有匹配结果"
              body="尝试缩短关键词，或移除筛选条件。"
            />
          )}
        </main>
      </div>
    </div>
  );
}

export function InboxPage() {
  const queryClient = useQueryClient();
  const [filter, setFilter] = useState("unread");
  const q = useQuery({
    queryKey: ["notifications"],
    queryFn: api.notifications,
  });
  const markAll = useMutation({
    mutationFn: api.markAllNotificationsRead,
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["notifications"] }),
  });
  const filters = [
    ["unread", "未读"],
    ["approval", "等待权限"],
    ["input", "等待输入"],
    ["result", "执行结果"],
    ["change", "文件审核"],
    ["knowledge", "知识提案"],
  ] as const;
  const matches = (notice: Record<string, unknown>, value: string) => {
    const type = String(notice.type || "").toLowerCase();
    if (value === "unread") return !notice.readAt;
    return (
      type.includes(value) ||
      (value === "approval" && type.includes("permission")) ||
      (value === "result" &&
        (type.includes("run") ||
          type.includes("succeeded") ||
          type.includes("failed")))
    );
  };
  const notices = (q.data || []).filter((notice) => matches(notice, filter));
  return (
    <div className="page canvas-scroll">
      <PageTitle
        eyebrow="ACTION INBOX"
        title="通知收件箱"
        description="只保留需要你知道、判断或批准的事件。"
        actions={
          <Button onClick={() => markAll.mutate()} disabled={markAll.isPending}>
            <Check />
            全部标为已读
          </Button>
        }
      />
      <div className="inbox-layout">
        <aside>
          {filters.map(([value, label]) => (
            <button
              className={filter === value ? "active" : ""}
              aria-current={filter === value ? "page" : undefined}
              onClick={() => setFilter(value)}
              key={value}
            >
              {label}
              <span>
                {
                  (q.data || []).filter((notice) => matches(notice, value))
                    .length
                }
              </span>
            </button>
          ))}
        </aside>
        <main>
          {notices.length ? (
            notices.map((n) => (
              <article key={String(n.id)}>
                <span className="notice-icon">
                  <Inbox />
                </span>
                <div>
                  <span className="eyebrow">{String(n.type)}</span>
                  <h2>{String(n.title)}</h2>
                  <p>{String(n.body)}</p>
                  <small>{relative(String(n.createdAt))}</small>
                </div>
                <Button>处理</Button>
              </article>
            ))
          ) : (
            <EmptyState
              title="这个分类没有通知"
              body="新的 Agent 请求、执行结果和审核事项会出现在这里。"
            />
          )}
        </main>
      </div>
    </div>
  );
}

export function AutomationPage() {
  const { pid } = useParams();
  const q = useQuery({
    queryKey: ["automations", pid],
    queryFn: () => api.automations(pid!),
    enabled: !!pid,
  });
  const samples = [
    ["任务进入进行中", "启动 Codex · 自动实现", "启用"],
    ["Agent 执行成功", "运行测试 → 移至验证中", "启用"],
    ["Agent 连续失败三次", "停止重试 → 标记风险 → 通知", "启用"],
    ["任务完成", "创建知识提炼审核请求", "暂停"],
  ];
  return (
    <div className="page canvas-scroll">
      <PageTitle
        eyebrow="RULE ENGINE"
        title="自动化规则"
        description="让重复流程推进到人工确认边界，不越过批准、合并和完成。"
        actions={
          <Button variant="primary">
            <Plus />
            新建规则
          </Button>
        }
      />
      <div className="automation-explainer">
        <span>
          <Zap />
        </span>
        <div>
          <h2>触发器 → 条件 → 动作</h2>
          <p>
            规则可以启动
            Agent、运行验证和创建提案，但不能批准危险命令或自动完成任务。
          </p>
        </div>
        <Button>查看安全边界</Button>
      </div>
      <div className="automation-list">
        {(q.data?.length ? q.data : samples).map((r: any, i) => (
          <article key={String(r.id || r[0])}>
            <div className={`rule-number ${i === 3 ? "paused" : ""}`}>
              {String(i + 1).padStart(2, "0")}
            </div>
            <div>
              <span className="eyebrow">WHEN</span>
              <b>{r.name || r[0]}</b>
            </div>
            <ArrowRight />
            <div>
              <span className="eyebrow">THEN</span>
              <b>{r.trigger_type || r[1]}</b>
            </div>
            <span />
            <label className="switch">
              <input
                type="checkbox"
                defaultChecked={r.enabled !== 0 && r[2] !== "暂停"}
              />
              <i />
            </label>
            <IconButton label="更多">
              <MoreHorizontal />
            </IconButton>
          </article>
        ))}
      </div>
    </div>
  );
}

export function TemplatesPage() {
  const q = useQuery({ queryKey: ["templates"], queryFn: api.templates });
  const [kind, setKind] = useState("task");
  const visibleTemplates = (q.data || []).filter(
    (template: any) => template.kind === kind,
  );
  return (
    <div className="page canvas-scroll">
      <PageTitle
        eyebrow="REUSABLE STARTS"
        title="模板库"
        description="内置模板保持稳定，复制后形成可编辑的本地模板。"
        actions={
          <Button variant="primary">
            <Plus />
            创建自定义模板
          </Button>
        }
      />
      <div className="template-tabs">
        {[
          ["task", "任务模板"],
          ["project", "项目模板"],
          ["agent", "AI 指令模板"],
        ].map(([value, label]) => (
          <button
            key={value}
            className={kind === value ? "active" : ""}
            aria-current={kind === value ? "page" : undefined}
            onClick={() => setKind(value)}
          >
            {label}
          </button>
        ))}
      </div>
      <div className="template-grid">
        {visibleTemplates.map((t: any, i) => (
          <article key={String(t.id)}>
            <span className={`template-icon tone-${i % 4}`}>
              {t.kind === "project" ? <FolderGit2 /> : <ClipboardCheck />}
            </span>
            <Tag>{t.builtin ? "内置" : "自定义"}</Tag>
            <h2>{String(t.name)}</h2>
            <p>{String(t.description)}</p>
            <div className="template-preview">
              <span>优先级</span>
              <b>高</b>
              <span>验收标准</span>
              <b>3 项</b>
              <span>推荐 Agent</span>
              <b>自动实现</b>
            </div>
            <footer>
              <Button>
                <Copy />
                复制并编辑
              </Button>
              <IconButton label="更多">
                <MoreHorizontal />
              </IconButton>
            </footer>
          </article>
        ))}
        {!q.isLoading && !visibleTemplates.length && (
          <EmptyState
            title="这个分类还没有模板"
            body="创建一个自定义模板，或从其他类型复制后修改。"
          />
        )}
      </div>
    </div>
  );
}

export function AuditPage() {
  const q = useQuery({ queryKey: ["audit"], queryFn: api.audit });
  const fallback = [
    [
      "14:02:10",
      "启动 Agent",
      "本地用户",
      "Codex · 自动实现 · 知识 ×3 · worktree 可写",
    ],
    ["14:03:22", "执行命令", "Codex", "npm run test · 退出码 0"],
    ["14:04:08", "读取知识", "Codex", "项目架构与模块边界 · v12"],
    ["14:06:41", "创建子 worktree", "本地用户", "安全审查 Agent · 已批准"],
    ["14:12:16", "审核修改", "本地用户", "接受 6 文件 · 拒绝 1 区块"],
  ];
  return (
    <div className="page canvas-scroll">
      <PageTitle
        eyebrow="IMMUTABLE TRACE"
        title="安全与审计日志"
        description="谁在何时以什么权限做了什么，均可追溯。"
        actions={
          <Button>
            <FileText />
            导出脱敏日志
          </Button>
        }
      />
      <div className="audit-filters">
        <button>
          <Filter />
          全部操作
        </button>
        <button>全部 Agent</button>
        <button>今天</button>
        <span />
        <div className="inline-search">
          <Search />
          <input placeholder="搜索操作、Run ID…" />
        </div>
      </div>
      <div className="audit-table">
        <div className="audit-head">
          <span>时间</span>
          <span>操作</span>
          <span>执行者</span>
          <span>详情</span>
          <span>结果</span>
        </div>
        {(q.data?.length ? q.data : fallback).map((a: any, i) => (
          <div className="audit-row" key={String(a.id || i)}>
            <time>
              {a.createdAt
                ? new Date(a.createdAt).toLocaleTimeString("zh-CN")
                : a[0]}
            </time>
            <Tag>{a.action || a[1]}</Tag>
            <span>{a.actor || a[2]}</span>
            <p>{a.detail || a[3]}</p>
            <Verified />
          </div>
        ))}
      </div>
    </div>
  );
}

export function SettingsPage() {
  useQuery({ queryKey: ["settings"], queryFn: api.settings });
  const [active, setActive] = useState("基本信息");
  const [saved, setSaved] = useState(false);
  const tabs = [
    ["基本信息", Settings2],
    ["本地目录", Folder],
    ["Git 与 worktree", GitBranch],
    ["Agent 默认值", Bot],
    ["执行与并发", Activity],
    ["安全与网络", Shield],
    ["通知", Bell],
    ["存储与备份", Database],
  ] as const;
  return (
    <div className="page settings-page">
      <PageTitle
        eyebrow="PROJECT CONTROL"
        title="项目设置"
        description="目录、Git、执行、安全和数据保留策略。"
        actions={
          <Button variant="primary" onClick={() => setSaved(true)}>
            {saved ? (
              <>
                <Check />
                已保存
              </>
            ) : (
              "保存更改"
            )}
          </Button>
        }
      />
      <div className="settings-layout">
        <aside>
          {tabs.map(([label, Icon]) => (
            <button
              className={active === label ? "active" : ""}
              aria-current={active === label ? "page" : undefined}
              onClick={() => {
                setActive(label);
                setSaved(false);
              }}
              key={label}
            >
              <Icon />
              {label}
            </button>
          ))}
        </aside>
        <main>
          {active === "基本信息" && (
            <section>
              <span className="eyebrow">GENERAL</span>
              <h2>基本信息</h2>
              <div className="form-grid">
                <label>
                  项目名称
                  <input defaultValue="ProjectBoard" />
                </label>
                <label>
                  项目颜色
                  <input defaultValue="#c9f24e" />
                </label>
                <label className="full">
                  项目描述
                  <textarea defaultValue="本地 AI 项目执行中心" />
                </label>
              </div>
            </section>
          )}
          {active === "本地目录" && (
            <section>
              <span className="eyebrow">LOCAL DIRECTORY</span>
              <h2>项目目录</h2>
              <div className="form-grid">
                <label className="full">
                  当前本地目录
                  <input defaultValue="D:\\Github\\ProjectBoard" />
                </label>
                <label className="full">
                  额外只读目录
                  <input placeholder="未授权额外目录" />
                </label>
              </div>
              <div className="warning-box">
                <Folder />
                <div>
                  <b>目录权限由任务 worktree 隔离</b>
                  <p>Agent 默认只能写入当前任务的受管工作区。</p>
                </div>
              </div>
            </section>
          )}
          {active === "Git 与 worktree" && (
            <section>
              <span className="eyebrow">GIT WORKSPACES</span>
              <h2>Git 与 worktree</h2>
              <div className="form-grid">
                <label>
                  默认分支
                  <input defaultValue="main" />
                </label>
                <label>
                  任务分支前缀
                  <input defaultValue="projectboard/task-" />
                </label>
                <label className="full">
                  worktree 根目录
                  <input defaultValue="%LOCALAPPDATA%\\ProjectBoard\\worktrees" />
                </label>
              </div>
            </section>
          )}
          {active === "Agent 默认值" && (
            <section>
              <span className="eyebrow">AGENT DEFAULTS</span>
              <h2>Agent 默认值</h2>
              <div className="form-grid">
                <label>
                  默认 Agent
                  <select>
                    <option>Codex · 自动实现</option>
                  </select>
                </label>
                <label>
                  默认模型
                  <input placeholder="使用 Codex 默认" />
                </label>
                <label>
                  思考强度
                  <select>
                    <option>high</option>
                    <option>medium</option>
                    <option>xhigh</option>
                  </select>
                </label>
                <label>
                  网络
                  <select>
                    <option>按 Agent 档案</option>
                    <option>禁止</option>
                  </select>
                </label>
              </div>
            </section>
          )}
          {active === "执行与并发" && (
            <section>
              <span className="eyebrow">EXECUTION</span>
              <h2>Agent 与并发</h2>
              <div className="form-grid">
                <label>
                  默认 Agent
                  <select>
                    <option>Codex · 自动实现</option>
                  </select>
                </label>
                <label>
                  全局并发
                  <input type="number" defaultValue={10} />
                </label>
                <label>
                  默认模型
                  <input placeholder="使用 Codex 默认" />
                </label>
                <label>
                  思考强度
                  <select>
                    <option>high</option>
                    <option>medium</option>
                    <option>xhigh</option>
                  </select>
                </label>
              </div>
            </section>
          )}
          {active === "安全与网络" && (
            <section>
              <span className="eyebrow">NETWORK</span>
              <h2>本地服务</h2>
              <div className="form-grid">
                <label>
                  监听地址
                  <input defaultValue="127.0.0.1:5173" />
                </label>
                <label>
                  允许的 Origin
                  <input placeholder="默认仅同源" />
                </label>
              </div>
              <div className="warning-box">
                <ShieldAlert />
                <div>
                  <b>外部监听首版不提供鉴权</b>
                  <p>
                    修改为非回环地址后，任何可连接客户端都可能访问项目和执行接口。
                  </p>
                </div>
              </div>
            </section>
          )}
          {active === "通知" && (
            <section>
              <span className="eyebrow">NOTIFICATIONS</span>
              <h2>通知</h2>
              <div className="settings-checks">
                <label>
                  <input type="checkbox" defaultChecked />
                  站内通知
                </label>
                <label>
                  <input type="checkbox" />
                  页面非活跃时发送 Windows 系统通知
                </label>
                <label>
                  <input type="checkbox" defaultChecked />
                  危险操作与等待输入始终提醒
                </label>
              </div>
            </section>
          )}
          {active === "存储与备份" && (
            <section>
              <span className="eyebrow">LOCAL DATA</span>
              <h2>存储与备份</h2>
              <div className="storage-meter">
                <div>
                  <span style={{ width: "38%" }} />
                </div>
                <b>1.8 GB / 本地</b>
                <small>%LOCALAPPDATA%\ProjectBoard</small>
              </div>
              <div className="settings-actions">
                <Button>
                  <Database />
                  立即备份
                </Button>
                <Button>导出数据</Button>
                <Button>清理 90 天前日志</Button>
              </div>
            </section>
          )}
        </main>
      </div>
    </div>
  );
}

function relative(value: string) {
  if (!value) return "刚刚";
  const diff = Date.now() - new Date(value).getTime();
  if (diff < 60_000) return "刚刚";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
  return `${Math.floor(diff / 86_400_000)} 天前`;
}
