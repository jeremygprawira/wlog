# Async adapter setup

One setup line per Track D adapter. `make snippets` compiles every block, so a copy of one
block builds. Each block needs a broker, a queue, or a function service to run.

## Kafka: sarama

```go
package main

import (
	"context"

	"github.com/IBM/sarama"

	"github.com/jeremygprawira/wlog"
	wlogsarama "github.com/jeremygprawira/wlog/queue/sarama"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	group, _ := sarama.NewConsumerGroup([]string{"localhost:9092"}, "workers", sarama.NewConfig())
	handler := wlogsarama.Handler(logger, func(ctx context.Context, msg *sarama.ConsumerMessage) error {
		wlog.Set(ctx, "order_id", string(msg.Key))
		return nil
	})
	for {
		_ = group.Consume(context.Background(), []string{"orders"}, handler)
	}
}
```

## Kafka: confluent

```go
package main

import (
	"context"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"github.com/jeremygprawira/wlog"
	wlogconfluent "github.com/jeremygprawira/wlog/queue/confluent"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	consumer, _ := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":  "localhost:9092",
		"group.id":           "workers",
		"auto.offset.reset":  "earliest",
		"enable.auto.commit": false,
	})
	_ = wlogconfluent.Consume(context.Background(), logger, consumer, func(ctx context.Context, msg *kafka.Message) error {
		wlog.Set(ctx, "order_id", string(msg.Key))
		return nil
	})
}
```

## AWS SQS

```go
package main

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/jeremygprawira/wlog"
	wlogsqs "github.com/jeremygprawira/wlog/queue/sqs"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	ctx := context.Background()
	awsConfig, _ := config.LoadDefaultConfig(ctx)
	client := sqs.NewFromConfig(awsConfig)
	input := &sqs.ReceiveMessageInput{QueueUrl: aws.String("https://sqs.us-east-1.amazonaws.com/123456789012/orders")}
	_ = wlogsqs.Receive(ctx, logger, client, input, func(ctx context.Context, msg sqstypes.Message) error {
		wlog.Set(ctx, "order_id", msg.MessageId)
		return nil
	})
}
```

## NATS

`JetStreamHandler` covers a JetStream consumer, with the same shape.

```go
package main

import (
	"context"

	"github.com/nats-io/nats.go"

	"github.com/jeremygprawira/wlog"
	wlognats "github.com/jeremygprawira/wlog/queue/nats"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	conn, _ := nats.Connect("nats://localhost:4222")
	_, _ = conn.Subscribe("orders", wlognats.Handler(logger, func(ctx context.Context, msg *nats.Msg) error {
		wlog.Set(ctx, "order_id", msg.Subject)
		return nil
	}))
}
```

## RabbitMQ

```go
package main

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/jeremygprawira/wlog"
	wlogamqp "github.com/jeremygprawira/wlog/queue/amqp"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	conn, _ := amqp.Dial("amqp://guest:guest@localhost:5672/")
	channel, _ := conn.Channel()
	deliveries, _ := channel.Consume("orders", "", false, false, false, false, nil)
	_ = wlogamqp.Consume(context.Background(), logger, deliveries, "orders", func(ctx context.Context, delivery amqp.Delivery) error {
		wlog.Set(ctx, "order_id", delivery.MessageId)
		return nil
	})
}
```

## Google Cloud Pub/Sub

```go
package main

import (
	"context"

	"cloud.google.com/go/pubsub/v2"

	"github.com/jeremygprawira/wlog"
	wlogpubsub "github.com/jeremygprawira/wlog/queue/pubsub"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	ctx := context.Background()
	client, _ := pubsub.NewClient(ctx, "my-project")
	subscription := client.Subscriber("orders-sub")
	_ = wlogpubsub.Receive(ctx, logger, subscription, func(ctx context.Context, msg *pubsub.Message) error {
		wlog.Set(ctx, "order_id", msg.ID)
		return nil
	})
}
```

## asynq

