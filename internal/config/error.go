package config

import (
	"fmt"
	"sort"
	"strings"
)

// InvalidConfigError is an error that contains a list of all invalid fields in the config.
type InvalidConfigError []invalidFieldError

func (i InvalidConfigError) Error() string {
	var errs []string
	for _, err := range i {
		errs = append(errs, err.Error())
	}
	sort.Strings(errs)
	return strings.Join(errs, ",\n")
}

func (e *InvalidConfigError) appendFieldError(field, format string, v ...any) {
	*e = append(*e, invalidFieldError{
		name: field,
		err:  fmt.Sprintf(format, v...),
	})
}

// invalidFieldError is the detailed error of an invalid rule for a field in the config.
type invalidFieldError struct {
	// name is the name of the field.
	name string
	// err is the error message.
	err string
}

func (f invalidFieldError) Error() string {
	return fmt.Sprintf("%s %s", f.name, f.err)
}
