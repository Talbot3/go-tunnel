# go-tunnel 多路复用扩展设计

## 一、问题分析

### 1.1 当前架构限制

当前 `Forwarder` 接口设计：

```go
type Forwarder interface {
    Forward(src, dst net.Conn) error
}
```

**限制**：直接双向转发，无法插入协议前缀进行多路复用。

### 1.2 业务场景对比

| 场景 | 架构 | 数据流 | go-tunnel 支持 |
|------|------|--------|----------------|
| 服务端 TCP | 独立连接 | 双向流 | ✅ Forwarder |
| 服务端 HTTP | 请求-响应 | 单向 | ❌ 不适用 |
| 客户端 TCP | 共享 Stream + 协议前缀 | 双向流 + 封装 | ❌ 需扩展 |
| 客户端 HTTP | 共享 Stream | 请求-响应 | ❌ 需扩展 |

### 1.3 客户端架构

```
┌─────────────────────────────────────────────────────────────┐
│                   QUIC Stream (共享)                         │
│  HANDSHAKE | REQUEST:req1:... | DATA:conn1:... | ...       │
└─────────────────────────────────────────────────────────────┘

每个数据包需要协议前缀：
- DATA:<conn_id>:<payload>
- CLOSE:<conn_id>
- REQUEST:<req_id>:<http_request>
- RESPONSE:<req_id>:<http_response>
```

---

## 二、扩展设计方案

### 2.1 核心接口扩展

