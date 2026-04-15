# go-tunnel 集成部署指南

go-tunnel 是一个嵌入式高性能转发库，用户将其集成到自己的应用中实现各种转发场景。本文档说明如何在不同场景下集成和部署。

---

## 集成模式概览

```
┌─────────────────────────────────────────────────────────────┐
│                      用户应用                                │
│  ┌───────────────────────────────────────────────────────┐  │
│  │                    业务逻辑                            │  │
│  └───────────────────────────────────────────────────────┘  │
│                           │                                 │
│                           ▼                                 │
│  ┌───────────────────────────────────────────────────────┐  │
│  │                 go-tunnel 库                          │  │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  │  │
│  │  │   TCP   │  │ HTTP/2  │  │ HTTP/3  │  │  QUIC   │  │  │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘  │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

---

## 场景一：API 网关集成

### 集成示例

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/http2"
    "github.com/Talbot3/go-tunnel/http3"
    autotls "github.com/Talbot3/go-tunnel/tls"
)

func main() {
    // 1. 自动 TLS 证书管理
    tlsMgr, err := autotls.QuickSetup(
        "ops@company.com",
        "api.example.com",
    )
    if err != nil {
        log.Fatal(err)
    }

    // 2. 创建 HTTP/2 转发隧道
    cfg := tunnel.Config{
        ListenAddr: ":443",
        TargetAddr: "localhost:8080", // 后端服务
        TLSConfig:  tlsMgr.TLSConfig(),
    }

    t, err := tunnel.New(cfg)
    if err != nil {
        log.Fatal(err)
    }
    t.SetProtocol(http2.New(cfg.TLSConfig))

    // 3. 启动隧道
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    if err := t.Start(ctx); err != nil {
        log.Fatal(err)
    }

    log.Printf("API Gateway started on :443 -> localhost:8080")

    // 4. 健康检查端点（用户自行实现）
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        stats := t.Stats()
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(fmt.Sprintf("Connections: %d", stats.Connections())))
    })
    go http.ListenAndServe(":9090", nil)

    // 5. 优雅关闭
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    log.Println("Shutting down...")
    t.Stop()
}
```

### 部署方式

```dockerfile
# Dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -o gateway ./cmd/gateway

FROM alpine:3.19
COPY --from=builder /app/gateway /usr/local/bin/
EXPOSE 443 9090
ENTRYPOINT ["gateway"]
```

```yaml
# kubernetes.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway
spec:
  replicas: 3
  selector:
    matchLabels:
      app: api-gateway
  template:
    metadata:
      labels:
        app: api-gateway
    spec:
      containers:
        - name: gateway
          image: api-gateway:latest
          ports:
            - containerPort: 443
            - containerPort: 9090
          livenessProbe:
            httpGet:
              path: /health
              port: 9090
          readinessProbe:
            httpGet:
              path: /health
              port: 9090
```

---

## 场景二：内网穿透客户端/服务端

### 服务端集成

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/quic"
    autotls "github.com/Talbot3/go-tunnel/tls"
)

func main() {
    // 自动 TLS
    tlsMgr, _ := autotls.QuickSetup("admin@example.com", "tunnel.example.com")

    // 服务器预设：高并发
    cfg := tunnel.ServerPreset()
    cfg.ListenAddr = ":443"
    cfg.TargetAddr = "localhost:8080" // 本地服务
    cfg.TLSConfig = tlsMgr.TLSConfig()

    t, _ := tunnel.New(cfg)
    t.SetProtocol(quic.New(cfg.TLSConfig, quic.DefaultConfig()))

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    t.Start(ctx)
    log.Println("Tunnel server started on :443")

    // 等待信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    t.Stop()
}
```

### 客户端集成

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    // 客户端预设：高吞吐
    cfg := tunnel.ClientPreset()
    cfg.ListenAddr = ":8080" // 本地监听
    cfg.TargetAddr = "tunnel.example.com:443"
    cfg.TLSConfig = &tls.Config{
        InsecureSkipVerify: false,
        NextProtos:         []string{"quic-proxy"},
    }

    t, _ := tunnel.New(cfg)
    t.SetProtocol(quic.New(cfg.TLSConfig, quic.DefaultConfig()))

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    t.Start(ctx)
    log.Println("Tunnel client started on :8080")

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    t.Stop()
}
```

---

## 场景三：微服务 Sidecar 代理

### 集成示例

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/tcp"
)

func main() {
    // 业务服务
    go startBusinessService()

    // Sidecar 代理
    cfg := tunnel.Config{
        ListenAddr: ":8443",
        TargetAddr: "localhost:8080",
    }

    t, _ := tunnel.New(cfg)
    t.SetProtocol(tcp.New())

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    t.Start(ctx)
    log.Println("Sidecar proxy started on :8443 -> localhost:8080")

    // Prometheus 指标端点
    http.Handle("/metrics", promhttp.Handler())
    go http.ListenAndServe(":9090", nil)

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    t.Stop()
}

func startBusinessService() {
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Hello from business service"))
    })
    http.ListenAndServe(":8080", nil)
}
```

---

## 场景四：数据库代理

### 集成示例

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/internal/pool"
    "github.com/Talbot3/go-tunnel/tcp"
)

func main() {
    // 创建连接池
    dialer := pool.NewDialer("tcp", "mysql-master:3306", pool.ConnPoolConfig{
        MaxIdle:     50,
        MaxAge:      30 * time.Minute,
        DialTimeout: 5 * time.Second,
        KeepAlive:   30 * time.Second,
    })
    connPool := dialer.NewPool()
    defer connPool.Close()

    // 代理配置
    cfg := tunnel.Config{
        ListenAddr:        ":3306",
        TargetAddr:        "mysql-master:3306",
        MaxConnections:    1000,
        ConnectionTimeout: 10 * time.Minute,
    }

    t, _ := tunnel.New(cfg)
    t.SetProtocol(tcp.New())

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    t.Start(ctx)
    log.Println("MySQL proxy started on :3306")

    // 统计信息
    go func() {
        for {
            time.Sleep(30 * time.Second)
            stats := connPool.GetStats()
            log.Printf("Pool stats: created=%d, reused=%d, idle=%d",
                stats.Created, stats.Reused, stats.Idle)
        }
    }()

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    t.Stop()
}
```

