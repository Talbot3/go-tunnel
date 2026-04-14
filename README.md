# go-tunnel

[![Go Reference](https://pkg.go.dev/badge/github.com/Talbot3/go-tunnel.svg)](https://pkg.go.dev/github.com/Talbot3/go-tunnel)

跨平台高性能多协议转发库，支持 TCP、HTTP/2、HTTP/3、QUIC 协议。

## 特性

- **多协议支持**: TCP、HTTP/2、HTTP/3、QUIC
- **跨平台优化**:
  - Linux: `unix.Splice` 零拷贝
  - macOS: TCP_NOTSENT_LOWAT 优化
  - Windows: IOCP 大缓冲区优化
- **高性能**: 10+ Gbps 吞吐量，亚毫秒级延迟
- **背压控制**: 自动流量控制，防止内存溢出
- **连接池**: sync.Pool 缓冲池复用
- **连接管理**: 服务器场景连接限制、跟踪、生命周期管理
- **连接复用**: 客户端场景连接池复用，提升吞吐
- **配置预设**: Server/Client/HighThroughput 预设优化
- **自动 TLS**: 基于 ACME 协议自动申请/续期证书

## 安装

```bash
go get github.com/Talbot3/go-tunnel
```

## 快速开始

### 基本使用

```go
package main

import (
    "context"
    "log"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/tcp"
)

func main() {
    // 创建 TCP 协议处理器
    p := tcp.New()

    // 配置隧道
    cfg := tunnel.Config{
        ListenAddr: ":8080",
        TargetAddr: "127.0.0.1:80",
    }

    // 创建隧道实例
    t, err := tunnel.New(cfg)
    if err != nil {
        log.Fatal(err)
    }

    // 设置协议处理器
    t.SetProtocol(p)

    // 启动隧道
    if err := t.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer t.Stop()

    // 获取统计信息
    stats := t.Stats()
    log.Printf("Connections: %d, Bytes: %d",
        stats.Connections(),
        stats.BytesSent() + stats.BytesReceived())

    select {} // 阻塞等待
}
```

### 简单转发

```go
package main

import (
    "github.com/Talbot3/go-tunnel"
)

func main() {
    // 监听端口
    listener, _ := tunnel.Listen("tcp", ":8080")

    for {
        src, _ := listener.Accept()
        dst, _ := tunnel.Dial("tcp", "127.0.0.1:80")

        // 双向转发
        go tunnel.Forward(src, dst)
    }
}
```

### 使用 HandlePair

```go
package main

import (
    "github.com/Talbot3/go-tunnel"
)

func main() {
    listener, _ := tunnel.Listen("tcp", ":8080")

    for {
        src, _ := listener.Accept()
        dst, _ := tunnel.Dial("tcp", "127.0.0.1:80")

        // HandlePair 会自动关闭连接
        go tunnel.HandlePair(src, dst)
    }
}
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

## 依赖

- `golang.org/x/sys` - 系统调用
- `golang.org/x/net` - HTTP/2 支持
- `github.com/quic-go/quic-go` - QUIC/HTTP/3 支持
- `github.com/caddyserver/certmagic` - 自动 TLS 证书管理
- `gopkg.in/yaml.v3` - YAML 配置解析

## License

MIT License
