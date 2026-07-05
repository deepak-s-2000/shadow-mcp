package daemon

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
)

// Run starts a Daemon for configPath, binds a loopback admin+MCP listener on
// an OS-assigned port, publishes the connection info other shadow-mcp
// processes look for, and blocks until ctx is cancelled.
func Run(ctx context.Context, configPath string) error {
	d, err := New(ctx, configPath)
	if err != nil {
		return err
	}
	defer d.Close()

	token, err := GenerateToken()
	if err != nil {
		return err
	}

	addr := d.ConfiguredHTTPAddr()
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port

	if err := WriteInfo(Info{PID: os.Getpid(), Port: port, Token: token}); err != nil {
		return err
	}
	defer RemoveInfo()

	server := &http.Server{Handler: NewHandler(d, token)}
	go func() {
		<-ctx.Done()
		server.Close()
	}()

	log.Printf("shadow-mcp daemon listening on 127.0.0.1:%d (pid %d)", port, os.Getpid())
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
