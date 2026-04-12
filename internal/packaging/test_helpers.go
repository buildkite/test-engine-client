package packaging

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"strings"
	"testing"
)

// ReadTarball opens a tar.gz file and returns a map of entry name to content.
// Intended for use in tests across packages.
func ReadTarball(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening tarball: %v", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("creating gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	files := make(map[string]string)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading tar entry: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("reading tar content for %s: %v", hdr.Name, err)
		}
		files[hdr.Name] = string(data)
	}
	return files
}

// FindTarEntry returns the content of the first entry whose name ends with
// the given suffix. Fails the test if no match is found.
func FindTarEntry(t *testing.T, files map[string]string, suffix string) string {
	t.Helper()
	for name, content := range files {
		if strings.HasSuffix(name, suffix) {
			return content
		}
	}
	t.Fatalf("no tar entry ending with %q", suffix)
	return ""
}

// HasTarEntry returns true if any entry name ends with the given suffix.
func HasTarEntry(files map[string]string, suffix string) bool {
	for name := range files {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
