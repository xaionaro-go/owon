// Command owonweb serves a browser bridge to owond.
//
// Example: owonweb --grpc unix:///run/owond/owond.sock.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/owon/pkg/owonlog"
)

// main starts the web bridge and reports startup or runtime failures.
//
// Example: `owonweb --listen 127.0.0.1:8080` serves the embedded control client.
func main() {
	ctx, err := owonlog.WithContext(context.Background(), owonlog.Config{Level: logger.LevelInfo, Service: "owonweb"}, os.Stderr)
	if err != nil {
		panic(err)
	}
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		logger.Fatalf(ctx, "%v", err)
	}
}