---

## 场景五：多协议网关

### 集成示例

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "os"
    "os/signal"
    "sync"
    "syscall"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/http2"
    "github.com/Talbot3/go-tunnel/http3"
    "github.com/Talbot3/go-tunnel/quic"
    "github.com/Talbot3/go-tunnel/tcp"
)

func main() {
    tlsConfig := loadTLSConfig()
    targetAddr := "localhost:8080"

    var tunnels []*tunnel.Tunnel
    var wg sync.WaitGroup

    // TCP 转发
    wg.Add(1)
    go func() {
        defer wg.Done()
        t, _ := tunnel.New(tunnel.Config{
            ListenAddr: ":80",
            TargetAddr: targetAddr,
        })
        t.SetProtocol(tcp.New())
        t.Start(context.Background())
        tunnels = append(tunnels, t)
        log.Println("TCP tunnel started on :80")
    }()

    // HTTP/2 转发
    wg.Add(1)
    go func() {
        defer wg.Done()
        t, _ := tunnel.New(tunnel.Config{
            ListenAddr: ":443",
            TargetAddr: targetAddr,
            TLSConfig:  tlsConfig,
        })
        t.SetProtocol(http2.New(tlsConfig))
        t.Start(context.Background())
        tunnels = append(tunnels, t)
        log.Println("HTTP/2 tunnel started on :443")
    }()

    // HTTP/3 转发
    wg.Add(1)
    go func() {
        defer wg.Done()
        tlsConfigH3 := tlsConfig.Clone()
        tlsConfigH3.NextProtos = []string{"h3"}
        t, _ := tunnel.New(tunnel.Config{
            ListenAddr: ":443",
            TargetAddr: targetAddr,
            TLSConfig:  tlsConfigH3,
        })
        t.SetProtocol(http3.New(tlsConfigH3, nil))
        t.Start(context.Background())
        tunnels = append(tunnels, t)
        log.Println("HTTP/3 tunnel started on :443 (UDP)")
    }()

    wg.Wait()

    // 优雅关闭
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    log.Println("Shutting down...")
    for _, t := range tunnels {
        t.Stop()
    }
}

func loadTLSConfig() *tls.Config {
    // 加载 TLS 配置
    return &tls.Config{
        Certificates: []tls.Certificate{ /* ... */ },
    }
}
```

---

## 监控集成

### Prometheus 指标

```go
package main

import (
    "net/http"

    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/Talbot3/go-tunnel/internal/metrics"
)

func main() {
    // 创建指标收集器
    collector := metrics.NewCollector(metrics.Config{
        Namespace: "my_app",
    })

    // 在转发时记录指标
    collector.IncConnections()
    collector.AddBytesSent(1024)

    // 暴露指标端点
    http.Handle("/metrics", promhttp.Handler())
    http.ListenAndServe(":9090", nil)
}
```

### 自定义统计

```go
func monitorTunnel(t *tunnel.Tunnel) {
    ticker := time.NewTicker(10 * time.Second)
    for range ticker.C {
        stats := t.Stats()
        log.Printf("Connections: %d, Bytes: sent=%d recv=%d, Errors: %d",
            stats.Connections(),
            stats.BytesSent(),
            stats.BytesReceived(),
            stats.Errors(),
        )
    }
}
```

---

## 场景六：客户端-服务器隧道架构

go-tunnel 作为底层转发引擎，可以构建类似 ngrok/frp 的客户端-服务器隧道系统。本节详细描述实现方案。

### 架构概览

```
                              公网服务器端
┌─────────────────────────────────────────────────────────────────────┐
│                                                                     │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────────┐   │
│  │  控制通道     │     │  数据通道     │     │  外部访问端口     │   │
│  │  (HTTP/2)    │     │  (多隧道)     │     │  (TCP/HTTP)      │   │
│  │              │     │              │     │                  │   │
│  │  - 隧道注册   │     │  - 隧道数据   │     │  - 公网访问入口   │   │
│  │  - 心跳保活   │     │  - 流量转发   │     │  - 域名/端口映射  │   │
│  │  - 命令下发   │     │              │     │                  │   │
│  └──────┬───────┘     └──────┬───────┘     └────────┬─────────┘   │
│         │                    │                      │             │
│         └────────────────────┴──────────────────────┘             │
│                              │                                     │
│                    ┌─────────┴─────────┐                          │
│                    │   隧道管理器       │                          │
│                    │   (上层实现)       │                          │
│                    └─────────┬─────────┘                          │
│                              │                                     │
│                    ┌─────────┴─────────┐                          │
│                    │   go-tunnel       │                          │
│                    │   转发引擎         │                          │
│                    └───────────────────┘                          │
│                                                                     │
└──────────────────────────────┼──────────────────────────────────────┘
                               │
                      ═════════╪═════════  公网互联网
                               │
┌──────────────────────────────┼──────────────────────────────────────┐
│                              │                                     │
│                    ┌─────────┴─────────┐                          │
│                    │   go-tunnel       │                          │
│                    │   转发引擎         │                          │
│                    └─────────┬─────────┘                          │
│                              │                                     │
│                    ┌─────────┴─────────┐                          │
│                    │   隧道客户端       │                          │
│                    │   (上层实现)       │                          │
│                    └─────────┬─────────┘                          │
│                              │                                     │
│  ┌──────────────┐     ┌──────┴───────┐                            │
│  │  控制通道     │     │  本地服务     │                            │
│  │  客户端       │────▶│  (应用)      │                            │
│  │              │     │              │                            │
│  └──────────────┘     └──────────────┘                            │
│                                                                     │
│                         内网客户端环境                              │
└─────────────────────────────────────────────────────────────────────┘
```

### 数据流

```
外部用户 ──▶ 公网端口 ──▶ 隧道服务器 ──▶ 控制通道 ──▶ 客户端 ──▶ 本地服务
                              │
                              └── go-tunnel 转发
