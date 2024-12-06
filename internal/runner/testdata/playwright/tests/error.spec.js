import { test, expect } from '@playwright/test';

boom();

test.describe('test group', () => {
  test('failed', () => {
    expect(1).toBe(2);
  })
});
