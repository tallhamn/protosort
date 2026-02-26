package main

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config represents the .protosort.toml configuration file.
type Config struct {
	Ordering ConfigOrdering `toml:"ordering"`
	Verify   ConfigVerify   `toml:"verify"`
}

// ConfigOrdering holds ordering-related config.
type ConfigOrdering struct {
	SharedOrder        string `toml:"shared_order"`
	PreserveDividers   *bool  `toml:"preserve_dividers"`
	StripCommentedCode *bool  `toml:"strip_commented_code"`
}

// ConfigVerify holds verification-related config.
type ConfigVerify struct {
	Compiler   string   `toml:"compiler"`
	ProtoPaths []string `toml:"proto_paths"`
	SkipVerify *bool    `toml:"skip_verify"`
}

// findConfigFile walks up from the current directory to find .protosort.toml,
// stopping at the repository root (directory containing .git).
func findConfigFile() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		candidate := filepath.Join(dir, ".protosort.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		// Check if we're at a repo root
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return "" // reached repo root without finding config
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "" // reached filesystem root
		}
		dir = parent
	}
}

// LoadConfig reads and parses a .protosort.toml file.
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// MergeConfig applies config file values to opts, but only for fields not
// explicitly set via CLI flags. The setFlags map contains flag names that
// were explicitly passed on the command line.
func MergeConfig(opts *Options, cfg *Config, setFlags map[string]bool) {
	if cfg == nil {
		return
	}

	if cfg.Ordering.SharedOrder != "" && !setFlags["shared-order"] {
		opts.SharedOrder = cfg.Ordering.SharedOrder
	}
	if cfg.Ordering.PreserveDividers != nil && !setFlags["preserve-dividers"] {
		opts.PreserveDividers = *cfg.Ordering.PreserveDividers
	}
	if cfg.Ordering.StripCommentedCode != nil && !setFlags["strip-commented-code"] {
		opts.StripCommented = *cfg.Ordering.StripCommentedCode
	}

	if cfg.Verify.Compiler != "" && !setFlags["protoc"] {
		opts.ProtocPath = cfg.Verify.Compiler
	}
	if len(cfg.Verify.ProtoPaths) > 0 && !setFlags["proto-path"] {
		opts.ProtoPaths = cfg.Verify.ProtoPaths
	}
	if cfg.Verify.SkipVerify != nil && !setFlags["skip-verify"] {
		opts.SkipVerify = *cfg.Verify.SkipVerify
	}
}
