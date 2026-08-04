export const stages = ['todo', 'discussion', 'execution', 'acceptance', 'completed', 'abandoned'] as const;
export type Stage = (typeof stages)[number];
export type Actor = { type: 'human' | 'agent' | 'system'; id: string | null; name?: string };
export type ProjectRole = 'developer' | 'viewer';
export type SystemRole = 'administrator' | 'user';

export interface WorkItem {
  id: string;
  project_id: string;
  number: number;
  parent_id: string | null;
  title: string;
  description_markdown: string;
  acceptance_criteria_markdown: string;
  priority: 'urgent' | 'high' | 'medium' | 'low';
  stage: Stage;
  discussion_mode: 'manual' | 'auto';
  execution_mode: 'manual' | 'auto';
  acceptance_mode: 'human' | 'independent_agent' | 'auto';
  target_branch: string;
  assignee_kind: 'human' | 'agent' | null;
  assignee_id: string | null;
  assignment_state: 'reserved' | 'active' | null;
  blocked_at: string | null;
  blocked_reason: string | null;
  abandoned_at: string | null;
  abandoned_reason: string | null;
  abandoned_from_stage: Stage | null;
  version: number;
  created_at: string;
  updated_at: string;
  completed_at: string | null;
}

export class DomainError extends Error {
  constructor(public status: number, public code: string, message: string, public details?: unknown) {
    super(message);
  }
}
