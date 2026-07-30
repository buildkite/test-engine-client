// Package notest has source files but no tests. It exists so target discovery
// can be verified to exclude packages that contain zero tests.
package notest

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a + b
}
