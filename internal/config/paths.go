package config

import (
	"errors"
	"fmt"
	"path/filepath"
)

const appName = "cronen"

// Paths are the files and directories cronen reads and writes.
type Paths struct {
	// Config is the TOML config file. It is only ever read.
	Config string
	// State is the JSON state file.
	State string
	// CacheDir holds cached HTTP responses.
	CacheDir string
}

// ResolvePaths resolves Paths from the XDG base directory variables,
// falling back to the spec's defaults under $HOME. getenv is usually
// os.Getenv. Per the spec, relative XDG_* values are ignored.
func ResolvePaths(getenv func(string) string) (Paths, error) {
	home := getenv("HOME")
	dir := func(env, fallback string) (string, error) {
		if v := getenv(env); filepath.IsAbs(v) {
			return filepath.Join(v, appName), nil
		}
		if !filepath.IsAbs(home) {
			return "", fmt.Errorf("can't resolve $%s: $HOME is unset or not absolute", env)
		}
		return filepath.Join(home, fallback, appName), nil
	}

	configDir, errConfig := dir("XDG_CONFIG_HOME", ".config")
	stateDir, errState := dir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	cacheDir, errCache := dir("XDG_CACHE_HOME", ".cache")
	if err := errors.Join(errConfig, errState, errCache); err != nil {
		return Paths{}, err
	}
	return Paths{
		Config:   filepath.Join(configDir, "config.toml"),
		State:    filepath.Join(stateDir, "state.json"),
		CacheDir: cacheDir,
	}, nil
}
