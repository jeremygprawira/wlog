module github.com/jeremygprawira/wlog/queue/kafkago

go 1.21

require (
	github.com/jeremygprawira/wlog v0.8.0
	github.com/segmentio/kafka-go v0.4.48
)

require (
	github.com/klauspost/compress v1.15.9 // indirect
	github.com/pierrec/lz4/v4 v4.1.15 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
