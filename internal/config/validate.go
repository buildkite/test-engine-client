package config

import (
	"github.com/go-playground/validator/v10"
)

// validate checks if the Config struct is valid using the "github.com/go-playground/validator" package to validate the struct.
// The validation rules are defined in the struct tags.
// It returns an InvalidConfigError if the struct is invalid.
func (c *Config) validate() error {
	validate := validator.New(validator.WithRequiredStructEnabled())
	validationErrors := validate.Struct(c)

	if validationErrors != nil {
		var errs InvalidConfigError
		for _, err := range validationErrors.(validator.ValidationErrors) {
			errs = append(errs, InvalidFieldError{
				name:  err.Field(),
				rule:  err.Tag(),
				param: err.Param(),
			})
		}
		return errs
	}
	return nil
}
