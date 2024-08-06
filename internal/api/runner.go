package api

import "strings"

//go:generate go run golang.org/x/tools/cmd/stringer -type=Runner
type Runner int

const (
	Rspec Runner = iota
	Jest
)

func (r Runner) MarshalText() ([]byte, error) {
	return []byte(strings.ToLower(r.String())), nil
}
