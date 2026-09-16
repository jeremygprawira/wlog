package entry

// Framework package paths that carry no dependency into this module.
const (
	netHTTPPath = "net/http"
	muxPath     = "github.com/gorilla/mux"
)

// registrations returns every known registration, framework by framework.
func registrations() []registration {
	var all []registration
	all = append(all, netHTTPRegistrations()...)
	all = append(all, echoRegistrations()...)
	all = append(all, ginRegistrations()...)
	return all
}

// httpMethods lists the registration names that are themselves the HTTP method. Any
// registers a route for every method. Echo and Gin share the set.
var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "Any"}

// httpMethodRegistrations returns one registration per HTTP verb for one framework.
func httpMethodRegistrations(pkgPath string) []registration {
	all := make([]registration, 0, len(httpMethods))
	for _, name := range httpMethods {
		method := name
		if name == "Any" {
			method = ""
		}
		all = append(all, registration{pkgPath: pkgPath, name: name, method: method, pathArg: 0})
	}
	return all
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