```go
// forward/mux.go - 多路复用转发接口

package forward

import (
    "context"
    "io"
    "net"
)

// ============================================
// 核心接口定义
// ============================================

// Forwarder 原有接口（独立连接双向转发）
type Forwarder interface {
    Forward(src, dst net.Conn) error
}

// MuxEncoder 多路复用编码器接口
// 负责将数据封装成带协议前缀的消息
type MuxEncoder interface {
    // EncodeData 编码数据消息
    // 返回格式如: "DATA:<conn_id>:<payload>"
    EncodeData(connID string, data []byte) ([]byte, error)

    // EncodeClose 编码关闭消息
    // 返回格式如: "CLOSE:<conn_id>"
    EncodeClose(connID string) ([]byte, error)

    // EncodeRequest 编码请求消息（HTTP 场景）
    // 返回格式如: "REQUEST:<req_id>:<http_request>"
    EncodeRequest(reqID string, data []byte) ([]byte, error)

    // EncodeResponse 编码响应消息（HTTP 场景）
    // 返回格式如: "RESPONSE:<req_id>:<http_response>"
    EncodeResponse(reqID string, data []byte) ([]byte, error)
}

// MuxDecoder 多路复用解码器接口
type MuxDecoder interface {
    // Decode 解码消息
    // 返回消息类型、ID、payload
    Decode(data []byte) (msgType MessageType, id string, payload []byte, err error)
}

// MessageType 消息类型
type MessageType int

const (
    MsgTypeData     MessageType = iota // 数据消息
    MsgTypeClose                        // 关闭消息
    MsgTypeRequest                      // 请求消息
    MsgTypeResponse                     // 响应消息
    MsgTypeHandshake                    // 握手消息
)

// MuxForwarder 多路复用转发器接口
// 支持在共享连接上进行多路转发
type MuxForwarder interface {
    // ForwardMux 多路转发
    // localConn: 本地连接
    // muxConn: 多路复用连接（如 QUIC Stream）
    // connID: 连接标识（用于编码）
    // encoder: 编码器
    ForwardMux(ctx context.Context, localConn net.Conn, muxConn io.ReadWriter, connID string, encoder MuxEncoder) error
}

// ============================================
// 默认编码器实现
// ============================================

// DefaultMuxEncoder 默认编码器（文本协议）
type DefaultMuxEncoder struct {
    // Delimiter 消息分隔符，默认 "\n"
    Delimiter byte
}

func NewDefaultMuxEncoder() *DefaultMuxEncoder {
    return &DefaultMuxEncoder{Delimiter: '\n'}
}

func (e *DefaultMuxEncoder) EncodeData(connID string, data []byte) ([]byte, error) {
    // 格式: DATA:<conn_id>:<length>:<payload>\n
    // 使用长度前缀避免 payload 中包含分隔符
    header := fmt.Sprintf("DATA:%s:%d:", connID, len(data))
    result := make([]byte, len(header)+len(data)+1)
    copy(result, header)
    copy(result[len(header):], data)
    result[len(result)-1] = e.Delimiter
    return result, nil
}

func (e *DefaultMuxEncoder) EncodeClose(connID string) ([]byte, error) {
    return []byte(fmt.Sprintf("CLOSE:%s%c", connID, e.Delimiter)), nil
}

func (e *DefaultMuxEncoder) EncodeRequest(reqID string, data []byte) ([]byte, error) {
    header := fmt.Sprintf("REQUEST:%s:%d:", reqID, len(data))
    result := make([]byte, len(header)+len(data)+1)
    copy(result, header)
    copy(result[len(header):], data)
    result[len(result)-1] = e.Delimiter
    return result, nil
}

func (e *DefaultMuxEncoder) EncodeResponse(reqID string, data []byte) ([]byte, error) {
    header := fmt.Sprintf("RESPONSE:%s:%d:", reqID, len(data))
    result := make([]byte, len(header)+len(data)+1)
    copy(result, header)
    copy(result[len(header):], data)
    result[len(result)-1] = e.Delimiter
    return result, nil
}

// DefaultMuxDecoder 默认解码器
type DefaultMuxDecoder struct {
    Delimiter byte
}

func NewDefaultMuxDecoder() *DefaultMuxDecoder {
    return &DefaultMuxDecoder{Delimiter: '\n'}
}

func (d *DefaultMuxDecoder) Decode(data []byte) (MessageType, string, []byte, error) {
    // 移除末尾分隔符
    if len(data) > 0 && data[len(data)-1] == d.Delimiter {
        data = data[:len(data)-1]
    }

    // 解析格式: TYPE:ID:LENGTH:PAYLOAD
    parts := bytes.SplitN(data, []byte(":"), 4)
    if len(parts) < 2 {
        return MsgTypeData, "", nil, fmt.Errorf("invalid message format")
    }

    msgTypeStr := string(parts[0])
    id := string(parts[1])

    var msgType MessageType
    switch msgTypeStr {
    case "DATA":
        msgType = MsgTypeData
    case "CLOSE":
        msgType = MsgTypeClose
    case "REQUEST":
        msgType = MsgTypeRequest
    case "RESPONSE":
        msgType = MsgTypeResponse
    case "HANDSHAKE":
        msgType = MsgTypeHandshake
    default:
        return MsgTypeData, "", nil, fmt.Errorf("unknown message type: %s", msgTypeStr)
    }

    // 对于 DATA/REQUEST/RESPONSE，解析长度和 payload
    var payload []byte
    if len(parts) >= 4 && (msgType == MsgTypeData || msgType == MsgTypeRequest || msgType == MsgTypeResponse) {
        length, _ := strconv.Atoi(string(parts[2]))
        payload = parts[3]
        if len(payload) != length {
            // 可能数据不完整
            payload = payload[:min(length, len(payload))]
        }
    }

    return msgType, id, payload, nil
}

// ============================================
// 多路转发器实现
// ============================================

// muxForwarder 多路转发器实现
type muxForwarder struct {
    bufferSize int
}

func NewMuxForwarder() MuxForwarder {
    return &muxForwarder{
        bufferSize: 32 * 1024,
    }
}

func (f *muxForwarder) ForwardMux(ctx context.Context, localConn net.Conn, muxConn io.ReadWriter, connID string, encoder MuxEncoder) error {
    buf := make([]byte, f.bufferSize)

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // 从本地连接读取
        n, err := localConn.Read(buf)
        if err != nil {
            if err == io.EOF || IsClosedErr(err) {
                // 发送关闭消息
                closeMsg, _ := encoder.EncodeClose(connID)
                muxConn.Write(closeMsg)
                return nil
            }
            return err
        }

        if n == 0 {
            continue
        }

        // 编码并发送
        encoded, err := encoder.EncodeData(connID, buf[:n])
        if err != nil {
            return err
        }

        _, err = muxConn.Write(encoded)
        if err != nil {
            return err
        }
    }
}

// ============================================
// 双向多路转发
// ============================================

// BidirectionalMuxForwarder 双向多路转发器
// 同时处理本地->远程 和 远程->本地
type BidirectionalMuxForwarder interface {
    // ForwardBidirectionalMux 双向多路转发
    // 自动处理两个方向的数据转发
    ForwardBidirectionalMux(
        ctx context.Context,
        localConn net.Conn,
        muxConn io.ReadWriter,
        connID string,
        encoder MuxEncoder,
        decoder MuxDecoder,
        onResponse func(payload []byte), // 收到响应时的回调
    ) error
}

type bidirectionalMuxForwarder struct {
    bufferSize int
}

func NewBidirectionalMuxForwarder() BidirectionalMuxForwarder {
    return &bidirectionalMuxForwarder{
        bufferSize: 32 * 1024,
    }
}

func (f *bidirectionalMuxForwarder) ForwardBidirectionalMux(
    ctx context.Context,
    localConn net.Conn,
    muxConn io.ReadWriter,
    connID string,
    encoder MuxEncoder,
    decoder MuxDecoder,
    onResponse func(payload []byte),
) error {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    errCh := make(chan error, 2)

    // 本地 -> 远程
    go func() {
        buf := make([]byte, f.bufferSize)
        for {
            select {
            case <-ctx.Done():
                errCh <- nil
                return
            default:
            }

            n, err := localConn.Read(buf)
            if err != nil {
                if err == io.EOF || IsClosedErr(err) {
                    closeMsg, _ := encoder.EncodeClose(connID)
                    muxConn.Write(closeMsg)
                    errCh <- nil
                    return
                }
                errCh <- err
                return
            }

            if n > 0 {
                encoded, _ := encoder.EncodeData(connID, buf[:n])
                if _, err := muxConn.Write(encoded); err != nil {
                    errCh <- err
                    return
                }
            }
        }
    }()

    // 远程 -> 本地
    go func() {
        buf := make([]byte, f.bufferSize)
        for {
            select {
            case <-ctx.Done():
                errCh <- nil
                return
            default:
            }

            n, err := muxConn.Read(buf)
            if err != nil {
                errCh <- err
                return
            }

            if n > 0 {
                // 解码消息
                msgType, _, payload, err := decoder.Decode(buf[:n])
                if err != nil {
                    continue
                }

                switch msgType {
                case MsgTypeData, MsgTypeResponse:
                    if onResponse != nil {
                        onResponse(payload)
                    }
                    localConn.Write(payload)
                case MsgTypeClose:
                    localConn.Close()
                    errCh <- nil
                    return
                }
            }
        }
    }()

    return <-errCh
}
```

