package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/go-playground/validator"
)

type Config struct {
	// Parallelism is the number of parallel tasks to run.
	Parallelism int `validate:"required,gt=0,lte=100"`
	// ServerBaseUrl is the base URL of the test splitter server.
	ServerBaseUrl string `validate:"required,url"`
	// SuiteToken is the token of the test suite.
	SuiteToken string `validate:"required,max=1024"`
	// Mode is the mode of the test splitter.
	Mode string `validate:"required,oneof=static"`
	// Identifier is the identifier of the build.
	Identifier string `validate:"required,max=1024"`
	// Node index is index of the current node.
	NodeIndex int `validate:"required,gte=0,ltfield=Parallelism"`
}

var ErrInvalidConfig = errors.New("invalid config")

func (c *Config) fetchFromEnv() error {
	var errs []error
	c.SuiteToken = os.Getenv("BUILDKITE_SUITE_TOKEN")
	c.Identifier = os.Getenv("BUILDKITE_BUILD_ID")
	c.ServerBaseUrl = getEnvWithDefault("BUILDKITE_SPLITTER_BASE_URL", "https://buildkite.com")
	c.Mode = getEnvWithDefault("BUILDKITE_SPLITTER_MODE", "static")

	parallelism := os.Getenv("BUILDKITE_PARALLEL_JOB_COUNT")
	parallelismInt, err := strconv.Atoi(parallelism)
	if err != nil {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidConfig, "parallelism must be an integer"))
	} else {
		c.Parallelism = parallelismInt
	}

	nodeIndex := os.Getenv("BUILDKITE_PARALLEL_JOB")
	nodeIndexInt, err := strconv.Atoi(nodeIndex)
	if err != nil {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidConfig, "node index must be an integer"))
	} else {
		c.NodeIndex = nodeIndexInt
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (c *Config) validate() error {
	validate := validator.New()
	return validate.Struct(c)
}

func New() (Config, error) {
	var errs []error

	c := &Config{}

	// Fetch from environment variables
	err := c.fetchFromEnv()
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}

	return *c, nil
}
