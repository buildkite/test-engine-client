package main

import (
	"context"

	"github.com/urfave/cli/v3"
)

// Values from the Buildkite build environment
var organizationSlugFlag = &cli.StringFlag{
	Name:        "organization-slug",
	Category:    "BUILD ENVIRONMENT",
	Usage:       "Buildkite organization slug",
	Sources:     cli.EnvVars("BUILDKITE_ORGANIZATION_SLUG"),
	Destination: &cfg.OrganizationSlug,
	Hidden:      true,
}

var buildIDFlag = &cli.StringFlag{
	Name:        "build-id",
	Category:    "BUILD ENVIRONMENT",
	Usage:       "Buildkite build id",
	Sources:     cli.EnvVars("BUILDKITE_BUILD_ID"),
	Destination: &cfg.BuildId,
	Hidden:      true,
}

var jobIDFlag = &cli.StringFlag{
	Name:        "job-id",
	Category:    "BUILD ENVIRONMENT",
	Usage:       "Buildkite job id",
	Sources:     cli.EnvVars("BUILDKITE_JOB_ID"),
	Destination: &cfg.JobId,
	Hidden:      true,
}

var stepIDFlag = &cli.StringFlag{
	Name:        "step-id",
	Category:    "BUILD ENVIRONMENT",
	Usage:       "Buildkite step id",
	Sources:     cli.EnvVars("BUILDKITE_STEP_ID"),
	Destination: &cfg.StepId,
	Hidden:      true,
}

var branchFlag = &cli.StringFlag{
	Name:        "branch",
	Category:    "BUILD ENVIRONMENT",
	Usage:       "Branch",
	Sources:     cli.EnvVars("BUILDKITE_BRANCH"),
	Destination: &cfg.Branch,
	Hidden:      true,
}

var retryCountFlag = &cli.IntFlag{
	Name:        "retry-count",
	Category:    "BUILD ENVIRONMENT",
	Usage:       "Retry count",
	Sources:     cli.EnvVars("BUILDKITE_RETRY_COUNT"),
	Destination: &cfg.JobRetryCount,
	Hidden:      true,
}

var parallelJobFlag = &cli.IntFlag{
	Name:        "parallel-job",
	Category:    "BUILD ENVIRONMENT",
	Usage:       "Parallel job",
	Sources:     cli.EnvVars("BUILDKITE_PARALLEL_JOB"),
	Destination: &cfg.NodeIndex,
	Hidden:      true,
}

var parallelismFlag = &cli.IntFlag{
	Name:        "parallelism",
	Category:    "BUILD ENVIRONMENT",
	Usage:       "Run the specified number of bktec processes in parallel",
	Sources:     cli.EnvVars("BUILDKITE_PARALLEL_JOB_COUNT"),
	Destination: &cfg.Parallelism,
}

// Test Engine specific flags
var accessTokenFlag = &cli.StringFlag{
	Name:        "access-token",
	Category:    "TEST ENGINE",
	Usage:       "Buildkite API access token",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN"),
	Destination: &cfg.AccessToken,
}

var suiteSlugFlag = &cli.StringFlag{
	Name:        "suite-slug",
	Category:    "TEST ENGINE",
	Usage:       "Buildkite suite slug",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_SUITE_SLUG"),
	Destination: &cfg.SuiteSlug,
}

var baseURLFlag = &cli.StringFlag{
	Name:        "base-url",
	Category:    "TEST ENGINE",
	Usage:       "Buildkite API base URL",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_BASE_URL"),
	Value:       "https://api.buildkite.com",
	Destination: &cfg.ServerBaseUrl,
	Hidden:      true,
}

// Test Runner specific flags
var filesFlag = &cli.StringFlag{
	Name:     "files",
	Category: "TEST RUNNER",
	Value:    "",
	Usage:    "Override the default test file discovery by providing a path to a file containing a list of test files (one per line)",
}

var testCommandFlag = &cli.StringFlag{
	Name:        "test-command",
	Category:    "TEST RUNNER",
	Usage:       "Test command",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_TEST_CMD"),
	Destination: &cfg.TestCommand,
}

var testFilePatternFlag = &cli.StringFlag{
	Name:        "test-file-pattern",
	Category:    "TEST RUNNER",
	Usage:       "Test file pattern",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN"),
	Destination: &cfg.TestFilePattern,
}

var testFileExcludePatternFlag = &cli.StringFlag{
	Name:        "test-file-exclude-pattern",
	Category:    "TEST RUNNER",
	Usage:       "Test file exclude pattern",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN"),
	Destination: &cfg.TestFileExcludePattern,
}

var testRunnerFlag = &cli.StringFlag{
	Name:        "test-runner",
	Category:    "TEST RUNNER",
	Usage:       "Test runner",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_TEST_RUNNER"),
	Destination: &cfg.TestRunner,
}

var resultPathFlag = &cli.StringFlag{
	Name:        "result-path",
	Category:    "TEST RUNNER",
	Usage:       "Path to the output file for the test runner",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_RESULT_PATH"),
	Destination: &cfg.ResultPath,
}

var splitByExampleFlag = &cli.BoolFlag{
	Name:        "split-by-example",
	Category:    "TEST RUNNER",
	Usage:       "Enable split by example (not supported by all test runners)",
	Value:       false,
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE"),
	Destination: &cfg.SplitByExample,
}