### 2.2 HTTP 请求-响应模式支持

```go
// forward/http_mux.go - HTTP 多路复用支持

package forward

import (
    "bufio"
    "bytes"
    "context"
    "fmt"
    "io"
    "net"
    "net/http"
)

// HTTPMuxForwarder HTTP 多路转发器
// 处理 HTTP 请求-响应模式
type HTTPMuxForwarder interface {
    // ForwardHTTPMux HTTP 请求转发
    // 将 HTTP 请求编码发送，等待响应
    ForwardHTTPMux(
        ctx context.Context,
        req *http.Request,
        muxConn io.ReadWriter,
        reqID string,
        encoder MuxEncoder,
        decoder MuxDecoder,
    ) (*http.Response, error)
}

type httpMuxForwarder struct {
    timeout time.Duration
}

func NewHTTPMuxForwarder(timeout time.Duration) HTTPMuxForwarder {
    return &httpMuxForwarder{timeout: timeout}
}

func (f *httpMuxForwarder) ForwardHTTPMux(
    ctx context.Context,
    req *http.Request,
    muxConn io.ReadWriter,
    reqID string,
    encoder MuxEncoder,
    decoder MuxDecoder,
) (*http.Response, error) {
    // 序列化 HTTP 请求
    var buf bytes.Buffer
    req.Write(&buf)

    // 编码并发送
    encoded, err := encoder.EncodeRequest(reqID, buf.Bytes())
    if err != nil {
        return nil, err
    }

    if _, err := muxConn.Write(encoded); err != nil {
        return nil, err
    }

    // 等待响应
    respBuf := make([]byte, 64*1024)
    n, err := muxConn.Read(respBuf)
    if err != nil {
        return nil, err
    }

    // 解码响应
    msgType, id, payload, err := decoder.Decode(respBuf[:n])
    if err != nil {
        return nil, err
    }

    if msgType != MsgTypeResponse || id != reqID {
        return nil, fmt.Errorf("unexpected message: type=%d, id=%s", msgType, id)
    }

    // 解析 HTTP 响应
    resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(payload)), req)
    if err != nil {
        return nil, err
    }

    return resp, nil
}

// HTTPMuxHandler HTTP 多路处理器（服务端）
type HTTPMuxHandler interface {
    // HandleHTTPMux 处理接收到的 HTTP 请求
    // 转发到本地服务并返回响应
    HandleHTTPMux(
        ctx context.Context,
        reqData []byte,
        localAddr string,
        reqID string,
        muxConn io.ReadWriter,
        encoder MuxEncoder,
    ) error
}

type httpMuxHandler struct {
    client *http.Client
}

func NewHTTPMuxHandler() HTTPMuxHandler {
    return &httpMuxHandler{
        client: &http.Client{
            Timeout: 30 * time.Second,
        },
    }
}

func (h *httpMuxHandler) HandleHTTPMux(
    ctx context.Context,
    reqData []byte,
    localAddr string,
    reqID string,
    muxConn io.ReadWriter,
    encoder MuxEncoder,
) error {
    // 解析 HTTP 请求
    reader := bufio.NewReader(bytes.NewReader(reqData))
    req, err := http.ReadRequest(reader)
    if err != nil {
        return err
    }

    // 修改目标地址
    req.URL.Scheme = "http"
    req.URL.Host = localAddr

    // 转发到本地服务
    resp, err := h.client.Do(req.WithContext(ctx))
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    // 序列化响应
    var respBuf bytes.Buffer
    resp.Write(&respBuf)

    // 编码并发送
    encoded, err := encoder.EncodeResponse(reqID, respBuf.Bytes())
    if err != nil {
        return err
    }

    _, err = muxConn.Write(encoded)
    return err
}
```

