const { defineConfig, devices } = require('@playwright/test');

/**
 * @see https://playwright.dev/docs/test-configuration
 */
module.exports = defineConfig({
  testDir: './tests',
  reporter: [
    ['line'],
    ['json', { outputFile: './test-results/results.json' }]
  ],
  webServer: {
    command: 'yarn start',
    url: 'http://127.0.0.1:8987',
  },
  use: {
    baseURL: 'http://localhost:8987/',
  },
  projects: [
    // Runs before the browser projects whenever they run, whichever test files
    // are selected. Set PLAYWRIGHT_FAIL_SETUP=1 to make it fail.
    {
      name: 'setup',
      testMatch: /.*\.setup\.js/,
    },
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
      dependencies: ['setup'],
    },
    {
      name: 'firefox',
      use: { ...devices['Desktop Firefox'] },
      dependencies: ['setup'],
    },
  ],
});

