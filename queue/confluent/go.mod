module github.com/jeremygprawira/wlog/queue/confluent

go 1.21

require (
	github.com/confluentinc/confluent-kafka-go/v2 v2.12.0
	github.com/jeremygprawira/wlog v0.8.0
)

replace github.com/jeremygprawira/wlog => ../..
