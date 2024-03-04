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
	return strings.Join(errs, ",\n")
}

// InvalidFieldError is the detailed error of an invalid rule for a field in the config.
type InvalidFieldError struct {
	// name is the name of the field.
	name string
	// err is the error message.
	err string
}

func (f InvalidFieldError) Error() string {
	return fmt.Sprintf("%s %s", f.name, f.err)
}
