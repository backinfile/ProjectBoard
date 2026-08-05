import { afterEach, describe, expect, it } from 'vitest';
import { login } from '../src/server/auth.js';
import { createUserWithGeneratedPassword, resetUserWithGeneratedPassword } from '../src/server/users.js';
import { validatePassword } from '../src/server/security.js';
import { fixture } from './helpers.js';

describe('generated user passwords', () => {
  const cleanups: Array<() => void> = [];
  afterEach(() => cleanups.splice(0).forEach(cleanup => cleanup()));

  it('creates a user with a generated password that can authenticate without a forced change', async () => {
    const test = fixture('create-user-password');
    cleanups.push(test.cleanup);

    const created = await createUserWithGeneratedPassword(test.db, test.actors.admin, {
      displayName: 'New User',
      username: 'new-user',
      systemRole: 'user',
    });

    expect(validatePassword(created.temporaryPassword)).toBe(true);
    const session = await login(test.db, 'new-user', created.temporaryPassword, { ip: '127.0.0.1' });
    expect(session.user.mustChangePassword).toBe(false);
  });

  it('resets a user to a newly generated temporary password and invalidates the old one', async () => {
    const test = fixture('reset-user-password');
    cleanups.push(test.cleanup);
    const created = await createUserWithGeneratedPassword(test.db, test.actors.admin, {
      displayName: 'Reset User',
      username: 'reset-user',
      systemRole: 'user',
    });

    const reset = await resetUserWithGeneratedPassword(test.db, test.actors.admin, created.id);

    expect(reset.temporaryPassword).not.toBe(created.temporaryPassword);
    expect(validatePassword(reset.temporaryPassword)).toBe(true);
    await expect(login(test.db, 'reset-user', created.temporaryPassword, { ip: '127.0.0.2' })).rejects.toMatchObject({ code: 'INVALID_CREDENTIALS' });
    const session = await login(test.db, 'reset-user', reset.temporaryPassword, { ip: '127.0.0.3' });
    expect(session.user.mustChangePassword).toBe(false);
  });
});
