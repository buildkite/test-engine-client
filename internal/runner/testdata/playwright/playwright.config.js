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
    url: 'http://127.0.0.1:8080',
  },
  use: {
    baseURL: 'http://localhost:8080/',
  },
});

