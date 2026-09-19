module github.com/jeremygprawira/wlog/log/zerolog

go 1.26.0

require github.com/rs/zerolog v1.35.1

require (
	github.com/jeremygprawira/wlog v0.5.0
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
