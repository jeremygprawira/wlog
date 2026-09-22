module github.com/jeremygprawira/wlog/faas/lambda

go 1.21

require (
	github.com/aws/aws-lambda-go v1.54.0
	github.com/jeremygprawira/wlog v0.8.0
)

replace github.com/jeremygprawira/wlog => ../..
