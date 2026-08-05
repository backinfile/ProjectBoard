import { defineConfig } from '@playwright/test';

const port = Number(process.env.PROJECTBOARD_E2E_PORT ?? 3333);
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: 'tests/e2e',
  workers: 1,
  use: { baseURL },
  webServer: {
    command: 'pnpm start',
    url: `${baseURL}/health`,
    reuseExistingServer: false,
    env: {
      PORT: String(port),
      PROJECTBOARD_DB: `./data/e2e-${Date.now()}.db`,
      PROJECTBOARD_BOOTSTRAP_ADMIN: 'admin',
      PROJECTBOARD_BOOTSTRAP_PASSWORD: 'admin-password-123',
      NODE_ENV: 'production',
      PROJECTBOARD_BASE_URL: baseURL,
    },
  },
});
