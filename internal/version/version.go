// Package version holds wlog's own version string, used by every HTTP-based drain in
// its identity headers (User-Agent: wlog/<version>).
package version

// Version is wlog's release version. Bumped by hand at each tag.
const Version = "0.1.0"
