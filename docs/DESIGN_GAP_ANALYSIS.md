# QUIC 隧道系统设计差距分析与实现计划

## 一、差距分析总结

### 1.1 完全达成的设计目标 ✅

| 目标 | 实现位置 | 说明 |
|------|----------|------|
| 控制面/数据面分离 | `quic/quic.go` | 通过 StreamTypeControl (0x00) 和 StreamTypeData (0x01) 区分 |
| QUIC 原生多路复用 | `quic/quic.go` | 每个外部连接映射到一个 QUIC Stream |
| 安全认证 | `quic/quic.go` | Token 认证 + 常量时间比较防时序攻击 |
| 流隔离 | `forward/mux.go` | sync.Map 管理连接，独立流控 |
| 心跳保活 | `quic/quic.go` | 15s 心跳间隔，超时触发重连 |
| 熔断器 | `internal/circuit/circuit.go` | 三态熔断器（关闭/打开/半开） |
| 指数退避重试 | `internal/retry/retry.go` | 支持抖动的指数退避 |
| 优雅关闭 | `internal/shutdown/shutdown.go` | 优先级回调 + 超时控制 |
| 资源限制 | `internal/limiter/limiter.go` | 连接/速率/内存/Goroutine 限制 |

### 1.2 部分达成的目标 ⚠️

| 目标 | 当前状态 | 差距 |
|------|----------|------|
| 多协议支持 | TCP/HTTP/QUIC | 缺少 UDP、HTTP/2、HTTP/3 特定处理 |
| 连接迁移 | 依赖 quic-go | 无应用层追踪 |

### 1.3 未实现的目标 ❌

| 目标 | 设计要求 | 当前状态 |
|------|----------|----------|
| 自定义扩展 | TLS 扩展/Transport Parameters | 未实现 |

---

## 测试覆盖率

**当前覆盖率: 80.1%** (100+ 测试用例，全部通过)

### 内部函数覆盖情况

| 函数 | 覆盖率 | 说明 |
|------|--------|------|
| `handleDataStream` | 82.7% | 处理服务端数据流 |
| `forwardBidirectional` | 91.9% | 双向数据转发 |
| `handleDatagramHeartbeat` | 91.7% | DATAGRAM 心跳处理 |
| `sendStreamHeartbeat` | 100% | 流心跳发送 |
| `handleData` | 95.8% | 客户端数据消息处理 |

**新增测试文件**: `quic/quic_internal_test.go` - 使用 mock 实现直接测试内部函数

---

## 二、端口复用方案

### 2.1 当前实现

每个隧道需要独立端口：
- TCP 隧道：独立 TCP 端口（由 PortManager 分配）
- QUIC 隧道：独立 UDP 端口

### 2.2 端口复用方案

所有隧道共用一个 QUIC 端口：

```
                    ┌─────────────────────────────────────┐
                    │         QUIC Server (:443)          │
                    │                                     │
外部用户 ───────────►│  通过 SNI/ALPN 或流首帧路由        │
                    │                                     │
                    │  ┌─────────┐  ┌─────────┐          │
                    │  │ Tunnel 1│  │ Tunnel 2│  ...     │
                    │  │ 目标:8080│  │ 目标:3306│         │
                    │  └────┬────┘  └────┬────┘          │
                    │       │            │               │
                    └───────┼────────────┼───────────────┘
                            │            │
                      内网服务:8080  内网服务:3306
```

**实现方式**：
1. **SNI 路由**：不同域名指向不同隧道（如 `app1.tunnel.com` → Tunnel 1）
2. **流首帧路由**：每个数据流首帧携带隧道 ID 或目标地址

**优势**：
- 单端口暴露，防火墙友好
- 完全复用 QUIC 的 TLS 1.3 加密
- 支持多路复用和连接迁移

---

## 三、实现计划

### 3.1 阶段 1：统一会话头格式（高优先级）

**目标**：每个数据流首帧携带协议类型和目标地址，支持动态路由

**当前数据流格式**：
```
[1B StreamType][2B connID_len][connID][data...]
```

**新会话头格式**：
```
[1B StreamType][1B ProtoType][2B TargetLen][Target][1B Flags][4B ConnID][data...]
```

**协议类型扩展**：
```go
const (
    ProtocolTCP   byte = 0x01  // 已有
    ProtocolHTTP  byte = 0x02  // 已有
    ProtocolQUIC  byte = 0x03  // 已有
    ProtocolHTTP2 byte = 0x04  // 新增：HTTP/2 特定处理
    ProtocolHTTP3 byte = 0x05  // 新增：HTTP/3 流映射
)
```

**Flags 定义**：
```go
const (
    FlagKeepAlive       byte = 0x01  // 保活标志
    FlagHTTP2FrameMap   byte = 0x04  // HTTP/2 流映射优化
)
```

**修改文件**：
- `quic/quic.go`：
  - 添加 `SessionHeader` 结构体
  - 修改 `handleDataStreams()` 解析新格式
  - 修改客户端 `OpenStreamSync()` 发送新格式

### 3.2 阶段 2：DATAGRAM 支持（中优先级）

**目标**：使用 QUIC DATAGRAM 帧（RFC 9221）进行轻量信令

**配置启用**：
```go
func DefaultConfig() *quic.Config {
    return &quic.Config{
        MaxIncomingStreams:    10000,
        MaxIncomingUniStreams: 1000,
        KeepAlivePeriod:       30 * time.Second,
        MaxIdleTimeout:        5 * time.Minute,
        EnableDatagrams:       true,  // 新增
        MaxDatagramFrameSize:  1200,  // 新增
    }
}
```

**DATAGRAM 消息类型**：
```go
const (
    DgramTypeHeartbeat byte = 0x01  // 心跳
    DgramTypeHeartbeatAck byte = 0x02  // 心跳响应
    DgramTypeNetworkQuality byte = 0x03  // 网络质量上报
)
```

