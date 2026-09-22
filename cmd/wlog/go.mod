module github.com/jeremygprawira/wlog/cmd/wlog

go 1.26.0

require (
	github.com/gin-gonic/gin v1.12.0
	github.com/golangci/plugin-module-register v0.1.2
	github.com/gorilla/mux v1.8.1
	github.com/jeremygprawira/wlog/middleware/echo v0.7.0
	github.com/jeremygprawira/wlog/middleware/echo5 v0.7.0
	github.com/jeremygprawira/wlog/middleware/gin v0.7.0
	github.com/labstack/echo/v4 v4.15.4
	github.com/labstack/echo/v5 v5.3.1
	github.com/modelcontextprotocol/go-sdk v1.8.0
	golang.org/x/tools v0.50.0
	gopkg.in/yaml.v3 v3.0.1
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
	github.com/google/jsonschema-go v0.4.3 // indirect
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
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.3.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.mongodb.org/mongo-driver/v2 v2.5.0 // indirect
	golang.org/x/arch v0.22.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

require (
	github.com/jeremygprawira/wlog v0.7.0
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..

replace github.com/jeremygprawira/wlog/errors/herr => ../../errors/herr

replace github.com/jeremygprawira/wlog/log/logrus => ../../log/logrus

replace github.com/jeremygprawira/wlog/log/zap => ../../log/zap

replace github.com/jeremygprawira/wlog/log/zerolog => ../../log/zerolog

replace github.com/jeremygprawira/wlog/middleware/echo => ../../middleware/echo

replace github.com/jeremygprawira/wlog/middleware/echo5 => ../../middleware/echo5

replace github.com/jeremygprawira/wlog/middleware/gin => ../../middleware/gin

replace github.com/jeremygprawira/wlog/trace/otel => ../../trace/otel
