//go:build tools

// Package toolsdeps holds the modules that the map fixture apps import, without
// importing them from the CLI.
//
// The fixture apps under cmd/wlog/testdata use the gorilla/mux, echo, and gin
// routers, and the wlog middleware modules. go mod tidy reads this file with
// every build tag enabled, so those modules stay in the build list of cmd/wlog
// even though no CLI package imports them. The package itself never compiles
// into the binary, because the tools tag is off.
package toolsdeps

import (
	_ "github.com/gin-gonic/gin"
	_ "github.com/gorilla/mux"
	_ "github.com/jeremygprawira/wlog/middleware/echo"
	_ "github.com/jeremygprawira/wlog/middleware/echo5"
	_ "github.com/jeremygprawira/wlog/middleware/gin"
	_ "github.com/labstack/echo/v4"
	_ "github.com/labstack/echo/v5"
)
