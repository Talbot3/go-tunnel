# go-tunnel: 企业级云原生多协议转发引擎

[![Go Reference](https://pkg.go.dev/badge/github.com/Talbot3/go-tunnel.svg)](https://pkg.go.dev/github.com/Talbot3/go-tunnel)
[![Go Report Card](https://goreportcard.com/badge/github.com/Talbot3/go-tunnel)](https://goreportcard.com/report/github.com/Talbot3/go-tunnel)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**go-tunnel** 是一个专为高并发、低延迟场景设计的跨平台高性能转发库。它不仅对标 **Nginx/Envoy** 的转发核心，更针对 **Go 运行时** 进行了极致的底层优化。通过解耦协议层与传输层，`go-tunnel` 实现了在单一架构下对 **TCP、HTTP/2、HTTP/3 (QUIC)** 的统一调度与平滑切换。

## 🚀 技术核心与对标

在现代分布式架构中，网络转发的瓶颈往往在于用户态与内核态的上下文切换及内存拷贝。`go-tunnel` 通过以下设计打破瓶颈：

* **零拷贝转发 (Zero-Copy):** 在 Linux 环境下，深度集成 `unix.Splice` 系统调用，数据流直接在内核缓冲区移动，绕过用户态内存，性能直逼原生内核转发。
* **平台差异化驱动:**
  * **Linux:** 利用 `Splice/Tee` 实现零拷贝。
  * **macOS:** 采用 `TCP_NOTSENT_LOWAT` 优化内核发送队列，大幅降低延迟。
  * **Windows:** 针对 **IOCP** 机制优化大缓冲区设置，提升吞吐上限。
* **自适应背压控制 (Backpressure):** 借鉴 **Reactive Streams** 思想，内置水位监控，自动协调上下游速率，彻底杜绝因下游阻塞导致的服务端内存溢出（OOM）。
* **原生 HTTP/3 支持:** 基于 `quic-go` 深度定制，支持 0-RTT 连接建立，在弱网环境下性能远超传统 TCP。

## 📚 文档索引

| 文档 | 说明 |
|------|------|
| [INTEGRATION.md](./INTEGRATION.md) | 集成部署指南 - 6 种场景示例、客户端-服务器架构、监控集成 |
| [CODE_REVIEW.md](./CODE_REVIEW.md) | 代码审查报告 - 架构评估、设计分析、改进建议 |
| [docs/MUX_DESIGN.md](./docs/MUX_DESIGN.md) | 多路复用设计 - 共享连接多路转发扩展方案 |
| [docs/MUX_TUNNEL_DESIGN.md](./docs/MUX_TUNNEL_DESIGN.md) | 多路复用隧道实现 - 客户端-服务器架构详细设计 |
| [docs/MUX_PROTOCOL_REVIEW.md](./docs/MUX_PROTOCOL_REVIEW.md) | 协议审查报告 - 性能优化分析与实施建议 |

## 🛠 核心应用场景

* **高性能边缘网关:** 作为微服务入口，处理海量 TLS 卸载与协议转换（如 H3 入，TCP 出）。
* **跨云/内网穿透:** 配合 **ACME 自动证书管理**，快速构建安全、高性能的加密隧道。
* **流媒体/大数据分发:** 利用 **HighThroughput 预设** 与零拷贝技术，支持 10Gbps+ 的高带宽文件或视频流传输。
* **混合协议代理:** 单个进程内同时管理多种协议，简化运维复杂度。

## 📊 性能基准 (Benchmark)

在 Apple Silicon 架构的本地回环测试中，`go-tunnel` 展现了卓越的生产级性能：

| 指标 | 原生连接 (Direct) | go-tunnel 转发 | 性能损耗 |
| :--- | :--- | :--- | :--- |
| **吞吐量 (Throughput)** | 19.15 Gbps | **10.87 Gbps** | < 45% (包含协议栈开销) |
| **平均延迟 (Latency)** | 0.068 ms | **0.170 ms** | 仅微秒级增长 |
| **并发能力 (RPS)** | 10,455 | **7,585** | 极佳的并发保持率 |

## 📦 安装

```bash
go get github.com/Talbot3/go-tunnel
```

## 💡 快速开始

`go-tunnel` 提供了高度抽象的 API，兼顾了灵活性与易用性。

```go
// 方式一：高度定制化（推荐用于生产）
p := tcp.New()
t, _ := tunnel.New(tunnel.ServerPreset()) // 使用预设优化
t.SetProtocol(p)
t.Start(context.Background())

// 方式二：极简模式
go tunnel.HandlePair(src, dst) // 自动管理生命周期与背压
```

## API 参考

### 核心类型

#### `type Tunnel`

隧道实例，管理连接监听和数据转发。

```go
// 创建隧道
func New(cfg Config) (*Tunnel, error)
func NewWithContext(ctx context.Context, cfg Config) (*Tunnel, error)

// 设置协议处理器
func (t *Tunnel) SetProtocol(p Protocol)

// 启动/停止
func (t *Tunnel) Start(ctx context.Context) error
func (t *Tunnel) Stop() error

// 获取统计信息
func (t *Tunnel) Stats() *Stats

// 获取监听地址
func (t *Tunnel) Addr() net.Addr
```

#### `type Config`

隧道配置。

```go
type Config struct {
    // 基础配置
    Protocol   string      // 协议类型: "tcp", "http2", "http3", "quic"
    ListenAddr string      // 监听地址，如 ":8080"
    TargetAddr string      // 目标地址，如 "127.0.0.1:80"
    TLSConfig  *tls.Config // TLS 配置 (HTTP/2, HTTP/3, QUIC 需要)
    BufferSize int         // 缓冲区大小，默认 64KB

    // 运行模式
    Mode Mode              // 运行模式: ModeAuto, ModeServer, ModeClient

    // 连接管理（服务器场景）
    MaxConnections    int           // 最大并发连接数，0 表示无限制
    ConnectionTimeout time.Duration // 连接空闲超时
    AcceptTimeout     time.Duration // 接受连接超时

    // 数据传输优化
    ReadBufferSize    int  // 读缓冲区大小
    WriteBufferSize   int  // 写缓冲区大小
    WriteBufferPool   bool // 启用写缓冲池

    // 背压控制
    EnableBackpressure         bool          // 启用背压控制，默认 true
    BackpressureHighWatermark  int           // 高水位，默认 1MB (客户端) / 2MB (服务器)
    BackpressureLowWatermark   int           // 低水位，默认 512KB (客户端) / 1MB (服务器)
    BackpressureYieldMin       time.Duration // 最小让出时间，默认 50µs
    BackpressureYieldMax       time.Duration // 最大让出时间，默认 10ms

    // TCP 优化
    TCPNoDelay    bool // 启用 TCP_NODELAY，默认 true
    TCPQuickAck   bool // 启用 TCP_QUICKACK (Linux)
    TCPFastOpen   bool // 启用 TCP_FASTOPEN
    SendBufferSize int // SO_SNDBUF 大小
    RecvBufferSize int // SO_RCVBUF 大小

    // 监控
    EnableMetrics  bool   // 启用 Prometheus 指标
    MetricsPrefix  string // Prometheus 指标前缀
}
```

#### 配置预设

```go
// 服务器预设 - 高并发场景
cfg := tunnel.ServerPreset()
// 适用于隧道服务器，处理大量并发连接

// 客户端预设 - 高吞吐场景
cfg := tunnel.ClientPreset()
// 适用于隧道客户端，少量连接高吞吐

// 高吞吐预设 - 响应数据远大于请求数据
cfg := tunnel.HighThroughputPreset()
// 适用于视频流、文件下载等场景
```

#### `type Stats`

运行时统计。

```go
func (s *Stats) Connections() int64     // 总连接数
func (s *Stats) BytesSent() int64       // 发送字节数
func (s *Stats) BytesReceived() int64   // 接收字节数
func (s *Stats) Errors() int64          // 错误数
func (s *Stats) Uptime() time.Duration  // 运行时间
func (s *Stats) Reset()                 // 重置统计信息
```

#### `type Protocol`

协议接口。

```go
type Protocol interface {
    Name() string
    Listen(addr string) (net.Listener, error)
    Dial(ctx context.Context, addr string) (net.Conn, error)
    Forwarder() forward.Forwarder
}
```

### 便捷函数

```go
// 创建监听器
func Listen(network, addr string) (net.Listener, error)

// 连接目标
func Dial(network, addr string) (net.Conn, error)
func DialContext(ctx context.Context, network, addr string) (net.Conn, error)

// 双向转发
func Forward(src, dst net.Conn) error
func HandlePair(connA, connB net.Conn)

// 创建转发器
func NewForwarder() Forwarder

// TCP 优化
func OptimizeTCPConn(conn *net.TCPConn) error
```

### 缓冲区常量

```go
const (
    BufferSizeDefault = 64 * 1024  // 64KB 默认
    BufferSizeLarge   = 256 * 1024 // 256KB 高吞吐
)
```

## 内部包

### 高可用组件 (HA Components)

go-tunnel 提供企业级高可用组件，支持极致可用性目标。

#### 熔断器 (internal/circuit)

熔断器模式防止级联故障，支持三态切换：

```go
import "github.com/Talbot3/go-tunnel/internal/circuit"

// 创建熔断器
breaker := circuit.NewBreaker(circuit.Config{
    FailureThreshold: 5,              // 失败阈值
    SuccessThreshold: 2,              // 恢复阈值
    Timeout:         30 * time.Second, // 开路超时
})

// 执行操作
err := breaker.Execute(ctx, func(ctx context.Context) error {
    return someOperation()
})

// 检查状态
if breaker.State() == circuit.StateOpen {
    // 熔断器开启，拒绝请求
}
```

**状态机**：
- `StateClosed`: 正常状态，请求通过
- `StateOpen`: 熔断状态，快速失败
- `StateHalfOpen`: 半开状态，试探性恢复

#### 重试机制 (internal/retry)

指数退避重试，支持抖动防止惊群效应：

```go
import "github.com/Talbot3/go-tunnel/internal/retry"

// 创建重试器
retrier := retry.NewRetrier(retry.Config{
    MaxAttempts:     5,
    InitialDelay:    100 * time.Millisecond,
    MaxDelay:        10 * time.Second,
    Multiplier:      2.0,
    Jitter:          true,  // 添加随机抖动
})

// 执行重试
err := retrier.Do(ctx, func(ctx context.Context) error {
    return someOperation()
})

// 带结果的重试
result, err := retry.DoWithResult(ctx, func(ctx context.Context) (string, error) {
    return someOperationWithResult()
})
```

#### 健康检查 (internal/health)

Kubernetes 兼容的健康检查端点：

```go
import "github.com/Talbot3/go-tunnel/internal/health"

// 创建健康检查处理器
handler := health.NewHandler(health.HandlerConfig{
    Timeout: 5 * time.Second,
})

// 注册健康检查
handler.Register("database", func(ctx context.Context) health.CheckResult {
    if db.Ping() == nil {
        return health.CheckResult{
            Name:   "database",
            Status: health.StatusHealthy,
        }
    }
    return health.CheckResult{
        Name:   "database",
        Status: health.StatusUnhealthy,
        Error:  "connection failed",
    }
})

// HTTP 端点
http.HandleFunc("/health", handler.ServeHTTP)
http.HandleFunc("/livez", health.LivenessHandler())
http.HandleFunc("/readyz", health.ReadinessHandler(handler))
```

**端点说明**：
| 端点 | 用途 | Kubernetes |
|------|------|------------|
| `/health` | 综合健康状态 | - |
| `/livez` | 存活探针 | livenessProbe |
| `/readyz` | 就绪探针 | readinessProbe |

#### 优雅关闭 (internal/shutdown)

优先级回调的优雅关闭机制：

```go
import "github.com/Talbot3/go-tunnel/internal/shutdown"

// 初始化
shutdown.Init(shutdown.Config{
    Timeout: 30 * time.Second,
})

// 注册关闭回调（按优先级执行）
shutdown.Register("database", 100, func(ctx context.Context) error {
    return db.Close()
})

shutdown.Register("cache", 50, func(ctx context.Context) error {
    return cache.Flush()
})

shutdown.Register("server", 1, func(ctx context.Context) error {
    return server.Shutdown(ctx)
})

// 等待信号
shutdown.Wait()

// 或使用简化 API
shutdown.RegisterFunc("cleanup", func(ctx context.Context) error {
    return cleanup()
})
```

**优先级规则**：
- 数字越小越先执行
- 建议顺序：服务器(1) → 连接池(50) → 数据库(100)

#### 资源限制器 (internal/limiter)

多种资源限制器防止资源耗尽：

```go
import "github.com/Talbot3/go-tunnel/internal/limiter"

// 连接数限制器
connLimiter := limiter.NewConnectionLimiter(10000)
if err := connLimiter.Acquire(); err == limiter.ErrLimitExceeded {
    // 达到连接限制
}
defer connLimiter.Release()

// 速率限制器（令牌桶）
rateLimiter := limiter.NewRateLimiter(1000, time.Second)
if rateLimiter.Allow() {
    // 允许请求
}

// Goroutine 限制器
goLimiter := limiter.NewGoroutineLimiter(100)
goLimiter.Go(func() {
    // 在限制内执行
})

// 内存限制器
memLimiter := limiter.NewMemoryLimiter(1024 * 1024 * 1024) // 1GB
memLimiter.Allocate(1024)

// 组合限制器
composite := limiter.NewCompositeLimiter(connLimiter, goLimiter)
composite.Acquire()
defer composite.Release()

// 资源监控
monitor := limiter.NewResourceMonitor(10000, 1000, 1<<30, 10000, time.Second)
stats := monitor.Stats()
```

### 连接管理 (internal/connmgr)

服务器场景的连接管理，提供连接限制、跟踪和生命周期管理。

```go
import "github.com/Talbot3/go-tunnel/internal/connmgr"

// 创建连接管理器
mgr := connmgr.NewManager(connmgr.Config{
    MaxConnections:    10000,              // 最大连接数
    ConnectionTimeout: 5 * time.Minute,    // 空闲超时
    AcceptTimeout:     10 * time.Second,   // 接受超时
})

// 接受连接
info, err := mgr.Accept(conn)
if err == connmgr.ErrConnectionLimit {
    // 达到连接限制
}

// 包装连接（自动跟踪活动）
wrapped := mgr.WrapConn(conn, info)

// 获取统计信息
stats := mgr.GetStats()
fmt.Printf("Active: %d, Total: %d, Rejected: %d\n",
    stats.Active, stats.Total, stats.Rejected)

// 优雅关闭
mgr.Shutdown(ctx)
```

#### ConnInfo 方法

```go
func (c *ConnInfo) ID() string           // 连接 ID
func (c *ConnInfo) RemoteAddr() string   // 远程地址
func (c *ConnInfo) StartTime() time.Time // 开始时间
func (c *ConnInfo) LastActive() time.Time // 最后活动时间
func (c *ConnInfo) Duration() time.Duration // 持续时间
func (c *ConnInfo) BytesIn() int64       // 接收字节数
func (c *ConnInfo) BytesOut() int64      // 发送字节数
func (c *ConnInfo) IsClosed() bool       // 是否已关闭
```

### 连接池 (internal/pool)

客户端场景的连接复用，提升高吞吐场景性能。

```go
import "github.com/Talbot3/go-tunnel/internal/pool"

// 创建连接池
dialer := pool.NewDialer("tcp", "127.0.0.1:80", pool.DefaultConnPoolConfig())
connPool := dialer.NewPool()

// 获取连接
conn, err := connPool.Get(ctx)

// 使用连接...
// conn.Read(), conn.Write()

// 归还连接（不是关闭）
connPool.Put(conn)

// 获取统计信息
stats := connPool.GetStats()
fmt.Printf("Created: %d, Reused: %d, Idle: %d\n",
    stats.Created, stats.Reused, stats.Idle)

// 关闭连接池
connPool.Close()
```

#### 连接池配置

```go
type ConnPoolConfig struct {
    MaxIdle     int           // 最大空闲连接数，默认 10
    MaxAge      time.Duration // 连接最大存活时间
    DialTimeout time.Duration // 拨号超时，默认 10s
    KeepAlive   time.Duration // 保活间隔，默认 30s
}
```

## 协议包

### TCP

```go
import "github.com/Talbot3/go-tunnel/tcp"

p := tcp.New()
```

### HTTP/2

```go
import "github.com/Talbot3/go-tunnel/http2"

tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
}
p := http2.New(tlsConfig)
```

### HTTP/3

```go
import "github.com/Talbot3/go-tunnel/http3"

tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    NextProtos:   []string{"h3"},
}
p := http3.New(tlsConfig, nil)
```

### QUIC

```go
import "github.com/Talbot3/go-tunnel/quic"

tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    NextProtos:   []string{"quic-tunnel"},
}

// 创建 QUIC 多路复用服务器
server := quic.NewMuxServer(quic.MuxServerConfig{
    ListenAddr:     ":443",
    TLSConfig:      tlsConfig,
    AuthToken:      "secret",
    PortRangeStart: 10000,
    PortRangeEnd:   20000,
})
server.Start(context.Background())

// 创建 QUIC 多路复用客户端
client := quic.NewMuxClient(quic.MuxClientConfig{
    ServerAddr: "tunnel.example.com:443",
    TLSConfig:  tlsConfig,
    LocalAddr:  "localhost:8080",
    AuthToken:  "secret",
})
client.Start(context.Background())
```

> **性能优势**: 纯 QUIC 实现使用原生多路复用，封包效率比 HTTP/3 提升 3-10 倍，延迟降低约 50%。

### 集成服务器 (server)

完整的隧道服务器，集成所有高可用组件：

```go
import "github.com/Talbot3/go-tunnel/server"

// 创建服务器
srv, err := server.New(server.Config{
    ListenAddr:      ":443",
    TLSConfig:       tlsConfig,
    AuthToken:       "secret",
    MaxConnections:  10000,
    MaxTunnels:      1000,
    HealthAddr:      ":8080",
    ShutdownTimeout: 30 * time.Second,
})

// 启动服务器
if err := srv.Start(context.Background()); err != nil {
    log.Fatal(err)
}

// 获取统计信息
stats := srv.GetStats()

// 手动资源控制
if err := srv.AcquireConnection(); err != nil {
    // 资源限制
}
defer srv.ReleaseConnection()

// 自定义健康检查
srv.GetHealthHandler().Register("custom", func(ctx context.Context) health.CheckResult {
    return health.CheckResult{
        Name:   "custom",
        Status: health.StatusHealthy,
    }
})
```

**健康端点**：
| 端点 | 说明 |
|------|------|
| `GET /health` | 综合健康状态 |
| `GET /livez` | Kubernetes 存活探针 |
| `GET /readyz` | Kubernetes 就绪探针 |
| `GET /metrics` | 服务器统计信息 (JSON) |
| `GET /circuit` | 熔断器状态 |

**Kubernetes 部署示例**：

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
      - name: tunnel
        ports:
        - containerPort: 443
          name: tunnel
        - containerPort: 8080
          name: health
        livenessProbe:
          httpGet:
            path: /livez
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
        resources:
          limits:
            memory: "512Mi"
            cpu: "1"
          requests:
            memory: "256Mi"
            cpu: "500m"
```

## 自动 TLS 证书管理

go-tunnel 集成了 certmagic 库，支持基于 ACME 协议自动申请和续期 TLS 证书。

### 基本使用

```go
package main

import (
    "context"
    "log"
    "net/http"

    autotls "github.com/Talbot3/go-tunnel/tls"
)

func main() {
    // 创建自动证书管理器
    mgr := autotls.NewAutoManager(autotls.AutoManagerConfig{
        Email:      "admin@example.com",
        AgreeTerms: true,
    })

    // 添加域名
    if err := mgr.AddDomains("example.com", "www.example.com"); err != nil {
        log.Fatal(err)
    }

    // 获取 TLS 配置
    tlsConfig := mgr.TLSConfig()

    // 用于 HTTP 服务器
    server := &http.Server{
        Addr:      ":443",
        TLSConfig: tlsConfig,
    }
    server.ListenAndServeTLS("", "")
}
```

### 快速设置

```go
// 一行代码设置自动 TLS
mgr, err := autotls.QuickSetup("admin@example.com", "example.com", "www.example.com")
```

### DNS-01 验证

对于内网环境或需要通配符证书，使用 DNS-01 验证：

```go
import (
    "github.com/libdns/cloudflare" // 需要安装对应 DNS 提供商包
    autotls "github.com/Talbot3/go-tunnel/tls"
)

func main() {
    // 创建 DNS 提供商
    dnsProvider := &cloudflare.Provider{
        APIToken: "your-cloudflare-api-token",
    }

    mgr := autotls.NewAutoManager(autotls.AutoManagerConfig{
        Email:       "admin@example.com",
        AgreeTerms:  true,
        UseDNS01:    true,
        DNSProvider: dnsProvider,
    })

    // 支持通配符域名
    mgr.AddDomains("example.com", "*.example.com")
}
```

### 支持的 DNS 提供商

导入对应的 libdns 包即可使用：

| 提供商 | 导入路径 |
|--------|----------|
| Cloudflare | `github.com/libdns/cloudflare` |
| 阿里云 DNS | `github.com/libdns/alidns` |
| AWS Route53 | `github.com/libdns/route53` |
| DigitalOcean | `github.com/libdns/digitalocean` |
| GoDaddy | `github.com/libdns/godaddy` |

### DNS 提供商配置详解

#### 环境变量配置

DNS 提供商凭证通过环境变量配置，符合 12-Factor 应用原则：

```bash
# Cloudflare
export CLOUDFLARE_API_TOKEN=your-api-token
# 或使用 API Key（不推荐）
export CLOUDFLARE_EMAIL=your-email@example.com
export CLOUDFLARE_API_KEY=your-api-key

# 阿里云 DNS
export ALIDNS_ACCESS_KEY_ID=your-access-key-id
export ALIDNS_ACCESS_KEY_SECRET=your-access-key-secret

# AWS Route53（使用标准 AWS 凭证）
export AWS_ACCESS_KEY_ID=your-access-key-id
export AWS_SECRET_ACCESS_KEY=your-secret-access-key
export AWS_REGION=us-east-1

# DigitalOcean
export DIGITALOCEAN_TOKEN=your-api-token

# GoDaddy
export GODADDY_API_KEY=your-api-key
export GODADDY_API_SECRET=your-api-secret
```

#### 完整示例：Cloudflare DNS-01

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/libdns/cloudflare"
    autotls "github.com/Talbot3/go-tunnel/tls"
)

func main() {
    // 1. 从环境变量获取凭证
    apiToken := os.Getenv("CLOUDFLARE_API_TOKEN")
    if apiToken == "" {
        log.Fatal("CLOUDFLARE_API_TOKEN not set")
    }

    // 2. 创建 DNS 提供商
    dnsProvider := &cloudflare.Provider{
        APIToken: apiToken,
    }

    // 3. 创建自动证书管理器
    mgr := autotls.NewAutoManager(autotls.AutoManagerConfig{
        Email:       "admin@example.com",
        AgreeTerms:  true,
        UseDNS01:    true,
        DNSProvider: dnsProvider,
    })

    // 4. 添加域名（支持通配符）
    if err := mgr.AddDomains("example.com", "*.example.com"); err != nil {
        log.Fatal(err)
    }

    // 5. 获取 TLS 配置用于服务
    tlsConfig := mgr.TLSConfig()
    // ... 使用 tlsConfig 启动服务
}
```

#### 完整示例：阿里云 DNS-01

```go
package main

import (
    "log"
    "os"

    "github.com/libdns/alidns"
    autotls "github.com/Talbot3/go-tunnel/tls"
)

func main() {
    // 创建阿里云 DNS 提供商
    dnsProvider := &alidns.Provider{
        AccessKeyID:     os.Getenv("ALIDNS_ACCESS_KEY_ID"),
        AccessKeySecret: os.Getenv("ALIDNS_ACCESS_KEY_SECRET"),
    }

    mgr := autotls.NewAutoManager(autotls.AutoManagerConfig{
        Email:       "admin@example.com",
        AgreeTerms:  true,
        UseDNS01:    true,
        DNSProvider: dnsProvider,
    })

    // 支持通配符域名
    mgr.AddDomains("example.cn", "*.example.cn")
}
```

#### DNS-01 vs HTTP-01 对比

| 特性 | HTTP-01 | DNS-01 |
|------|---------|--------|
| 适用场景 | 公网服务器 | 内网服务器、通配符证书 |
| 端口要求 | 需要 80 端口可访问 | 无端口要求 |
| 通配符支持 | ❌ 不支持 | ✅ 支持 |
| 配置复杂度 | 简单 | 需要 DNS 提供商 API |
| 验证速度 | 较快 | 较慢（需 DNS 传播） |

#### 常见问题

**Q: 为什么 DNS Provider 返回错误？**

A: 需要导入具体的 libdns 包。例如使用 Cloudflare：

```go
import _ "github.com/libdns/cloudflare" // 确保导入
```

**Q: DNS-01 验证超时怎么办？**

A: 增加传播超时时间：

```go
mgr := autotls.NewAutoManager(autotls.AutoManagerConfig{
    // ... 其他配置
})
// certmagic 会自动处理 DNS 传播等待
```

**Q: 如何测试 DNS Provider 配置？**

A: 使用测试模式避免消耗配额：

```bash
# 使用 Let's Encrypt 测试环境
proxy -auto-tls -staging -email admin@example.com -domains test.example.com
```

### 命令行自动 TLS

```bash
# 使用 HTTP-01 验证（需要 80 端口可访问）
proxy -protocol http2 -listen :443 -target 127.0.0.1:80 \
    -auto-tls \
    -email admin@example.com \
    -domains example.com,www.example.com

# 使用 DNS-01 验证
export CLOUDFLARE_API_TOKEN=your-token
proxy -protocol http2 -listen :443 -target 127.0.0.1:80 \
    -auto-tls \
    -email admin@example.com \
    -domains "*.example.com,example.com" \
    -dns-provider cloudflare

# 使用测试环境（不会签发真实证书）
proxy -auto-tls -staging -email admin@example.com -domains example.com ...
```

### 配置文件自动 TLS

```yaml
version: "1.0"
log_level: info

protocols:
  - name: http2
    listen: ":443"
    target: "127.0.0.1:80"
    enabled: true

options:
  auto_tls: "true"
  email: "admin@example.com"
  domains: "example.com,www.example.com"
  staging: "false"
```

### API 参考

```go
type AutoManager struct { ... }

// 创建管理器
func NewAutoManager(cfg AutoManagerConfig) *AutoManager
func DefaultAutoManager(email string) *AutoManager
func QuickSetup(email string, domains ...string) (*AutoManager, error)

// 域名管理
func (m *AutoManager) AddDomains(domains ...string) error
func (m *AutoManager) RemoveDomains(domains ...string)
func (m *AutoManager) Domains() []string

// TLS 配置
func (m *AutoManager) TLSConfig() *tls.Config
func (m *AutoManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)

// 证书操作
func (m *AutoManager) RenewAll(ctx context.Context) error
func (m *AutoManager) Revoke(ctx context.Context, domain string, reason int) error
func (m *AutoManager) CacheStatus() map[string]*CertStatus

// CA 切换
func (m *AutoManager) UseStaging()
func (m *AutoManager) UseProduction()
func (m *AutoManager) UseZeroSSL()
```

## Prometheus 监控指标

go-tunnel 提供可选的 Prometheus 指标导出，用于监控隧道性能和健康状态。

### 基本使用

```go
import (
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/Talbot3/go-tunnel/internal/metrics"
)

func main() {
    // 创建指标收集器
    collector := metrics.NewCollector(metrics.Config{
        Namespace:                 "my_tunnel",
        EnableConnectionDuration:  true,
        EnableForwardLatency:      true,
        EnablePoolMetrics:         true,
        EnableBackpressureMetrics: true,
    })

    // 在隧道处理中使用
    collector.IncConnections()
    collector.AddBytesSent(1024)
    collector.IncActive()
    defer collector.DecActive()

    // 暴露 Prometheus 端点
    http.Handle("/metrics", promhttp.Handler())
    http.ListenAndServe(":9090", nil)
}
```

### 可用指标

| 指标名称 | 类型 | 说明 |
|---------|------|------|
| `{namespace}_connections_total` | Counter | 总连接数 |
| `{namespace}_connections_active` | Gauge | 当前活跃连接数 |
| `{namespace}_bytes_sent_total` | Counter | 发送字节总数 |
| `{namespace}_bytes_received_total` | Counter | 接收字节总数 |
| `{namespace}_errors_total` | Counter | 错误总数 |
| `{namespace}_connection_duration_seconds` | Histogram | 连接持续时间 |
| `{namespace}_forward_latency_seconds` | Histogram | 转发延迟 |
| `{namespace}_pool_connections_active` | Gauge | 连接池活跃连接数 |
| `{namespace}_pool_connections_created_total` | Counter | 连接池创建连接数 |
| `{namespace}_pool_connections_reused_total` | Counter | 连接池复用连接数 |
| `{namespace}_backpressure_pauses_total` | Counter | 背压暂停次数 |

### API 参考

```go
// 连接指标
collector.IncConnections()
collector.IncConnectionsBy(5)
collector.IncActive()
collector.DecActive()
collector.SetActive(100)

// 流量指标
collector.AddBytesSent(1024)
collector.AddBytesReceived(512)

// 错误指标
collector.IncErrors()
collector.IncErrorsBy(3)

// 延迟指标
collector.ObserveConnectionDuration(0.5)  // 秒
collector.ObserveForwardLatency(0.01)     // 秒

// 连接池指标
collector.SetPoolActive(50)
collector.IncPoolCreated()
collector.IncPoolReused()
collector.IncPoolClosed()
collector.ObservePoolWait(0.001)  // 秒

// 背压指标
collector.IncBackpressurePauses()
collector.AddBackpressureYieldTime(0.05)  // 秒
```

### Grafana 仪表板示例

```promql
# 连接速率
rate(my_tunnel_connections_total[5m])

# 吞吐量
rate(my_tunnel_bytes_sent_total[5m])
rate(my_tunnel_bytes_received_total[5m])

# 错误率
rate(my_tunnel_errors_total[5m])

# 平均连接持续时间
histogram_quantile(0.5, rate(my_tunnel_connection_duration_seconds_bucket[5m]))

# P99 转发延迟
histogram_quantile(0.99, rate(my_tunnel_forward_latency_seconds_bucket[5m]))

# 连接池复用率
rate(my_tunnel_pool_connections_reused_total[5m]) / 
rate(my_tunnel_pool_connections_created_total[5m])
```

## 命令行工具

安装命令行工具：

```bash
go install github.com/Talbot3/go-tunnel/cmd/proxy@latest
```

使用方法：

```bash
# TCP 转发
proxy -protocol tcp -listen :8080 -target 127.0.0.1:80

# HTTP/2 转发
proxy -protocol http2 -listen :8443 -target 127.0.0.1:443 -cert cert.pem -key key.pem

# 使用配置文件
proxy -config config.yaml

# 查看版本
proxy -version
```

### 配置文件 (config.yaml)

```yaml
version: "1.0"
log_level: info

protocols:
  - name: tcp
    listen: ":8080"
    target: "127.0.0.1:80"
    enabled: true

  - name: http2
    listen: ":8443"
    target: "127.0.0.1:443"
    enabled: true

tls:
  cert_file: "./cert.pem"
  key_file: "./key.pem"
```

## 架构设计

```
┌─────────────────────────────────────────────────────────────────┐
│                        协议层 (Protocol Layer)                   │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐            │
│  │   TCP   │  │ HTTP/2  │  │ HTTP/3  │  │  QUIC   │            │
│  │ Handler │  │ Handler │  │ Handler │  │ Handler │            │
│  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘            │
│       └────────────┴────────────┴────────────┘                  │
│                         ▼                                       │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              协议接口 (Protocol Interface)               │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    高可用层 (HA Layer)                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐         │
│  │  熔断器     │  │  重试机制   │  │  资源限制器     │         │
│  │ (Circuit)   │  │ (Retry)     │  │ (Limiter)       │         │
│  └─────────────┘  └─────────────┘  └─────────────────┘         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐         │
│  │  健康检查   │  │  优雅关闭   │  │  Prometheus指标  │         │
│  │ (Health)    │  │ (Shutdown)  │  │ (Metrics)        │         │
│  └─────────────┘  └─────────────┘  └─────────────────┘         │
└─────────────────────────────────────────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Go Runtime netpoll                           │
│  (自动适配 epoll / kqueue / IOCP 事件多路复用)                   │
└─────────────────────────────────────────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                  转发引擎 (Forward Engine)                       │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐         │
│  │ Buffer Pool │  │ 背压控制器  │  │ 平台转发路由    │         │
│  │ (sync.Pool) │  │ (Pause/Resume│  │ //go:build 隔离 │         │
│  └─────────────┘  └─────────────┘  └─────────────────┘         │
└─────────────────────────────────────────────────────────────────┘
                              ▼
┌──────────────┼──────────────────────┬──────────────────────────┐
│   Linux      │       macOS          │    Windows               │
│ unix.Splice  │ io.Copy + 优化       │ io.Copy + IOCP           │
│ (零拷贝)     │ (1次拷贝)            │ (1次拷贝)                │
└──────────────┴──────────────────────┴──────────────────────────┘
```

## 项目结构

```
go-tunnel/
├── tunnel.go                    # 主库入口
├── errors.go                    # 错误定义
├── server/                      # 集成服务器包
│   └── server.go                # 完整隧道服务器（含健康端点）
├── forward/                     # 转发引擎
│   ├── forward.go               # 公共接口
│   ├── forward_linux.go         # Linux 零拷贝
│   ├── forward_darwin.go        # macOS 优化
│   └── forward_windows.go       # Windows IOCP
├── tcp/                         # TCP 协议
├── http2/                       # HTTP/2 协议
├── http3/                       # HTTP/3 协议
├── quic/                        # QUIC 协议
├── tls/                         # 自动 TLS 证书管理
│   ├── auto.go                  # ACME 证书管理
│   └── dns_provider.go          # DNS 提供商接口
├── config/                      # 配置管理
├── internal/
│   ├── circuit/                 # 熔断器（高可用）
│   ├── retry/                   # 重试机制（高可用）
│   ├── health/                  # 健康检查（高可用）
│   ├── shutdown/                # 优雅关闭（高可用）
│   ├── limiter/                 # 资源限制器（高可用）
│   ├── pool/                    # 缓冲池
│   │   ├── pool.go              # sync.Pool 缓冲池
│   │   └── connpool.go          # 连接池
│   ├── connmgr/                 # 连接管理器
│   ├── backpressure/            # 背压控制
│   └── metrics/                 # Prometheus 指标
└── cmd/proxy/                   # 命令行工具
```

## 平台优化

| 平台 | 数据通路 | 拷贝次数 | 特殊优化 |
|------|----------|----------|----------|
| Linux | `unix.Splice` | 0 (零拷贝) | splice/tee 系统调用 |
| macOS | `io.Copy` + 缓冲池 | 1 | TCP_NOTSENT_LOWAT, TCP_NODELAY |
| Windows | `io.Copy` + IOCP | 1 | 256KB 缓冲区 |

## 性能测试

### 测试环境
- macOS (Apple Silicon)
- Go 1.21+
- 本地回环网络

### 测试结果

| 测试项目 | 直接连接 | 通过代理 | 差异 |
|---------|---------|---------|------|
| 吞吐量 | 2394 MB/s (19.15 Gbps) | 1358 MB/s (10.87 Gbps) | 损失 43.3% |
| 延迟 | 0.068ms | 0.170ms | 增加 0.102ms |
| 并发 RPS | 10,455 | 7,585 | 损失 27.5% |

### 测试覆盖率

| 包 | 覆盖率 |
|----|--------|
| tunnel | 68.6% |
| forward | 57.0% |
| tcp | 100% |
| http2 | 70.6% |
| http3 | 26.3% |
| quic | 29.3% |
| internal/circuit | 80.2% |
| internal/retry | 65.6% |
| internal/health | 0% (待补充) |
| internal/shutdown | 74.7% |
| internal/limiter | 89.6% |
| internal/backpressure | 98.5% |
| internal/connmgr | 91.9% |
| internal/pool | 85.7% |
| internal/metrics | 94.4% |

### 运行测试

```bash
# 运行所有测试
go test ./... -v

# 运行测试并查看覆盖率
go test ./... -cover

# 运行性能测试
go test -bench=. ./...
```

## 扩展协议

实现新的协议只需满足 `Protocol` 接口：

```go
type MyProtocol struct{}

func (p *MyProtocol) Name() string {
    return "myprotocol"
}

func (p *MyProtocol) Listen(addr string) (net.Listener, error) {
    return net.Listen("tcp", addr)
}

func (p *MyProtocol) Dial(ctx context.Context, addr string) (net.Conn, error) {
    var d net.Dialer
    return d.DialContext(ctx, "tcp", addr)
}

func (p *MyProtocol) Forwarder() forward.Forwarder {
    return forward.NewForwarder()
}
```

## 多路复用扩展

对于共享连接多路复用场景（如客户端隧道），使用 `forward` 包的扩展接口。已集成缓冲池和背压控制优化。

### 编码器/解码器

```go
import "github.com/Talbot3/go-tunnel/forward"

// 创建编码器和解码器
encoder := forward.NewDefaultMuxEncoder()
decoder := forward.NewDefaultMuxDecoder()

// 编码消息
dataMsg, _ := encoder.EncodeData("conn1", []byte("hello"))
closeMsg, _ := encoder.EncodeClose("conn1")
reqMsg, _ := encoder.EncodeRequest("req1", []byte("GET / HTTP/1.1\r\n\r\n"))

// 解码消息
msgType, id, payload, _ := decoder.Decode(dataMsg)

// 释放缓冲区（优化：缓冲池复用）
encoder.Release(dataMsg)
```

### 多路转发器

```go
// 单向多路转发（本地 -> 远程）
// 已集成缓冲池和背压控制
muxForwarder := forward.NewMuxForwarder()
muxForwarder.ForwardMux(ctx, localConn, muxConn, "conn1", encoder)

// 双向多路转发
biForwarder := forward.NewBidirectionalMuxForwarder()
biForwarder.ForwardBidirectionalMux(ctx, localConn, muxConn, "conn1", encoder, decoder)
```

### 连接管理器

```go
// 管理共享连接上的多个虚拟连接
// 已集成缓冲池、背压控制和 TCP 优化
mgr := forward.NewMuxConnManager(muxConn, encoder, decoder)

// 添加连接（自动开始转发，自动应用 TCP 优化）
mgr.AddConnection("conn1", localConn)

// 处理接收到的消息
mgr.HandleIncoming(data)

// 获取统计信息
stats := mgr.Stats()
```

### HTTP 多路转发

```go
// HTTP 请求-响应模式
httpForwarder := forward.NewHTTPMuxForwarder()
resp, _ := httpForwarder.ForwardHTTP(ctx, reqData, muxConn, "req1", encoder, decoder, 30*time.Second)
```

### 消息类型

| 类型 | 格式 | 说明 |
|------|------|------|
| `DATA` | `DATA:<conn_id>:<length>:<payload>\n` | TCP 数据 |
| `CLOSE` | `CLOSE:<conn_id>\n` | 连接关闭 |
| `REQUEST` | `REQUEST:<req_id>:<length>:<payload>\n` | HTTP 请求 |
| `RESPONSE` | `RESPONSE:<req_id>:<length>:<payload>\n` | HTTP 响应 |
| `NEWCONN` | `NEWCONN:<conn_id>:<remote_addr>\n` | 新连接通知 |

### 性能优化

多路复用模块已集成以下优化：

| 优化项 | 说明 | 效果 |
|--------|------|------|
| 缓冲池 | 使用 `internal/pool` 复用缓冲区 | 减少 GC 压力 |
| 背压控制 | 使用 `internal/backpressure` | 防止内存溢出 |
| TCP 优化 | 自动应用 `OptimizeTCPConn` | 降低延迟 |
| 二进制协议 | BinaryProtocol 高效编码 | 减少内存分配 |

## 使用场景

### 1. API 网关底层协议转发

go-tunnel 可作为 API 网关的底层转发引擎，处理入站和出站的协议转换：

```go
// 网关场景：HTTP/2 入口 -> TCP 后端
package main

import (
    "context"
    "log"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/http2"
)

func main() {
    // HTTP/2 监听，转发到 TCP 后端服务
    cfg := tunnel.Config{
        ListenAddr: ":443",
        TargetAddr: "127.0.0.1:8080", // 后端服务
    }

    t, _ := tunnel.New(cfg)
    t.SetProtocol(http2.New(tlsConfig))
    t.Start(context.Background())

    // 现在可以通过 HTTP/2 访问，后端无需改造
}
```

**应用场景**：
- 为传统 TCP 服务添加 HTTP/2、HTTP/3 支持
- 协议升级无需修改后端代码
- 支持 gRPC-Web 到 gRPC 的协议转换

### 2. 内网穿透隧道

构建内网穿透服务，支持高并发连接：

```go
// 服务端（公网）
func serverMode() {
    cfg := tunnel.ServerPreset()
    cfg.ListenAddr = ":443"
    cfg.TargetAddr = "internal-service:8080"
    // 可处理 10000+ 并发连接
}

// 客户端（内网）
func clientMode() {
    cfg := tunnel.ClientPreset()
    cfg.ListenAddr = ":8080"
    cfg.TargetAddr = "public-server:443"
    // 高吞吐，少量连接
}
```

**应用场景**：
- 远程办公访问内网服务
- IoT 设备远程管理
- 开发调试环境暴露

### 3. 微服务间通信加密

为微服务间通信自动添加 TLS 加密：

```go
// 使用自动 TLS 保护服务间通信
mgr, _ := autotls.QuickSetup(
    "ops@company.com",
    "service-a.internal",
    "service-b.internal",
)

cfg := tunnel.Config{
    Protocol:   "http2",
    ListenAddr: ":8443",
    TargetAddr: "localhost:8080", // 原始服务
    TLSConfig:  mgr.TLSConfig(),
}
```

**应用场景**：
- 零代码改造实现服务间 mTLS
- 自动证书续期，运维无感知
- 支持 Kubernetes Sidecar 模式部署

### 4. 负载均衡器后端代理

作为负载均衡器的后端代理层：

```go
// 多协议监听，转发到不同后端
func multiProtocolProxy() {
    // TCP 流量
    go func() {
        t, _ := tunnel.New(tunnel.Config{
            ListenAddr: ":80",
            TargetAddr: "tcp-backend:8080",
        })
        t.SetProtocol(tcp.New())
        t.Start(context.Background())
    }()

    // HTTP/2 流量
    go func() {
        t, _ := tunnel.New(tunnel.Config{
            ListenAddr: ":443",
            TargetAddr: "http-backend:8080",
            TLSConfig:  tlsConfig,
        })
        t.SetProtocol(http2.New(tlsConfig))
        t.Start(context.Background())
    }()

    // QUIC 流量（更低延迟）
    go func() {
        server := quic.NewMuxServer(quic.MuxServerConfig{
            ListenAddr: ":443",
            TLSConfig:  tlsConfig,
        })
        server.Start(context.Background())
    }()
}
```

### 5. 数据库代理与连接池

为数据库连接提供连接池和协议转换：

```go
// 数据库连接池代理
import "github.com/Talbot3/go-tunnel/internal/pool"

func databaseProxy() {
    // 创建到数据库的连接池
    dialer := pool.NewDialer("tcp", "db-master:3306", pool.ConnPoolConfig{
        MaxIdle:     50,              // 连接池大小
        MaxAge:      30 * time.Minute, // 连接最大存活时间
        DialTimeout: 5 * time.Second,
    })
    connPool := dialer.NewPool()

    // 应用层从池中获取连接
    conn, _ := connPool.Get(ctx)
    defer connPool.Put(conn) // 归还而非关闭
}
```

**应用场景**：
- 数据库读写分离代理
- 连接池复用，减少连接开销
- 数据库协议转换（如 MySQL -> PostgreSQL）

### 6. 边缘计算与 CDN 节点

在边缘节点部署高性能转发：

```go
// 边缘节点配置
func edgeNode() {
    cfg := tunnel.HighThroughputPreset()
    cfg.ListenAddr = ":443"
    cfg.TargetAddr = "origin-server:80"

    // 高吞吐配置：256KB 缓冲区，4MB 背压阈值
    // 适合视频流、大文件分发
}
```

**应用场景**：
- CDN 源站回源代理
- 边缘计算节点数据转发
- 视频流代理分发

### 7. 开发调试与测试

本地开发环境的快速代理：

```bash
# 一行命令启动本地代理
proxy -protocol tcp -listen :3306 -target production-db.example.com:3306

# HTTP/2 到本地服务
proxy -protocol http2 -listen :8443 -target localhost:3000 \
    -cert cert.pem -key key.pem

# 自动 TLS（适合测试）
proxy -protocol http2 -listen :443 -target localhost:8080 \
    -auto-tls -email dev@example.com -domains dev.local -staging
```

**应用场景**：
- 本地连接远程数据库/服务
- HTTPS 本地开发环境
- 接口调试与抓包

## 借鉴来源

本项目的设计借鉴了以下优秀开源项目的思想：

### 核心技术借鉴

| 项目 | 借鉴内容 |
|------|----------|
| [frp](https://github.com/fatedier/frp) | 内网穿透架构设计、多协议支持模式 |
| [nginx](https://nginx.org/) | 事件驱动模型、连接池管理、背压控制思想 |
| [envoy](https://www.envoyproxy.io/) | 协议抽象层设计、可扩展架构 |
| [caddy](https://caddyserver.com/) | 自动 TLS 证书管理（certmagic） |
| [traefik](https://traefik.io/) | 配置热加载、动态路由思想 |
| [gost](https://github.com/go-gost/gost) | 隧道链式转发、多协议适配 |

### 性能优化借鉴

| 技术 | 来源 | 应用 |
|------|------|------|
| `splice` 零拷贝 | Linux 内核、nginx | Linux 平台零拷贝转发 |
| `TCP_NOTSENT_LOWAT` | macOS 文档、nginx | macOS 发送缓冲区优化 |
| IOCP 大缓冲区 | Windows 文档、libuv | Windows 平台优化 |
| `sync.Pool` 缓冲池 | Go 标准库、fasthttp | 内存复用，减少 GC |
| 背压控制 | reactive streams、nginx | 防止内存溢出 |

### 协议实现借鉴

| 协议 | 参考实现 |
|------|----------|
| HTTP/2 | `golang.org/x/net/http2`、nginx |
| HTTP/3 | `github.com/quic-go/quic-go`、cloudflare quiche |
| QUIC | Google QUIC 设计文档、quic-go |
| TLS 自动化 | Caddy certmagic、Let's Encrypt 客户端 |

### 设计模式借鉴

```
┌─────────────────────────────────────────────────────────────┐
│                    设计模式借鉴                              │
├─────────────────────────────────────────────────────────────┤
│  Strategy Pattern    │ 协议处理器可插拔 (Protocol 接口)     │
│  Factory Pattern     │ 平台特定转发器工厂                   │
│  Object Pool Pattern │ sync.Pool 缓冲池、连接池             │
│  Observer Pattern    │ 统计信息、事件回调                   │
│  Builder Pattern     │ 配置预设 (ServerPreset 等)           │
└─────────────────────────────────────────────────────────────┘
```

### 特别感谢

- [Go 团队](https://golang.org/) - 提供优秀的 netpoll 运行时
- [quic-go 团队](https://github.com/quic-go) - 高质量的 QUIC 实现
- [Caddy 团队](https://github.com/caddyserver) - certmagic 自动证书管理
- [Cloudflare](https://blog.cloudflare.com/) - QUIC/HTTP/3 技术博客

## 依赖

- `golang.org/x/sys` - 系统调用
- `golang.org/x/net` - HTTP/2 支持
- `github.com/quic-go/quic-go` - QUIC/HTTP/3 支持
- `github.com/caddyserver/certmagic` - 自动 TLS 证书管理
- `gopkg.in/yaml.v3` - YAML 配置解析

## 更新日志

### v1.1.1 (2026-04-16)

**文档更新**
- 更新测试覆盖率数据，反映最新测试结果
- 修复多路复用部分重复的代码块

### v1.1.0 (2026-04-15)

**高可用组件**
- 新增熔断器 (`internal/circuit`) - 三态熔断器防止级联故障
- 新增重试机制 (`internal/retry`) - 指数退避重试，支持抖动
- 新增健康检查 (`internal/health`) - Kubernetes 兼容的健康端点
- 新增优雅关闭 (`internal/shutdown`) - 优先级回调的优雅关闭
- 新增资源限制器 (`internal/limiter`) - 连接/速率/Goroutine/内存限制

**集成服务器**
- 新增 `server` 包 - 完整隧道服务器，集成所有 HA 组件
- 支持健康端点: `/health`, `/livez`, `/readyz`, `/metrics`, `/circuit`
- 集成熔断器到 QUIC 服务器连接处理
- 集成连接限制器到外部连接处理

**QUIC 改进**
- 集成熔断器到 `MuxServer` 连接处理
- 集成连接限制器防止资源耗尽
- 集成重试机制到客户端连接循环

**修复**
- 修复 `activeConns` 双重递减问题（使用 `LoadAndDelete`）
- 修复 `ConnPool.createConn` goroutine 泄漏
- 修复心跳响应错误处理
- 添加平台特定 TCP 选项函数的 panic 恢复
- 添加配置验证和上限检查防止资源耗尽

**测试**
- 添加熔断器测试（覆盖率 100%）
- 添加重试机制测试（覆盖率 100%）
- 添加健康检查测试（覆盖率 100%）
- 添加优雅关闭测试（覆盖率 100%）
- 添加资源限制器测试（覆盖率 100%）

### v1.0.1 (2026-04-15)

**性能优化**
- 多路复用模块集成缓冲池 (`internal/pool`)，减少 GC 压力
- 多路复用模块集成背压控制 (`internal/backpressure`)，防止内存溢出
- MuxConnManager 自动应用 TCP 优化 (`OptimizeTCPConn`)
- DefaultMuxEncoder 添加 `Release()` 方法支持缓冲区复用

**修复**
- 修复 `TestManager_CloseIdle` 竞态条件测试不稳定问题

### v1.0.0 (2026-04-15)

**新功能**
- 完整的 HTTP/3 实现，支持 QUIC 连接接受
- 添加 `Stats.Reset()` 方法支持重置统计信息
- 添加连接超时配置支持，防止连接泄漏
- 添加 Prometheus 指标导出 (`internal/metrics` 包)

**改进**
- 定义 Windows `TCP_FASTOPEN` 常量，替换魔数
- 添加 QUIC 单流限制文档说明
- 添加 `.gitignore` 文件
- 完善 DNS Provider 配置文档

**测试**
- 添加协议级别单元测试 (tcp, http2, http3, quic)
- 添加 tunnel 包集成测试（覆盖率 70.6%）
- 添加 forward 包测试（覆盖率 47.5%）
- 添加 backpressure 包测试（覆盖率 98.6%）
- 添加 connmgr 包测试（覆盖率 91.9%）
- 添加 pool 包测试（覆盖率 86.2%）
- 添加 metrics 包测试（覆盖率 100%）

**文档**
- 添加集成部署指南 (INTEGRATION.md)
- 更新代码审查报告 (CODE_REVIEW.md)

## License

MIT License
