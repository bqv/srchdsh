package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"

	"gopkg.in/yaml.v3"

	"srchdsh/search"
)

// Top-level configuration structure.
//
// The fields of this struct are setup to be unmarshaled to by a YAML parser,
// usually from $XDG_CONFIG_HOME/srchdsh/config.yaml.
type config struct {
	// Specifies a list of rules to rewrite domain names in results.
	//
	// This will eventually provide more functionality, but works for my
	// uses right now.
	Rewrite []struct {
		// Regular expression that matches against the link of a search
		// result.
		Regexp string `yaml:"find"`

		// Matches an exact hostname.
		Hostname string `yaml:"hostname"`

		// Replace the affected part with this value.
		//
		// Using an empty string will outright delete the search
		// result. (This specifically is deprecated and will be removed
		// soon.)
		ReplaceWith string `yaml:"replace"`

		r *regexp.Regexp
	}

	// Specifies a list of file paths containing uBlacklist blocklists.
	// All file paths are relative to the configuration file directory.
	//
	// Currently, srchdsh supports match patterns and Go syntax regular
	// expressions; this is enough to cover most use cases.
	//
	// This will be used to configure a blacklist that is used to filter
	// out search results.
	Blacklists []string

	// Specifies the default HTTP proxy.
	// Overrides the HTTP_PROXY environment variable, but can be overridden
	// by an engine's http_proxy setting.
	//
	// The special value "-" will cause srchdsh to behave as if HTTP_PROXY
	// is not set.
	//
	// By default, this is blank and as such HTTP_PROXY will be used if it
	// is set.
	HttpProxy string `yaml:"http_proxy"`

	// Specifies configuration settings for engines supported by srchdsh.
	// The key of an engine specifies the name of the engine, and the value
	// is the configuration of that engine.
	//
	// Engines that are listed here, are not enabled by default, and are
	// not explicitly disabled will be implicitly enabled by having a
	// configuration.
	Engines map[string]search.Config `yaml:"engines"`

	// A list of engine names that should be explicitly disabled.
	//
	// Engines listed here will never be used at any point by srchdsh, even
	// if requested by a client.
	Disabled []string `yaml:"disabled"`
}

// Default configuration.
var defaultConfig = config{
	Engines: map[string]search.Config{},
}

var cfg = defaultConfig

func loadConfig(path string) error {
	h, err := os.Open(path)
	if err != nil {
		return err
	}
	defer h.Close()

	if err := yaml.NewDecoder(h).Decode(&cfg); err != nil {
		return err
	}

	// Load all of the regexp rules
	for i, v := range cfg.Rewrite {
		if v.Regexp != "" && v.Hostname != "" {
			return fmt.Errorf("regexp and hostname defined in rule")
		}

		if v.Hostname == "" {
			v.r = regexp.MustCompile(v.Regexp)
			cfg.Rewrite[i] = v
		}

		// Notice for the empty ReplaceWith string being a deprecated
		// method for blocking websites
		if v.ReplaceWith == "" {
			rule := ""
			if v.Regexp != "" {
				rule = v.Regexp
			} else {
				rule = v.Hostname
			}

			log.Printf(`rewrite rule for %q: replace = "" is deprecated; use blacklists instead`, rule)
		}
	}

	// Rewrite all configuration file paths
	configFileAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path to configuration file: %w", err)
	}

	configDir := filepath.Dir(configFileAbs)

	for i, v := range cfg.Blacklists {
		if filepath.IsAbs(v) {
			// Already absolute.
			continue
		}

		cfg.Blacklists[i] = filepath.Join(configDir, v)
	}

	return nil
}

// Attempt to rewrite a URL.
//
// Stops on the first rule that matches the URL.
func rewriteUrl(in string) string {
	var parsedUrl *url.URL
	var err error
	for _, v := range cfg.Rewrite {
		if v.r != nil {
			// v.r != nil when v.Hostname == ""

			if v.r.MatchString(in) {
				if v.ReplaceWith == "" {
					// Return nothing, which will cause the
					// result to be removed
					return ""
				}

				return v.r.ReplaceAllString(in, v.ReplaceWith)
			}
		} else if err == nil { // v.Hostname != ""
			// Note that err == nil is checked because we lazily
			// initialize the URL, and we get err for free if we
			// failed to parse the URL.

			// Lazily parse the URL.
			if parsedUrl == nil {
				parsedUrl, err = url.Parse(in)
				if err != nil {
					continue
				}
			}

			// Swap out the hostname.
			if parsedUrl.Host == v.Hostname {
				if v.ReplaceWith == "" {
					// Return nothing, which will cause the
					// result to be removed
					return ""
				}

				parsedUrl.Host = v.ReplaceWith
				return parsedUrl.String()
			}
		}
	}

	return in
}

// Attempts to initialize an engine.
//
// Uses the engine's configuration as specified in the configuration, and also
// merges in the default config.
func initializeEngine(name string) (search.Engine, error) {
	engCfg, ok := cfg.Engines[name]
	if !ok {
		// The map returns a zero-struct in this case so this is safe.
		engCfg.Type = name
	}
	engCfg.Name = name

	// Set the HTTP proxy.
	//
	// TODO: A global config would be nice.
	// An older version of config.go did have a "default" engine that was
	// sorta hacked in after the fact and when I redid the configuration
	// system it was left out because I didn't have much of a use for it.
	if engCfg.HttpProxy == "" {
		engCfg.HttpProxy = cfg.HttpProxy
	}

	return engCfg.New()
}

// Determines if a specific engine has been disabled.
//
// An engine is disabled if it is explicitly disabled, or if it has no
// configuration and is not an engine enabled by default.
func engineIsDisabled(name string) bool {
	// Check if it is explicitly disabled first.
	if slices.Contains(cfg.Disabled, name) {
		// Engine was disabled.
		return true
	}

	// Not explicitly disabled.
	// Check to see if it was configured.
	_, ok := cfg.Engines[name]
	if ok {
		// Was configured.
		return false
	}

	// Finally, check to see if it is supposed to be enabled by default.
	if slices.Contains(search.DefaultEngines(), name) {
		// Enabled by default.
		return false
	}

	// All of the above checks failed, so it is disabled.
	return true
}

// Returns a list of enabled engines.
//
// An enabled engine is one that is not explicitly disabled and is either set
// to be a "default" engine or has been configured.
var enabledEngines = sync.OnceValue(func() []string {
	engines := make([]string, 0)

	// Copy in all default engines provided they aren't disabled.
	for _, v := range search.DefaultEngines() {
		if !engineIsDisabled(v) {
			engines = append(engines, v)
		}
	}

	// Copy in everything that was explicitly configured.
	for k := range cfg.Engines {
		if !engineIsDisabled(k) && !slices.Contains(engines, k) {
			engines = append(engines, k)
		}
	}

	return engines
})
