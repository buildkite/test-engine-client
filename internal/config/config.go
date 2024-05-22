package config

// Config is the internal representation of the complete test splitter client configuration.
type Config struct {
	// AccessToken is the access token for the API.
	AccessToken string
	// Identifier is the identifier of the build.
	Identifier string
	// Mode is the mode of the test splitter.
	Mode string
	// Node index is index of the current node.
	NodeIndex int
	// OrganizationSlug is the slug of the organization.
	OrganizationSlug string
	// Parallelism is the number of parallel tasks to run.
	Parallelism int
	// ServerBaseUrl is the base URL of the test splitter server.
	ServerBaseUrl string
	// SuiteSlug is the slug of the suite.
	SuiteSlug string
	// TestCommand is the command to run the tests.
	TestCommand string
}

// New wraps the readFromEnv and validate functions to create a new Config struct.
// It returns Config struct and an InvalidConfigError if there is an invalid configuration.
func New() (Config, error) {
	var errs InvalidConfigError

	c := Config{}

	if err := c.readFromEnv(); err != nil {
		errs = append(errs, err.(InvalidConfigError)...)
	}

	if err := c.validate(); err != nil {
		errs = append(errs, err.(InvalidConfigError)...)
	}

	if len(errs) > 0 {
		return Config{}, errs
	}

	return c, nil
}
