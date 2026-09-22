module github.com/jeremygprawira/wlog/middleware/huma

go 1.21

require (
	github.com/danielgtaylor/huma/v2 v2.13.0
	github.com/go-chi/chi/v5 v5.0.12
	github.com/jeremygprawira/wlog v0.8.0
	github.com/jeremygprawira/wlog/middleware/chi v0.8.0
)

require github.com/go-chi/chi v4.1.2+incompatible // indirect

replace (
	github.com/jeremygprawira/wlog => ../..
	github.com/jeremygprawira/wlog/middleware/chi => ../chi
)
