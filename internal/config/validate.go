package config

import (
	"fmt"

	"github.com/go-playground/validator"
)

func (c *Config) validate() error {
	validate := validator.New()
	err := validate.Struct(c)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidConfig, err)
	}
	return nil
}