**心跳 DATAGRAM 格式**：
```
[1B DgramTypeHeartbeat][4B Timestamp][4B LastRTT]
```

**优势**：
- 比 Stream 开销低 1-2 RTT
- 适合高频信令

**修改文件**：
- `quic/quic.go`：
  - 启用 DATAGRAM 配置
  - 添加 `datagramLoop()` 处理 DATAGRAM 收发
  - 修改心跳逻辑，优先使用 DATAGRAM

### 3.3 阶段 3：动态配置更新（中优先级）

**目标**：支持运行时动态更新路由/限流规则

**新增消息类型**：
```go
const (
    MsgTypeConfigUpdate byte = 0x09  // 动态配置更新
    MsgTypeConfigAck    byte = 0x0A  // 配置更新确认
)
```

**配置更新格式**：
```
[1B MsgTypeConfigUpdate][2B ConfigLen][ConfigJSON]
```

**配置内容**：
```json
{
    "tunnel_id": "tunnel-123",
    "action": "update_rate_limit",
    "params": {
        "max_conns": 100,
        "rate_limit": 1000
    }
}
```

**修改文件**：
- `quic/quic.go`：
  - 添加 `handleConfigUpdate()` 处理配置更新
  - 添加 `ConfigManager` 管理动态配置

### 3.4 阶段 4：增强心跳（低优先级）

**目标**：心跳携带网络质量指标

**当前心跳格式**：
```
[1B MsgTypeHeartbeat]
```

**增强心跳格式**：
```
[1B MsgTypeHeartbeat][4B Timestamp][4B RTT][4B BytesIn][4B BytesOut][2B LossRate]
```

**用途**：
- 监控网络质量
- 自适应调整缓冲区大小
- 触发预警

### 3.5 阶段 5：0-RTT 显式支持（低优先级）

**目标**：显式启用 0-RTT，缓存隧道状态

**TLS 配置**：
```go
tlsConfig := &tls.Config{
    NextProtos:         []string{"penet-v2"},
    ClientSessionCache: tls.NewLRUClientSessionCache(100),  // 启用会话票据缓存
}
```

**隧道状态缓存**：
```go
type TunnelState struct {
    TunnelID    string
    LocalAddr   string
    Protocol    byte
    LastActive  time.Time
}

// 客户端缓存隧道状态
func (c *MuxClient) cacheTunnelState(state *TunnelState) {
    // 持久化到本地文件或内存
}
```

**快速恢复流程**：
1. 断线后读取缓存的隧道状态
2. 使用 0-RTT 快速重连
3. 跳过 REGISTER 直接恢复数据传输

---

## 四、文件修改清单

| 文件 | 修改内容 | 优先级 |
|------|---------|--------|
| `quic/quic.go` | 统一会话头、DATAGRAM、ConfigUpdate、增强心跳 | 高 |
| `quic/quic_test.go` | 新功能单元测试 | 高 |
| `forward/mux.go` | 协议类型编码扩展 | 中 |
| `config/config.go` | 新配置选项（EnableDatagram 等） | 中 |
| `docs/QUIC_MUX_DESIGN.md` | 更新设计文档 | 低 |

---

## 五、验证计划

### 5.1 单元测试

```go
// quic/quic_test.go

func TestSessionHeader(t *testing.T) {
    // 测试会话头编码/解码
    header := &SessionHeader{
        Protocol: ProtocolTCP,
        Target:   "127.0.0.1:8080",
        Flags:    FlagKeepAlive,
        ConnID:   "conn-123",
    }
    encoded := header.Encode()
    decoded, err := DecodeSessionHeader(encoded)
    assert.NoError(t, err)
    assert.Equal(t, header, decoded)
}

func TestDatagramHeartbeat(t *testing.T) {
    // 测试 DATAGRAM 心跳
}

func TestConfigUpdate(t *testing.T) {
    // 测试动态配置更新
}
```

### 5.2 集成测试

```bash
# 启动服务端
./proxy server --config server.yaml

# 启动客户端
./proxy client --config client.yaml

# 测试端口复用
curl https://app1.tunnel.example.com
curl https://app2.tunnel.example.com

# 测试动态配置
./proxy config update --tunnel tunnel-123 --rate-limit 2000
```

### 5.3 性能测试

```bash
# DATAGRAM vs Stream 心跳延迟对比
benchstat datagram.txt stream.txt

# 吞吐量测试
iperf3 -c localhost -p 443 --quic
```

### 5.4 兼容性测试

- 确保新客户端可以连接旧服务端（向后兼容）
- 确保旧客户端可以连接新服务端（向前兼容）
- 使用版本协商机制

---

## 六、风险评估

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 会话头格式变更导致不兼容 | 高 | 使用版本号协商，旧版本使用旧格式 |
| DATAGRAM 大小超 MTU | 中 | 限制 maxDatagramFrameSize ≤ 1200 |
| 动态配置冲突 | 中 | 使用乐观锁 + 版本号 |
| 0-RTT 重放攻击 | 高 | 限制 0-RTT 数据为幂等操作 |

---

## 七、时间估算

| 阶段 | 工作量 | 说明 |
|------|--------|------|
| 阶段 1：统一会话头 | 2-3 天 | 核心功能，需仔细测试兼容性 |
| 阶段 2：DATAGRAM | 1-2 天 | 相对独立，风险低 |
| 阶段 3：动态配置 | 1-2 天 | 需设计配置格式 |
| 阶段 4：增强心跳 | 0.5 天 | 扩展现有逻辑 |
| 阶段 5：0-RTT | 1 天 | 需测试重放攻击防护 |

**总计**：约 5-8 天
