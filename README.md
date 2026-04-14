# go-tunnel: 企业级云原生多协议转发引擎

[![Go Reference](https://pkg.go.dev/badge/github.com/Talbot3/go-tunnel.svg)](https://pkg.go.dev/github.com/Talbot3/go-tunnel)

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
    NextProtos:   []string{"quic-proxy"},
}
p := quic.New(tlsConfig, quic.DefaultConfig())
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
│   ├── pool/                    # 缓冲池
│   │   ├── pool.go              # sync.Pool 缓冲池
│   │   └── connpool.go          # 连接池
│   ├── connmgr/                 # 连接管理器
│   └── backpressure/            # 背压控制
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

### 运行测试

```bash
# 端到端测试
./test_e2e.sh

# 性能测试
./benchmark.sh
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
        t, _ := tunnel.New(tunnel.Config{
            ListenAddr: ":443",
            TargetAddr: "quic-backend:8080",
            TLSConfig:  tlsConfig,
        })
        t.SetProtocol(quic.New(tlsConfig, nil))
        t.Start(context.Background())
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

## License

MIT License
