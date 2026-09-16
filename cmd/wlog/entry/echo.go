package entry

// Package paths of the Echo adapters this module recognizes without importing them.
const (
	echo4Path = "github.com/labstack/echo/v4"
	echo5Path = "github.com/labstack/echo/v5"
)

// echoRegistrations covers Echo v4 and v5. The registration name is the HTTP method,
// and Handle takes the method as its first argument.
func echoRegistrations() []registration {
	var all []registration
	all = append(all, httpMethodRegistrations(echo4Path)...)
	all = append(all, httpMethodRegistrations(echo5Path)...)
	all = append(all,
		registration{pkgPath: echo4Path, name: "Handle", handle: true},
		registration{pkgPath: echo5Path, name: "Handle", handle: true},
	)
	return all
}
