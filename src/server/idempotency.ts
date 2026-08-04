import type { Db } from './db.js';

export function idempotent<T>(db: Db, scope: string, requestId: string, fn: () => T): T {
  const found = db.get<any>('SELECT response_json FROM idempotency_records WHERE scope=? AND request_id=?', scope, requestId);
  if (found) return JSON.parse(found.response_json) as T;
  const value = fn();
  db.run('INSERT INTO idempotency_records(scope,request_id,response_json,status_code,created_at) VALUES(?,?,?,?,?)', scope, requestId, JSON.stringify(value), 200, db.now());
  return value;
}
