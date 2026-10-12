import { test } from '@playwright/test';

test('set up app', () => {
  if (process.env.PLAYWRIGHT_FAIL_SETUP) {
    throw new Error('setup failed');
  }
});
