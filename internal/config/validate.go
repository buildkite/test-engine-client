package config

import (
	"github.com/go-playground/validator"
)

func (c *Config) validate() error {
	validate := validator.New()
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
