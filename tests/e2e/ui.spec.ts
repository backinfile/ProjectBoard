import { expect, test } from '@playwright/test';

test('language switch and overlay drawer preserve the application layout', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel('用户名').fill('admin');
  await page.getByLabel('密码').fill('admin-password-123');
  await page.getByRole('button', { name: '登录' }).click();
  await expect(page.getByRole('heading', { name: '任务队列' })).toBeVisible();

  if (await page.getByTestId('no-projects').isVisible()) {
    await page.getByRole('button', { name: '创建项目' }).click();
    const drawer = page.getByRole('dialog');
    await drawer.locator('input[name="name"]').fill('Layout project');
    await drawer.locator('input[name="key"]').fill('layout');
    await drawer.locator('input[name="repo"]').fill('https://github.com/acme/layout.git');
    await drawer.getByRole('button', { name: 'Submit' }).click();
    await expect(page.getByText('Layout project', { exact: true }).first()).toBeVisible();
  }

  const shell = await page.locator('.shell').boundingBox();
  await page.getByRole('button', { name: 'Switch to English' }).click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await page.getByRole('button', { name: 'New work item' }).click();
  await expect(page.locator('.drawer')).toBeVisible();
  expect(await page.locator('.shell').boundingBox()).toEqual(shell);
});