```

### 控制协议定义

控制协议是客户端与服务器之间的通信协议，需要上层实现。

```go
// protocol.go - 控制协议定义

package tunnelproto

import "encoding/json"

// 消息类型
const (
    MsgTypeRegister    = "register"    // 隧道注册
    MsgTypeRegisterAck = "register_ack" // 注册响应
    MsgTypeHeartbeat   = "heartbeat"   // 心跳
    MsgTypeData        = "data"        // 数据传输
    MsgTypeNewConn     = "new_conn"    // 新连接通知
    MsgTypeCloseConn   = "close_conn"  // 连接关闭
    MsgTypeCommand     = "command"     // 服务器命令
)

// 基础消息结构
type Message struct {
    Type    string          `json:"type"`
    TunnelID string         `json:"tunnel_id,omitempty"`
    ConnID  string          `json:"conn_id,omitempty"`
    Payload json.RawMessage `json:"payload,omitempty"`
}

// 隧道注册请求
type RegisterRequest struct {
    ID         string `json:"id"`          // 客户端生成的隧道ID
    Protocol   string `json:"protocol"`    // tcp / http
    Subdomain  string `json:"subdomain"`   // 申请的子域名（可选）
    LocalAddr  string `json:"local_addr"`  // 本地服务地址
   AuthToken  string `json:"auth_token"`  // 认证令牌
}

// 隧道注册响应
type RegisterResponse struct {
    ID         string `json:"id"`
    PublicURL  string `json:"public_url"`  // 分配的公网访问地址
    Port       int    `json:"port"`         // 分配的端口（TCP）
    Error      string `json:"error,omitempty"`
}

// 心跳消息
type Heartbeat struct {
    Timestamp int64 `json:"timestamp"`
}

// 新连接通知（服务器 -> 客户端）
type NewConnection struct {
    ConnID     string `json:"conn_id"`
    RemoteAddr string `json:"remote_addr"`
}

// 连接关闭通知
type CloseConnection struct {
    ConnID string `json:"conn_id"`
}

// 编码消息
func EncodeMsg(msg interface{}) ([]byte, error) {
    return json.Marshal(msg)
}

// 解码消息
func DecodeMsg(data []byte, msg interface{}) error {
    return json.Unmarshal(data, msg)
}
```

### 服务器端实现

```go
// server.go - 隧道服务器

package server

import (
    "context"
    "crypto/tls"
    "encoding/json"
    "fmt"
    "log"
    "net"
    "sync"
    "time"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/http2"
    "github.com/Talbot3/go-tunnel/tcp"
    autotls "github.com/Talbot3/go-tunnel/tls"
    "github.com/Talbot3/go-tunnel/internal/connmgr"
)

// TunnelInfo 隧道信息
type TunnelInfo struct {
    ID         string
    Protocol   string
    LocalAddr  string
    PublicURL  string
    ClientConn net.Conn      // 控制通道连接
    DataConns  map[string]net.Conn // 数据连接 (connID -> conn)
    CreatedAt  time.Time
}

