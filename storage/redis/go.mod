module github.com/apizbe/golagram/storage/redis

go 1.24

toolchain go1.26.5

replace github.com/apizbe/golagram => ../..

require (
	github.com/alicebob/miniredis/v2 v2.38.0
	github.com/apizbe/golagram v0.0.0-00010101000000-000000000000
	github.com/redis/go-redis/v9 v9.21.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
)
