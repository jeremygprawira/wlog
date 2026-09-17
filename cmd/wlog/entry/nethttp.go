package entry

// Framework package paths that carry no dependency into this module.
const (
	NetHTTPPath = "net/http"
	MuxPath     = "github.com/gorilla/mux"
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
//
// handlerArg is the argument that holds the handler: Gin takes its handlers last, with any
// middleware in front, while Echo takes the handler first and its per-route middleware after.
func httpMethodRegistrations(pkgPath string, handlerArg int) []registration {
	all := make([]registration, 0, len(httpMethods))
	for _, name := range httpMethods {
		method := name
		if name == "Any" {
			method = ""
		}
		all = append(all, registration{pkgPath: pkgPath, name: name, method: method, pathArg: 0, handlerArg: handlerArg})
	}
	return all
}

// netHTTPRegistrations covers net/http's ServeMux and gorilla/mux. Both take a route
// pattern first and a handler last. A pattern may start with an HTTP method, which
// routeFor splits off.
func netHTTPRegistrations() []registration {
	return []registration{
		{pkgPath: NetHTTPPath, name: "HandleFunc", pathArg: 0},
		{pkgPath: NetHTTPPath, name: "Handle", pathArg: 0},
		{pkgPath: MuxPath, name: "HandleFunc", pathArg: 0},
		{pkgPath: MuxPath, name: "Handle", pathArg: 0},
		// A mux route built as r.Methods("GET").Path("/orders").HandlerFunc(h) registers the
		// handler here, and the chain carries the method and the path.
		{pkgPath: MuxPath, name: "HandlerFunc", pathArg: 0},
	}
}