// Server 隧道服务器
type Server struct {
    // 配置
    config ServerConfig

    // 控制通道
    controlListener net.Listener
    controlProtocol tunnel.Protocol

    // 外部访问端口管理
    externalListener net.Listener
    portManager      *PortManager

    // 隧道管理
    tunnels sync.Map // tunnelID -> *TunnelInfo

    // 连接管理器
    connMgr *connmgr.Manager

    // go-tunnel 转发器
    forwarder tunnel.Forwarder

    // 生命周期
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

// ServerConfig 服务器配置
type ServerConfig struct {
    // 控制通道
    ControlAddr string // 控制通道地址，如 ":443"
    TLSCertFile string // TLS 证书文件
    TLSKeyFile  string // TLS 密钥文件

    // 自动 TLS（推荐）
    AutoTLS  bool
    Email    string
    Domains  []string

    // 外部访问端口范围
    PortRangeStart int // 如 10000
    PortRangeEnd   int // 如 20000

    // 连接管理
    MaxConnections int
    Timeout        time.Duration

    // 认证
    AuthToken string // 简单令牌认证
}

// NewServer 创建隧道服务器
func NewServer(cfg ServerConfig) (*Server, error) {
    // 创建连接管理器
    connMgr := connmgr.NewManager(connmgr.Config{
        MaxConnections:    cfg.MaxConnections,
        ConnectionTimeout: cfg.Timeout,
    })

    // 创建端口管理器
    portMgr := NewPortManager(cfg.PortRangeStart, cfg.PortRangeEnd)

    return &Server{
        config:     cfg,
        portManager: portMgr,
        connMgr:    connMgr,
        forwarder:  tunnel.NewForwarder(),
    }, nil
}

// Start 启动服务器
func (s *Server) Start(ctx context.Context) error {
    s.ctx, s.cancel = context.WithCancel(ctx)

    // 1. 设置 TLS
    var tlsConfig *tls.Config
    var err error

    if s.config.AutoTLS {
        // 自动 TLS
        mgr, err := autotls.QuickSetup(s.config.Email, s.config.Domains...)
        if err != nil {
            return fmt.Errorf("auto TLS setup failed: %w", err)
        }
        tlsConfig = mgr.TLSConfig()
    } else {
        // 手动加载证书
        cert, err := tls.LoadX509KeyPair(s.config.TLSCertFile, s.config.TLSKeyFile)
        if err != nil {
            return fmt.Errorf("load TLS cert failed: %w", err)
        }
        tlsConfig = &tls.Config{
            Certificates: []tls.Certificate{cert},
            MinVersion:   tls.VersionTLS12,
        }
    }

    // 2. 启动控制通道（HTTP/2，支持多路复用）
    s.controlProtocol = http2.New(tlsConfig)
    s.controlListener, err = s.controlProtocol.Listen(s.config.ControlAddr)
    if err != nil {
        return fmt.Errorf("listen control channel failed: %w", err)
    }

    log.Printf("Control channel started on %s", s.config.ControlAddr)

    // 3. 接受客户端连接
    s.wg.Add(1)
    go s.acceptClients()

    // 4. 启动健康检查
    s.wg.Add(1)
    go s.healthCheck()

    return nil
}

// acceptClients 接受客户端连接
func (s *Server) acceptClients() {
    defer s.wg.Done()

    for {
        select {
        case <-s.ctx.Done():
            return
        default:
        }

        conn, err := s.controlListener.Accept()
        if err != nil {
            if s.ctx.Err() != nil {
                return
            }
            log.Printf("Accept error: %v", err)
            continue
        }

        s.wg.Add(1)
        go s.handleClient(conn)
    }
}

// handleClient 处理客户端连接
func (s *Server) handleClient(conn net.Conn) {
    defer s.wg.Done()
    defer conn.Close()

    // 设置读写超时
    conn.SetDeadline(time.Now().Add(30 * time.Second))

    // 1. 读取注册请求
    var msg json.RawMessage
    decoder := json.NewDecoder(conn)
    if err := decoder.Decode(&msg); err != nil {
        log.Printf("Read register message failed: %v", err)
        return
    }

    // 解析消息类型
    var baseMsg struct {
        Type string `json:"type"`
    }
    if err := json.Unmarshal(msg, &baseMsg); err != nil {
        return
    }

    if baseMsg.Type != "register" {
        log.Printf("Expected register message, got: %s", baseMsg.Type)
        return
    }

    // 解析注册请求
    var regReq struct {
        Payload RegisterRequest `json:"payload"`
    }
    if err := json.Unmarshal(msg, &regReq); err != nil {
        return
    }
    req := regReq.Payload

    // 2. 验证认证令牌
    if s.config.AuthToken != "" && req.AuthToken != s.config.AuthToken {
        s.sendRegisterAck(conn, req.ID, "", 0, "authentication failed")
        return
    }

    // 3. 分配公网访问地址
    var publicURL string
    var port int

    if req.Protocol == "http" && req.Subdomain != "" {
        // HTTP 隧道，使用子域名
        publicURL = fmt.Sprintf("https://%s.%s", req.Subdomain, s.config.Domains[0])
    } else {
        // TCP 隧道，分配端口
        port, err = s.portManager.Allocate()
        if err != nil {
            s.sendRegisterAck(conn, req.ID, "", 0, err.Error())
            return
        }
        publicURL = fmt.Sprintf("tcp://:%d", port)
    }

    // 4. 创建隧道信息
    info := &TunnelInfo{
        ID:        req.ID,
        Protocol:  req.Protocol,
        LocalAddr: req.LocalAddr,
        PublicURL: publicURL,
        ClientConn: conn,
        DataConns: make(map[string]net.Conn),
        CreatedAt: time.Now(),
    }
    s.tunnels.Store(req.ID, info)

    // 5. 发送注册成功响应
    s.sendRegisterAck(conn, req.ID, publicURL, port, "")

    log.Printf("Tunnel registered: %s -> %s (%s)", req.ID, publicURL, req.Protocol)

    // 6. 启动外部访问端口（TCP 隧道）
    if req.Protocol == "tcp" && port > 0 {
        go s.startExternalListener(req.ID, port)
    }

    // 7. 清除连接超时，进入消息循环
    conn.SetDeadline(time.Time{})

    // 8. 消息循环
    s.messageLoop(conn, req.ID)
}

// sendRegisterAck 发送注册响应
func (s *Server) sendRegisterAck(conn net.Conn, id, publicURL string, port int, errMsg string) {
    resp := struct {
        Type    string          `json:"type"`
        Payload RegisterResponse `json:"payload"`
    }{
        Type: "register_ack",
        Payload: RegisterResponse{
            ID:        id,
            PublicURL: publicURL,
            Port:      port,
            Error:     errMsg,
        },
    }

    data, _ := json.Marshal(resp)
    conn.Write(data)
}

// startExternalListener 启动外部访问端口
func (s *Server) startExternalListener(tunnelID string, port int) {
    // 创建 TCP 监听
    listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
    if err != nil {
        log.Printf("Failed to listen on port %d: %v", port, err)
        return
    }
    defer listener.Close()

    log.Printf("External listener started for tunnel %s on :%d", tunnelID, port)

    for {
        select {
        case <-s.ctx.Done():
            return
        default:
        }

        externalConn, err := listener.Accept()
        if err != nil {
            continue
        }

        // 通知客户端有新连接
        go s.notifyNewConnection(tunnelID, externalConn)
    }
}

// notifyNewConnection 通知客户端有新连接
func (s *Server) notifyNewConnection(tunnelID string, externalConn net.Conn) {
    // 获取隧道信息
    infoVal, ok := s.tunnels.Load(tunnelID)
    if !ok {
        externalConn.Close()
        return
    }
    info := infoVal.(*TunnelInfo)

    // 生成连接ID
    connID := generateConnID()

    // 保存连接
    info.DataConns[connID] = externalConn

    // 发送新连接通知
    notify := struct {
        Type    string       `json:"type"`
        TunnelID string      `json:"tunnel_id"`
        ConnID  string       `json:"conn_id"`
        Payload NewConnection `json:"payload"`
    }{
        Type:     "new_conn",
        TunnelID: tunnelID,
        ConnID:   connID,
        Payload: NewConnection{
            ConnID:     connID,
            RemoteAddr: externalConn.RemoteAddr().String(),
        },
    }

    data, _ := json.Marshal(notify)
    info.ClientConn.Write(data)

    // 等待客户端建立本地连接后，开始数据转发
    // 数据传输通过单独的数据通道或复用控制通道
    go s.handleDataTransfer(info, connID, externalConn)
}

// handleDataTransfer 处理数据转发
func (s *Server) handleDataTransfer(info *TunnelInfo, connID string, externalConn net.Conn) {
    defer func() {
        externalConn.Close()
        delete(info.DataConns, connID)
    }()

    // 方案一：数据通过控制通道传输（需要协议支持）
    // 方案二：客户端建立独立的数据连接

    // 这里使用方案一：在控制连接上传输数据
    // 客户端收到 new_conn 后，连接本地服务，然后通过控制连接发送数据

    // 创建一个 pipe 用于数据交换
    // 外部连接的数据需要封装成消息发送给客户端
    // 客户端返回的数据需要解封装后写入外部连接

    // 简化实现：等待客户端建立数据通道
    // 这里需要更复杂的协议处理，参考下面的完整实现
}

// messageLoop 消息循环
func (s *Server) messageLoop(conn net.Conn, tunnelID string) {
    decoder := json.NewDecoder(conn)

    for {
        select {
        case <-s.ctx.Done():
            return
        default:
        }

        var msg json.RawMessage
        if err := decoder.Decode(&msg); err != nil {
            if s.ctx.Err() == nil {
                log.Printf("Message loop error for tunnel %s: %v", tunnelID, err)
            }
            // 清理隧道
            s.tunnels.Delete(tunnelID)
            return
        }

        // 处理消息
        go s.handleMessage(conn, tunnelID, msg)
    }
}

// handleMessage 处理消息
func (s *Server) handleMessage(conn net.Conn, tunnelID string, msg json.RawMessage) {
    var baseMsg struct {
        Type string `json:"type"`
    }
    if err := json.Unmarshal(msg, &baseMsg); err != nil {
        return
    }

    switch baseMsg.Type {
    case "heartbeat":
        // 心跳响应
        resp := struct {
            Type string `json:"type"`
        }{Type: "heartbeat_ack"}
        data, _ := json.Marshal(resp)
        conn.Write(data)

    case "data":
        // 数据传输
        s.handleDataMessage(tunnelID, msg)

    case "close_conn":
        // 连接关闭
        s.handleCloseConn(tunnelID, msg)
    }
}

// handleDataMessage 处理数据消息
func (s *Server) handleDataMessage(tunnelID string, msg json.RawMessage) {
    var dataMsg struct {
        ConnID string `json:"conn_id"`
        Data   []byte `json:"data"`
    }
    if err := json.Unmarshal(msg, &dataMsg); err != nil {
        return
    }

    // 获取隧道信息
    infoVal, ok := s.tunnels.Load(tunnelID)
    if !ok {
        return
    }
    info := infoVal.(*TunnelInfo)

    // 找到对应的外部连接
    externalConn, ok := info.DataConns[dataMsg.ConnID]
    if !ok {
        return
    }

    // 写入数据
    externalConn.Write(dataMsg.Data)
}

// handleCloseConn 处理连接关闭
func (s *Server) handleCloseConn(tunnelID string, msg json.RawMessage) {
    var closeMsg struct {
        ConnID string `json:"conn_id"`
    }
    if err := json.Unmarshal(msg, &closeMsg); err != nil {
        return
    }

    infoVal, ok := s.tunnels.Load(tunnelID)
    if !ok {
        return
    }
    info := infoVal.(*TunnelInfo)

    if conn, ok := info.DataConns[closeMsg.ConnID]; ok {
        conn.Close()
        delete(info.DataConns, closeMsg.ConnID)
    }
}

// healthCheck 健康检查
func (s *Server) healthCheck() {
    defer s.wg.Done()

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-s.ctx.Done():
            return
        case <-ticker.C:
            // 检查隧道状态，清理过期隧道
            s.tunnels.Range(func(key, value interface{}) bool {
                info := value.(*TunnelInfo)
                // 检查客户端连接是否存活
                // 可以通过最后心跳时间判断
                return true
            })
        }
    }
}

