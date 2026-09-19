module github.com/jeremygprawira/wlog/log/logrus

go 1.26.0

require github.com/sirupsen/logrus v1.10.2

require (
	github.com/jeremygprawira/wlog v0.5.0
	golang.org/x/sys v0.48.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
