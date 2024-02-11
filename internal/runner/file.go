package runner

import (
	"encoding/json"
	"os"
)

// readJsonFile reads a json file and unmarshals it into v.
// v must be a pointer to a struct.
//
// https://golang.org/pkg/encoding/json/#Unmarshal
func readJsonFile(filename string, v any) error {
	content, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	return json.Unmarshal(content, v)
}
