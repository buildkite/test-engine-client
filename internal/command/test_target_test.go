package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/runner"
	"github.com/google/go-cmp/cmp"
)

type targetDiscoveryTestRunner struct {
	runner.Jest
	targets         []string
	discoveryCalls  int
	selectorSupport bool
}

func (r *targetDiscoveryTestRunner) DiscoverTestTargets() ([]string, error) {
	r.discoveryCalls++
	return r.targets, nil
}

func (r *targetDiscoveryTestRunner) SupportedFeatures() runner.SupportedFeatures {
	return runner.SupportedFeatures{SplitBySelector: r.selectorSupport}
}

func TestGetTestTargets(t *testing.T) {
	tempDir := t.TempDir()
	selectorList := filepath.Join(tempDir, "selectors.txt")
	testFileList := filepath.Join(tempDir, "files.txt")
	if err := os.WriteFile(selectorList, []byte("selector-a\nselector-b\n"), 0o600); err != nil {
		t.Fatalf("write selector list: %v", err)
	}
	if err := os.WriteFile(testFileList, []byte("file-a\nfile-b\n"), 0o600); err != nil {
		t.Fatalf("write test file list: %v", err)
	}

	tests := []struct {
		name             string
		selectorSupport  bool
		selectorListPath string
		testFileList     string
		want             []string
		wantDiscoveries  int
	}{
		{
			name:             "supported runner uses selector list",
			selectorSupport:  true,
			selectorListPath: selectorList,
			testFileList:     testFileList,
			want:             []string{"selector-a", "selector-b"},
		},
		{
			name:            "supported runner uses file list when selector list is absent",
			selectorSupport: true,
			testFileList:    testFileList,
			want:            []string{"file-a", "file-b"},
		},
		{
			name:            "supported runner discovers targets when lists are absent",
			selectorSupport: true,
			want:            []string{"discovered-a", "discovered-b"},
			wantDiscoveries: 1,
		},
		{
			name:             "unsupported runner retains file list precedence",
			selectorListPath: selectorList,
			testFileList:     testFileList,
			want:             []string{"file-a", "file-b"},
		},
		{
			name:         "file mode uses test file list",
			testFileList: testFileList,
			want:         []string{"file-a", "file-b"},
		},
		{
			name:            "file mode discovers targets when test file list is absent",
			want:            []string{"discovered-a", "discovered-b"},
			wantDiscoveries: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testRunner := &targetDiscoveryTestRunner{
				targets:         []string{"discovered-a", "discovered-b"},
				selectorSupport: test.selectorSupport,
			}
			cfg := config.Config{SelectorListPath: test.selectorListPath}

			got, err := getTestTargets(&cfg, testRunner, test.testFileList)
			if err != nil {
				t.Fatalf("getTestTargets() error = %v", err)
			}
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("getTestTargets() diff (-got +want):\n%s", diff)
			}
			if testRunner.discoveryCalls != test.wantDiscoveries {
				t.Errorf("DiscoverTestTargets() calls = %d, want %d", testRunner.discoveryCalls, test.wantDiscoveries)
			}
		})
	}
}
