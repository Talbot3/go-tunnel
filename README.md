# go-tunnel: 企业级安全隧道服务引擎

[![Go Reference](https://pkg.go.dev/badge/github.com/Talbot3/go-tunnel.svg)](https://pkg.go.dev/github.com/Talbot3/go-tunnel)
[![Go Report Card](https://goreportcard.com/badge/github.com/Talbot3/go-tunnel)](https://goreportcard.com/report/github.com/Talbot3/go-tunnel)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**go-tunnel** 是一款面向企业级场景的安全隧道服务引擎，专为高并发、低延迟、零信任架构设计。所有入口协议均强制 TLS 加密，确保数据传输安全，满足企业合规要求。

## 🔒 企业级安全特性

### 零信任安全架构

```
┌─────────────────────────────────────────────────────────────────┐
│                    企业级安全隧道架构                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   外部用户                    服务端                    客户端    │
│      │                          │                         │     │
│      │  ┌──────────────────────┐│                         │     │
│      │  │  入口协议 (全部TLS)  ││                         │     │
│      ├──► • HTTP/3 (ALPN h3)  ││                         │     │
│      │  │ • QUIC+TLS          ││                         │     │
│      │  │ • TCP+TLS           ││                         │     │
│      │  └──────────────────────┘│                         │     │
│      │                          │                         │     │
│      │      TLS 1.3 加密通道    │    QUIC Stream 透传     │     │
│      │  ════════════════════════╪═════════════════════════╪═══► │
│      │                          │                         │     │
│      │                          │    ┌─────────────────┐  │     │
│      │                          │    │ 后端协议支持    │  │     │
│      │                          │    │ • HTTP/3       │  │     │
│      │                          │    │ • HTTP/2       │  │     │
│      │                          │    │ • HTTP         │  │     │
│      │                          │    │ • QUIC         │  │     │
│      │                          │    │ • TCP          │──┼───► │
│      │                          │    └─────────────────┘  │     │
│      │                          │                         │     │
│                                  │                         ▼     │
│                                  │                    内网服务   │
└─────────────────────────────────────────────────────────────────┘
```

### 安全设计原则

| 原则 | 实现方式 | 安全收益 |
|------|----------|----------|
| **传输加密** | 所有入口协议强制 TLS 1.3 | 防止中间人攻击、数据窃听 |
| **身份认证** | Token 认证 + TLS 双向认证 | 确保只有授权客户端可连接 |
| **协议隔离** | ALPN 协议协商区分服务类型 | 防止协议混淆攻击 |
| **资源保护** | 熔断器 + 连接限制器 | 防止 DDoS 和资源耗尽 |
| **审计追踪** | 完整连接日志 + 统计指标 | 满足合规审计要求 |

### 入口协议安全矩阵

| 入口协议 | 加密方式 | ALPN | 适用场景 | 安全等级 |
|---------|----------|------|----------|----------|
| **HTTP/3** | TLS 1.3 | `h3` | 浏览器、Web 应用 | ⭐⭐⭐⭐⭐ |
| **QUIC+TLS** | TLS 1.3 | `quic-tunnel` | 高性能隧道客户端 | ⭐⭐⭐⭐⭐ |
| **TCP+TLS** | TLS 1.3 | - | 需要 TCP 直连的场景 | ⭐⭐⭐⭐⭐ |

> ⚠️ **安全声明**: v1.3.0 起移除普通 TCP 入口支持，所有入口协议均强制 TLS 加密。

## 📚 文档索引

| 文档 | 说明 |
|------|------|
| [INTEGRATION.md](./INTEGRATION.md) | 集成部署指南 - 安全配置、监控集成 |
| [CODE_REVIEW.md](./CODE_REVIEW.md) | 代码审查报告 - 安全评估、设计分析 |
| [docs/MUX_DESIGN.md](./docs/MUX_DESIGN.md) | 多路复用设计 - 安全架构详解 |
| [docs/MUX_TUNNEL_DESIGN.md](./docs/MUX_TUNNEL_DESIGN.md) | 隧道实现 - 端到端加密方案 |

## 🚀 技术核心

### 高性能转发引擎

* **零拷贝转发 (Zero-Copy):** Linux 环境下深度集成 `unix.Splice`，数据流直接在内核缓冲区移动。
* **平台差异化优化:**
  * **Linux:** `Splice/Tee` 零拷贝
  * **macOS:** `TCP_NOTSENT_LOWAT` 低延迟
  * **Windows:** IOCP 大缓冲区高吞吐
* **自适应背压控制:** 内置水位监控，自动协调上下游速率，防止 OOM。
* **原生 HTTP/3 支持:** 基于 `quic-go` 深度定制，支持 0-RTT 快速重连。

### 高可用组件

| 组件 | 功能 | 企业价值 |
|------|------|----------|
| **熔断器** | 三态熔断防止级联故障 | 服务稳定性保障 |
| **重试机制** | 指数退避 + 抖动 | 弱网环境容错 |
| **健康检查** | Kubernetes 兼容端点 | 自动故障检测 |
| **优雅关闭** | 优先级回调机制 | 零停机部署 |
| **资源限制器** | 连接/速率/内存限制 | 资源保护 |

## 📊 性能基准

Apple Silicon 本地回环测试结果：

| 指标 | 原生连接 | go-tunnel 转发 | 性能损耗 |
|:---|:---|:---|:---|
| **吞吐量** | 19.15 Gbps | **10.87 Gbps** | < 45% |
| **平均延迟** | 0.068 ms | **0.170 ms** | 微秒级 |
| **并发能力** | 10,455 RPS | **7,585 RPS** | 72% 保持率 |

## 📦 安装

```bash
go get github.com/Talbot3/go-tunnel
```

## 💡 快速开始

### 安全隧道服务端

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    
    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    // 加载 TLS 证书（生产环境建议使用 certmagic 自动管理）
    cert, err := tls.LoadX509KeyPair("server.crt", "server.key")
    if err != nil {
        log.Fatal(err)
    }
    
    tlsConfig := &tls.Config{
        Certificates: []tls.Certificate{cert},
        MinVersion:   tls.VersionTLS13, // 强制 TLS 1.3
    }
    
    // 创建安全隧道服务器
    server := quic.NewMuxServer(quic.MuxServerConfig{
        ListenAddr: ":443",
        TLSConfig:  tlsConfig,
        AuthToken:  "your-secure-token", // 客户端认证令牌
        EntryProtocols: quic.EntryProtocolConfig{
            EnableHTTP3:  true,  // HTTP/3 入口
            EnableQUIC:   true,  // QUIC+TLS 入口
            EnableTCPTLS: true,  // TCP+TLS 入口
        },
    })
    
    // 启动服务
    if err := server.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer server.Stop()
    
    log.Println("安全隧道服务已启动，监听 :443")
    select {} // 阻塞主线程
}
```

### 安全隧道客户端

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    
    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    // 客户端 TLS 配置
    tlsConfig := &tls.Config{
        InsecureSkipVerify: false, // 生产环境应验证证书
        MinVersion:         tls.VersionTLS13,
    }
    
    // 创建安全隧道客户端
    client := quic.NewMuxClient(quic.MuxClientConfig{
        ServerAddr: "tunnel.example.com:443",
        TLSConfig:  tlsConfig,
        Protocol:   quic.ProtocolTCP,
        LocalAddr:  "127.0.0.1:8080", // 内网服务地址
        Domain:     "app.example.com", // 域名标识
        AuthToken:  "your-secure-token",
    })
    
    // 启动客户端
    if err := client.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer client.Stop()
    
    log.Printf("隧道已建立: %s", client.PublicURL())
    select {} // 阻塞主线程
}
```

## 🔐 安全配置指南

### TLS 证书管理

**生产环境推荐使用 certmagic 自动管理：**

```go
import "github.com/caddyserver/certmagic"

func main() {
    // 自动获取和续期 Let's Encrypt 证书
    err := certmagic.HTTPS([]string{"tunnel.example.com"}, handler)
    if err != nil {
        log.Fatal(err)
    }
}
```

### 认证配置

```go
// 服务端配置
server := quic.NewMuxServer(quic.MuxServerConfig{
    AuthToken: "strong-random-token-min-32-chars",
    // ... 其他配置
})

// 客户端配置（必须匹配服务端令牌）
client := quic.NewMuxClient(quic.MuxClientConfig{
    AuthToken: "strong-random-token-min-32-chars",
    // ... 其他配置
})
```

### 资源保护配置

```go
server := quic.NewMuxServer(quic.MuxServerConfig{
    MaxTunnels:       1000,  // 最大隧道数
    MaxConnsPerTunnel: 100,  // 每隧道最大连接数
    // ... 其他配置
})
```

## 🛠 核心应用场景

### 1. 企业内网穿透

```
外部用户 ──HTTPS──► 隧道服务器 ──QUIC──► 内网客户端 ──► 内网服务
                    (公网)              (内网)
```

**安全优势：**
- 全链路 TLS 1.3 加密
- 无需开放内网端口
- 支持域名隔离的多租户

### 2. 微服务安全网关

```
客户端 ──HTTP/3──► 隧道网关 ──► 服务网格
                    (TLS终止)
```

**安全优势：**
- 统一 TLS 卸载
- 协议转换 (H3 → TCP)
- 熔断保护后端服务

### 3. 跨云安全互联

```
云A服务 ◄──QUIC隧道──► 云B服务
        (TLS 1.3加密)
```

**安全优势：**
- 零信任网络架构
- 端到端加密传输
- 支持双向认证

## 📖 API 参考

### 入口协议配置

```go
type EntryProtocolConfig struct {
    EnableHTTP3  bool   // HTTP/3 入口 (ALPN "h3")
    EnableQUIC   bool   // QUIC+TLS 入口 (ALPN "quic-tunnel")
    EnableTCPTLS bool   // TCP+TLS 入口
    QUICALPN     string // 自定义 QUIC ALPN
}
```

### 协议支持矩阵

| 协议 | 代码 | 入口访问 | 后端代理 |
|------|------|----------|----------|
| TCP | `ProtocolTCP` (0x01) | ✅ TCP+TLS | ✅ |
| HTTP | `ProtocolHTTP` (0x02) | - | ✅ |
| QUIC | `ProtocolQUIC` (0x03) | ✅ QUIC+TLS | ✅ |
| HTTP/2 | `ProtocolHTTP2` (0x04) | - | ✅ |
| HTTP/3 | `ProtocolHTTP3` (0x05) | ✅ HTTP/3 | ✅ |

### SDK 工具函数

```go
// 连接到 TCP+TLS 隧道入口
conn, err := quic.DialTunnel("app.example.com", "tunnel.example.com:443")

// 带初始数据的连接
conn, err := quic.DialTunnelWithPayload(
    "app.example.com", 
    "tunnel.example.com:443",
    []byte("GET / HTTP/1.1\r\nHost: app.example.com\r\n\r\n"),
)
```

## 🏢 企业级部署

### Docker 部署

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o tunnel-server ./cmd/server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/tunnel-server /usr/local/bin/
EXPOSE 443/udp 443/tcp
CMD ["tunnel-server"]
```

### Kubernetes 部署

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tunnel-server
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: tunnel-server
        image: tunnel-server:latest
        ports:
        - containerPort: 443
          protocol: UDP
        - containerPort: 443
          protocol: TCP
        resources:
          limits:
            memory: "512Mi"
            cpu: "1000m"
        livenessProbe:
          httpGet:
            path: /livez
            port: 8080
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
```

### 监控集成

```go
// Prometheus 指标端点
http.Handle("/metrics", promhttp.Handler())

// 健康检查端点
http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
})
```

## 📋 合规性支持

| 标准 | 支持情况 | 实现方式 |
|------|----------|----------|
| **数据加密** | ✅ | TLS 1.3 强制加密 |
| **访问控制** | ✅ | Token 认证 + TLS 双向认证 |
| **审计日志** | ✅ | 完整连接日志 |
| **高可用** | ✅ | 熔断器 + 健康检查 |
| **资源隔离** | ✅ | 连接限制器 |

## 🤝 贡献指南

欢迎提交 Issue 和 Pull Request。请确保：

1. 代码通过 `go test -race` 测试
2. 新功能包含单元测试
3. 遵循 Go 代码规范

## 📄 许可证

[MIT License](LICENSE)

## 🙏 特别感谢

- [Go 团队](https://golang.org/) - 优秀的 netpoll 运行时
- [quic-go 团队](https://github.com/quic-go) - 高质量的 QUIC 实现
- [Caddy 团队](https://github.com/caddyserver) - certmagic 自动证书管理
- [Cloudflare](https://blog.cloudflare.com/) - QUIC/HTTP/3 技术分享

## 更新日志

### v1.3.0 (2026-04-18)

**🔒 安全增强**
- **移除普通 TCP 入口** - 所有入口协议强制 TLS 加密
- 入口协议全部升级为 TLS 1.3
- 新增 `EntryProtocolConfig` 统一安全配置

**入口协议支持**
- HTTP/3 (ALPN "h3") - 浏览器友好
- QUIC+TLS (ALPN "quic-tunnel") - 高性能隧道
- TCP+TLS - TLS 握手后域名路由

**并发安全修复**
- 修复 `forwardQUICStream` 竞态条件
- 修复 `MuxClient` 状态字段并发访问
- 修复 `Tunnel` 外部监听器并发访问

### v1.2.0 (2026-04-17)

- 新增 TCP+TLS 单端口多路复用
- 新增 SDK 工具函数
- 明确协议支持范围

### v1.1.0 (2026-04-15)

- 新增高可用组件（熔断器、重试、健康检查、优雅关闭、资源限制器）
- 新增集成服务器 `server` 包
- Kubernetes 兼容健康端点

### v1.0.0

- 初始版本
- 支持 TCP、HTTP、HTTP/2、HTTP/3、QUIC 协议
- 零拷贝转发优化
- 自适应背压控制
