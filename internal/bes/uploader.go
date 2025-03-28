package bes

import (
	"context"
	"log/slog"

	"github.com/buildkite/test-engine-client/internal/upload"
)

type Uploader struct {
	Config    upload.Config
	RunEnv    upload.RunEnvMap
	Format    string
	Filenames chan string
	Responses chan string
	Errs      chan error

	stopping bool
}

func NewUploader(cfg upload.Config, runEnv upload.RunEnvMap, format string) *Uploader {
	// a channel to pass filenames from BES server to uploader
	filenames := make(chan string, 1024)

	// a channel to receive response upload URLs
	responses := make(chan string)

	// a channel to receive errors from the uploader
	errs := make(chan error)

	return &Uploader{
		Config:    cfg,
		RunEnv:    runEnv,
		Format:    format,
		Filenames: filenames,
		Responses: responses,
		Errs:      errs,
	}
}

func (u *Uploader) Start(ctx context.Context) {
	for filename := range u.Filenames {
		if ctx.Err() != nil {
			slog.Debug("Uploader context canceled")
			break
		}
		resp, err := u.UploadFile(ctx, filename)
		if err != nil {
			u.Errs <- err
			continue
		}
		u.Responses <- resp["upload_url"]
	}
	slog.Debug("Uploader finished")
	close(u.Responses)
}

// Stop closes the Filenames channel; filenames already buffered on the channel
// will be uploaded before finishing.
func (u *Uploader) Stop() {
	if u.stopping {
		slog.Warn("Uploader GracefulStop: already stopping")
		return
	}
	slog.Debug("Uploader GracefulStop")
	u.stopping = true
	close(u.Filenames)
}

func (u *Uploader) UploadFile(ctx context.Context, filename string) (map[string]string, error) {
	resp, err := upload.Upload(ctx, u.Config, u.RunEnv, u.Format, filename)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
