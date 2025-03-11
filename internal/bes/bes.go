package bes

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"

	bb_bes "github.com/buildbarn/bb-portal/third_party/bazel/gen/bes"

	"google.golang.org/genproto/googleapis/devtools/build/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"
)

var host = "127.0.0.1"
var port = 60242 // 0 for OS-allocated

type BuildEventServer struct {
}

func Listen() error {
	addr := fmt.Sprintf("%s:%d", host, port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	fmt.Println("Bazel BES listener: grpc://" + listener.Addr().String())

	opts := []grpc.ServerOption{}
	grpcServer := grpc.NewServer(opts...)

	build.RegisterPublishBuildEventServer(grpcServer, newServer())
	grpcServer.Serve(listener)

	return nil
}

func newServer() build.PublishBuildEventServer {
	return BuildEventServer{}
}

// PublishLifecycleEvent handles life cycle events.
func (s BuildEventServer) PublishLifecycleEvent(ctx context.Context, request *build.PublishLifecycleEventRequest) (*emptypb.Empty, error) {
	slog.DebugContext(ctx, "Received event", "event", protojson.Format(request.BuildEvent.GetEvent()))
	return &emptypb.Empty{}, nil
}

// PublishBuildToolEventStream handles a build tool event stream.
// bktec thanks buildbarn/bb-portal for the basis of this :D
func (s BuildEventServer) PublishBuildToolEventStream(stream build.PublishBuildEvent_PublishBuildToolEventStreamServer) error {
	ctx := stream.Context()

	slog.InfoContext(ctx, "Stream started")

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			slog.InfoContext(ctx, "Stream finished")
			return nil
		} else if err != nil {
			slog.ErrorContext(ctx, "Recv failed", "err", err)
			return err
		}

		streamID := req.OrderedBuildEvent.GetStreamId()
		seq := req.OrderedBuildEvent.GetSequenceNumber()

		event := req.GetOrderedBuildEvent().GetEvent()
		slog.DebugContext(ctx, "stream event", "seq", seq, "event", event)

		if event.GetBazelEvent() == nil {
			slog.DebugContext(ctx, "not a bazel event", seq, seq)
			continue
		}

		var bazelEvent bb_bes.BuildEvent
		if err = event.GetBazelEvent().UnmarshalTo(&bazelEvent); err != nil {
			//return fmt.Errorf("unmarshaling bazel event: %w", err)
			slog.InfoContext(ctx, "error unmarshalling")
		}

		// slog.InfoContext(ctx, "unmarshalled bazel event", "event", &bazelEvent)

		payload := bazelEvent.GetPayload()
		if testResult, ok := payload.(*bb_bes.BuildEvent_TestResult); ok {
			r := testResult.TestResult
			files := []string{}
			for _, x := range r.GetTestActionOutput() {
				if x.GetName() == "test.xml" {
					files = append(files, x.GetUri())
				}
			}
			slog.InfoContext(ctx, "TestResult",
				"status", r.GetStatus(),
				"cached", r.GetCachedLocally(),
				"dur", r.GetTestAttemptDuration().AsDuration().String(),
				"files", files,
			)
		}

		// ack
		ack := &build.PublishBuildToolEventStreamResponse{StreamId: streamID, SequenceNumber: seq}
		if err := stream.Send(ack); err != nil {
			grpcErr := status.Convert(err)
			if grpcErr.Code() == codes.Unavailable &&
				grpcErr.Message() == "transport is closing" {
				return nil
			}

			slog.ErrorContext(ctx, "ack failed",
				"err", err,
				"stream", streamID,
				"seq", seq,
			)
		}
	}
}
