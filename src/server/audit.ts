import type { Db } from './db.js';
import type { Actor } from '../shared/types.js';
import { redact } from './security.js';

export function audit(db: Db, actor: Actor, eventType: string, objectType: string, objectId: string, projectId: string | null, payload: unknown, source = 'web') {
  const id = db.id();
  db.run('INSERT INTO activity_events(id,project_id,actor_type,actor_id,event_type,object_type,object_id,source,payload_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)', id, projectId, actor.type, actor.id, eventType, objectType, objectId, source, JSON.stringify(redact(payload)), db.now());
  return id;
}
export function timeline(db: Db, workItemId: string, kind: string, stage: string, actor: Actor, payload: unknown, relatedVersion?: number) {
  const id = db.id();
  db.run('INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,related_version,created_at) VALUES(?,?,?,?,?,?,?,?,?)', id, workItemId, kind, stage, actor.type, actor.id, JSON.stringify(redact(payload)), relatedVersion ?? null, db.now());
  return id;
}
