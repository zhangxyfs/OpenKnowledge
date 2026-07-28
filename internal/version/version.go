// Package version 持有构建期注入的应用版本号。
package version

// Version 由 build-dist.sh 经 -ldflags -X 注入（事实源 installer/openknowledge.iss 的 AppVersion）；
// 裸 go build 为 dev。
var Version = "dev"
