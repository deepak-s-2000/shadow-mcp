package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shadow-code/shadow-mcp/internal/config"
)

// StatusResponse is the /status admin API response.
type StatusResponse struct {
	Uptime   string         `json:"uptime"`
	Servers  []ServerHealth `json:"servers"`
	Profiles []string       `json:"profiles"`
}

// ServerHealth reports one downstream server's reachability.
type ServerHealth struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "up" or "down: <reason>"
}

func (d *Daemon) statusResponse(ctx context.Context) StatusResponse {
	d.mu.RLock()
	dm := d.dm
	cfg := d.cfg
	d.mu.RUnlock()

	var servers []ServerHealth
	for _, name := range dm.ServerNames() {
		status := "down: not connected"
		if _, ok := dm.Session(name); ok {
			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			if err := dm.Ping(pingCtx, name); err != nil {
				status = "down: " + err.Error()
			} else {
				status = "up"
			}
			cancel()
		}
		servers = append(servers, ServerHealth{Name: name, Status: status})
	}

	var profiles []string
	for _, p := range cfg.Profiles {
		profiles = append(profiles, p.Name)
	}

	return StatusResponse{Uptime: time.Since(d.startedAt).String(), Servers: servers, Profiles: profiles}
}

// NewHandler builds the daemon's whole loopback HTTP surface: the admin API
// (/status, /calls/recent, /reload) and the per-profile MCP relay endpoints
// (/mcp/<profile>, /mcp/_all). Every request must carry
// `Authorization: Bearer <token>` matching the per-run token in the daemon
// info file - this is local-machine-only tooling, so a shared bearer token
// over loopback HTTP is a simpler and sufficient alternative to a Unix
// socket/named pipe.
func NewHandler(d *Daemon, token string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, d.statusResponse(r.Context()))
	})

	mux.HandleFunc("GET /calls/recent", func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				limit = n
			}
		}
		writeJSON(w, d.stats.recent(limit))
	})

	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, d.configSnapshot())
	})

	mux.HandleFunc("POST /reload", func(w http.ResponseWriter, r *http.Request) {
		if err := d.Reload(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "reloaded"})
	})

	registerCRUD(mux, "servers", crudOps[config.DownstreamServer]{
		get:    d.RawServer,
		upsert: d.UpsertServer,
		delete: d.DeleteServer,
	})
	registerCRUD(mux, "profiles", crudOps[config.Profile]{
		get:    d.RawProfile,
		upsert: d.UpsertProfile,
		delete: d.DeleteProfile,
	})
	registerCRUD(mux, "rules", crudOps[config.Rule]{
		get:    d.RawRule,
		upsert: d.UpsertRule,
		delete: d.DeleteRule,
	})

	mux.Handle("/mcp/", mcpHandler(d))

	return authMiddleware(token, mux)
}

// crudOps is the daemon-side surface registerCRUD needs for one entity kind
// (server/profile/rule) - kept generic so the three near-identical route
// sets (GET/POST/PUT/DELETE under /config/<kind>) share one implementation.
type crudOps[T any] struct {
	get    func(name string) (T, bool)
	upsert func(ctx context.Context, originalName string, v T) error
	delete func(ctx context.Context, name string) error
}

// registerCRUD wires GET/PUT/DELETE /config/<kind>/{name} and POST
// /config/<kind> for one entity kind onto mux. Get returns the raw
// (pre-interpolation) entity, matching what the TUI's edit form should
// pre-fill with - see RawServer/RawProfile/RawRule.
func registerCRUD[T any](mux *http.ServeMux, kind string, ops crudOps[T]) {
	mux.HandleFunc("GET /config/"+kind+"/{name}", func(w http.ResponseWriter, r *http.Request) {
		v, ok := ops.get(r.PathValue("name"))
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, v)
	})

	mux.HandleFunc("POST /config/"+kind, func(w http.ResponseWriter, r *http.Request) {
		var v T
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := ops.upsert(r.Context(), "", v); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, v)
	})

	mux.HandleFunc("PUT /config/"+kind+"/{name}", func(w http.ResponseWriter, r *http.Request) {
		var v T
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := ops.upsert(r.Context(), r.PathValue("name"), v); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, v)
	})

	mux.HandleFunc("DELETE /config/"+kind+"/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := ops.delete(r.Context(), r.PathValue("name")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{"status": "deleted"})
	})
}

func mcpHandler(d *Daemon) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		server, err := d.ProfileServerForPath(r.URL.Path)
		if err != nil {
			// getServer has no error return; expose an empty server rather
			// than panicking so the client sees a valid but toolless session.
			return mcp.NewServer(&mcp.Implementation{Name: "shadow-mcp-unknown-profile"}, nil)
		}
		return server
	}, nil)
}

func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
