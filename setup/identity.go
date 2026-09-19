// This file holds the service identity that setup reads: the environment first, and the
// build info of the running binary second.
package setup

import "runtime/debug"

// identity is the service identity that setup applies to a Logger.
type identity struct {
	name    string
	version string
	env     string
}

// readIdentity reads the service name, version, and environment from env, and fills what
// env leaves empty from the build info of the binary.
func readIdentity(env Env) identity {
	id := identity{
		name:    firstOr(env, "", "WLOG_SERVICE", "OTEL_SERVICE_NAME", "SERVICE_NAME"),
		version: firstOr(env, "", "WLOG_VERSION", "APP_VERSION", "SERVICE_VERSION"),
		env:     firstOr(env, "", "WLOG_ENV", "APP_ENV", "ENVIRONMENT"),
	}
	if id.name == "" || id.version == "" {
		name, version := buildInfo()
		if id.name == "" {
			id.name = name
		}
		if id.version == "" {
			id.version = version
		}
	}
	return id
}

// firstOr returns the first set value of the names, and the fallback when none is set.
func firstOr(env Env, fallback string, names ...string) string {
	if value, ok := first(env, names...); ok {
		return value
	}
	return fallback
}

// buildInfo reads the module path and the source revision of the running binary. The
// revision keeps its first 12 characters, which is what a reader compares by.
func buildInfo() (name, version string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && len(setting.Value) >= 12 {
			version = setting.Value[:12]
		}
	}
	return info.Main.Path, version
}
