import { test, expect } from '@playwright/test';

test('says good bye', async ({ page }) => {
  await page.goto('/');

  await expect(page).toHaveText('good bye');
});

test('it passes', () => {
  expect(true).toBeTruthy();
});
