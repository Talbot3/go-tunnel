# go-tunnel 使用示例

本文档提供 go-tunnel 的完整使用示例。接口详细说明请使用 `go doc` 命令查看。

## 目录

- [安全隧道服务端](#安全隧道服务端)
- [安全隧道客户端](#安全隧道客户端)
- [入口协议配置](#入口协议配置)
- [后端协议配置](#后端协议配置)
- [SDK 工具函数](#sdk-工具函数)
- [企业级部署](#企业级部署)

---

## 安全隧道服务端

### 基础服务端

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    // 加载 TLS 证书
    cert, err := tls.LoadX509KeyPair("server.crt", "server.key")
    if err != nil {
        log.Fatalf("加载证书失败: %v", err)
    }

    tlsConfig := &tls.Config{
        Certificates: []tls.Certificate{cert},
        MinVersion:   tls.VersionTLS13, // 强制 TLS 1.3
    }

    // 创建服务端
    server := quic.NewMuxServer(quic.MuxServerConfig{
        ListenAddr: ":443",
        TLSConfig:  tlsConfig,
        AuthToken:  "your-secure-token",
    })

    // 启动服务
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    if err := server.Start(ctx); err != nil {
        log.Fatalf("启动失败: %v", err)
    }
    defer server.Stop()

    log.Println("安全隧道服务已启动，监听 :443")

    // 等待中断信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    log.Println("正在关闭服务...")
}
```

### 完整配置的服务端

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "time"

    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    cert, _ := tls.LoadX509KeyPair("server.crt", "server.key")

    server := quic.NewMuxServer(quic.MuxServerConfig{
        ListenAddr: ":443",
        TLSConfig: &tls.Config{
            Certificates: []tls.Certificate{cert},
            MinVersion:   tls.VersionTLS13,
        },
        
        // 入口协议配置
        EntryProtocols: quic.EntryProtocolConfig{
            EnableHTTP3:  true,  // HTTP/3 入口
            EnableQUIC:   true,  // QUIC+TLS 入口
            EnableTCPTLS: true,  // TCP+TLS 入口
            QUICALPN:     "quic-tunnel",
        },
        
        // 认证配置
        AuthToken: "strong-random-token-32chars",
        
        // 端口范围（用于外部访问）
        PortRangeStart: 10000,
        PortRangeEnd:   20000,
        
        // 资源限制
        MaxTunnels:       1000,
        MaxConnsPerTunnel: 100,
        
        // 超时配置
        TunnelTimeout: 5 * time.Minute,
        ConnTimeout:   10 * time.Minute,
    })

    ctx := context.Background()
    if err := server.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer server.Stop()

    // 获取统计信息
    stats := server.GetStats()
    log.Printf("活跃隧道: %d, 活跃连接: %d", 
        stats.ActiveTunnels, stats.ActiveConns)

    select {}
}
```

---

## 安全隧道客户端

### 基础客户端

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

    // 创建客户端
    client := quic.NewMuxClient(quic.MuxClientConfig{
        ServerAddr: "tunnel.example.com:443",
        TLSConfig:  tlsConfig,
        Protocol:   quic.ProtocolTCP,
        LocalAddr:  "127.0.0.1:8080", // 内网服务地址
        AuthToken:  "your-secure-token",
    })

    // 启动客户端
    ctx := context.Background()
    if err := client.Start(ctx); err != nil {
        log.Fatalf("启动失败: %v", err)
    }
    defer client.Stop()

    log.Printf("隧道已建立: %s", client.PublicURL())
    log.Printf("隧道 ID: %s", client.TunnelID())

    select {} // 阻塞主线程
}
```

### 带域名的客户端（单端口多路复用）

```go
package main

import (
    "context"
    "crypto/tls"
    "log"

    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    tlsConfig := &tls.Config{
        InsecureSkipVerify: true, // 测试环境
        MinVersion:         tls.VersionTLS13,
    }

    // 创建带域名标识的客户端
    client := quic.NewMuxClient(quic.MuxClientConfig{
        ServerAddr: "tunnel.example.com:443",
        TLSConfig:  tlsConfig,
        Protocol:   quic.ProtocolTCP,
        LocalAddr:  "127.0.0.1:3000", // 本地服务
        Domain:     "app.example.com", // 域名标识
        AuthToken:  "your-secure-token",
    })

    ctx := context.Background()
    if err := client.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer client.Stop()

    // 公网访问地址
    log.Printf("公网访问: https://%s", client.PublicURL())

    select {}
}
```

### 完整配置的客户端

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "time"

    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    client := quic.NewMuxClient(quic.MuxClientConfig{
        ServerAddr: "tunnel.example.com:443",
        TLSConfig: &tls.Config{
            InsecureSkipVerify: false,
            MinVersion:         tls.VersionTLS13,
        },
        
        // 隧道配置
        Protocol:  quic.ProtocolTCP,
        LocalAddr: "127.0.0.1:8080",
        Domain:    "myapp.example.com",
        AuthToken: "your-secure-token",
        
        // 重连配置
        ReconnectInterval: 5 * time.Second,
        MaxReconnectTries: 10,
        
        // 0-RTT 快速重连
        Enable0RTT:    true,
        SessionCache:  tls.NewLRUClientSessionCache(100),
    })

    ctx := context.Background()
    if err := client.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer client.Stop()

    // 获取统计信息
    stats := client.GetStats()
    log.Printf("已连接: %v, 活跃连接: %d", 
        stats.Connected, stats.ActiveConns)

    select {}
}
```

---

## 入口协议配置

### HTTP/3 入口

```go
// HTTP/3 入口配置
server := quic.NewMuxServer(quic.MuxServerConfig{
    ListenAddr: ":443",
    TLSConfig:  tlsConfig,
    EntryProtocols: quic.EntryProtocolConfig{
        EnableHTTP3: true, // ALPN "h3"
    },
})
```

**访问方式：**
```bash
# 使用 curl 访问
curl --http3 https://app.example.com:443/api
```

### QUIC+TLS 入口

```go
// QUIC+TLS 入口配置
server := quic.NewMuxServer(quic.MuxServerConfig{
    ListenAddr: ":443",
    TLSConfig:  tlsConfig,
    EntryProtocols: quic.EntryProtocolConfig{
        EnableQUIC: true, // ALPN "quic-tunnel"
    },
})
```

**访问方式：**
```go
// 使用 QUIC 客户端连接
client := quic.NewMuxClient(quic.MuxClientConfig{
    ServerAddr: "tunnel.example.com:443",
    Protocol:   quic.ProtocolQUIC,
    // ...
})
```

### TCP+TLS 入口

```go
// TCP+TLS 入口配置
server := quic.NewMuxServer(quic.MuxServerConfig{
    ListenAddr: ":443",
    TLSConfig:  tlsConfig,
    EntryProtocols: quic.EntryProtocolConfig{
        EnableTCPTLS: true,
    },
})
```

**访问方式：**
```go
// 使用 SDK 连接
conn, err := quic.DialTunnel("app.example.com", "tunnel.example.com:443")
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

// 发送数据
conn.Write([]byte("Hello!"))
```

---

## 后端协议配置

### TCP 后端

```go
client := quic.NewMuxClient(quic.MuxClientConfig{
    ServerAddr: "tunnel.example.com:443",
    Protocol:   quic.ProtocolTCP,  // 0x01
    LocalAddr:  "127.0.0.1:8080",  // 本地 TCP 服务
})
```

### HTTP 后端

```go
client := quic.NewMuxClient(quic.MuxClientConfig{
    ServerAddr: "tunnel.example.com:443",
    Protocol:   quic.ProtocolHTTP,  // 0x02
    LocalAddr:  "127.0.0.1:80",     // 本地 HTTP 服务
})
```

### QUIC 后端

```go
client := quic.NewMuxClient(quic.MuxClientConfig{
    ServerAddr: "tunnel.example.com:443",
    Protocol:   quic.ProtocolQUIC,  // 0x03
    LocalAddr:  "127.0.0.1:443",    // 本地 QUIC 服务
})
```

### HTTP/2 后端

```go
client := quic.NewMuxClient(quic.MuxClientConfig{
    ServerAddr: "tunnel.example.com:443",
    Protocol:   quic.ProtocolHTTP2,  // 0x04
    LocalAddr:  "127.0.0.1:8080",   // 本地 HTTP/2 服务
})
```

### HTTP/3 后端

```go
client := quic.NewMuxClient(quic.MuxClientConfig{
    ServerAddr: "tunnel.example.com:443",
    Protocol:   quic.ProtocolHTTP3,  // 0x05
    LocalAddr:  "127.0.0.1:443",     // 本地 HTTP/3 服务
})
```

---

## SDK 工具函数

### DialTunnel - 连接隧道

```go
package main

import (
    "io"
    "log"
    
    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    // 连接到 TCP+TLS 隧道入口
    conn, err := quic.DialTunnel(
        "app.example.com",        // 域名标识
        "tunnel.example.com:443", // 隧道服务器地址
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    // 发送 HTTP 请求
    conn.Write([]byte("GET /api HTTP/1.1\r\nHost: app.example.com\r\n\r\n"))

    // 读取响应
    buf := make([]byte, 4096)
    n, _ := conn.Read(buf)
    log.Printf("响应: %s", string(buf[:n]))
}
```

### DialTunnelWithPayload - 带初始数据连接

```go
package main

import (
    "log"
    
    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    // 构造 HTTP 请求
    httpRequest := []byte("GET /api HTTP/1.1\r\n" +
        "Host: app.example.com\r\n" +
        "Connection: close\r\n\r\n")

    // 连接并发送初始数据
    conn, err := quic.DialTunnelWithPayload(
        "app.example.com",
        "tunnel.example.com:443",
        httpRequest,
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    // 读取响应
    buf := make([]byte, 4096)
    n, _ := conn.Read(buf)
    log.Printf("响应: %s", string(buf[:n]))
}
```

### TunnelConn - 带域名信息的连接

```go
package main

import (
    "log"
    
    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    // 创建带域名信息的连接
    conn, err := quic.DialTunnelConn(
        "app.example.com",
        "tunnel.example.com:443",
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    // 访问域名信息
    log.Printf("连接域名: %s", conn.Domain)

    // 使用连接
    conn.Write([]byte("Hello!"))
}
```

---

## 企业级部署

### Docker Compose 部署

```yaml
# docker-compose.yml
version: '3.8'

services:
  tunnel-server:
    build: .
    ports:
      - "443:443/udp"
      - "443:443/tcp"
    volumes:
      - ./certs:/etc/certs:ro
    environment:
      - AUTH_TOKEN=your-secure-token
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
```

### Kubernetes 部署

```yaml
# kubernetes.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tunnel-config
data:
  AUTH_TOKEN: "your-secure-token"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tunnel-server
spec:
  replicas: 3
  selector:
    matchLabels:
      app: tunnel-server
  template:
    metadata:
      labels:
        app: tunnel-server
    spec:
      containers:
      - name: tunnel-server
        image: tunnel-server:latest
        ports:
        - containerPort: 443
          protocol: UDP
        - containerPort: 443
          protocol: TCP
        envFrom:
        - configMapRef:
            name: tunnel-config
        resources:
          limits:
            memory: "512Mi"
            cpu: "1000m"
          requests:
            memory: "256Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /livez
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: tunnel-server
spec:
  type: LoadBalancer
  ports:
  - name: udp
    port: 443
    protocol: UDP
    targetPort: 443
  - name: tcp
    port: 443
    protocol: TCP
    targetPort: 443
  selector:
    app: tunnel-server
```

### 健康检查端点

```go
package main

import (
    "net/http"
    
    "github.com/Talbot3/go-tunnel/quic"
)

func main() {
    server := quic.NewMuxServer(quic.MuxServerConfig{
        // ... 配置
    })

    // 健康检查端点
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        stats := server.GetStats()
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("OK"))
    })

    http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
        // 检查服务是否就绪
        w.WriteHeader(http.StatusOK)
    })

    http.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
        // 检查服务是否存活
        w.WriteHeader(http.StatusOK)
    })

    go http.ListenAndServe(":8080", nil)

    // ... 启动隧道服务
}
```

---

## 查看接口文档

使用 `go doc` 命令查看详细的接口说明：

```bash
# 查看包概览
go doc github.com/Talbot3/go-tunnel/quic

# 查看服务端配置
go doc github.com/Talbot3/go-tunnel/quic.MuxServerConfig

# 查看客户端配置
go doc github.com/Talbot3/go-tunnel/quic.MuxClientConfig

# 查看入口协议配置
go doc github.com/Talbot3/go-tunnel/quic.EntryProtocolConfig

# 查看 SDK 函数
go doc github.com/Talbot3/go-tunnel/quic.DialTunnel
```
