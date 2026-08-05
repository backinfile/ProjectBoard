import { expect, test, type Page } from '@playwright/test';

test.describe.configure({ mode: 'serial' });

async function login(page: Page) {
  await page.goto('/');
  await page.locator('input[name="username"]').fill('admin');
  await page.locator('input[name="password"]').fill('admin-password-123');
  await page.locator('form').getByRole('button').click();
  await expect(page.getByRole('heading', { name: '任务队列' })).toBeVisible();
}

async function createProject(page: Page, name: string, key: string) {
  await page.getByRole('button', { name: '创建项目' }).click();
  const drawer = page.getByRole('dialog');
  await drawer.locator('input[name="name"]').fill(name);
  await drawer.locator('input[name="key"]').fill(key);
  await drawer.locator('input[name="repo"]').fill(`https://github.com/acme/${key}.git`);
  await drawer.getByRole('button', { name: 'Submit' }).click();
}

async function ensureProject(page: Page) {
  if (await page.getByTestId('no-projects').isVisible()) {
    await createProject(page, 'Regression project', 'regression');
    await expect(page.getByText('Regression project', { exact: true }).first()).toBeVisible();
  }
}

test('a second authenticated tab can perform protected actions', async ({ page, context }) => {
  await login(page);
  const secondTab = await context.newPage();
  await secondTab.goto('/');
  await expect(secondTab.getByRole('heading', { name: '任务队列' })).toBeVisible();

  await createProject(secondTab, 'Cross-tab project', 'cross-tab');
  await expect(secondTab.getByText('Cross-tab project', { exact: true }).first()).toBeVisible();
  await expect(secondTab.getByText('CSRF token invalid')).toHaveCount(0);
});

test('member submission is disabled when there are no eligible users', async ({ page }) => {
  await login(page);
  await ensureProject(page);
  await page.getByRole('button', { name: '成员' }).click();
  await page.getByRole('button', { name: '添加成员' }).click();
  const drawer = page.getByRole('dialog');

  await expect(drawer.locator('select[name="user"] option')).toHaveCount(0);
  await expect(drawer.getByRole('button', { name: 'Submit' })).toBeDisabled();
});

test('agent pairing code remains visible after creation', async ({ page }) => {
  await login(page);
  await ensureProject(page);
  await page.getByRole('button', { name: 'Agents' }).click();
  await page.getByRole('button', { name: '创建 Agent' }).click();
  const drawer = page.getByRole('dialog');
  await drawer.locator('input[name="name"]').fill('review-runner');
  await drawer.locator('textarea[name="purpose"]').fill('Regression test runner');
  await drawer.getByRole('button', { name: 'Submit' }).click();

  await expect(drawer.getByRole('heading', { name: 'Connect Runner' })).toBeVisible();
  await expect(drawer.locator('.pair-code')).toHaveText(/^PB-[A-Z0-9_-]{4}-[A-Z0-9_-]{4}$/);
});

test('saving project settings closes the drawer and refreshes the page', async ({ page }) => {
  await login(page);
  await ensureProject(page);
  await page.getByRole('button', { name: '项目设置' }).click();
  await page.getByRole('button', { name: '编辑设置' }).click();
  const drawer = page.getByRole('dialog');
  await drawer.locator('textarea[name="allowed"]').fill('main\nrelease/*');
  await drawer.getByRole('button', { name: 'Submit' }).click();

  await expect(drawer).toHaveCount(0);
  await expect(page.getByText('main, release/*', { exact: true })).toBeVisible();
});

test('Runner download page serves the installable release package', async ({ page }) => {
  await login(page);
  await page.getByRole('button', { name: 'Runner 下载' }).click();
  await expect(page.getByRole('heading', { name: 'Runner 下载' })).toBeVisible();
  await expect(page.getByText('projectboard-runner-1.0.0.tgz', { exact: true })).toBeVisible();

  const downloadPromise = page.waitForEvent('download');
  await page.getByRole('link', { name: '下载 Runner' }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe('projectboard-runner-1.0.0.tgz');
});
