package bes

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/buildbarn/bb-portal/third_party/bazel/gen/bes"
	"google.golang.org/genproto/googleapis/devtools/build/v1"
)

// BuildEventChannel in bktec mimics the bb-portal interface so that the
// BuildEventServer.PublishBuildEventServer code can be used verbatim.
//
// BuildEventChannel handles a single BuildEvent stream
type BuildEventChannel interface {
	// HandleBuildEvent processes a single BuildEvent
	// This method should be called for each received event.
	HandleBuildEvent(event *build.BuildEvent) error

	// Finalize does post-processing of a stream of BuildEvents.
	// This method should be called after receiving the EOF event.
	Finalize() error
}

type buildEventChannel struct {
	ctx       context.Context
	streamID  *build.StreamId
	filenames chan<- string
}

// HandleBuildEvent implements BuildEventChannel.HandleBuildEvent.
func (c *buildEventChannel) HandleBuildEvent(event *build.BuildEvent) error {
	if event.GetBazelEvent() == nil {
		return nil
	}
	var bazelEvent bes.BuildEvent
	if err := event.GetBazelEvent().UnmarshalTo(&bazelEvent); err != nil {
		slog.ErrorContext(c.ctx, "UnmarshalTo failed", "err", err)
		return err
	}

	payload := bazelEvent.GetPayload()
	if testResult, ok := payload.(*bes.BuildEvent_TestResult); ok {
		r := testResult.TestResult
		files := []string{}
		for _, x := range r.GetTestActionOutput() {
			if x.GetName() == "test.xml" {
				path, err := pathFromURI(x.GetUri())
				if err != nil {
					return err // maybe just a log a warning?
				}
				files = append(files, path)
				c.filenames <- path
			}
		}
		slog.Info("TestResult",
			"status", r.GetStatus(),
			"cached", r.GetCachedLocally(),
			"dur", r.GetTestAttemptDuration().AsDuration().String(),
			"files", files,
		)
	}

	return nil
}

func pathFromURI(uri string) (string, error) {
	url, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if url.Scheme != "file" {
		return "", fmt.Errorf("expected file://..., got %v://...", url.Scheme)
	}
	return url.Path, nil
}

// Finalize implements BuildEventChannel.Finalize.
func (c *buildEventChannel) Finalize() error {
	// defer the ctx so its not reaped when the client closes the connection
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*24)
	defer cancel()

	slog.Info("finalizing build event channel")
	_ = ctx
	// TODO: finalize anything that needs finalizing?

	cancel()
	return nil
}

// noOpBuildEventChannel is an implementation of BuildEventChannel which does no processing of events.
// It is used when receiving a stream of events that we wish to ack without processing.
type noOpBuildEventChannel struct{}

// HandleBuildEvent implements BuildEventChannel.HandleBuildEvent.
func (c *noOpBuildEventChannel) HandleBuildEvent(event *build.BuildEvent) error {
	return nil
}

// Finalize implements BuildEventChannel.Finalize.
func (c *noOpBuildEventChannel) Finalize() error {
	return nil
}