// Stop 停止服务器
func (s *Server) Stop() error {
    if s.cancel != nil {
        s.cancel()
    }
    if s.controlListener != nil {
        s.controlListener.Close()
    }
    s.wg.Wait()
    return nil
}

// GetStats 获取统计信息
func (s *Server) GetStats() map[string]interface{} {
    var tunnelCount int
    s.tunnels.Range(func(_, _ interface{}) bool {
        tunnelCount++
        return true
    })

    return map[string]interface{}{
        "tunnels": tunnelCount,
    }
}

// 辅助函数
func generateConnID() string {
    return fmt.Sprintf("%d", time.Now().UnixNano())
}

// PortManager 端口管理器
type PortManager struct {
    start, end int
    used       map[int]bool
    mu         sync.Mutex
}

func NewPortManager(start, end int) *PortManager {
    return &PortManager{
        start: start,
        end:   end,
        used:  make(map[int]bool),
    }
}

func (m *PortManager) Allocate() (int, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    for port := m.start; port <= m.end; port++ {
        if !m.used[port] {
            m.used[port] = true
            return port, nil
        }
    }
    return 0, fmt.Errorf("no available port")
}

func (m *PortManager) Release(port int) {
    m.mu.Lock()
    defer m.mu.Unlock()
    delete(m.used, port)
}
```

### 客户端实现

```go
// client.go - 隧道客户端

package client

