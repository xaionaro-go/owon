package owonrpc

const (
	// MaximumGRPCMessageBytes bounds both service and client serialized messages.
	//
	// Example: a sixteen MiB waveform plus metadata fits while a thirty-three MiB message fails.
	MaximumGRPCMessageBytes = 32 << 20
	// MaximumGRPCConcurrentStreams bounds HTTP/2 streams on one client connection.
	//
	// Example: a single connection cannot create an unbounded set of idle handlers.
	MaximumGRPCConcurrentStreams = 64
)
