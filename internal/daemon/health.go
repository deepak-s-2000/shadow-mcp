package daemon

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	healthCheckInterval    = 30 * time.Second
	healthPingTimeout      = 5 * time.Second
	healthReconnectTimeout = 60 * time.Second // covers a slow cold start, e.g. a uvx server doing a fresh git clone + install
)

// healthLoop periodically pings every downstream server and transparently
// reconnects any whose transport died - an idle remote closing an SSE/HTTP
// connection, or the host sleeping and tearing down stdio pipes are both
// silent from the daemon's point of view until something tries to use the
// session. Without this, a dead server stays reported "down" (and unusable)
// until someone notices and triggers a manual reload. Runs until ctx is
// cancelled (see Daemon.Close).
func (d *Daemon) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.checkHealth()
		}
	}
}

func (d *Daemon) checkHealth() {
	d.mu.RLock()
	dm := d.dm
	d.mu.RUnlock()

	for _, name := range dm.ServerNames() {
		session, ok := dm.Session(name)
		if !ok {
			continue
		}

		pingCtx, cancel := context.WithTimeout(context.Background(), healthPingTimeout)
		err := session.Ping(pingCtx, nil)
		cancel()
		if err == nil {
			continue
		}
		if !errors.Is(err, mcp.ErrConnectionClosed) {
			continue // some other transient failure; don't reconnect on every kind of error
		}

		reconnectCtx, cancel := context.WithTimeout(context.Background(), healthReconnectTimeout)
		rErr := dm.Reconnect(reconnectCtx, name, session)
		cancel()
		if rErr != nil {
			log.Printf("shadow-mcp: background reconnect to downstream server %q failed: %v", name, rErr)
		} else {
			log.Printf("shadow-mcp: reconnected to downstream server %q after its connection dropped", name)
		}
	}
}
