import { test, expect } from '@playwright/test';


test.describe('test group', () => {
  test('failed', () => {
    expect(1).toBe(2);
  })
});

test('it passes', () => {
  expect(true).toBeTruthy();
});
