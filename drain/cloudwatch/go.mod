module github.com/jeremygprawira/wlog/drain/cloudwatch

go 1.21

require (
	github.com/aws/aws-sdk-go-v2 v1.17.1
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.15.22
	github.com/aws/smithy-go v1.13.4
	github.com/jeremygprawira/wlog v0.8.0
)

require (
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.1.25 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.4.19 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
