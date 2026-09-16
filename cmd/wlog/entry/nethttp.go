package entry

// Framework package paths that carry no dependency into this module.
const (
	netHTTPPath = "net/http"
	muxPath     = "github.com/gorilla/mux"
)

// registrations returns every known registration, framework by framework.
func registrations() []registration {
	return netHTTPRegistrations()
}

// netHTTPRegistrations covers net/http's ServeMux and gorilla/mux. Both take a route
// pattern first and a handler last. A pattern may start with an HTTP method, which
// routeFor splits off.
func netHTTPRegistrations() []registration {
	return []registration{
		{pkgPath: netHTTPPath, name: "HandleFunc", pathArg: 0},
		{pkgPath: netHTTPPath, name: "Handle", pathArg: 0},
		{pkgPath: muxPath, name: "HandleFunc", pathArg: 0},
		{pkgPath: muxPath, name: "Handle", pathArg: 0},
	}
}
