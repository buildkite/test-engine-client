package command

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/buildkite/test-engine-client/v2/internal/api"
	"github.com/buildkite/test-engine-client/v2/internal/config"
)

// newSchedulerClient mints an OIDC token carrying the claims required by the
// Test Scheduler API and returns an API client authenticated with it, along
// with the decoded claims (organization, pipeline, build and job UUIDs).
func newSchedulerClient(cfg *config.Config) (*api.Client, api.OIDCClaims, error) {
	token, err := cfg.GenerateSchedulerOIDCToken()
	if err != nil {
		return nil, api.OIDCClaims{}, fmt.Errorf("generating Test Scheduler OIDC token: %w", err)
	}

	claims, err := api.DecodeOIDCClaims(token)
	if err != nil {
		return nil, api.OIDCClaims{}, err
	}

	client := api.NewClient(api.ClientConfig{
		AccessToken:      token,
		OrganizationSlug: cfg.OrganizationSlug,
		ServerBaseURL:    cfg.ServerBaseURL,
	})

	return client, claims, nil
}

// createSchedulerPool creates a Test Scheduler pool from the test plan
// identified by cfg.Identifier. When the pool already exists (e.g. a job
// retry re-running `bktec plan`), the existing pool is resolved and reused.
func createSchedulerPool(ctx context.Context, cfg *config.Config) error {
	client, claims, err := newSchedulerClient(cfg)
	if err != nil {
		return err
	}

	suite, err := client.FetchSuite(ctx, cfg.SuiteSlug)
	if err != nil {
		return fmt.Errorf("fetching suite %q: %w", cfg.SuiteSlug, err)
	}

	response, err := client.CreateSchedulerPlan(ctx, api.CreateSchedulerPlanParams{
		OrganizationID:     claims.OrganizationID,
		SuiteID:            suite.ID,
		PipelineID:         claims.PipelineID,
		BuildID:            claims.BuildID,
		Key:                cfg.PoolName,
		TestPlanIdentifier: cfg.Identifier,
	})
	if err != nil {
		conflictError := new(api.ConflictError)
		if !errors.As(err, &conflictError) {
			return fmt.Errorf("creating test pool: %w", err)
		}

		// The pool (pipeline_id, build_id, key) already exists, e.g. when the
		// plan step is retried. Resolve the existing pool and treat as success.
		pools, err := client.FetchSchedulerPools(ctx, api.FetchSchedulerPoolsParams{
			PipelineID: claims.PipelineID,
			BuildID:    claims.BuildID,
			Key:        cfg.PoolName,
		})
		if err != nil {
			return fmt.Errorf("resolving existing test pool: %w", err)
		}
		if len(pools) == 0 {
			return fmt.Errorf("test pool %q already exists but could not be found", cfg.PoolName)
		}

		fmt.Fprintf(os.Stderr, "Using existing test pool %s (%s)\n", pools[0].ID, pools[0].State)
		return nil
	}

	fmt.Fprintf(os.Stderr, "Created test pool %s (%s) with %d entries\n", response.Pool.ID, response.Pool.State, response.UploadedEntriesCount)
	return nil
}
