// Command middleware_app is a fixture for the wlog map middleware-coverage test: main wraps the
// router that the server package builds, which is the common layout.
package main

import (
	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"

	"github.com/jeremygprawira/wlog/cmd/wlog/testdata/middleware_app/server"
)

func main() {
	logger := wlog.New()
	router := server.Router()
	_ = wlogstd.Middleware(logger)
	_ = router
}