```go
package main

import (
	"context"

	"github.com/hibiken/asynq"

	"github.com/jeremygprawira/wlog"
	wlogasynq "github.com/jeremygprawira/wlog/job/asynq"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	mux := asynq.NewServeMux()
	mux.Use(wlogasynq.Middleware(logger))
	mux.HandleFunc("reindex", func(ctx context.Context, task *asynq.Task) error {
		wlog.Set(ctx, "order_id", task.Type())
		return nil
	})
	_ = mux
}
```

## River

```go
package main

import (
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/jeremygprawira/wlog"
	wlogriver "github.com/jeremygprawira/wlog/job/river"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	config := &river.Config{
		WorkerMiddleware:    []rivertype.WorkerMiddleware{wlogriver.New(logger)},
		JobInsertMiddleware: []rivertype.JobInsertMiddleware{wlogriver.InsertMiddleware()},
	}
	_ = config
}
```

## Temporal

```go
package main

import (
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"

	"github.com/jeremygprawira/wlog"
	wlogtemporal "github.com/jeremygprawira/wlog/job/temporal"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	var temporalClient client.Client
	options := worker.Options{Interceptors: []interceptor.WorkerInterceptor{wlogtemporal.Interceptor(logger)}}
	_ = worker.New(temporalClient, "orders", options)
}
```

## cron

```go
package main

import (
	"context"

	"github.com/robfig/cron/v3"

	"github.com/jeremygprawira/wlog"
	wlogcron "github.com/jeremygprawira/wlog/job/cron"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	scheduler := cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)))
	_, _ = scheduler.AddJob("@every 5m", wlogcron.Job(logger, "reindex", "@every 5m", func(ctx context.Context) error {
		wlog.Set(ctx, "order_id", "ord-1")
		return nil
	}))
	scheduler.Start()
}
```

## AWS Lambda

```go
package main

import (
	"context"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/jeremygprawira/wlog"
	wloglambda "github.com/jeremygprawira/wlog/faas/lambda"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	lambda.StartWithOptions(
		wloglambda.Wrap(logger, func(ctx context.Context, in events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
			wlog.Set(ctx, "order_id", in.PathParameters["id"])
			return events.APIGatewayProxyResponse{StatusCode: 200}, nil
		}),
		wloglambda.SIGTERMFlush(logger),
	)
}
```

## Google Cloud Functions

```go
package main

import (
	"context"
	"net/http"

	"github.com/GoogleCloudPlatform/functions-framework-go/funcframework"

	"github.com/jeremygprawira/wlog"
	wloggcf "github.com/jeremygprawira/wlog/faas/gcf"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	ctx := context.Background()
	_ = funcframework.RegisterHTTPFunctionContext(ctx, "/", wloggcf.HTTP(logger, func(w http.ResponseWriter, r *http.Request) {
		wlog.Set(r.Context(), "order_id", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
}
```

## cobra

```go
package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/jeremygprawira/wlog"
	wlogcobra "github.com/jeremygprawira/wlog/command/cobra"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	root := &cobra.Command{Use: "app", RunE: func(cmd *cobra.Command, args []string) error {
		wlog.Set(cmd.Context(), "order_id", "ord-1")
		return nil
	}}
	os.Exit(wlogcobra.Execute(context.Background(), logger, root))
}
```

## urfave/cli

```go
package main

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/jeremygprawira/wlog"
	wlogurfave "github.com/jeremygprawira/wlog/command/urfave"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	cmd := &cli.Command{Name: "app", Action: func(ctx context.Context, cmd *cli.Command) error {
		wlog.Set(ctx, "order_id", "ord-1")
		return nil
	}}
	os.Exit(wlogurfave.Run(context.Background(), logger, cmd, os.Args))
}
```

## kong

```go
package main

import (
	"context"
	"os"

	"github.com/jeremygprawira/wlog"
	wlogkong "github.com/jeremygprawira/wlog/command/kong"
)

// grammar is the command line of the tool.
var grammar struct {
	Verbose bool `help:"noisy"`
}

func main() {
	logger := wlog.New(wlog.WithService("orders", "0.0.1", "prod"))
	os.Exit(wlogkong.Run(context.Background(), logger, &grammar, os.Args[1:]))
}
```
