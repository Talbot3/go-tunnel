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
