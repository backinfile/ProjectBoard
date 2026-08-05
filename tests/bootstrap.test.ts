import { afterEach, describe, expect, it } from 'vitest';
import { bootstrapAdministrator } from '../src/server/bootstrap.js';
import { login } from '../src/server/auth.js';
import { validatePassword } from '../src/server/security.js';
import { fixture } from './helpers.js';

describe('bootstrap administrator', () => {
  const cleanups: Array<() => void> = [];
  afterEach(() => cleanups.splice(0).forEach((cleanup) => cleanup()));

  it('generates a secure password that can authenticate the first administrator', async () => {
    const test = fixture('bootstrap-generated-password', { empty: true });
    cleanups.push(test.cleanup);

    const credentials = await bootstrapAdministrator(test.db, { username: 'admin' });

    expect(credentials).not.toBeNull();
    expect(validatePassword(credentials!.password)).toBe(true);
    const session = await login(test.db, credentials!.username, credentials!.password, { ip: '127.0.0.1' });
    expect(session.user.mustChangePassword).toBe(false);
  });

  it('honors an explicitly configured secure password without requiring a change', async () => {
    const test = fixture('bootstrap-configured-password', { empty: true });
    cleanups.push(test.cleanup);
    const password = 'ConfiguredAdminPassword2026';

    const credentials = await bootstrapAdministrator(test.db, { username: 'owner', password });

    expect(credentials).toEqual({ username: 'owner', password, generated: false });
    const session = await login(test.db, 'owner', password, { ip: '127.0.0.1' });
    expect(session.user.mustChangePassword).toBe(false);
  });
});