import (
    "context"
    "crypto/tls"
    "encoding/json"
    "fmt"
    "log"
    "net"
    "sync"
    "time"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/http2"
)

// Client 隧道客户端
type Client struct {
    config ClientConfig

    // 控制连接
    controlConn net.Conn
    encoder     *json.Encoder
    decoder     *json.Decoder

    // 本地连接映射
    localConns map[string]net.Conn // connID -> localConn
    connMu     sync.RWMutex

    // go-tunnel 转发器
    forwarder tunnel.Forwarder

    // 生命周期
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup

    // 重连
    reconnectCh chan struct{}
}

// ClientConfig 客户端配置
type ClientConfig struct {
    // 服务器地址
    ServerAddr string // 如 "tunnel.example.com:443"

    // 隧道配置
    TunnelID   string // 隧道ID（可选，自动生成）
    Protocol   string // tcp / http
    Subdomain  string // 子域名（HTTP 隧道可选）
    LocalAddr  string // 本地服务地址，如 "localhost:8080"

    // 认证
    AuthToken string

    // TLS
    InsecureSkipVerify bool // 跳过证书验证（仅测试用）

    // 重连
    ReconnectInterval time.Duration // 重连间隔
}

// NewClient 创建客户端
func NewClient(cfg ClientConfig) (*Client, error) {
    if cfg.TunnelID == "" {
        cfg.TunnelID = generateTunnelID()
    }
    if cfg.ReconnectInterval == 0 {
        cfg.ReconnectInterval = 5 * time.Second
    }

    return &Client{
        config:      cfg,
        localConns:  make(map[string]net.Conn),
        forwarder:   tunnel.NewForwarder(),
        reconnectCh: make(chan struct{}, 1),
    }, nil
}

// Start 启动客户端
func (c *Client) Start(ctx context.Context) error {
    c.ctx, c.cancel = context.WithCancel(ctx)

    // 启动连接循环
    c.wg.Add(1)
    go c.connectLoop()

    return nil
}

// connectLoop 连接循环（支持重连）
func (c *Client) connectLoop() {
    defer c.wg.Done()

    for {
        select {
        case <-c.ctx.Done():
            return
        default:
        }

        // 连接服务器
        if err := c.connect(); err != nil {
            log.Printf("Connect failed: %v, reconnecting in %v...", err, c.config.ReconnectInterval)

            select {
            case <-c.ctx.Done():
                return
            case <-time.After(c.config.ReconnectInterval):
                continue
            }
        }

        // 连接成功，等待断开或重连信号
        select {
        case <-c.ctx.Done():
            return
        case <-c.reconnectCh:
            log.Println("Reconnecting...")
        }
    }
}

// connect 连接服务器
func (c *Client) connect() error {
    // 1. 创建控制连接
    tlsConfig := &tls.Config{
        InsecureSkipVerify: c.config.InsecureSkipVerify,
    }

    // 使用 HTTP/2 协议连接
    p := http2.New(tlsConfig)
    conn, err := p.Dial(c.ctx, c.config.ServerAddr)
    if err != nil {
        return fmt.Errorf("dial server failed: %w", err)
    }
    c.controlConn = conn
    c.encoder = json.NewEncoder(conn)
    c.decoder = json.NewDecoder(conn)

    log.Printf("Connected to %s", c.config.ServerAddr)

    // 2. 注册隧道
    if err := c.registerTunnel(); err != nil {
        conn.Close()
        return fmt.Errorf("register tunnel failed: %w", err)
    }

    // 3. 启动心跳
    c.wg.Add(1)
    go c.heartbeatLoop()

    // 4. 启动消息处理
    c.wg.Add(1)
    go c.messageLoop()

    // 5. 等待连接关闭
    <-c.ctx.Done()
    conn.Close()

    return nil
}

// registerTunnel 注册隧道
func (c *Client) registerTunnel() error {
    // 发送注册请求
    regReq := struct {
        Type    string          `json:"type"`
        Payload RegisterRequest `json:"payload"`
    }{
        Type: "register",
        Payload: RegisterRequest{
            ID:        c.config.TunnelID,
            Protocol:  c.config.Protocol,
            Subdomain: c.config.Subdomain,
            LocalAddr: c.config.LocalAddr,
            AuthToken: c.config.AuthToken,
        },
    }

    if err := c.encoder.Encode(&regReq); err != nil {
        return err
    }

    // 读取响应
    var resp struct {
        Type    string           `json:"type"`
        Payload RegisterResponse `json:"payload"`
    }
    if err := c.decoder.Decode(&resp); err != nil {
        return err
    }

    if resp.Payload.Error != "" {
        return fmt.Errorf("server error: %s", resp.Payload.Error)
    }

    log.Printf("Tunnel registered: %s -> %s", c.config.TunnelID, resp.Payload.PublicURL)
    return nil
}

// heartbeatLoop 心跳循环
func (c *Client) heartbeatLoop() {
    defer c.wg.Done()

    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-c.ctx.Done():
            return
        case <-ticker.C:
            // 发送心跳
            heartbeat := struct {
                Type string `json:"type"`
            }{Type: "heartbeat"}

            if err := c.encoder.Encode(&heartbeat); err != nil {
                log.Printf("Heartbeat failed: %v", err)
                select {
                case c.reconnectCh <- struct{}{}:
                default:
                }
                return
            }
        }
    }
}

// messageLoop 消息循环
func (c *Client) messageLoop() {
    defer c.wg.Done()

    for {
        select {
        case <-c.ctx.Done():
            return
        default:
        }

        var msg json.RawMessage
        if err := c.decoder.Decode(&msg); err != nil {
            if c.ctx.Err() == nil {
                log.Printf("Message loop error: %v", err)
                select {
                case c.reconnectCh <- struct{}{}:
                default:
                }
            }
            return
        }

        // 处理消息
        go c.handleMessage(msg)
    }
}

