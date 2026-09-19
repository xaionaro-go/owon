package owonserver

import (
	"github.com/xaionaro-go/owon/pkg/owonrpc"
	"google.golang.org/grpc"
)

// GRPCServerOptions returns the shared inbound and outbound message limits.
//
// Example: owond passes these options to grpc.NewServer before adding credentials.
func GRPCServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.MaxRecvMsgSize(owonrpc.MaximumGRPCMessageBytes),
		grpc.MaxSendMsgSize(owonrpc.MaximumGRPCMessageBytes),
		grpc.MaxConcurrentStreams(owonrpc.MaximumGRPCConcurrentStreams),
	}
}
