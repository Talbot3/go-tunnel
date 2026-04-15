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
│                    │   - MuxForwarder  │                          │
│                    │   - MuxConnManager│                          │
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
│                    │   - MuxForwarder  │                          │
│                    │   - MuxConnManager│                          │
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
                              └── go-tunnel MuxForwarder 转发
```

### 核心组件：forward/mux.go

go-tunnel 提供了完整的多路复用组件，已集成缓冲池和背压控制优化：

| 组件 | 说明 | 优化项 |
|------|------|--------|
| `MuxEncoder` | 消息编码器 | 缓冲池支持、Release 方法 |
| `MuxDecoder` | 消息解码器 | 零拷贝解码 |
| `MuxForwarder` | 单向多路转发 | 缓冲池 + 背压控制 |
| `BidirectionalMuxForwarder` | 双向多路转发 | 缓冲池 + 背压控制 |
| `MuxConnManager` | 连接管理器 | 缓冲池 + 背压控制 + TCP 优化 |
| `HTTPMuxForwarder` | HTTP 请求转发 | HTTP 请求-响应模式 |

### 服务器端实现

```go
// server.go - 隧道服务器

package server

import (
    "context"
    "crypto/tls"
    "fmt"
    "log"
    "net"
    "sync"
    "time"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/forward"
    "github.com/Talbot3/go-tunnel/http2"
    autotls "github.com/Talbot3/go-tunnel/tls"
    "github.com/Talbot3/go-tunnel/internal/connmgr"
)

// TunnelInfo 隧道信息
type TunnelInfo struct {
    ID         string
    Protocol   string
    LocalAddr  string
    PublicURL  string
    Port       int
    
    // 控制连接
    ControlConn net.Conn
    
    // 多路复用管理器
    MuxMgr      *forward.MuxConnManager
    
    // 外部连接映射
    ExternalConns sync.Map // connID -> net.Conn
    
    CreatedAt  time.Time
}