// handleMessage 处理消息
func (c *Client) handleMessage(msg json.RawMessage) {
    var baseMsg struct {
        Type string `json:"type"`
    }
    if err := json.Unmarshal(msg, &baseMsg); err != nil {
        return
    }

    switch baseMsg.Type {
    case "heartbeat_ack":
        // 心跳响应，忽略

    case "new_conn":
        // 新连接通知
        c.handleNewConnection(msg)

    case "close_conn":
        // 连接关闭通知
        c.handleCloseConnection(msg)

    case "data":
        // 数据消息
        c.handleDataMessage(msg)
    }
}

// handleNewConnection 处理新连接
func (c *Client) handleNewConnection(msg json.RawMessage) {
    var newConnMsg struct {
        TunnelID string `json:"tunnel_id"`
        ConnID   string `json:"conn_id"`
    }
    if err := json.Unmarshal(msg, &newConnMsg); err != nil {
        return
    }

    // 连接本地服务
    localConn, err := net.Dial("tcp", c.config.LocalAddr)
    if err != nil {
        log.Printf("Failed to connect local service: %v", err)
        // 通知服务器关闭连接
        c.sendCloseConnection(newConnMsg.ConnID)
        return
    }

    // 保存本地连接
    c.connMu.Lock()
    c.localConns[newConnMsg.ConnID] = localConn
    c.connMu.Unlock()

    log.Printf("New connection: %s -> %s", newConnMsg.ConnID, c.config.LocalAddr)

    // 启动双向转发
    go c.forwardLocalToServer(newConnMsg.ConnID, localConn)
    go c.forwardServerToLocal(newConnMsg.ConnID, localConn)
}

// forwardLocalToServer 从本地服务转发到服务器
func (c *Client) forwardLocalToServer(connID string, localConn net.Conn) {
    defer func() {
        localConn.Close()
        c.sendCloseConnection(connID)
        c.connMu.Lock()
        delete(c.localConns, connID)
        c.connMu.Unlock()
    }()

    buf := make([]byte, 32*1024)

    for {
        select {
        case <-c.ctx.Done():
            return
        default:
        }

        n, err := localConn.Read(buf)
        if err != nil {
            return
        }

        // 发送数据消息
        dataMsg := struct {
            Type    string `json:"type"`
            ConnID  string `json:"conn_id"`
            Data    []byte `json:"data"`
        }{
            Type:   "data",
            ConnID: connID,
            Data:   buf[:n],
        }

        if err := c.encoder.Encode(&dataMsg); err != nil {
            return
        }
    }
}

// forwardServerToLocal 从服务器转发到本地服务
func (c *Client) forwardServerToLocal(connID string, localConn net.Conn) {
    // 数据通过 messageLoop 接收，存储在通道中
    // 这里从通道读取并写入本地连接
    // 需要配合 handleDataMessage 使用

    // 简化实现：在 handleDataMessage 中直接写入
}

// handleDataMessage 处理数据消息
func (c *Client) handleDataMessage(msg json.RawMessage) {
    var dataMsg struct {
        ConnID string `json:"conn_id"`
        Data   []byte `json:"data"`
    }
    if err := json.Unmarshal(msg, &dataMsg); err != nil {
        return
    }

    c.connMu.RLock()
    localConn, ok := c.localConns[dataMsg.ConnID]
    c.connMu.RUnlock()

    if !ok {
        return
    }

    // 写入本地连接
    localConn.Write(dataMsg.Data)
}

// handleCloseConnection 处理连接关闭
func (c *Client) handleCloseConnection(msg json.RawMessage) {
    var closeMsg struct {
        ConnID string `json:"conn_id"`
    }
    if err := json.Unmarshal(msg, &closeMsg); err != nil {
        return
    }

    c.connMu.Lock()
    defer c.connMu.Unlock()

    if conn, ok := c.localConns[closeMsg.ConnID]; ok {
        conn.Close()
        delete(c.localConns, closeMsg.ConnID)
    }
}

// sendCloseConnection 发送连接关闭消息
func (c *Client) sendCloseConnection(connID string) {
    closeMsg := struct {
        Type   string `json:"type"`
        ConnID string `json:"conn_id"`
    }{
        Type:   "close_conn",
        ConnID: connID,
    }

    c.encoder.Encode(&closeMsg)
}

// Stop 停止客户端
func (c *Client) Stop() error {
    if c.cancel != nil {
        c.cancel()
    }
    if c.controlConn != nil {
        c.controlConn.Close()
    }
    c.wg.Wait()
    return nil
}

// 辅助函数
func generateTunnelID() string {
    return fmt.Sprintf("tunnel-%d", time.Now().UnixNano())
}
```

### 使用示例

#### 服务器端启动

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/Talbot3/go-tunnel/examples/tunnel-server/server"
)

func main() {
    cfg := server.ServerConfig{
        ControlAddr:     ":443",
        AutoTLS:         true,
        Email:           "admin@example.com",
        Domains:         []string{"tunnel.example.com"},
        PortRangeStart:  10000,
        PortRangeEnd:    20000,
        MaxConnections:  10000,
        AuthToken:       os.Getenv("TUNNEL_AUTH_TOKEN"),
    }

    srv, err := server.NewServer(cfg)
    if err != nil {
        log.Fatal(err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    if err := srv.Start(ctx); err != nil {
        log.Fatal(err)
    }

    log.Println("Tunnel server started")

    // 等待信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    log.Println("Shutting down...")
    srv.Stop()
}
```

#### 客户端启动

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/Talbot3/go-tunnel/examples/tunnel-client/client"
)

