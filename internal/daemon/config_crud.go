package daemon

import (
	"context"
	"fmt"
	"os"

	"github.com/shadow-code/shadow-mcp/internal/config"
)

// mutateRaw is the single write path every CRUD operation goes through: it
// clones the current raw (pre-interpolation) config, lets mutate edit the
// clone, validates the result, persists it to disk, and reloads - so the
// file on disk is always the actual source of truth after a successful
// call, exactly like a hand-edit-then-/reload would produce, and downstream
// connections/catalog/rule engine all stay consistent with what's on disk.
//
// If reload fails after the file was already written (e.g. the edit is
// structurally valid but points at a downstream server that won't actually
// connect), the previous file content is restored before returning the
// error. Without this, a single bad edit would persist to disk despite the
// API reporting failure, and - since every subsequent CRUD call also ends
// with a full reload - would keep failing every future edit too, not just
// ones touching the broken entry.
func (d *Daemon) mutateRaw(ctx context.Context, mutate func(*config.Config) error) error {
	d.mu.RLock()
	clone, err := d.rawCfg.Clone()
	d.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("cloning config: %w", err)
	}

	previous, err := os.ReadFile(d.configPath)
	if err != nil {
		return fmt.Errorf("snapshotting config before edit: %w", err)
	}

	if err := mutate(clone); err != nil {
		return err
	}
	if err := config.Validate(clone); err != nil {
		return err
	}
	if err := config.Save(d.configPath, clone); err != nil {
		return err
	}

	if err := d.reload(ctx); err != nil {
		if restoreErr := os.WriteFile(d.configPath, previous, 0o600); restoreErr == nil {
			_ = d.reload(ctx) // best-effort resync back to the known-good state; a failure here leaves the previous reload's state in place
		}
		return err
	}
	return nil
}

// RawServer returns the raw (pre-interpolation) DownstreamServer named name,
// for pre-filling an edit form with e.g. its literal "${VAR}" env values
// rather than a resolved secret or a redacted placeholder.
func (d *Daemon) RawServer(name string) (config.DownstreamServer, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, s := range d.rawCfg.DownstreamServers {
		if s.Name == name {
			return s, true
		}
	}
	return config.DownstreamServer{}, false
}

// RawProfile returns the raw Profile named name.
func (d *Daemon) RawProfile(name string) (config.Profile, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, p := range d.rawCfg.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return config.Profile{}, false
}

// RawRule returns the raw Rule named name.
func (d *Daemon) RawRule(name string) (config.Rule, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, r := range d.rawCfg.Rules {
		if r.Name == name {
			return r, true
		}
	}
	return config.Rule{}, false
}

// UpsertServer creates a new downstream server (originalName == "") or
// replaces the one currently named originalName (which may itself rename it,
// since s.Name is what's actually written). Fails if s.Name collides with a
// different existing entry.
func (d *Daemon) UpsertServer(ctx context.Context, originalName string, s config.DownstreamServer) error {
	return d.mutateRaw(ctx, func(cfg *config.Config) error {
		idx := -1
		for i, existing := range cfg.DownstreamServers {
			if originalName != "" && existing.Name == originalName {
				idx = i
			}
		}
		for i, existing := range cfg.DownstreamServers {
			if i != idx && existing.Name == s.Name {
				return fmt.Errorf("a downstream server named %q already exists", s.Name)
			}
		}
		if idx >= 0 {
			cfg.DownstreamServers[idx] = s
		} else {
			cfg.DownstreamServers = append(cfg.DownstreamServers, s)
		}
		return nil
	})
}

// DeleteServer removes the downstream server named name.
func (d *Daemon) DeleteServer(ctx context.Context, name string) error {
	return d.mutateRaw(ctx, func(cfg *config.Config) error {
		out := cfg.DownstreamServers[:0]
		found := false
		for _, s := range cfg.DownstreamServers {
			if s.Name == name {
				found = true
				continue
			}
			out = append(out, s)
		}
		if !found {
			return fmt.Errorf("no downstream server named %q", name)
		}
		cfg.DownstreamServers = out
		return nil
	})
}

// UpsertProfile creates a new profile (originalName == "") or replaces the
// one currently named originalName.
func (d *Daemon) UpsertProfile(ctx context.Context, originalName string, p config.Profile) error {
	return d.mutateRaw(ctx, func(cfg *config.Config) error {
		idx := -1
		for i, existing := range cfg.Profiles {
			if originalName != "" && existing.Name == originalName {
				idx = i
			}
		}
		for i, existing := range cfg.Profiles {
			if i != idx && existing.Name == p.Name {
				return fmt.Errorf("a profile named %q already exists", p.Name)
			}
		}
		if idx >= 0 {
			cfg.Profiles[idx] = p
		} else {
			cfg.Profiles = append(cfg.Profiles, p)
		}
		return nil
	})
}

// DeleteProfile removes the profile named name.
func (d *Daemon) DeleteProfile(ctx context.Context, name string) error {
	return d.mutateRaw(ctx, func(cfg *config.Config) error {
		out := cfg.Profiles[:0]
		found := false
		for _, p := range cfg.Profiles {
			if p.Name == name {
				found = true
				continue
			}
			out = append(out, p)
		}
		if !found {
			return fmt.Errorf("no profile named %q", name)
		}
		cfg.Profiles = out
		return nil
	})
}

// UpsertRule creates a new rule (originalName == "") or replaces the one
// currently named originalName.
func (d *Daemon) UpsertRule(ctx context.Context, originalName string, r config.Rule) error {
	return d.mutateRaw(ctx, func(cfg *config.Config) error {
		idx := -1
		for i, existing := range cfg.Rules {
			if originalName != "" && existing.Name == originalName {
				idx = i
			}
		}
		for i, existing := range cfg.Rules {
			if i != idx && existing.Name == r.Name {
				return fmt.Errorf("a rule named %q already exists", r.Name)
			}
		}
		if idx >= 0 {
			cfg.Rules[idx] = r
		} else {
			cfg.Rules = append(cfg.Rules, r)
		}
		return nil
	})
}

// DeleteRule removes the rule named name.
func (d *Daemon) DeleteRule(ctx context.Context, name string) error {
	return d.mutateRaw(ctx, func(cfg *config.Config) error {
		out := cfg.Rules[:0]
		found := false
		for _, r := range cfg.Rules {
			if r.Name == name {
				found = true
				continue
			}
			out = append(out, r)
		}
		if !found {
			return fmt.Errorf("no rule named %q", name)
		}
		cfg.Rules = out
		return nil
	})
}
