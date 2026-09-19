module github.com/jeremygprawira/wlog/client/aws

go 1.21

require (
	github.com/aws/aws-sdk-go-v2 v1.17.0
	github.com/aws/smithy-go v1.13.3
	github.com/jeremygprawira/wlog v0.6.0
)

require (
	github.com/aws/aws-sdk-go-v2/credentials v1.12.0
	github.com/aws/aws-sdk-go-v2/service/s3 v1.29.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.4.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.1.24 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.4.18 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.0.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.9.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.1.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.9.18 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.13.18 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
