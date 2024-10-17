import { test, expect } from '@playwright/test';

test.describe('home page', () => {
  test('has title', async ({ page }) => {
    await page.goto('/');

    await expect(page).toHaveTitle(/Playwright example/);
  });

  test('says hello', async ({ page }) => {
    await page.goto('/');

    const h1 = await page.locator('h1');
    await expect(h1).toHaveText('Hello, World!');
  })
});
