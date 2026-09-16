package entry

// ginPath is the Gin package path this module recognizes without importing it.
const ginPath = "github.com/gin-gonic/gin"

// ginRegistrations covers Gin's Engine and RouterGroup. The registration name is the
// HTTP method, and Handle takes the method as its first argument.
func ginRegistrations() []registration {
	all := httpMethodRegistrations(ginPath)
	return append(all, registration{pkgPath: ginPath, name: "Handle", handle: true})
}
