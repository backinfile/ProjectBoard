import type { Actor } from '../shared/types.js';
import { DomainError } from '../shared/types.js';
import { audit } from './audit.js';
import type { Db } from './db.js';
import { hashPassword, randomToken } from './security.js';

type NewUser = {
  displayName: string;
  username: string;
  systemRole?: 'administrator' | 'user';
};

function generatedTemporaryPassword() {
  return `${randomToken(18)}-Aa1`;
}

export async function createUserWithGeneratedPassword(db: Db, actor: Actor, input: NewUser) {
  const id = db.id();
  const now = db.now();
  const temporaryPassword = generatedTemporaryPassword();
  const systemRole = input.systemRole ?? 'user';
  db.run(
    "INSERT INTO users(id,username,display_name,password_hash,system_role,status,must_change_password,created_at,updated_at) VALUES(?,?,?,?,?,'active',0,?,?)",
    id,
    input.username,
    input.displayName,
    await hashPassword(temporaryPassword),
    systemRole,
    now,
    now,
  );
  audit(db, actor, 'account.created', 'user', id, null, { username: input.username, systemRole });
  return { id, username: input.username, displayName: input.displayName, temporaryPassword };
}

export async function resetUserWithGeneratedPassword(db: Db, actor: Actor, userId: string) {
  const target = db.get<{ id: string }>('SELECT id FROM users WHERE id=?', userId);
  if (!target) throw new DomainError(404, 'NOT_FOUND', 'User not found');

  const temporaryPassword = generatedTemporaryPassword();
  const nextHash = await hashPassword(temporaryPassword);
  db.transaction(() => {
    db.run('UPDATE users SET password_hash=?,must_change_password=0,updated_at=? WHERE id=?', nextHash, db.now(), userId);
    db.run('UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL', db.now(), userId);
    audit(db, actor, 'account.password_reset', 'user', userId, null, {});
  });
  return { temporaryPassword };
}