### 2.3 连接管理器扩展

```go
// forward/conn_mux.go - 连接管理器扩展

package forward

import (
    "context"
    "io"
    "net"
    "sync"
)

// MuxConnManager 多路连接管理器
// 管理共享连接上的多个虚拟连接
type MuxConnManager struct {
    // connections 连接映射: connID -> localConn
    connections sync.Map

    // encoder 编码器
    encoder MuxEncoder

    // decoder 解码器
    decoder MuxDecoder

    // muxConn 多路复用连接
    muxConn io.ReadWriter

    // forwarder 多路转发器
    forwarder MuxForwarder

    // ctx 上下文
    ctx    context.Context
    cancel context.CancelFunc
}

func NewMuxConnManager(muxConn io.ReadWriter, encoder MuxEncoder, decoder MuxDecoder) *MuxConnManager {
    ctx, cancel := context.WithCancel(context.Background())
    return &MuxConnManager{
        encoder:   encoder,
        decoder:   decoder,
        muxConn:   muxConn,
        forwarder: NewMuxForwarder(),
        ctx:       ctx,
        cancel:    cancel,
    }
}

// AddConnection 添加新连接
func (m *MuxConnManager) AddConnection(connID string, localConn net.Conn) {
    m.connections.Store(connID, localConn)

    // 启动转发
    go m.forwarder.ForwardMux(m.ctx, localConn, m.muxConn, connID, m.encoder)
}

// RemoveConnection 移除连接
func (m *MuxConnManager) RemoveConnection(connID string) {
    if conn, ok := m.connections.Load(connID); ok {
        conn.(net.Conn).Close()
        m.connections.Delete(connID)
    }
}

// GetConnection 获取连接
func (m *MuxConnManager) GetConnection(connID string) (net.Conn, bool) {
    conn, ok := m.connections.Load(connID)
    if !ok {
        return nil, false
    }
    return conn.(net.Conn), true
}

// HandleIncoming 处理接收到的消息
func (m *MuxConnManager) HandleIncoming(data []byte) error {
    msgType, id, payload, err := m.decoder.Decode(data)
    if err != nil {
        return err
    }

    switch msgType {
    case MsgTypeData:
        // 写入对应连接
        if conn, ok := m.GetConnection(id); ok {
            conn.Write(payload)
        }

    case MsgTypeClose:
        // 关闭连接
        m.RemoveConnection(id)

    case MsgTypeRequest:
        // HTTP 请求，需要特殊处理
        // 由上层处理
        return fmt.Errorf("HTTP request needs upper layer handling")

    case MsgTypeResponse:
        // HTTP 响应，写入对应连接
        if conn, ok := m.GetConnection(id); ok {
            conn.Write(payload)
        }
    }

    return nil
}

// Close 关闭管理器
func (m *MuxConnManager) Close() {
    m.cancel()

    // 关闭所有连接
    m.connections.Range(func(key, value interface{}) bool {
        value.(net.Conn).Close()
        return true
    })
}

// Stats 统计信息
func (m *MuxConnManager) Stats() MuxConnStats {
    var count int
    m.connections.Range(func(_, _ interface{}) bool {
        count++
        return true
    })

    return MuxConnStats{
        ActiveConnections: count,
    }
}

type MuxConnStats struct {
    ActiveConnections int
}
```

---

## 三、使用示例

### 3.1 客户端 TCP 隧道

