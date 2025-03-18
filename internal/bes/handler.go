package bes

import (
	"context"

	"google.golang.org/genproto/googleapis/devtools/build/v1"
)

// BuildEventHandler in bktec mimics the bb-portal handler so that the
// BuildEventServer.PublishBuildToolEventStream code can be used verbatim.
//
// BuildEventHandler orchestrates the handling of incoming Build Event streams.
// For each incoming stream, and BuildEventChannel is created, which handles that stream.
// BuildEventHandler is responsible for managing the things that are common to these event streams.
type BuildEventHandler struct {
	filenames chan<- string
}

// NewBuildEventHandler constructs a new BuildEventHandler
func NewBuildEventHandler(filenames chan<- string) *BuildEventHandler {
	return &BuildEventHandler{
		filenames: filenames,
	}
}

// CreateEventChannel creates a new BuildEventChannel
func (h *BuildEventHandler) CreateEventChannel(ctx context.Context, initialEvent *build.OrderedBuildEvent) BuildEventChannel {
	// If the first event does not have sequence number 1, we have processed this
	// invocation previously, and should skip all processing.
	if initialEvent.SequenceNumber != 1 {
		return &noOpBuildEventChannel{}
	}

	return &buildEventChannel{
		ctx:       ctx,
		streamID:  initialEvent.StreamId,
		filenames: h.filenames,
	}
}
