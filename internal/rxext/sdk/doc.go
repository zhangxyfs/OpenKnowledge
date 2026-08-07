// Package sdk 是 Reasonix Extension Protocol Go SDK 的 vendor 快照。
//
// 上游：D:\develop\DeepSeek-Reasonix\sdk\go（module github.com/esengine/DeepSeek-Reasonix/sdk/go）
// 同步方式：上游 sdk.go / wire.go / types_ext.go / types_generated.go 四个文件整文件覆盖
// 本目录同名文件（包名 extension 保持上游原名），然后跑 go build ./... 与
// go test ./internal/rxext/... 验证。禁止在本目录就地修改逻辑——改动请回上游。
//
// 快照日期：2026-08-07（上游 schema hash sha256:22338e66…，协议 major v1）
package extension