// Server 隧道服务器
type Server struct {
    config ServerConfig
    
    // 控制通道
    controlListener net.Listener
    
    // 外部访问端口管理
    portManager *PortManager
    
    // 隧道管理
    tunnels sync.Map // tunnelID -> *TunnelInfo
    
    // 连接管理器
    connMgr *connmgr.Manager
    
    // 生命周期
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

// ServerConfig 服务器配置
type ServerConfig struct {
    ControlAddr     string   // 控制通道地址，如 ":443"
    AutoTLS         bool
    Email           string
    Domains         []string
    TLSCertFile     string
    TLSKeyFile      string
    PortRangeStart  int      // TCP 隧道端口范围
    PortRangeEnd    int
    MaxConnections  int
    Timeout         time.Duration
    AuthToken       string
}

// NewServer 创建隧道服务器
func NewServer(cfg ServerConfig) (*Server, error) {
    connMgr := connmgr.NewManager(connmgr.Config{
        MaxConnections:    cfg.MaxConnections,
        ConnectionTimeout: cfg.Timeout,
    })
    
    portMgr := NewPortManager(cfg.PortRangeStart, cfg.PortRangeEnd)
    
    return &Server{
        config:      cfg,
        portManager: portMgr,
        connMgr:     connMgr,
    }, nil
}

// Start 启动服务器
func (s *Server) Start(ctx context.Context) error {
    s.ctx, s.cancel = context.WithCancel(ctx)
    
    // 1. 设置 TLS
    var tlsConfig *tls.Config
    var err error
    
    if s.config.AutoTLS {
        mgr, err := autotls.QuickSetup(s.config.Email, s.config.Domains...)
        if err != nil {
            return fmt.Errorf("auto TLS setup failed: %w", err)
        }
        tlsConfig = mgr.TLSConfig()
    } else {
        cert, err := tls.LoadX509KeyPair(s.config.TLSCertFile, s.config.TLSKeyFile)
        if err != nil {
            return fmt.Errorf("load TLS cert failed: %w", err)
        }
        tlsConfig = &tls.Config{
            Certificates: []tls.Certificate{cert},
            MinVersion:   tls.VersionTLS12,
        }
    }
    
    // 2. 启动控制通道（HTTP/2 支持多路复用）
    p := http2.New(tlsConfig)
    s.controlListener, err = p.Listen(s.config.ControlAddr)
    if err != nil {
        return fmt.Errorf("listen control channel failed: %w", err)
    }
    
    log.Printf("Control channel started on %s", s.config.ControlAddr)
    
    // 3. 接受客户端连接
    s.wg.Add(1)
    go s.acceptClients()
    
    // 4. 健康检查
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
    
    // 设置超时
    conn.SetDeadline(time.Now().Add(30 * time.Second))
    
    // 1. 读取注册请求
    regReq, err := s.readRegisterRequest(conn)
    if err != nil {
        log.Printf("Read register failed: %v", err)
        return
    }
    
    // 2. 验证认证
    if s.config.AuthToken != "" && regReq.AuthToken != s.config.AuthToken {
        s.sendRegisterAck(conn, regReq.ID, "", 0, "authentication failed")
        return
    }
    
    // 3. 分配公网地址
    var publicURL string
    var port int
    
    if regReq.Protocol == "http" {
        publicURL = fmt.Sprintf("https://%s.%s", regReq.Subdomain, s.config.Domains[0])
    } else {
        port, err = s.portManager.Allocate()
        if err != nil {
            s.sendRegisterAck(conn, regReq.ID, "", 0, err.Error())
            return
        }
        publicURL = fmt.Sprintf("tcp://:%d", port)
    }
    
    // 4. 创建隧道信息
    info := &TunnelInfo{
        ID:           regReq.ID,
        Protocol:     regReq.Protocol,
        LocalAddr:    regReq.LocalAddr,
        PublicURL:    publicURL,
        Port:         port,
        ControlConn:  conn,
        CreatedAt:    time.Now(),
    }
    s.tunnels.Store(regReq.ID, info)
    
    // 5. 发送注册成功响应
    s.sendRegisterAck(conn, regReq.ID, publicURL, port, "")
    
    log.Printf("Tunnel registered: %s -> %s (%s)", regReq.ID, publicURL, regReq.Protocol)
    
    // 6. 创建多路复用管理器
    encoder := forward.NewDefaultMuxEncoder()
    decoder := forward.NewDefaultMuxDecoder()
    info.MuxMgr = forward.NewMuxConnManager(conn, encoder, decoder)
    
    // 7. 启动外部监听（TCP 隧道）
    if regReq.Protocol == "tcp" && port > 0 {
        go s.startExternalListener(info, port)
    }
    
    // 8. 清除超时，进入消息循环
    conn.SetDeadline(time.Time{})
    
    // 9. 消息循环
    s.messageLoop(info)
}

// startExternalListener 启动外部访问端口
func (s *Server) startExternalListener(info *TunnelInfo, port int) {
    listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
    if err != nil {
        log.Printf("Listen on port %d failed: %v", port, err)
        return
    }
    defer listener.Close()
    
    log.Printf("External listener started for tunnel %s on :%d", info.ID, port)
    
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
        
        // 生成连接 ID
        connID := generateConnID()
        
        // 保存外部连接
        info.ExternalConns.Store(connID, externalConn)
        
        // 发送新连接通知给客户端
        encoder := forward.NewDefaultMuxEncoder()
        newConnMsg, _ := encoder.EncodeNewConn(connID, externalConn.RemoteAddr().String())
        info.ControlConn.Write(newConnMsg)
        encoder.Release(newConnMsg)
        
        // 启动数据转发（外部连接 -> 控制通道）
        go s.forwardExternalToMux(info, connID, externalConn)
    }
}

