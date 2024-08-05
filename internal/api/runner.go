package api

//go:generate go run golang.org/x/tools/cmd/stringer -type=Runner
type Runner int

const (
	Rspec Runner = iota
	Jest
)

func (r Runner) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}
