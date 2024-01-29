package util

import (
	"encoding/json"
	"os"
)

// ReadJsonFile reads a json file and unmarshals it into v.
// v must be a pointer to a struct
//
// https://golang.org/pkg/encoding/json/#Unmarshal
func ReadJsonFile(filename string, v any) error {
	content, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	return json.Unmarshal(content, v)
}
