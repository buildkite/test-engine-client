package config

import (
	"fmt"
	"sort"
	"strings"
)

// InvalidConfigError is an error that contains a list of all invalid fields in the config.
type InvalidConfigError map[string][]error

func (i InvalidConfigError) Error() string {
	var errs []string
	for field, value := range i {
		for _, v := range value {
			errs = append(errs, fmt.Sprintf("%s %s", field, v))
		}
	}
	sort.Strings(errs)
	return strings.Join(errs, "\n")
}

func (i InvalidConfigError) Unwrap() []error {
	errs := make([]error, 0, len(i))
	for field, value := range i {
		for _, v := range value {
			errs = append(errs, fmt.Errorf("%s %s", field, v))
		}
	}
	return errs
}

func (e InvalidConfigError) appendFieldError(field, format string, v ...any) {
	if e[field] == nil {
		e[field] = make([]error, 0)
	}
	e[field] = append(e[field], fmt.Errorf(format, v...))
}
