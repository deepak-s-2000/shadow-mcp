package config

import "gopkg.in/yaml.v3"

// Clone returns a deep copy of c, via a YAML round-trip so nested
// slices/maps (Env, Headers, Allow/Deny, ...) don't share backing arrays
// with the original - callers that mutate a clone (the admin API's CRUD
// path) must never risk writing through to the daemon's live config.
func (c *Config) Clone() (*Config, error) {
	b, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	var clone Config
	if err := yaml.Unmarshal(b, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}
