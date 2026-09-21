module github.com/jeremygprawira/wlog/examples

go 1.26.0

require (
	github.com/aws/aws-lambda-go v1.54.0
	github.com/gin-gonic/gin v1.12.0
	github.com/go-chi/chi/v5 v5.3.2
	github.com/jeremygprawira/wlog/faas/lambda v0.7.0
	github.com/jeremygprawira/wlog/job/cron v0.7.0
	github.com/jeremygprawira/wlog/middleware/chi v0.7.0
	github.com/jeremygprawira/wlog/queue/kafkago v0.7.0
	github.com/labstack/echo/v4 v4.15.4
	github.com/labstack/echo/v5 v5.3.1
	github.com/robfig/cron/v3 v3.0.1
	github.com/segmentio/kafka-go v0.4.48
	go.uber.org/zap v1.28.0
	google.golang.org/grpc v1.67.3
)

require (
	github.com/klauspost/compress v1.17.6 // indirect
	github.com/pierrec/lz4/v4 v4.1.15 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240814211410-ddb44dafa142 // indirect
)

require (
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic v1.15.0 // indirect
	github.com/bytedance/sonic/loader v0.5.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/gabriel-vasile/mimetype v1.4.12 // indirect
	github.com/gin-contrib/sse v1.1.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.1 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/jeremygprawira/wlog v0.7.0
	github.com/jeremygprawira/wlog/log/zap v0.7.0
	github.com/jeremygprawira/wlog/middleware/echo v0.7.0
	github.com/jeremygprawira/wlog/middleware/echo5 v0.7.0
	github.com/jeremygprawira/wlog/middleware/gin v0.7.0
	github.com/jeremygprawira/wlog/rpc/grpc v0.7.0
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/labstack/gommon v0.5.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.59.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.3.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	go.mongodb.org/mongo-driver/v2 v2.5.0 // indirect
	golang.org/x/arch v0.22.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

replace github.com/jeremygprawira/wlog => ..

replace github.com/jeremygprawira/wlog/faas/lambda => ../faas/lambda

replace github.com/jeremygprawira/wlog/job/cron => ../job/cron

replace github.com/jeremygprawira/wlog/queue/kafkago => ../queue/kafkago

replace github.com/jeremygprawira/wlog/log/zap => ../log/zap

replace github.com/jeremygprawira/wlog/middleware/chi => ../middleware/chi

replace github.com/jeremygprawira/wlog/middleware/echo => ../middleware/echo

replace github.com/jeremygprawira/wlog/middleware/echo5 => ../middleware/echo5

replace github.com/jeremygprawira/wlog/middleware/gin => ../middleware/gin

replace github.com/jeremygprawira/wlog/rpc/grpc => ../rpc/grpc
