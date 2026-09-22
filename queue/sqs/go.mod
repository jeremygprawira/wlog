module github.com/jeremygprawira/wlog/queue/sqs

go 1.21

require (
	github.com/aws/aws-sdk-go-v2 v1.36.1
	github.com/aws/aws-sdk-go-v2/service/sns v1.33.19
	github.com/aws/aws-sdk-go-v2/service/sqs v1.37.14
	github.com/jeremygprawira/wlog v0.8.0
)

require (
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.32 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.32 // indirect
	github.com/aws/smithy-go v1.22.2 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
