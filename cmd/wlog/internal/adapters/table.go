// Package adapters holds the table of wlog integrations. `wlog init` matches a module's
// go.mod and its imports against this table, and `wlog doctor` reads the setup line of each
// adapter a project imports. One row per adapter keeps both tools on one list.
package adapters

// Adapter is one wlog integration: the third-party libraries that imply it, the wlog import
// path, and the one line that installs it.
type Adapter struct {
	Name  string   // the module id from CAPABILITIES.md, such as http-chi
	Wlog  string   // the wlog import path, such as github.com/jeremygprawira/wlog/middleware/chi
	Kind  string   // http, rpc, client, store, log, errors, flag, queue, job, faas, command, ai, trace, metrics
	Libs  []string // the third-party module paths that imply this adapter, empty when it wraps the standard library
	Setup string   // the one line a reader installs, empty when no source shows one
}

// Table lists every adapter the tool knows, sorted by name.
var Table = []Adapter{
	{Name: "ai-anthropic", Wlog: "github.com/jeremygprawira/wlog/ai/anthropic", Kind: "ai",
		Libs:  []string{"github.com/anthropics/anthropic-sdk-go"},
		Setup: "option.WithMiddleware(wloganthropic.Middleware())"},
	{Name: "ai-eino", Wlog: "github.com/jeremygprawira/wlog/ai/eino", Kind: "ai",
		Libs: []string{"github.com/cloudwego/eino"}, Setup: "wlogeino.Handler()"},
	{Name: "ai-genai", Wlog: "github.com/jeremygprawira/wlog/ai/genai", Kind: "ai",
		Libs: []string{"google.golang.org/genai"}, Setup: "wlogenai.FromGenerateContent(resp, backend)"},
	{Name: "ai-goopenai", Wlog: "github.com/jeremygprawira/wlog/ai/goopenai", Kind: "ai",
		Libs:  []string{"github.com/sashabaranov/go-openai"},
		Setup: "wlogopenai.FromChatCompletionResponse(resp)"},
	{Name: "ai-langchaingo", Wlog: "github.com/jeremygprawira/wlog/ai/langchaingo", Kind: "ai",
		Libs: []string{"github.com/tmc/langchaingo"}, Setup: "wloglangchaingo.Handler()"},
	{Name: "ai-mcpgo", Wlog: "github.com/jeremygprawira/wlog/ai/mcpgo", Kind: "ai",
		Libs: []string{"github.com/mark3labs/mcp-go"}, Setup: "server.WithHooks(wlogmcpgo.Hooks(log))"},
	{Name: "ai-mcpsdk", Wlog: "github.com/jeremygprawira/wlog/ai/mcpsdk", Kind: "ai",
		Libs:  []string{"github.com/modelcontextprotocol/go-sdk"},
		Setup: "server.AddReceivingMiddleware(wlogmcp.Middleware(log))"},
	{Name: "ai-openai", Wlog: "github.com/jeremygprawira/wlog/ai/openai", Kind: "ai",
		Libs:  []string{"github.com/openai/openai-go/v3"},
		Setup: "option.WithMiddleware(wlogopenai.Middleware())"},
	{Name: "client-aws", Wlog: "github.com/jeremygprawira/wlog/client/aws", Kind: "client",
		Libs: []string{"github.com/aws/aws-sdk-go-v2"}, Setup: "wlogaws.Append(&cfg.APIOptions)"},
	{Name: "client-http", Wlog: "github.com/jeremygprawira/wlog/client/http", Kind: "client",
		Setup: "wlogclient.Transport(next)"},
	{Name: "command-cobra", Wlog: "github.com/jeremygprawira/wlog/command/cobra", Kind: "command",
		Libs:  []string{"github.com/spf13/cobra"},
		Setup: "wlogcobra.Execute(context.Background(), logger, root)"},
	{Name: "command-kong", Wlog: "github.com/jeremygprawira/wlog/command/kong", Kind: "command",
		Libs:  []string{"github.com/alecthomas/kong"},
		Setup: "wlogkong.Run(context.Background(), logger, &grammar, os.Args[1:])"},
	{Name: "command-urfave", Wlog: "github.com/jeremygprawira/wlog/command/urfave", Kind: "command",
		Libs:  []string{"github.com/urfave/cli/v3"},
		Setup: "wlogurfave.Run(context.Background(), logger, cmd, os.Args)"},
	{Name: "errors-cockroach", Wlog: "github.com/jeremygprawira/wlog/errors/cockroach", Kind: "errors",
		Libs:  []string{"github.com/cockroachdb/errors"},
		Setup: "wlog.WithErrorExtractor(wlogcockroach.Extractor())"},
	{Name: "errors-herr", Wlog: "github.com/jeremygprawira/wlog/errors/herr", Kind: "errors",
		Libs:  []string{"github.com/jeremygprawira/herr"},
		Setup: "wlog.WithErrorExtractor(wlogherr.Extractor())"},
	{Name: "errors-oops", Wlog: "github.com/jeremygprawira/wlog/errors/oops", Kind: "errors",
		Libs:  []string{"github.com/samber/oops"},
		Setup: "wlog.WithErrorExtractor(wlogoops.Extractor())"},
	{Name: "errors-validator", Wlog: "github.com/jeremygprawira/wlog/errors/validator", Kind: "errors",
		Libs:  []string{"github.com/go-playground/validator/v10"},
		Setup: "wlog.WithErrorExtractor(wlogvalidator.Extractor())"},
	{Name: "faas-gcf", Wlog: "github.com/jeremygprawira/wlog/faas/gcf", Kind: "faas",
		Libs:  []string{"github.com/GoogleCloudPlatform/functions-framework-go"},
		Setup: "wloggcf.HTTP(logger, handler)"},
	{Name: "faas-lambda", Wlog: "github.com/jeremygprawira/wlog/faas/lambda", Kind: "faas",
		Libs: []string{"github.com/aws/aws-lambda-go"}, Setup: "wloglambda.Wrap(logger, handler)"},
	{Name: "flag-openfeature", Wlog: "github.com/jeremygprawira/wlog/flag/openfeature", Kind: "flag",
		Libs:  []string{"github.com/open-feature/go-sdk"},
		Setup: "openfeature.WithHooks(wlogopenfeature.Hook())"},
	{Name: "http-chi", Wlog: "github.com/jeremygprawira/wlog/middleware/chi", Kind: "http",
		Libs: []string{"github.com/go-chi/chi/v5"}, Setup: "wlogchi.Setup(r)"},
	{Name: "http-echo", Wlog: "github.com/jeremygprawira/wlog/middleware/echo", Kind: "http",
		Libs: []string{"github.com/labstack/echo/v4"}, Setup: "wlogecho.Middleware(log)"},
	{Name: "http-echo5", Wlog: "github.com/jeremygprawira/wlog/middleware/echo5", Kind: "http",
		Libs: []string{"github.com/labstack/echo/v5"}, Setup: "wlogecho5.Middleware(log)"},
	{Name: "http-fasthttp", Wlog: "github.com/jeremygprawira/wlog/middleware/fasthttp", Kind: "http",
		Libs: []string{"github.com/valyala/fasthttp"}, Setup: "wlogfasthttp.Middleware(log)"},
	{Name: "http-fiber", Wlog: "github.com/jeremygprawira/wlog/middleware/fiber", Kind: "http",
		Libs: []string{"github.com/gofiber/fiber/v2"}, Setup: "wlogfiber.Setup(app)"},
	{Name: "http-fiber3", Wlog: "github.com/jeremygprawira/wlog/middleware/fiber3", Kind: "http",
		Libs: []string{"github.com/gofiber/fiber/v3"}, Setup: "wlogfiber3.Setup(app)"},
	{Name: "http-gin", Wlog: "github.com/jeremygprawira/wlog/middleware/gin", Kind: "http",
		Libs: []string{"github.com/gin-gonic/gin"}, Setup: "wloggin.Middleware(log)"},
	{Name: "http-gozero", Wlog: "github.com/jeremygprawira/wlog/middleware/gozero", Kind: "http",
		Libs:  []string{"github.com/zeromicro/go-zero"},
		Setup: "rest.WithRouter(wloggozero.RouterOption(log, inner))"},
	{Name: "http-hertz", Wlog: "github.com/jeremygprawira/wlog/middleware/hertz", Kind: "http",
		Libs: []string{"github.com/cloudwego/hertz"}, Setup: "wloghertz.Setup(h)"},
	{Name: "http-httprouter", Wlog: "github.com/jeremygprawira/wlog/middleware/httprouter", Kind: "http",
		Libs: []string{"github.com/julienschmidt/httprouter"}, Setup: "wloghttprouter.New(log, router)"},
	{Name: "http-huma", Wlog: "github.com/jeremygprawira/wlog/middleware/huma", Kind: "http",
		Libs: []string{"github.com/danielgtaylor/huma/v2"}, Setup: "api.UseMiddleware(wloghuma.Middleware())"},
	{Name: "http-kratos", Wlog: "github.com/jeremygprawira/wlog/middleware/kratos", Kind: "http",
		Libs: []string{"github.com/go-kratos/kratos/v2"}, Setup: "khttp.Filter(wlogkratos.Filter(log))"},
	{Name: "http-nethttp", Wlog: "github.com/jeremygprawira/wlog/middleware/nethttp", Kind: "http",
		Setup: "wlogstd.Middleware(log)"},
	{Name: "job-asynq", Wlog: "github.com/jeremygprawira/wlog/job/asynq", Kind: "job",
		Libs: []string{"github.com/hibiken/asynq"}, Setup: "wlogasynq.Middleware(logger)"},
	{Name: "job-cron", Wlog: "github.com/jeremygprawira/wlog/job/cron", Kind: "job",
		Libs: []string{"github.com/robfig/cron/v3"}, Setup: `wlogcron.Job(logger, "reindex", spec, fn)`},
	{Name: "job-river", Wlog: "github.com/jeremygprawira/wlog/job/river", Kind: "job",
		Libs: []string{"github.com/riverqueue/river"}, Setup: "wlogriver.New(logger)"},
	{Name: "job-temporal", Wlog: "github.com/jeremygprawira/wlog/job/temporal", Kind: "job",
		Libs: []string{"go.temporal.io/sdk"}, Setup: "wlogtemporal.Interceptor(logger)"},
	{Name: "log-hclog", Wlog: "github.com/jeremygprawira/wlog/log/hclog", Kind: "log",
		Libs: []string{"github.com/hashicorp/go-hclog"}, Setup: "wlog.WithPlugins(wloghclog.Plugin(base))"},
	{Name: "log-logr", Wlog: "github.com/jeremygprawira/wlog/log/logr", Kind: "log",
		Libs: []string{"github.com/go-logr/logr"}, Setup: "wlog.WithPlugins(wloglogr.Plugin())"},
	{Name: "log-logrus", Wlog: "github.com/jeremygprawira/wlog/log/logrus", Kind: "log",
		Libs: []string{"github.com/sirupsen/logrus"}, Setup: "wloglogrus.Install(logger)"},
	{Name: "log-slog", Wlog: "github.com/jeremygprawira/wlog/log/slog", Kind: "log",
		Setup: "wlogslog.Handler(next)"},
	{Name: "log-std", Wlog: "github.com/jeremygprawira/wlog/log/std", Kind: "log",
		Setup: "wlogstdlog.Logger(ctx, prefix, flags)"},
	{Name: "log-zap", Wlog: "github.com/jeremygprawira/wlog/log/zap", Kind: "log",
		Libs: []string{"go.uber.org/zap"}, Setup: "wlogzap.Core(next)"},
	{Name: "log-zerolog", Wlog: "github.com/jeremygprawira/wlog/log/zerolog", Kind: "log",
		Libs: []string{"github.com/rs/zerolog"}, Setup: "wlogzerolog.Hook()"},
	{Name: "metrics-prometheus", Wlog: "github.com/jeremygprawira/wlog/metrics/prometheus", Kind: "metrics",
		Libs: []string{"github.com/prometheus/client_golang"}, Setup: "rec, err := wlogprom.New(reg)"},
	{Name: "queue-amqp", Wlog: "github.com/jeremygprawira/wlog/queue/amqp", Kind: "queue",
		Libs:  []string{"github.com/rabbitmq/amqp091-go"},
		Setup: `wlogamqp.Consume(context.Background(), logger, deliveries, "orders", handler)`},
	{Name: "queue-cloudevents", Wlog: "github.com/jeremygprawira/wlog/queue/cloudevents", Kind: "queue",
		Libs: []string{"github.com/cloudevents/sdk-go/v2"}, Setup: "wlogcloudevents.Observability(log)"},
	{Name: "queue-confluent", Wlog: "github.com/jeremygprawira/wlog/queue/confluent", Kind: "queue",
		Libs:  []string{"github.com/confluentinc/confluent-kafka-go/v2"},
		Setup: "wlogconfluent.Consume(context.Background(), logger, consumer, handler)"},
	{Name: "queue-franz", Wlog: "github.com/jeremygprawira/wlog/queue/franz", Kind: "queue",
		Libs: []string{"github.com/twmb/franz-go"}, Setup: "kgo.WithHooks(wlogfranz.Hooks())"},
	{Name: "queue-kafkago", Wlog: "github.com/jeremygprawira/wlog/queue/kafkago", Kind: "queue",
		Libs: []string{"github.com/segmentio/kafka-go"}, Setup: "wlogkafka.Consume(ctx, log, r, handle)"},
	{Name: "queue-nats", Wlog: "github.com/jeremygprawira/wlog/queue/nats", Kind: "queue",
		Libs: []string{"github.com/nats-io/nats.go"}, Setup: "wlognats.Handler(logger, handler)"},
	{Name: "queue-pubsub", Wlog: "github.com/jeremygprawira/wlog/queue/pubsub", Kind: "queue",
		Libs: []string{"cloud.google.com/go/pubsub/v2"}, Setup: "wlogpubsub.Receive(ctx, logger, subscription, handler)"},
	{Name: "queue-sarama", Wlog: "github.com/jeremygprawira/wlog/queue/sarama", Kind: "queue",
		Libs: []string{"github.com/IBM/sarama"}, Setup: "wlogsarama.Handler(logger, handler)"},
	{Name: "queue-sqs", Wlog: "github.com/jeremygprawira/wlog/queue/sqs", Kind: "queue",
		Libs:  []string{"github.com/aws/aws-sdk-go-v2/service/sqs"},
		Setup: "wlogsqs.Receive(ctx, logger, client, input, handler)"},
	{Name: "queue-watermill", Wlog: "github.com/jeremygprawira/wlog/queue/watermill", Kind: "queue",
		Libs:  []string{"github.com/ThreeDotsLabs/watermill"},
		Setup: "router.AddMiddleware(wlogwatermill.Middleware(log))"},
	{Name: "rpc-connect", Wlog: "github.com/jeremygprawira/wlog/rpc/connect", Kind: "rpc",
		Libs: []string{"connectrpc.com/connect"}, Setup: "wlogconnect.Interceptor(log)"},
	{Name: "rpc-gqlgen", Wlog: "github.com/jeremygprawira/wlog/rpc/gqlgen", Kind: "rpc",
		Libs: []string{"github.com/99designs/gqlgen"}, Setup: "srv.Use(wloggqlgen.Extension())"},
	{Name: "rpc-grpc", Wlog: "github.com/jeremygprawira/wlog/rpc/grpc", Kind: "rpc",
		Libs: []string{"google.golang.org/grpc"}, Setup: "wloggrpc.ServerOptions(log)"},
	{Name: "rpc-twirp", Wlog: "github.com/jeremygprawira/wlog/rpc/twirp", Kind: "rpc",
		Libs:  []string{"github.com/twitchtv/twirp"},
		Setup: "twirp.WithServerHooks(wlogtwirp.ServerHooks())"},
	{Name: "sqlshape", Wlog: "github.com/jeremygprawira/wlog/store/sqlshape", Kind: "store"},
	{Name: "store-bun", Wlog: "github.com/jeremygprawira/wlog/store/bun", Kind: "store",
		Libs: []string{"github.com/uptrace/bun"}, Setup: "db.AddQueryHook(wlogbun.Hook())"},
	{Name: "store-gorm", Wlog: "github.com/jeremygprawira/wlog/store/gorm", Kind: "store",
		Libs: []string{"gorm.io/gorm"}, Setup: "db.Use(wloggorm.Plugin())"},
	{Name: "store-mongo", Wlog: "github.com/jeremygprawira/wlog/store/mongo", Kind: "store",
		Libs:  []string{"go.mongodb.org/mongo-driver/v2"},
		Setup: "opts.SetMonitor(wlogmongo.Monitor(opts.Monitor))"},
	{Name: "store-pgx", Wlog: "github.com/jeremygprawira/wlog/store/pgx", Kind: "store",
		Libs: []string{"github.com/jackc/pgx/v5"}, Setup: "wlogpgx.Tracer(next)"},
	{Name: "store-redis", Wlog: "github.com/jeremygprawira/wlog/store/redis", Kind: "store",
		Libs: []string{"github.com/redis/go-redis/v9"}, Setup: "rdb.AddHook(wlogredis.Hook())"},
	{Name: "store-sql", Wlog: "github.com/jeremygprawira/wlog/store/sql", Kind: "store",
		Setup: "sql.OpenDB(wlogsql.Wrap(connector))"},
	{Name: "trace-otel", Wlog: "github.com/jeremygprawira/wlog/trace/otel", Kind: "trace",
		Libs: []string{"go.opentelemetry.io/otel"}, Setup: "p, err := wlogotel.Plugin()"},
	{Name: "trace-otellog", Wlog: "github.com/jeremygprawira/wlog/trace/otellog", Kind: "trace",
		Libs:  []string{"go.opentelemetry.io/otel/log"},
		Setup: "wlog.WithDrains(wlogotellog.New(provider))"},
}

// ByWlog returns the adapter with this wlog import path.
func ByWlog(path string) (Adapter, bool) {
	for _, adapter := range Table {
		if adapter.Wlog == path {
			return adapter, true
		}
	}
	return Adapter{}, false
}

// ByLib returns the adapters that wrap this third-party module. Several adapters can share one
// library, such as fasthttp, so the result is a list.
func ByLib(path string) []Adapter {
	var found []Adapter
	for _, adapter := range Table {
		for _, lib := range adapter.Libs {
			if lib == path {
				found = append(found, adapter)
			}
		}
	}
	return found
}

// SetupLines returns the setup line of every adapter that has one, keyed by the wlog import
// path. wlog doctor reads this to name the install line of an adapter a project imports.
func SetupLines() map[string]string {
	lines := map[string]string{}
	for _, adapter := range Table {
		if adapter.Setup != "" {
			lines[adapter.Wlog] = adapter.Setup
		}
	}
	return lines
}
