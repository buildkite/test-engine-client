package config

import (
	"fmt"
	"net/url"
	"strconv"
)

// validator is a helper struct to validate the Config struct.
// It contains errs which is a list of invalidFieldError and various validation functions.
type validator struct {
	errs *InvalidConfigError
}

func (v *validator) validateStringRequired(field string, value string) {
	if value == "" {
		*v.errs = append(*v.errs, invalidFieldError{
			name: field,
			err:  "can not be blank",
		})
	}
}

func (v *validator) validateStringMaxLen(field string, value string, max int) {
	if len(value) > max {
		*v.errs = append(*v.errs, invalidFieldError{
			name: field,
			err:  fmt.Sprintf("can not be longer than %d characters", max),
		})
	}
}

func (v *validator) validateMin(field string, value int, min int) {
	if value < min {
		*v.errs = append(*v.errs, invalidFieldError{
			name: field,
			err:  fmt.Sprintf("must be greater than or equal to %d", min),
		})
	}
}

func (v *validator) validateMax(field string, value int, max int) {
	if value > max {
		*v.errs = append(*v.errs, invalidFieldError{
			name: field,
			err:  fmt.Sprintf("can not be greater than %d", max),
		})
	}
}

func (v *validator) validateStringIn(field string, value string, validValues []string) {
	for _, validValue := range validValues {
		if value == validValue {
			return
		}
	}
	*v.errs = append(*v.errs, invalidFieldError{
		name: field,
		err:  fmt.Sprintf("%s is not a valid %s. Valid values are %v", value, field, validValues),
	})
}

func (v *validator) validateStringUrl(field string, value string) {
	if _, err := url.ParseRequestURI(value); err != nil {
		*v.errs = append(*v.errs, invalidFieldError{
			name: field,
			err:  "must be a valid URL",
		})
	}
}

func (v *validator) validateStringNumeric(field string, value string) (int, *invalidFieldError) {
	int, err := strconv.Atoi(value)
	if err != nil {
		fieldError := invalidFieldError{
			name: field,
			err:  "must be a number",
		}
		*v.errs = append(*v.errs, fieldError)
		return int, &fieldError
	}
	return int, nil
}
