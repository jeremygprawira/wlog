module github.com/jeremygprawira/wlog/queue/franz

go 1.21

require (
	github.com/jeremygprawira/wlog v0.7.0
	github.com/twmb/franz-go v1.18.1
)

require (
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.9.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
