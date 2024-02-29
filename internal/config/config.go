package config

import (
	"errors"
)

type Config struct {
	// Parallelism is the number of parallel tasks to run.
	Parallelism int `validate:"required,gt=0,lte=500"`
	// ServerBaseUrl is the base URL of the test splitter server.
	ServerBaseUrl string `validate:"required,url"`
	// SuiteToken is the token of the test suite.
	SuiteToken string `validate:"required,max=1024"`
	// Mode is the mode of the test splitter.
	Mode string `validate:"required,oneof=static"`
	// Identifier is the identifier of the build.
	Identifier string `validate:"required,max=1024"`
	// Node index is index of the current node.
	NodeIndex int `validate:"gte=0,ltfield=Parallelism"`
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