// forwardExternalToMux 从外部连接转发到多路复用通道
func (s *Server) forwardExternalToMux(info *TunnelInfo, connID string, externalConn net.Conn) {
    defer func() {
        externalConn.Close()
        info.ExternalConns.Delete(connID)
        
        // 发送关闭消息
        encoder := forward.NewDefaultMuxEncoder()
        closeMsg, _ := encoder.EncodeClose(connID)
        info.ControlConn.Write(closeMsg)
        encoder.Release(closeMsg)
    }()
    
    // 使用 MuxForwarder 进行转发
    encoder := forward.NewDefaultMuxEncoder()
    forwarder := forward.NewMuxForwarder()
    
    err := forwarder.ForwardMux(s.ctx, externalConn, info.ControlConn, connID, encoder)
    if err != nil {
        log.Printf("Forward error for %s: %v", connID, err)
    }
}

// messageLoop 消息循环
func (s *Server) messageLoop(info *TunnelInfo) {
    // 使用 MuxConnManager 处理接收到的消息
    buf := make([]byte, 64*1024)
    
    for {
        select {
        case <-s.ctx.Done():
            return
        default:
        }
        
        n, err := info.ControlConn.Read(buf)
        if err != nil {
            if s.ctx.Err() == nil {
                log.Printf("Read error for tunnel %s: %v", info.ID, err)
            }
            s.closeTunnel(info.ID)
            return
        }
        
        // 使用 MuxConnManager 处理消息
        if err := info.MuxMgr.HandleIncoming(buf[:n]); err != nil {
            log.Printf("Handle incoming error: %v", err)
        }
    }
}

// closeTunnel 关闭隧道
func (s *Server) closeTunnel(tunnelID string) {
    value, ok := s.tunnels.LoadAndDelete(tunnelID)
    if !ok {
        return
    }
    
    info := value.(*TunnelInfo)
    
    if info.ControlConn != nil {
        info.ControlConn.Close()
    }
    
    // 关闭所有外部连接
    info.ExternalConns.Range(func(key, value interface{}) bool {
        conn := value.(net.Conn)
        conn.Close()
        return true
    })
    
    // 释放端口
    if info.Port > 0 {
        s.portManager.Release(info.Port)
    }
    
    log.Printf("Tunnel closed: %s", tunnelID)
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

// 辅助函数
func generateConnID() string {
    return fmt.Sprintf("%d", time.Now().UnixNano())
}

func (s *Server) readRegisterRequest(conn net.Conn) (*RegisterRequest, error) {
    // 实现注册请求解析
    // ...
}

func (s *Server) sendRegisterAck(conn net.Conn, id, publicURL string, port int, errMsg string) {
    // 实现注册响应发送
    // ...
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
    "fmt"
    "log"
    "net"
    "sync"
    "time"

    "github.com/Talbot3/go-tunnel"
    "github.com/Talbot3/go-tunnel/forward"
    "github.com/Talbot3/go-tunnel/http2"
)

// Client 隧道客户端
type Client struct {
    config ClientConfig
    
    // 控制连接
    controlConn net.Conn
    
    // 多路复用管理器
    muxMgr *forward.MuxConnManager
    
    // 本地连接映射
    localConns sync.Map // connID -> net.Conn
    
    // 生命周期
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
    
    // 隧道信息
    tunnelID  string
    publicURL string
    
    // 重连
    reconnectCh chan struct{}
    connected   bool
}

// ClientConfig 客户端配置
type ClientConfig struct {
    ServerAddr         string        // 服务器地址
    TunnelID           string        // 隧道ID（可选）
    Protocol           string        // tcp / http
    Subdomain          string        // 子域名（HTTP 隧道）
    LocalAddr          string        // 本地服务地址
    AuthToken          string
    InsecureSkipVerify bool
    ReconnectInterval  time.Duration
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
        reconnectCh: make(chan struct{}, 1),
    }, nil
}

// Start 启动客户端
func (c *Client) Start(ctx context.Context) error {
    c.ctx, c.cancel = context.WithCancel(ctx)
    
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
        
        if err := c.connect(); err != nil {
            log.Printf("Connect failed: %v, reconnecting in %v...", err, c.config.ReconnectInterval)
            
            select {
            case <-c.ctx.Done():
                return
            case <-time.After(c.config.ReconnectInterval):
                continue
            }
        }
        
        c.connected = true
        
        select {
        case <-c.ctx.Done():
            return
        case <-c.reconnectCh:
            log.Println("Reconnecting...")
            c.connected = false
        }
    }
}