var failOnNoTestsFlag = &cli.BoolFlag{
	Name:        "fail-on-no-tests",
	Category:    "TEST RUNNER",
	Usage:       "Exit with an error if no tests are assigned to this node",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_FAIL_ON_NO_TESTS"),
	Destination: &cfg.FailOnNoTests,
}

// Test Runner Retry Flags
var testEngineRetryCountFlag = &cli.IntFlag{
	Name:        "test-engine-retry-count",
	Category:    "TEST RUNNER RETRY",
	Usage:       "Number of times to retry failing tests",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_RETRY_COUNT"),
	Destination: &cfg.MaxRetries,
}

var disableRetryMutedFlag = &cli.BoolFlag{
	Name:     "disable-retry-muted",
	Category: "TEST RUNNER RETRY",
	Usage:    "Disable retry for muted tests",
	Value:    false,
	Sources:  cli.EnvVars("BUILDKITE_TEST_ENGINE_DISABLE_RETRY_FOR_MUTED_TEST"),
	Action: func(ctx context.Context, cmd *cli.Command, v bool) error {
		// Note the config struct member is the logical opposite of the flag /
		// env var, so we need to invert the bool here.
		cfg.RetryForMutedTest = !v
		return nil
	},
}

var retryCommandFlag = &cli.StringFlag{
	Name:        "retry-command",
	Category:    "TEST RUNNER RETRY",
	Usage:       "Command to run when retrying failed tests.",
	Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_RETRY_CMD"),
	Destination: &cfg.RetryCommand,
}

var cliCommand = &cli.Command{
	Name:  "bktec",
	Usage: "Buildkite Test Engine Client",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:   "version",
			Usage:  "print version information and exit",
			Action: printVersion,
		},
		&cli.BoolFlag{
			Name:        "debug",
			Usage:       "Enable debug output",
			Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_DEBUG_ENABLED"),
			Destination: &cfg.DebugEnabled,
		},
	},
	Commands: []*cli.Command{
		{
			Name:   "run",
			Usage:  "Run tests (default)",
			Action: run,
			Flags: []cli.Flag{
				// Build Environment Flags
				organizationSlugFlag,
				buildIDFlag,
				jobIDFlag,
				stepIDFlag,
				branchFlag,
				retryCountFlag,
				parallelJobFlag,
				parallelismFlag,
				// Test Engine Flags
				accessTokenFlag,
				suiteSlugFlag,
				baseURLFlag,
				// Runner Environment Flags
				testCommandFlag,
				testFilePatternFlag,
				testFileExcludePatternFlag,
				testRunnerFlag,
				resultPathFlag,
				splitByExampleFlag,
				failOnNoTestsFlag,
				// Runner Retry Flags
				disableRetryMutedFlag,
				retryCommandFlag,
				testEngineRetryCountFlag,
				// Other Flags
				filesFlag,
				&cli.StringFlag{
					Name:        "plan-identifier",
					Value:       "",
					Usage:       "run the tests from a plan previously generated matching the provided plan-identifier",
					Destination: &cfg.Identifier,
					Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER"),
				},
			},
		},
		{
			Name:   "plan",
			Usage:  "Generate test plan without running tests",
			Action: plan,
			Flags: []cli.Flag{
				// Some of these flags are not strictly required for planning,
				// we will remove these in future iterations.

				// Build Environment Flags
				organizationSlugFlag,
				buildIDFlag,
				jobIDFlag,
				stepIDFlag,
				branchFlag,
				retryCountFlag,
				parallelJobFlag,
				// Test Engine Flags
				accessTokenFlag,
				suiteSlugFlag,
				baseURLFlag,
				// Runner Environment Flags
				testCommandFlag,
				testFilePatternFlag,
				testFileExcludePatternFlag,
				testRunnerFlag,
				resultPathFlag,
				splitByExampleFlag,
				// Runner Retry Flags
				disableRetryMutedFlag,
				retryCommandFlag,
				testEngineRetryCountFlag,
				// Other Flags
				filesFlag,
				&cli.IntFlag{
					Name:        "max-parallelism",
					Value:       0,
					Usage:       "instruct the test planner to calculate optimal parallelism for the build, not to exceed the provided value. When 0 this flag is ignored and the test plan parallelism will be derived from the BUILDKITE_PARALLEL_JOB_COUNT environment variable",
					Destination: &cfg.MaxParallelism,
					Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_MAX_PARALLELISM"),
				},
				&cli.DurationFlag{
					Name:        "target-time",
					Value:       0,
					Usage:       "the desired target time (e.g. 4m30s) for the entire test suite to complete. When 0 this flag is ignored and the test plan will not consider target time when deriving parallelism. Must be used in conjunction with --max-parallelism",
					Destination: &cfg.TargetTime,
					Sources:     cli.EnvVars("BUILDKITE_TEST_ENGINE_TARGET_TIME"),
				},
			},
			MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{
				{
					Required: true,
					Category: "PLAN OUTPUT",
					Flags: [][]cli.Flag{
						{
							&cli.BoolFlag{
								Name:  "json",
								Usage: "JSON format output",
							},
						},
						{
							&cli.StringFlag{
								Name:  "pipeline-upload",
								Usage: "buildkite-agent pipeline upload will be executed with the provided `template.yml`. The additional enviroment variables BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER and BUILDKITE_TEST_ENGINE_PARALLELISM from the generated plan will be available to the template.",
							},
						},
					},
				},
			},
		},
	},
}
