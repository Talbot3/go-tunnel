# go-tunnel

跨平台高性能多协议转发器，支持 TCP、HTTP/2、HTTP/3、QUIC 协议。

## 特性

- **多协议支持**: TCP、HTTP/2、HTTP/3、QUIC
- **跨平台优化**: 
  - Linux: `unix.Splice` 零拷贝
  - macOS: TCP_NOTSENT_LOWAT 优化
  - Windows: IOCP 大缓冲区优化
- **高性能**: 10+ Gbps 吞吐量，亚毫秒级延迟
- **背压控制**: 自动流量控制，防止内存溢出
- **连接池**: sync.Pool 缓冲池复用

## 安装

```bash
# 克隆仓库
git clone https://github.com/wld/go-tunnel.git
cd go-tunnel

# 编译
go build -o proxy .

# 交叉编译
GOOS=linux GOARCH=amd64 go build -o proxy_linux .
GOOS=windows GOARCH=amd64 go build -o proxy_win.exe .
```

## 使用方法

### 命令行

```bash
# TCP 转发
./proxy -protocol tcp -listen :8080 -target 127.0.0.1:80

# HTTP/2 转发
./proxy -protocol http2 -listen :8443 -target 127.0.0.1:443

# HTTP/3 转发
./proxy -protocol http3 -listen :8443 -target 127.0.0.1:443

# QUIC 转发
./proxy -protocol quic -listen :8443 -target 127.0.0.1:443

# 使用配置文件
./proxy -config config.yaml

# 查看版本
./proxy -version
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

  - name: http3
    listen: ":8444"
    target: "127.0.0.1:443"
    enabled: false

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
├── go.mod
├── main.go                      # 入口程序
├── config/
│   └── config.go                # 配置管理
├── protocol/
│   ├── protocol.go              # 协议接口
│   ├── tcp/tcp.go               # TCP 协议
│   ├── http2/http2.go           # HTTP/2 协议
│   ├── http3/http3.go           # HTTP/3 协议
│   └── quic/quic.go             # QUIC 协议
├── forward/
│   ├── forward.go               # 公共接口
│   ├── forward_linux.go         # Linux 零拷贝
│   ├── forward_darwin.go        # macOS 优化
│   └── forward_windows.go       # Windows IOCP
├── internal/
│   ├── pool/pool.go             # 缓冲池
│   └── backpressure/backpressure.go  # 背压控制
├── test_e2e.sh                  # 端到端测试
└── benchmark.sh                 # 性能测试
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
type Protocol interface {
    Name() string
    Listen(addr string) (net.Listener, error)
    Dial(addr string) (net.Conn, error)
    Forward(src, dst net.Conn) error
}
```

## 依赖

- `golang.org/x/sys` - 系统调用
- `golang.org/x/net` - HTTP/2 支持
- `github.com/quic-go/quic-go` - QUIC/HTTP/3 支持
- `gopkg.in/yaml.v3` - YAML 配置解析

## License

MIT License