// connect 连接服务器
func (c *Client) connect() error {
    // 1. 创建控制连接
    tlsConfig := &tls.Config{
        InsecureSkipVerify: c.config.InsecureSkipVerify,
    }
    
    p := http2.New(tlsConfig)
    conn, err := p.Dial(c.ctx, c.config.ServerAddr)
    if err != nil {
        return fmt.Errorf("dial server failed: %w", err)
    }
    c.controlConn = conn
    
    log.Printf("Connected to %s", c.config.ServerAddr)
    
    // 2. 注册隧道
    if err := c.registerTunnel(); err != nil {
        conn.Close()
        return fmt.Errorf("register tunnel failed: %w", err)
    }
    
    // 3. 创建多路复用管理器
    encoder := forward.NewDefaultMuxEncoder()
    decoder := forward.NewDefaultMuxDecoder()
    c.muxMgr = forward.NewMuxConnManager(conn, encoder, decoder)
    
    // 4. 启动心跳
    c.wg.Add(1)
    go c.heartbeatLoop()
    
    // 5. 启动消息处理
    c.wg.Add(1)
    go c.messageLoop()
    
    return nil
}

// registerTunnel 注册隧道
func (c *Client) registerTunnel() error {
    // 发送注册请求并接收响应
    // ...
    return nil
}

// heartbeatLoop 心跳循环
func (c *Client) heartbeatLoop() {
    defer c.wg.Done()
    
    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()
    
    encoder := forward.NewDefaultMuxEncoder()
    
    for {
        select {
        case <-c.ctx.Done():
            return
        case <-ticker.C:
            // 发送心跳（使用自定义消息类型）
            // ...
        }
    }
}

// messageLoop 消息循环
func (c *Client) messageLoop() {
    defer c.wg.Done()
    
    buf := make([]byte, 64*1024)
    
    for {
        select {
        case <-c.ctx.Done():
            return
        default:
        }
        
        n, err := c.controlConn.Read(buf)
        if err != nil {
            if c.ctx.Err() == nil {
                log.Printf("Read error: %v", err)
                select {
                case c.reconnectCh <- struct{}{}:
                default:
                }
            }
            return
        }
        
        // 使用 MuxConnManager 处理消息
        if err := c.muxMgr.HandleIncoming(buf[:n]); err != nil {
            log.Printf("Handle incoming error: %v", err)
        }
    }
}