```go
package main

import (
    "context"
    "fmt"
    "net"

    "github.com/Talbot3/go-tunnel/forward"
)

func main() {
    // 1. 建立 QUIC 连接（共享 Stream）
    quicConn, _ := quic.DialAddr(context.Background(), "server:443", tlsConfig, nil)
    stream, _ := quicConn.OpenStreamSync(context.Background())

    // 2. 创建多路连接管理器
    encoder := forward.NewDefaultMuxEncoder()
    decoder := forward.NewDefaultMuxDecoder()
    muxMgr := forward.NewMuxConnManager(stream, encoder, decoder)

    // 3. 启动消息接收循环
    go func() {
        buf := make([]byte, 64*1024)
        for {
            n, err := stream.Read(buf)
            if err != nil {
                break
            }
            muxMgr.HandleIncoming(buf[:n])
        }
    }()

    // 4. 当收到新连接通知时
    onNewConnection := func(connID string) {
        // 连接本地服务
        localConn, _ := net.Dial("tcp", "localhost:3000")

        // 添加到管理器，自动开始转发
        muxMgr.AddConnection(connID, localConn)
    }

    // 5. 当连接关闭时
    onCloseConnection := func(connID string) {
        muxMgr.RemoveConnection(connID)
    }
}
```

### 3.2 服务端 HTTP 隧道

```go
package main

import (
    "net/http"

    "github.com/Talbot3/go-tunnel/forward"
)

func main() {
    encoder := forward.NewDefaultMuxEncoder()
    handler := forward.NewHTTPMuxHandler()

    // 处理 HTTP 请求
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        // 从 Host 解析隧道
        tunnel := getTunnelByHost(r.Host)

        // 生成请求 ID
        reqID := generateReqID()

        // 序列化请求
        var reqBuf bytes.Buffer
        r.Write(&reqBuf)

        // 发送请求到客户端
        encoded, _ := encoder.EncodeRequest(reqID, reqBuf.Bytes())
        tunnel.stream.Write(encoded)

        // 等待响应（通过 channel 或回调）
        resp := waitForResponse(reqID)

        // 返回响应
        // ...
    })
}
```

---

## 四、架构对比

### 4.1 原架构

```
┌─────────────────────────────────────────────────────────────┐
│                    go-tunnel 原架构                          │
├─────────────────────────────────────────────────────────────┤
│  Forwarder.Forward(src, dst)                                │
│  - 直接双向转发                                              │
│  - 无协议封装                                                │
│  - 仅适用于独立连接                                          │
└─────────────────────────────────────────────────────────────┘

适用场景:
✅ 服务端 TCP Handler (独立连接)
❌ 客户端 TCP 隧道 (共享 Stream)
❌ HTTP 请求-响应 (非双向流)
```

### 4.2 扩展架构

```
┌─────────────────────────────────────────────────────────────┐
│                    go-tunnel 扩展架构                         │
├─────────────────────────────────────────────────────────────┤
│  Forwarder.Forward(src, dst)           // 原接口            │
│  MuxForwarder.ForwardMux(...)          // 多路转发          │
│  BidirectionalMuxForwarder.ForwardBidirectionalMux(...)     │
│  HTTPMuxForwarder.ForwardHTTPMux(...)  // HTTP 模式         │
│  MuxConnManager                        // 连接管理          │
└─────────────────────────────────────────────────────────────┘

适用场景:
✅ 服务端 TCP Handler (Forwarder)
✅ 客户端 TCP 隧道 (MuxForwarder + MuxConnManager)
✅ HTTP 请求-响应 (HTTPMuxForwarder)
```

---

## 五、接口总结

### 5.1 新增接口

| 接口 | 用途 | 场景 |
|------|------|------|
| `MuxEncoder` | 协议编码 | 多路复用 |
| `MuxDecoder` | 协议解码 | 多路复用 |
| `MuxForwarder` | 单向多路转发 | 客户端 TCP |
| `BidirectionalMuxForwarder` | 双向多路转发 | 客户端 TCP |
| `HTTPMuxForwarder` | HTTP 请求转发 | HTTP 隧道 |
| `HTTPMuxHandler` | HTTP 请求处理 | 服务端 HTTP |
| `MuxConnManager` | 连接管理 | 客户端 |

### 5.2 向后兼容

- 原 `Forwarder` 接口保持不变
- 新接口为增量扩展
- 不影响现有使用方式

---

## 六、实现优先级

| 优先级 | 组件 | 原因 |
|--------|------|------|
| P0 | MuxEncoder/Decoder | 基础组件 |
| P0 | MuxForwarder | 客户端 TCP 核心 |
| P1 | MuxConnManager | 简化使用 |
| P1 | BidirectionalMuxForwarder | 完整双向支持 |
| P2 | HTTPMuxForwarder | HTTP 隧道支持 |
| P2 | HTTPMuxHandler | 服务端 HTTP |
