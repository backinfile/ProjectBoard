import type { Db } from './db.js';
import type { Actor, ProjectRole } from '../shared/types.js';
import { DomainError } from '../shared/types.js';

export function requireHuman(actor: Actor) { if (actor.type !== 'human' || !actor.id) throw new DomainError(401, 'AUTH_REQUIRED', 'Human authentication required'); }
export function user(db: Db, actor: Actor) { requireHuman(actor); const row = db.get<any>('SELECT * FROM users WHERE id=?', actor.id); if (!row || row.status !== 'active') throw new DomainError(401, 'ACCOUNT_DISABLED', 'Account unavailable'); return row; }
export function requireAdmin(db: Db, actor: Actor) { const row = user(db, actor); if (row.system_role !== 'administrator') throw new DomainError(403, 'ADMIN_REQUIRED', 'Administrator required'); return row; }
export function projectRole(db: Db, actor: Actor, projectId: string): ProjectRole | 'administrator' {
  if (actor.type === 'human' && actor.id) {
    const u = user(db, actor); if (u.system_role === 'administrator') return 'administrator';
    const m = db.get<any>('SELECT role FROM project_memberships WHERE project_id=? AND user_id=?', projectId, actor.id);
    if (m) return m.role;
  }
  if (actor.type === 'agent' && actor.id) {
    const grant = db.get('SELECT 1 FROM agent_project_grants WHERE project_id=? AND agent_id=?', projectId, actor.id);
    if (grant) return 'developer';
  }
  throw new DomainError(404, 'NOT_FOUND', 'Project not found');
}
export function requireDeveloper(db: Db, actor: Actor, projectId: string) { const role = projectRole(db, actor, projectId); if (role === 'viewer') throw new DomainError(403, 'DEVELOPER_REQUIRED', 'Developer access required'); }
