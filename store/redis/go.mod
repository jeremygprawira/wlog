module github.com/jeremygprawira/wlog/store/redis

go 1.21

require (
	github.com/jeremygprawira/wlog v0.8.0
	github.com/redis/go-redis/v9 v9.7.3
)

require (
	github.com/alicebob/gopher-json v0.0.0-20200520072559-a9ecdc9d1d3a // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
)

require github.com/alicebob/miniredis/v2 v2.33.0 // test only

replace github.com/jeremygprawira/wlog => ../..
