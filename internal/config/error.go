package config

import (
	"fmt"
	"sort"
	"strings"
)

// InvalidConfigError is an error that contains a list of all invalid fields in the config.
type InvalidConfigError []InvalidFieldError

func (i InvalidConfigError) Error() string {
	var errs []string
	for _, err := range i {
		errs = append(errs, err.Error())
	}
	sort.Strings(errs)
	return fmt.Sprintf("invalid config: %s", strings.Join(errs, ",\n"))
}

// InvalidFieldError is the detailed error of an invalid rule for a field in the config.
// This error implement more human readable error messages than "github.com/go-playground/validator".ValidationErrors provides.
type InvalidFieldError struct {
	// name is the name of the field.
	name string
	// rule is the name of the rule that failed.
	rule string
	// param is the parameter of the rule that failed.
	param string
}

func (f InvalidFieldError) Error() string {
	var message string
	switch f.rule {
	case "required":
		message = "is required"
	case "url":
		message = "must be a valid URL"
	case "number":
		message = "must be a number"
	case "max":
		message = fmt.Sprintf("must be less than or equal to %s", f.param)
	case "oneof":
		message = fmt.Sprintf("must be one of %s", f.param)
	case "gt":
		message = fmt.Sprintf("must be greater than %s", f.param)
	case "gte":
		message = fmt.Sprintf("must be greater than or equal to %s", f.param)
	case "ltfield":
		message = fmt.Sprintf("must be less than %s", f.param)
	case "lte":
		message = fmt.Sprintf("must be less than or equal to %s", f.param)
	}
	return fmt.Sprintf("%s %s", f.name, message)
}
