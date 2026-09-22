module github.com/jeremygprawira/wlog/queue/nats

go 1.21.0

require (
	github.com/jeremygprawira/wlog v0.7.0
	github.com/nats-io/nats-server/v2 v2.10.22 // test only
	github.com/nats-io/nats.go v1.38.0
)

require (
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/minio/highwayhash v1.0.3 // indirect
	github.com/nats-io/jwt/v2 v2.5.8 // indirect
	github.com/nats-io/nkeys v0.4.9 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	golang.org/x/time v0.7.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
