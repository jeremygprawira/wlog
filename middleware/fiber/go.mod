module github.com/jeremygprawira/wlog/middleware/fiber

go 1.23.0

require (
	github.com/gofiber/fiber/v2 v2.20.0
	github.com/jeremygprawira/wlog v0.8.0
	github.com/jeremygprawira/wlog/middleware/fasthttp v0.8.0
	github.com/valyala/fasthttp v1.63.0
)

require (
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
)

replace (
	github.com/jeremygprawira/wlog => ../..
	github.com/jeremygprawira/wlog/middleware/fasthttp => ../fasthttp
)
