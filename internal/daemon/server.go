// Package daemon owns the persistent downstream connections, aggregated
// catalog, and rule engine shared by every stdio adapter, HTTP connection, and
// TUI session - the actual gateway work, extracted out of any one transport.
package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shadow-code/shadow-mcp/internal/aggregator"
	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/downstream"
	"github.com/shadow-code/shadow-mcp/internal/gateway"
	"github.com/shadow-code/shadow-mcp/internal/rules"
	"github.com/shadow-code/shadow-mcp/internal/rules/jsrule"
	"github.com/shadow-code/shadow-mcp/internal/rules/pyrule"
	"github.com/shadow-code/shadow-mcp/internal/usage"
)

// Daemon owns the state that used to be rebuilt per stdio invocation:
// downstream connections, the aggregated tool catalog, and the rule engine.
type Daemon struct {
	configPath string
	startedAt  time.Time
	stats      *statsRing
	usage      *usage.Store

	healthCancel context.CancelFunc
	healthDone   chan struct{}

	mu      sync.RWMutex
	cfg     *config.Config // interpolated - what's actually used to connect/dispatch
	rawCfg  *config.Config // pre-interpolation - what CRUD edits read/write, so a save never bakes a resolved secret into the file
	dm      *downstream.Manager
	catalog *aggregator.Catalog
	engine  *rules.Engine
}

// New loads configPath, connects to every downstream server, and builds the
// initial catalog and rule engine.
func New(ctx context.Context, configPath string) (*Daemon, error) {
	uPath, err := usagePath()
	if err != nil {
		return nil, err
	}
	usageStore, err := usage.Load(uPath)
	if err != nil {
		return nil, err
	}

	d := &Daemon{
		configPath: configPath,
		startedAt:  time.Now(),
		stats:      newStatsRing(200),
		usage:      usageStore,
	}
	if err := d.reload(ctx); err != nil {
		return nil, err
	}

	healthCtx, cancel := context.WithCancel(context.Background())
	d.healthCancel = cancel
	d.healthDone = make(chan struct{})
	go func() {
		defer close(d.healthDone)
		d.healthLoop(healthCtx)
	}()

	return d, nil
}

// record implements gateway.Recorder: it feeds the in-memory recent-calls
// ring buffer the TUI displays, and persists per-profile usage counts so
// lazy-loading profiles can rank tools by frequency after a restart.
func (d *Daemon) record(clientProfile, exposedName string, rulesFired []string, isError bool, err error) {
	d.stats.record(clientProfile, exposedName, rulesFired, isError, err)
	d.usage.Record(clientProfile, exposedName)
}

// Reload re-reads the config file, reconnects to every downstream server,
// rebuilds the catalog and rule engine, and swaps them in atomically. The
// previous downstream connections are closed only after the swap succeeds.
func (d *Daemon) Reload(ctx context.Context) error {
	return d.reload(ctx)
}

func (d *Daemon) reload(ctx context.Context) error {
	cfg, err := config.Load(d.configPath)
	if err != nil {
		return err
	}
	rawCfg, err := config.LoadRaw(d.configPath)
	if err != nil {
		return err
	}

	dm, err := downstream.NewManager(ctx, cfg.DownstreamServers)
	if err != nil {
		return err
	}

	catalog, err := aggregator.Build(ctx, dm, cfg.DownstreamServers)
	if err != nil {
		dm.Close()
		return err
	}

	engine := rules.NewEngine(cfg.Rules, map[string]rules.Runner{
		"js":     jsrule.NewRunner(),
		"python": pyrule.NewRunner(),
	})

	d.mu.Lock()
	old := d.dm
	d.cfg, d.rawCfg, d.dm, d.catalog, d.engine = cfg, rawCfg, dm, catalog, engine
	d.mu.Unlock()

	if old != nil {
		old.Close()
	}
	return nil
}

// ProfileServer builds a freshly-filtered mcp.Server for the named profile.
// An empty profileName returns everything, unfiltered.
func (d *Daemon) ProfileServer(profileName string) (*mcp.Server, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if profileName == "" {
		return gateway.BuildServer(d.catalog.Entries, d.dm, d.engine, "", d.record), nil
	}
	for i := range d.cfg.Profiles {
		if d.cfg.Profiles[i].Name == profileName {
			p := &d.cfg.Profiles[i]
			return gateway.BuildProfileServer(d.catalog, p, d.dm, d.engine, d.record, d.usage.Counts(p.Name)), nil
		}
	}
	return nil, fmt.Errorf("no profile named %q", profileName)
}

// PathForProfile returns the HTTP path a profile is mounted at: its
// configured identify.http_path, or "/mcp/<name>" by default. An empty
// profileName maps to the reserved unfiltered path "/mcp/_all".
func (d *Daemon) PathForProfile(profileName string) string {
	if profileName == "" {
		return "/mcp/_all"
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, p := range d.cfg.Profiles {
		if p.Name == profileName {
			if p.Identify.HTTPPath != "" {
				return p.Identify.HTTPPath
			}
			return "/mcp/" + p.Name
		}
	}
	return "/mcp/" + profileName
}

// ProfileServerForPath resolves whichever profile is mounted at an inbound
// HTTP path (see PathForProfile) and builds its server.
func (d *Daemon) ProfileServerForPath(path string) (*mcp.Server, error) {
	if path == "/mcp/_all" {
		return d.ProfileServer("")
	}

	d.mu.RLock()
	var match string
	for _, p := range d.cfg.Profiles {
		httpPath := p.Identify.HTTPPath
		if httpPath == "" {
			httpPath = "/mcp/" + p.Name
		}
		if httpPath == path {
			match = p.Name
			break
		}
	}
	d.mu.RUnlock()

	if match == "" {
		return nil, fmt.Errorf("no profile mounted at path %q", path)
	}
	return d.ProfileServer(match)
}

// ConfigSnapshot is what the TUI/admin API reads to display current
// servers/profiles/rules. It intentionally omits secrets like env var values.
type ConfigSnapshot struct {
	DownstreamServers []config.DownstreamServer `json:"downstream_servers"`
	Profiles          []config.Profile          `json:"profiles"`
	Rules             []config.Rule             `json:"rules"`
}

func (d *Daemon) configSnapshot() ConfigSnapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()

	servers := make([]config.DownstreamServer, len(d.cfg.DownstreamServers))
	for i, s := range d.cfg.DownstreamServers {
		s.Env = redactedValues(s.Env)
		s.Headers = redactedValues(s.Headers)
		servers[i] = s
	}

	return ConfigSnapshot{
		DownstreamServers: servers,
		Profiles:          d.cfg.Profiles,
		Rules:             d.cfg.Rules,
	}
}

// redactedValues returns a copy of m with every value replaced by "***" -
// downstream server env vars and HTTP headers may hold interpolated secrets
// (tokens, credentials) that shouldn't be exposed through the admin API even
// though it's local-machine-only.
func redactedValues(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k := range m {
		out[k] = "***"
	}
	return out
}

// ConfiguredHTTPAddr returns the configured http.addr, or "" if unset (in
// which case Run binds an OS-assigned loopback port).
func (d *Daemon) ConfiguredHTTPAddr() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg.HTTP.Addr
}

// Close stops the background health loop and closes every downstream
// connection.
func (d *Daemon) Close() error {
	if d.healthCancel != nil {
		d.healthCancel()
		<-d.healthDone
	}

	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.dm.Close()
}
