import type { Db } from './db.js';
import { hashPassword, randomToken, validatePassword } from './security.js';

export interface BootstrapOptions {
  username?: string;
  password?: string;
}

export interface BootstrapCredentials {
  username: string;
  password: string;
  generated: boolean;
}

export async function bootstrapAdministrator(
  db: Db,
  options: BootstrapOptions = {},
): Promise<BootstrapCredentials | null> {
  if ((db.get<{ n: number }>('SELECT COUNT(*) n FROM users')?.n ?? 0) > 0) return null;

  const username = options.username?.trim() || 'admin';
  const configuredPassword = options.password === undefined || options.password === '' ? undefined : options.password;
  const password = configuredPassword ?? `${randomToken(24)}-Aa1`;
  if (!validatePassword(password)) {
    throw new Error('PROJECTBOARD_BOOTSTRAP_PASSWORD must be 12-256 characters with letters and numbers');
  }

  const id = db.id();
  const now = db.now();
  db.run(
    "INSERT INTO users VALUES(?,?,?,?,'administrator','active',0,NULL,?,?,NULL)",
    id,
    username,
    'Administrator',
    await hashPassword(password),
    now,
    now,
  );
  return { username, password, generated: configuredPassword === undefined };
}
