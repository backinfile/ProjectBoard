import { z } from 'zod';

export const idempotent = z.object({ requestId: z.string().min(8).max(128) });
export const versioned = idempotent.extend({ expectedVersion: z.number().int().positive() });
export const workItemCreateSchema = idempotent.extend({
  projectId: z.string().min(1), title: z.string().min(1).max(240), descriptionMarkdown: z.string().max(100_000).default(''),
  acceptanceCriteriaMarkdown: z.string().max(100_000).default(''), priority: z.enum(['urgent','high','medium','low']).default('medium'),
  targetBranch: z.string().min(1).max(255).optional(), discussionMode: z.enum(['manual','auto']).optional(),
  executionMode: z.enum(['manual','auto']).optional(), acceptanceMode: z.enum(['human','independent_agent','auto']).optional(),
  assigneeKind: z.enum(['human','agent']).optional(), assigneeId: z.string().optional(), parentId: z.string().optional(),
});
export const conclusionSchema = versioned.extend({
  goalMarkdown: z.string().min(1), scopeMarkdown: z.string().min(1), outOfScopeMarkdown: z.string(),
  implementationPlanMarkdown: z.string().min(1), acceptanceCriteriaMarkdown: z.string().min(1), risksMarkdown: z.string(),
});
export const acceptanceSchema = versioned.extend({
  outcome: z.enum(['pass','changes_requested','scope_unclear','abandon']), noteMarkdown: z.string().min(1),
  criteriaResults: z.array(z.object({ criterion: z.string(), passed: z.boolean(), note: z.string().optional() })).default([]),
});
export const executionSchema = versioned.extend({
  summaryMarkdown: z.string().min(1), pushed: z.boolean(), worktreeClean: z.boolean(), forbiddenPathsClean: z.boolean(),
  noCodeReason: z.string().optional(), commits: z.array(z.string()).default([]), changedFiles: z.array(z.string()).default([]),
  validations: z.array(z.object({ name: z.string(), required: z.boolean(), exitCode: z.number().int(), durationMs: z.number().int().nonnegative(), logSummary: z.string() })).default([]),
  remainingRisksMarkdown: z.string().default(''),
});