// handleNewConnection 处理新连接通知
func (c *Client) handleNewConnection(connID, remoteAddr string) {
    // 连接本地服务
    localConn, err := net.Dial("tcp", c.config.LocalAddr)
    if err != nil {
        log.Printf("Failed to connect local service: %v", err)
        // 发送关闭消息
        encoder := forward.NewDefaultMuxEncoder()
        closeMsg, _ := encoder.EncodeClose(connID)
        c.controlConn.Write(closeMsg)
        encoder.Release(closeMsg)
        return
    }
    
    // 保存本地连接
    c.localConns.Store(connID, localConn)
    
    log.Printf("New connection: %s -> %s", connID, c.config.LocalAddr)
    
    // 使用 BidirectionalMuxForwarder 进行双向转发
    encoder := forward.NewDefaultMuxEncoder()
    decoder := forward.NewDefaultMuxDecoder()
    forwarder := forward.NewBidirectionalMuxForwarder()
    
    go func() {
        defer func() {
            localConn.Close()
            c.localConns.Delete(connID)
        }()
        
        forwarder.ForwardBidirectionalMux(c.ctx, localConn, c.controlConn, connID, encoder, decoder)
    }()
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

// PublicURL 获取公网地址
func (c *Client) PublicURL() string {
    return c.publicURL
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
        ControlAddr:    ":443",
        AutoTLS:        true,
        Email:          "admin@example.com",
        Domains:        []string{"tunnel.example.com"},
        PortRangeStart: 10000,
        PortRangeEnd:   20000,
        MaxConnections: 10000,
        AuthToken:      os.Getenv("TUNNEL_AUTH_TOKEN"),
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
    "time"

    "github.com/Talbot3/go-tunnel/examples/tunnel-client/client"
)

func main() {
    cfg := client.ClientConfig{
        ServerAddr:        "tunnel.example.com:443",
        Protocol:          "tcp",
        LocalAddr:         "localhost:8080",
        AuthToken:         os.Getenv("TUNNEL_AUTH_TOKEN"),
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
    
    log.Printf("Tunnel client started")
    log.Printf("Public URL: %s", c.PublicURL())
    
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    
    log.Println("Shutting down...")
    c.Stop()
}
```

### go-tunnel 多路复用 API 详解

#### MuxEncoder / MuxDecoder

```go
import "github.com/Talbot3/go-tunnel/forward"

// 创建编码器和解码器
encoder := forward.NewDefaultMuxEncoder()
decoder := forward.NewDefaultMuxDecoder()

// 编码消息
dataMsg, _ := encoder.EncodeData("conn1", []byte("hello"))
closeMsg, _ := encoder.EncodeClose("conn1")
newConnMsg, _ := encoder.EncodeNewConn("conn1", "192.168.1.1:12345")

// 解码消息
msgType, id, payload, _ := decoder.Decode(dataMsg)

// 释放缓冲区（重要：复用缓冲区减少 GC）
encoder.Release(dataMsg)
encoder.Release(closeMsg)
```

#### MuxForwarder

```go
// 单向转发：本地连接 -> 多路复用通道
forwarder := forward.NewMuxForwarder()
err := forwarder.ForwardMux(ctx, localConn, muxConn, "conn1", encoder)
```

#### BidirectionalMuxForwarder

```go
// 双向转发：同时处理两个方向
forwarder := forward.NewBidirectionalMuxForwarder()
err := forwarder.ForwardBidirectionalMux(ctx, localConn, muxConn, "conn1", encoder, decoder)
```

#### MuxConnManager

```go
// 管理多个连接
encoder := forward.NewDefaultMuxEncoder()
decoder := forward.NewDefaultMuxDecoder()
mgr := forward.NewMuxConnManager(muxConn, encoder, decoder)

// 添加连接（自动开始转发，自动应用 TCP 优化）
mgr.AddConnection("conn1", localConn)

// 处理接收到的消息
mgr.HandleIncoming(data)

// 获取统计信息
stats := mgr.Stats()
fmt.Printf("Active: %d, Total: %d\n", stats.ActiveConnections, stats.TotalConnections)
```

### 性能优化说明

go-tunnel 的多路复用组件已集成以下优化：

| 优化项 | 说明 | 效果 |
|--------|------|------|
| 缓冲池 | 使用 `internal/pool` 复用缓冲区 | 减少 GC 压力 |
| 背压控制 | 使用 `internal/backpressure` | 防止内存溢出 |
| TCP 优化 | 自动应用 `OptimizeTCPConn` | 降低延迟 |
| Release 方法 | 支持缓冲区复用 | 减少内存分配 |

### 架构优化建议

#### 1. 使用独立数据通道

```go
// 控制通道：隧道注册、心跳、命令
// 数据通道：专门用于数据传输

// 客户端建立两个连接：
// 1. 控制连接 (HTTP/2) - 长连接，处理控制消息
// 2. 数据连接池 (TCP/QUIC) - 按需创建，传输数据
```

#### 2. HTTP/2 多流模式

```go
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
```

### go-tunnel 提供的能力总结

| 能力 | 用途 | 相关 API |
|------|------|----------|
| 协议支持 | 控制通道协议选择 | `http2.New()`, `quic.New()` |
| 多路复用 | 共享连接多路转发 | `forward.MuxForwarder`, `forward.MuxConnManager` |
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
