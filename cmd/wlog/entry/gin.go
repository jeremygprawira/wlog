package entry

// GinPath is the Gin package path this module recognizes without importing it.
const GinPath = "github.com/gin-gonic/gin"

// ginRegistrations covers Gin's Engine and RouterGroup. The registration name is the
// HTTP method, and Handle takes the method as its first argument.
func ginRegistrations() []registration {
	all := httpMethodRegistrations(GinPath)
	return append(all, registration{pkgPath: GinPath, name: "Handle", handle: true})
}
