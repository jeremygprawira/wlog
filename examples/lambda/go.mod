module github.com/jeremygprawira/wlog/examples/lambda

go 1.26

require (
	github.com/aws/aws-lambda-go v1.55.0
	github.com/jeremygprawira/wlog v0.7.0
)

require github.com/stretchr/testify v1.12.1 // indirect

replace github.com/jeremygprawira/wlog => ../..
