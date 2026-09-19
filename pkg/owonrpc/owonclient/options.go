package owonclient

import (
	"github.com/xaionaro-go/owon/pkg/owonrpc"
	"google.golang.org/grpc"
)

// GRPCClientDialOptions returns matching default call limits for every RPC.
//
// Example: owonctl appends transport credentials before grpc.NewClient.
func GRPCClientDialOptions() []grpc.DialOption {
	return []grpc.DialOption{grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(owonrpc.MaximumGRPCMessageBytes),
		grpc.MaxCallSendMsgSize(owonrpc.MaximumGRPCMessageBytes),
	)}
}
