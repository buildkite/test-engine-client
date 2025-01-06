# Buildkite Test Engine Client

Buildkite Test Engine Client (bktec) is an open source tool to orchestrate your test suites. It uses your Buildkite Test Engine suite data to intelligently partition and parallelize your tests.

bktec supports multiple test runners and offers various features to enhance your testing workflow. Below is a comparison of the features supported by each test runner:

| Feature                                            | Rspec | Jest | Playwright | Cypress | Go |
| -------------------------------------------------- | :---: | :--: | :--------: | :-----: | :--: |
| Filter test files                                  |   ✅  |   ✅  |    ✅      |    ✅   |   ✅  |
| Automatically retry failed test                    |   ✅  |   ✅  |    ✅      |    ❌   |   ✅  |
| Split slow files by individual test example        |   ✅  |   ❌  |    ❌      |    ❌   |   ✅  |

## Installation
The latest version of bktec can be downloaded from https://github.com/buildkite/test-engine-client/releases

### Supported OS/Architecture
ARM and AMD architecture for linux and darwin

The available Go binaries
- bktec-darwin-amd64
- bktec-darwin-arm64
- bktec-linux-amd64
- bktec-linux-arm64

## Using bktec

### Buildkite Pipeline environment variables
bktec uses the following Buildkite Pipeline provided environment variables.
| Environment Variable | Description|
| -------------------- | ----------- |
| `BUILDKITE_BUILD_ID` | The UUID of the Buildkite build. bktec uses this UUID along with `BUILDKITE_STEP_ID` to uniquely identify the test plan. |
| `BUILDKITE_JOB_ID` | The UUID of the job in Buildkite build. |
| `BUILDKITE_ORGANIZATION_SLUG` | The slug of your Buildkite organization. |
| `BUILDKITE_PARALLEL_JOB` | The index number of a parallel job created from a Buildkite parallel build step. <br>Make sure you configure `parallelism` in your pipeline definition.  You can read more about Buildkite parallel build step on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism).|
| `BUILDKITE_PARALLEL_JOB_COUNT` | The total number of parallel jobs created from a Buildkite parallel build step. <br>Make sure you configure `parallelism` in your pipeline definition.  You can read more about Buildkite parallel build step on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism). |
| `BUILDKITE_STEP_ID` | The UUID of the step group in Buildkite build. bktec uses this UUID along with `BUILDKITE_BUILD_ID` to uniquely identify the test plan.

> [!IMPORTANT]
> Please make sure that the above environment variables are available in your testing environment, particularly if you use Docker or some other type of containerization to run your tests.

### Create API access token
To use bktec, you need a Buildkite API access token with `read_suites`, `read_test_plan`, and `write_test_plan` scopes. You can generate this token from your [Personal Settings](https://buildkite.com/user/api-access-tokens) in Buildkite. After creating the token, set the `BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN` environment variable with the token value.

```sh
export BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN=token
```

### Configure Test Engine suite slug
To use bktec, you need to configure the `BUILDKITE_TEST_ENGINE_SUITE_SLUG` environment variable with your Test Engine suite slug. You can find the suite slug in the URL of your suite. For example, in the URL `https://buildkite.com/organizations/my-organization/analytics/suites/my-suite`, the slug is `my-suite`.

```sh
export BUILDKITE_TEST_ENGINE_SUITE_SLUG=my-slug
```

### Configure the test runner
To configure the test runner for bktec, please refer to the detailed guides provided for each supported test runner. You can find the guides at the following links:
- [RSpec](./docs/rspec.md)
- [Jest](./docs/jest.md)
- [Playwright](./docs/playwright.md)
- [Cypress](./docs/cypress.md)
- [Go](./docs/go.md)

### Running bktec
Please download the executable and make it available in your testing environment.
To parallelize your tests in your Buildkite build, you can amend your pipeline step configuration to:
```
steps:
  - name: "Rspec"
    command: ./bktec
    parallelism: 10
    env:
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: my-suite
      BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN: your-secret-token
      BUILDKITE_TEST_ENGINE_TEST_RUNNER: rspec
      BUILDKITE_TEST_ENGINE_RESULT_PATH: tmp/result.json
```

> [!TIP]
> You can find example configurations and usage instructions for each test runner in our [examples repository](https://github.com/buildkite/test-engine-client-examples).


### Development

Testing bktec requires an environment with ruby, jest, playwright, cypress, and go installed. The easiest way to set up the environment is to use docker compose.

```bash
docker compose -f .buildkite/docker-compose.yml run ci ./.buildkite/steps/tests.sh
```

The images are built with the `linux/amd64` platform, so you'll need to run the tests on an AMD64 machine or use Rosetta 2 to run the tests on an Apple Silicon machine.

### Debugging

To enable debug mode, set the `BUILDKITE_TEST_ENGINE_DEBUG_ENABLED` environment variable to `true`. This will print detailed output to assist in debugging bktec.

### Possible exit statuses

bktec may exit with a variety of exit statuses, outlined below:

- If there is a configuration error, bktec will exit with
  status 16.
- If the test runner (e.g. RSpec) exits cleanly, the exit status of
  the runner is returned. This will likely be 0 for successful test runs, 1 for
  failing test runs, but may be any other error status returned by the runner.
- If the test runner is terminated by an OS level signal, such as SIGSEGV or
  SIGABRT, the exit status returned will be equal to 128 plus the signal number.
  For example, if the runner raises a SIGSEGV, the exit status will be (128 +
  11) = 139.