func main() {
    cfg := client.ClientConfig{
        ServerAddr:   "tunnel.example.com:443",
        Protocol:     "tcp",
        LocalAddr:    "localhost:8080", // 本地服务
        AuthToken:    os.Getenv("TUNNEL_AUTH_TOKEN"),
        ReconnectInterval: 5 * time.Second,
    }

    c, err := client.NewClient(cfg)
    if err != nil {
        log.Fatal(err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    if err := c.Start(ctx); err != nil {
        log.Fatal(err)
    }

    log.Println("Tunnel client started, press Ctrl+C to stop")

    // 等待信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    log.Println("Shutting down...")
    c.Stop()
}
```

### HTTP 隧道实现（可选）

对于 HTTP 隧道，需要在服务器端添加 HTTP 请求路由：

```go
// http_tunnel.go - HTTP 隧道处理

package server

import (
    "net/http"
    "strings"
)

// HTTPTunnelHandler HTTP 隧道处理器
func (s *Server) HTTPTunnelHandler(w http.ResponseWriter, r *http.Request) {
    // 从 Host 解析子域名
    host := r.Host
    subdomain := strings.Split(host, ".")[0]

    // 查找隧道
    var targetTunnel *TunnelInfo
    s.tunnels.Range(func(key, value interface{}) bool {
        info := value.(*TunnelInfo)
        if info.Protocol == "http" && info.Subdomain == subdomain {
            targetTunnel = info
            return false
        }
        return true
    })

    if targetTunnel == nil {
        http.Error(w, "Tunnel not found", http.StatusNotFound)
        return
    }

    // 将请求转发给客户端
    // 需要将 HTTP 请求序列化后发送
    s.forwardHTTPRequest(w, r, targetTunnel)
}

// forwardHTTPRequest 转发 HTTP 请求
func (s *Server) forwardHTTPRequest(w http.ResponseWriter, r *http.Request, tunnel *TunnelInfo) {
    // 生成请求ID
    reqID := generateConnID()

    // 序列化请求
    reqData, _ := serializeHTTPRequest(r)

    // 发送请求给客户端
    msg := struct {
        Type    string      `json:"type"`
        ReqID   string      `json:"req_id"`
        PayloadHTTPRequest `json:"payload"`
    }{
        Type:  "http_request",
        ReqID: reqID,
        Payload: HTTPRequest{
            Method:  r.Method,
            URL:     r.URL.String(),
            Headers: r.Header,
            Body:    reqData,
        },
    }

    // 发送并等待响应
    // ...
}
```

### 架构优化建议

#### 1. 使用独立数据通道

上面的示例将数据和控制消息复用在同一连接上。对于高吞吐场景，建议使用独立的数据通道：

```go
// 控制通道：隧道注册、心跳、命令
// 数据通道：专门用于数据传输

// 客户端建立两个连接：
// 1. 控制连接 (HTTP/2) - 长连接，处理控制消息
// 2. 数据连接池 (TCP/QUIC) - 按需创建，传输数据
```

#### 2. 多路复用优化

使用 HTTP/2 或 QUIC 的多流特性，单连接承载多个隧道：

```go
// HTTP/2 多流模式
// 一个 HTTP/2 连接，每个隧道使用独立的 stream
// 减少连接数，提高资源利用率
```

#### 3. 断线重连

```go
// 客户端断线重连
// 1. 检测连接断开
// 2. 自动重连服务器
// 3. 重新注册隧道
// 4. 恢复数据传输

func (c *Client) reconnect() {
    for {
        if err := c.connect(); err != nil {
            time.Sleep(c.config.ReconnectInterval)
            continue
        }
        break
    }
}
```

### go-tunnel 提供的能力总结

| 能力 | 用途 | 相关 API |
|------|------|----------|
| 协议支持 | 控制通道协议选择 | `http2.New()`, `quic.New()` |
| 数据转发 | 高性能双向转发 | `tunnel.HandlePair()`, `tunnel.NewForwarder()` |
| 服务器优化 | 高并发处理 | `tunnel.ServerPreset()` |
| 客户端优化 | 高吞吐传输 | `tunnel.ClientPreset()` |
| 连接管理 | 连接数限制、超时 | `internal/connmgr` |
| 连接池 | 连接复用 | `internal/pool` |
| 背压控制 | 防止 OOM | `internal/backpressure` |
| 自动 TLS | 证书管理 | `autotls.QuickSetup()` |
| 统计监控 | 运行时状态 | `tunnel.Stats()` |

### 上层需实现的功能

| 功能 | 说明 | 复杂度 |
|------|------|--------|
| 控制协议 | 隧道注册、心跳、命令 | 中等 |
| 端口管理 | 动态分配、回收端口 | 简单 |
| 用户认证 | Token/OAuth 验证 | 简单 |
| 域名路由 | 子域名到隧道映射 | 简单 |
| 断线重连 | 客户端自动重连 | 中等 |
| Web Dashboard | 可视化管理界面 | 较高 |
| 请求日志 | HTTP 请求记录查看 | 中等 |

---

## 部署检查清单

### 集成前

- [ ] 选择合适的协议 (TCP/HTTP/2/HTTP/3/QUIC)
- [ ] 选择合适的配置预设 (ServerPreset/ClientPreset/HighThroughputPreset)
- [ ] 配置 TLS 证书（安全协议必需）
- [ ] 实现健康检查端点（Kubernetes 部署必需）
- [ ] 实现优雅关闭逻辑

### 部署时

- [ ] 调整系统参数 (ulimit, sysctl)
- [ ] 配置监控指标端点
- [ ] 配置日志输出
- [ ] 配置资源限制

### 运维

- [ ] 监控连接数、吞吐量、错误率
- [ ] 设置告警规则
- [ ] 定期检查证书有效期（自动 TLS 会自动续期）

---

## 系统参数调优

```bash
# /etc/sysctl.conf
fs.file-max = 1000000
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.ipv4.tcp_tw_reuse = 1

# 应用
sysctl -p
```

```bash
# /etc/security/limits.conf
* soft nofile 1000000
* hard nofile 1000000
```
