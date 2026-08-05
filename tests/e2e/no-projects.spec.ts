import { expect, test } from '@playwright/test';

test('shows a stable empty state when no projects exist', async ({ page }) => {
  const browserErrors: string[] = [];
  page.on('pageerror', (error) => browserErrors.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error') browserErrors.push(message.text());
  });

  await page.goto('/');
  await page.getByLabel('用户名').fill('admin');
  await page.getByLabel('密码').fill('admin-password-123');
  await page.getByRole('button', { name: '登录' }).click();
  browserErrors.length = 0;

  await expect(page.getByRole('heading', { name: '任务队列' })).toBeVisible();
  await expect(page.getByTestId('no-projects')).toBeVisible();
  await expect(page.getByRole('button', { name: '创建项目' })).toBeVisible();
  await expect(page.getByRole('button', { name: '新建工单' })).toHaveCount(0);

  await page.getByRole('button', { name: '成员' }).click();
  await expect(page.getByTestId('no-projects')).toBeVisible();

  await page.getByRole('button', { name: 'Agents' }).click();
  await expect(page.getByTestId('no-projects')).toBeVisible();

  await page.getByRole('button', { name: '活动记录' }).click();
  await expect(page.getByTestId('no-projects')).toBeVisible();

  expect(browserErrors).toEqual([]);
});
