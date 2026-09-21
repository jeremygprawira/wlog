module github.com/jeremygprawira/wlog/client/aws

go 1.21

require (
	github.com/aws/aws-sdk-go-v2 v1.36.1
	github.com/aws/smithy-go v1.22.2
	github.com/jeremygprawira/wlog v0.7.0
)

require (
	github.com/aws/aws-sdk-go-v2/credentials v1.17.10
	github.com/aws/aws-sdk-go-v2/service/s3 v1.75.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.29 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.29 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.29 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.10 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.18.10 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
