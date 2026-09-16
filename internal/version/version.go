// Package version holds wlog's own version string, used by every HTTP-based
// drain in its identity headers, such as User-Agent: wlog/<version>.
package version

// Version is wlog's release version.
//
// The release flow sets it at build time, so a tag and the code cannot drift
// apart:
//
//	go build -ldflags "-X github.com/jeremygprawira/wlog/internal/version.Version=v0.5.0"
//
// A build from source keeps the fallback, so a development build never claims a
// release. Tests compare against this value and never against a literal.
var Version = "dev"

// UserAgent returns the identity that wlog sends to a backend, as in "wlog/dev".
func UserAgent() string { return "wlog/" + Version }
