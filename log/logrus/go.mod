module github.com/jeremygprawira/wlog/log/logrus

go 1.26.1

require github.com/sirupsen/logrus v1.10.2

require (
	github.com/jeremygprawira/wlog v0.1.0
	golang.org/x/sys v0.13.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
