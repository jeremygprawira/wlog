package entry

// Package paths of the Echo adapters this module recognizes without importing them.
const (
	Echo4Path = "github.com/labstack/echo/v4"
	Echo5Path = "github.com/labstack/echo/v5"
)

// echoRegistrations covers Echo v4 and v5. The registration name is the HTTP method,
// and Handle takes the method as its first argument.
func echoRegistrations() []registration {
	var all []registration
	all = append(all, httpMethodRegistrations(Echo4Path)...)
	all = append(all, httpMethodRegistrations(Echo5Path)...)
	all = append(all,
		registration{pkgPath: Echo4Path, name: "Handle", handle: true},
		registration{pkgPath: Echo5Path, name: "Handle", handle: true},
		// Add takes the method first, like Handle. Match takes a list of methods, so the
		// method is left unset and the path is read from the second argument.
		registration{pkgPath: Echo4Path, name: "Add", handle: true},
		registration{pkgPath: Echo5Path, name: "Add", handle: true},
		registration{pkgPath: Echo4Path, name: "Match", pathArg: 1},
		registration{pkgPath: Echo5Path, name: "Match", pathArg: 1},
	)
	return all
}
