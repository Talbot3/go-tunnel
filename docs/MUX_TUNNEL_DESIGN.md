# go-tunnel 多路复用隧道实现设计

## 一、架构概览

### 1.1 整体架构

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              公网服务器端 (Server)                               │
│                                                                                 │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────────────────────┐    │
│  │ 外部连接监听  │     │ 隧道管理器    │     │ QUIC/HTTP2 控制连接管理器     │    │
│  │ (TCP/HTTP)   │────▶│ TunnelManager │◀────│ MuxServer                   │    │
│  │              │     │              │     │                              │    │
│  │ :80/:443     │     │ tunnel_id -> │     │ Accept QUIC connections      │    │
│  │ :10000-20000 │     │   MuxConn    │     │ Handle multiplexed streams   │    │
│  └──────────────┘     └──────────────┘     └──────────────────────────────┘    │
│                              │                              │                   │
│                              │         ┌────────────────────┘                   │
│                              ▼         ▼                                        │
│                    ┌─────────────────────────────┐                              │
│                    │   MuxProtocol (协议层)       │                              │
│                    │   - MuxEncoder/Decoder       │                              │
│                    │   - 消息序列化/反序列化       │                              │
│                    └─────────────────────────────┘                              │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        │ QUIC/HTTP2 Stream (多路复用)
                                        │
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              内网客户端 (Client)                                 │
│                                                                                 │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────────────────────┐    │
│  │ 本地服务连接  │     │ 隧道客户端    │     │ QUIC/HTTP2 控制连接          │    │
│  │ LocalService │◀────│ TunnelClient │◀────│ MuxClient                   │    │
│  │              │     │              │     │                              │    │
│  │ localhost:   │     │ conn_id ->   │     │ Dial server                  │    │
│  │ 3000/:22     │     │   localConn  │     │ Handle multiplexed messages  │    │
│  └──────────────┘     └──────────────┘     └──────────────────────────────┘    │
│                              │                              │                   │
│                              ▼                              ▼                   │
│                    ┌─────────────────────────────┐                              │
│                    │   MuxProtocol (协议层)       │                              │
│                    │   - MuxEncoder/Decoder       │                              │
│                    │   - 消息序列化/反序列化       │                              │
│                    └─────────────────────────────┘                              │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 数据流

**TCP 隧道数据流：**

```
外部用户 ──▶ 外部连接 ──▶ TunnelManager ──▶ MuxServer ──▶ QUIC Stream
                                                              │
                                                              ▼
本地服务 ◀── LocalConn ◀── TunnelClient ◀── MuxClient ◀── QUIC Stream
```

**HTTP 隧道数据流：**

```
外部用户 ──▶ HTTP请求 ──▶ TunnelManager ──▶ MuxServer ──▶ REQUEST消息
                                                              │
                                                              ▼
本地服务 ◀── HTTP请求 ◀── TunnelClient ◀── MuxClient ◀── REQUEST消息
                                                              │
                                                              ▼
外部用户 ◀── HTTP响应 ◀── TunnelManager ◀── MuxServer ◀── RESPONSE消息
```

---

## 二、核心组件设计

### 2.1 协议层 (MuxProtocol)

协议层定义消息格式和编解码方式，支持自定义扩展。

```go
// tunnel/mux/protocol.go

package mux

import (
    "bytes"
    "encoding/binary"
    "fmt"
    "strconv"
)

// ============================================
// 消息类型定义
// ============================================

// MessageType 消息类型
type MessageType uint8

const (
    // 控制消息
    MsgTypeHandshake    MessageType = 0x01  // 握手
    MsgTypeHandshakeAck MessageType = 0x02  // 握手响应
    MsgTypeHeartbeat    MessageType = 0x03  // 心跳
    MsgTypeHeartbeatAck MessageType = 0x04  // 心跳响应
    MsgTypeRegister     MessageType = 0x05  // 隧道注册
    MsgTypeRegisterAck  MessageType = 0x06  // 注册响应
    MsgTypeUnregister   MessageType = 0x07  // 隧道注销

    // 数据消息
    MsgTypeData      MessageType = 0x10  // TCP 数据
    MsgTypeClose     MessageType = 0x11  // 连接关闭
    MsgTypeNewConn   MessageType = 0x12  // 新连接通知
    MsgTypeRequest   MessageType = 0x13  // HTTP 请求
    MsgTypeResponse  MessageType = 0x14  // HTTP 响应
    MsgTypeNewHTTP   MessageType = 0x15  // 新 HTTP 请求通知

    // 扩展消息 (0x20-0x7F 用户自定义)
    MsgTypeUserDefinedStart MessageType = 0x20
    MsgTypeUserDefinedEnd   MessageType = 0x7F
)

func (mt MessageType) String() string {
    switch mt {
    case MsgTypeHandshake:
        return "HANDSHAKE"
    case MsgTypeHandshakeAck:
        return "HANDSHAKE_ACK"
    case MsgTypeHeartbeat:
        return "HEARTBEAT"
    case MsgTypeHeartbeatAck:
        return "HEARTBEAT_ACK"
    case MsgTypeRegister:
        return "REGISTER"
    case MsgTypeRegisterAck:
        return "REGISTER_ACK"
    case MsgTypeUnregister:
        return "UNREGISTER"
    case MsgTypeData:
        return "DATA"
    case MsgTypeClose:
        return "CLOSE"
    case MsgTypeNewConn:
        return "NEWCONN"
    case MsgTypeRequest:
        return "REQUEST"
    case MsgTypeResponse:
        return "RESPONSE"
    case MsgTypeNewHTTP:
        return "NEWHTTP"
    default:
        if mt >= MsgTypeUserDefinedStart && mt <= MsgTypeUserDefinedEnd {
            return fmt.Sprintf("USER_DEFINED_%d", mt)
        }
        return fmt.Sprintf("UNKNOWN_%d", mt)
    }
}

// ============================================
// 消息结构定义
// ============================================

// Message 通用消息结构
type Message struct {
    Type     MessageType // 1 byte
    ID       string      // 变长: 连接ID 或 请求ID
    Payload  []byte      // 变长: 数据负载
}

// HandshakeMessage 握手消息
type HandshakeMessage struct {
    Version   uint8   // 协议版本
    TunnelID  string  // 隧道ID
    AuthToken string  // 认证令牌
    Protocol  string  // 协议类型: "tcp" / "http"
    LocalAddr string  // 本地服务地址
}

// RegisterMessage 隧道注册消息
type RegisterMessage struct {
    TunnelID   string // 隧道ID
    Protocol   string // "tcp" / "http"
    Subdomain  string // 子域名 (HTTP 隧道)
    LocalPort  int    // 本地端口 (TCP 隧道)
    AuthToken  string // 认证令牌
}

// RegisterAckMessage 注册响应消息
type RegisterAckMessage struct {
    TunnelID  string // 隧道ID
    PublicURL string // 公网访问地址
    Port      int    // 分配的端口 (TCP 隧道)
    Error     string // 错误信息
}

// NewConnMessage 新连接通知消息
type NewConnMessage struct {
    ConnID     string // 连接ID
    RemoteAddr string // 远程地址
}

// DataMessage 数据消息
type DataMessage struct {
    ConnID string // 连接ID
    Data   []byte // 数据
}

// CloseMessage 关闭消息
type CloseMessage struct {
    ConnID string // 连接ID
}

// RequestMessage HTTP 请求消息
type RequestMessage struct {
    ReqID   string // 请求ID
    Method  string // HTTP 方法
    URL     string // 请求 URL
    Headers map[string]string
    Body    []byte
}

// ResponseMessage HTTP 响应消息
type ResponseMessage struct {
    ReqID      string // 请求ID
    StatusCode int    // 状态码
    Headers    map[string]string
    Body       []byte
}

// ============================================
// 协议接口定义
// ============================================

// Protocol 协议接口
type Protocol interface {
    // Encode 编码消息
    Encode(msg *Message) ([]byte, error)

    // Decode 解码消息
    Decode(data []byte) (*Message, error)

    // EncodeHandshake 编码握手消息
    EncodeHandshake(msg *HandshakeMessage) ([]byte, error)

    // DecodeHandshake 解码握手消息
    DecodeHandshake(data []byte) (*HandshakeMessage, error)

    // EncodeRegister 编码注册消息
    EncodeRegister(msg *RegisterMessage) ([]byte, error)

    // DecodeRegister 解码注册消息
    DecodeRegister(data []byte) (*RegisterMessage, error)

    // EncodeData 编码数据消息
    EncodeData(connID string, data []byte) ([]byte, error)

    // DecodeData 解码数据消息
    DecodeData(data []byte) (*DataMessage, error)

    // EncodeClose 编码关闭消息
    EncodeClose(connID string) ([]byte, error)

    // EncodeNewConn 编码新连接通知
    EncodeNewConn(connID, remoteAddr string) ([]byte, error)

    // DecodeNewConn 解码新连接通知
    DecodeNewConn(data []byte) (*NewConnMessage, error)
}

// ============================================
// 二进制协议实现 (默认)
// ============================================

// BinaryProtocol 二进制协议实现
// 消息格式:
//   [1 byte: type][2 bytes: id_len][id][4 bytes: payload_len][payload]
type BinaryProtocol struct {
    // MaxMessageSize 最大消息大小
    MaxMessageSize int
}

func NewBinaryProtocol() *BinaryProtocol {
    return &BinaryProtocol{
        MaxMessageSize: 1024 * 1024, // 1MB
    }
}

// Encode 编码通用消息
func (p *BinaryProtocol) Encode(msg *Message) ([]byte, error) {
    idBytes := []byte(msg.ID)

    // 计算总长度
    totalLen := 1 + 2 + len(idBytes) + 4 + len(msg.Payload)

    buf := make([]byte, totalLen)
    offset := 0

    // 写入类型
    buf[offset] = byte(msg.Type)
    offset++

    // 写入 ID 长度和 ID
    binary.BigEndian.PutUint16(buf[offset:], uint16(len(idBytes)))
    offset += 2
    copy(buf[offset:], idBytes)
    offset += len(idBytes)

    // 写入 payload 长度和 payload
    binary.BigEndian.PutUint32(buf[offset:], uint32(len(msg.Payload)))
    offset += 4
    copy(buf[offset:], msg.Payload)

    return buf, nil
}

// Decode 解码通用消息
func (p *BinaryProtocol) Decode(data []byte) (*Message, error) {
    if len(data) < 7 { // 最小长度: 1 + 2 + 0 + 4 + 0
        return nil, fmt.Errorf("message too short: %d", len(data))
    }

    offset := 0

    // 读取类型
    msgType := MessageType(data[offset])
    offset++

    // 读取 ID 长度和 ID
    idLen := binary.BigEndian.Uint16(data[offset:])
    offset += 2

    if len(data) < offset+int(idLen)+4 {
        return nil, fmt.Errorf("invalid id length: %d", idLen)
    }

    id := string(data[offset : offset+int(idLen)])
    offset += int(idLen)

    // 读取 payload 长度和 payload
    payloadLen := binary.BigEndian.Uint32(data[offset:])
    offset += 4

    if len(data) < offset+int(payloadLen) {
        return nil, fmt.Errorf("invalid payload length: %d", payloadLen)
    }

    payload := data[offset : offset+int(payloadLen)]

    return &Message{
        Type:    msgType,
        ID:      id,
        Payload: payload,
    }, nil
}

// EncodeHandshake 编码握手消息
func (p *BinaryProtocol) EncodeHandshake(msg *HandshakeMessage) ([]byte, error) {
    // 格式: [version][tunnel_id_len][tunnel_id][auth_token_len][auth_token][protocol_len][protocol][local_addr_len][local_addr]
    buf := new(bytes.Buffer)

    buf.WriteByte(msg.Version)

    // tunnel_id
    tunnelIDBytes := []byte(msg.TunnelID)
    binary.Write(buf, binary.BigEndian, uint16(len(tunnelIDBytes)))
    buf.Write(tunnelIDBytes)

    // auth_token
    authTokenBytes := []byte(msg.AuthToken)
    binary.Write(buf, binary.BigEndian, uint16(len(authTokenBytes)))
    buf.Write(authTokenBytes)

    // protocol
    protocolBytes := []byte(msg.Protocol)
    binary.Write(buf, binary.BigEndian, uint16(len(protocolBytes)))
    buf.Write(protocolBytes)

    // local_addr
    localAddrBytes := []byte(msg.LocalAddr)
    binary.Write(buf, binary.BigEndian, uint16(len(localAddrBytes)))
    buf.Write(localAddrBytes)

    return p.Encode(&Message{
        Type:    MsgTypeHandshake,
        Payload: buf.Bytes(),
    })
}

// DecodeHandshake 解码握手消息
func (p *BinaryProtocol) DecodeHandshake(data []byte) (*HandshakeMessage, error) {
    msg, err := p.Decode(data)
    if err != nil {
        return nil, err
    }

    if msg.Type != MsgTypeHandshake {
        return nil, fmt.Errorf("expected HANDSHAKE, got %s", msg.Type)
    }

    payload := msg.Payload
    offset := 0

    result := &HandshakeMessage{}
    result.Version = payload[offset]
    offset++

    // tunnel_id
    tunnelIDLen := binary.BigEndian.Uint16(payload[offset:])
    offset += 2
    result.TunnelID = string(payload[offset : offset+int(tunnelIDLen)])
    offset += int(tunnelIDLen)

    // auth_token
    authTokenLen := binary.BigEndian.Uint16(payload[offset:])
    offset += 2
    result.AuthToken = string(payload[offset : offset+int(authTokenLen)])
    offset += int(authTokenLen)

    // protocol
    protocolLen := binary.BigEndian.Uint16(payload[offset:])
    offset += 2
    result.Protocol = string(payload[offset : offset+int(protocolLen)])
    offset += int(protocolLen)

    // local_addr
    localAddrLen := binary.BigEndian.Uint16(payload[offset:])
    offset += 2
    result.LocalAddr = string(payload[offset : offset+int(localAddrLen)])

    return result, nil
}

// EncodeRegister 编码注册消息
func (p *BinaryProtocol) EncodeRegister(msg *RegisterMessage) ([]byte, error) {
    buf := new(bytes.Buffer)

    // tunnel_id
    tunnelIDBytes := []byte(msg.TunnelID)
    binary.Write(buf, binary.BigEndian, uint16(len(tunnelIDBytes)))
    buf.Write(tunnelIDBytes)

    // protocol
    protocolBytes := []byte(msg.Protocol)
    binary.Write(buf, binary.BigEndian, uint16(len(protocolBytes)))
    buf.Write(protocolBytes)

    // subdomain
    subdomainBytes := []byte(msg.Subdomain)
    binary.Write(buf, binary.BigEndian, uint16(len(subdomainBytes)))
    buf.Write(subdomainBytes)

    // local_port
    binary.Write(buf, binary.BigEndian, uint16(msg.LocalPort))

    // auth_token
    authTokenBytes := []byte(msg.AuthToken)
    binary.Write(buf, binary.BigEndian, uint16(len(authTokenBytes)))
    buf.Write(authTokenBytes)

    return p.Encode(&Message{
        Type:    MsgTypeRegister,
        Payload: buf.Bytes(),
    })
}

// DecodeRegister 解码注册消息
func (p *BinaryProtocol) DecodeRegister(data []byte) (*RegisterMessage, error) {
    msg, err := p.Decode(data)
    if err != nil {
        return nil, err
    }

    if msg.Type != MsgTypeRegister {
        return nil, fmt.Errorf("expected REGISTER, got %s", msg.Type)
    }

    payload := msg.Payload
    offset := 0

    result := &RegisterMessage{}

    // tunnel_id
    tunnelIDLen := binary.BigEndian.Uint16(payload[offset:])
    offset += 2
    result.TunnelID = string(payload[offset : offset+int(tunnelIDLen)])
    offset += int(tunnelIDLen)

    // protocol
    protocolLen := binary.BigEndian.Uint16(payload[offset:])
    offset += 2
    result.Protocol = string(payload[offset : offset+int(protocolLen)])
    offset += int(protocolLen)

    // subdomain
    subdomainLen := binary.BigEndian.Uint16(payload[offset:])
    offset += 2
    result.Subdomain = string(payload[offset : offset+int(subdomainLen)])
    offset += int(subdomainLen)

    // local_port
    result.LocalPort = int(binary.BigEndian.Uint16(payload[offset:]))
    offset += 2

    // auth_token
    authTokenLen := binary.BigEndian.Uint16(payload[offset:])
    offset += 2
    result.AuthToken = string(payload[offset : offset+int(authTokenLen)])

    return result, nil
}

// EncodeData 编码数据消息
func (p *BinaryProtocol) EncodeData(connID string, data []byte) ([]byte, error) {
    return p.Encode(&Message{
        Type:    MsgTypeData,
        ID:      connID,
        Payload: data,
    })
}

// DecodeData 解码数据消息
func (p *BinaryProtocol) DecodeData(data []byte) (*DataMessage, error) {
    msg, err := p.Decode(data)
    if err != nil {
        return nil, err
    }

    if msg.Type != MsgTypeData {
        return nil, fmt.Errorf("expected DATA, got %s", msg.Type)
    }

    return &DataMessage{
        ConnID: msg.ID,
        Data:   msg.Payload,
    }, nil
}

// EncodeClose 编码关闭消息
func (p *BinaryProtocol) EncodeClose(connID string) ([]byte, error) {
    return p.Encode(&Message{
        Type: MsgTypeClose,
        ID:   connID,
    })
}

// EncodeNewConn 编码新连接通知
func (p *BinaryProtocol) EncodeNewConn(connID, remoteAddr string) ([]byte, error) {
    remoteAddrBytes := []byte(remoteAddr)

    buf := new(bytes.Buffer)
    binary.Write(buf, binary.BigEndian, uint16(len(remoteAddrBytes)))
    buf.Write(remoteAddrBytes)

    return p.Encode(&Message{
        Type:    MsgTypeNewConn,
        ID:      connID,
        Payload: buf.Bytes(),
    })
}

// DecodeNewConn 解码新连接通知
func (p *BinaryProtocol) DecodeNewConn(data []byte) (*NewConnMessage, error) {
    msg, err := p.Decode(data)
    if err != nil {
        return nil, err
    }

    if msg.Type != MsgTypeNewConn {
        return nil, fmt.Errorf("expected NEWCONN, got %s", msg.Type)
    }

    payload := msg.Payload
    remoteAddrLen := binary.BigEndian.Uint16(payload)
    remoteAddr := string(payload[2 : 2+remoteAddrLen])

    return &NewConnMessage{
        ConnID:     msg.ID,
        RemoteAddr: remoteAddr,
    }, nil
}

// ============================================
// 文本协议实现 (兼容旧版)
// ============================================

// TextProtocol 文本协议实现
// 消息格式: TYPE:ID:LENGTH:PAYLOAD\n
type TextProtocol struct {
    Delimiter byte
}

func NewTextProtocol() *TextProtocol {
    return &TextProtocol{Delimiter: '\n'}
}

// Encode 编码消息
func (p *TextProtocol) Encode(msg *Message) ([]byte, error) {
    header := fmt.Sprintf("%s:%s:%d:", msg.Type, msg.ID, len(msg.Payload))
    result := make([]byte, len(header)+len(msg.Payload)+1)
    copy(result, header)
    copy(result[len(header):], msg.Payload)
    result[len(result)-1] = p.Delimiter
    return result, nil
}

// Decode 解码消息
func (p *TextProtocol) Decode(data []byte) (*Message, error) {
    if len(data) > 0 && data[len(data)-1] == p.Delimiter {
        data = data[:len(data)-1]
    }

    parts := bytes.SplitN(data, []byte(":"), 4)
    if len(parts) < 3 {
        return nil, fmt.Errorf("invalid message format")
    }

    msgTypeStr := string(parts[0])
    id := string(parts[1])

    var msgType MessageType
    switch msgTypeStr {
    case "HANDSHAKE":
        msgType = MsgTypeHandshake
    case "DATA":
        msgType = MsgTypeData
    case "CLOSE":
        msgType = MsgTypeClose
    case "NEWCONN":
        msgType = MsgTypeNewConn
    case "REQUEST":
        msgType = MsgTypeRequest
    case "RESPONSE":
        msgType = MsgTypeResponse
    default:
        msgType = MsgTypeData
    }

    var payload []byte
    if len(parts) >= 4 {
        payload = parts[3]
    }

    return &Message{
        Type:    msgType,
        ID:      id,
        Payload: payload,
    }, nil
}

// 其他方法实现类似 BinaryProtocol...

func (p *TextProtocol) EncodeHandshake(msg *HandshakeMessage) ([]byte, error) {
    // 简化实现
    payload := fmt.Sprintf("%d:%s:%s:%s:%s", msg.Version, msg.TunnelID, msg.AuthToken, msg.Protocol, msg.LocalAddr)
    return p.Encode(&Message{Type: MsgTypeHandshake, Payload: []byte(payload)})
}

func (p *TextProtocol) DecodeHandshake(data []byte) (*HandshakeMessage, error) {
    msg, err := p.Decode(data)
    if err != nil {
        return nil, err
    }
    parts := bytes.Split(msg.Payload, []byte(":"))
    if len(parts) < 5 {
        return nil, fmt.Errorf("invalid handshake format")
    }
    return &HandshakeMessage{
        Version:   parts[0][0] - '0',
        TunnelID:  string(parts[1]),
        AuthToken: string(parts[2]),
        Protocol:  string(parts[3]),
        LocalAddr: string(parts[4]),
    }, nil
}

func (p *TextProtocol) EncodeRegister(msg *RegisterMessage) ([]byte, error) {
    payload := fmt.Sprintf("%s:%s:%s:%d:%s", msg.TunnelID, msg.Protocol, msg.Subdomain, msg.LocalPort, msg.AuthToken)
    return p.Encode(&Message{Type: MsgTypeRegister, Payload: []byte(payload)})
}

func (p *TextProtocol) DecodeRegister(data []byte) (*RegisterMessage, error) {
    msg, err := p.Decode(data)
    if err != nil {
        return nil, err
    }
    parts := bytes.Split(msg.Payload, []byte(":"))
    if len(parts) < 5 {
        return nil, fmt.Errorf("invalid register format")
    }
    localPort, _ := strconv.Atoi(string(parts[3]))
    return &RegisterMessage{
        TunnelID:  string(parts[0]),
        Protocol:  string(parts[1]),
        Subdomain: string(parts[2]),
        LocalPort: localPort,
        AuthToken: string(parts[4]),
    }, nil
}

func (p *TextProtocol) EncodeData(connID string, data []byte) ([]byte, error) {
    return p.Encode(&Message{Type: MsgTypeData, ID: connID, Payload: data})
}

func (p *TextProtocol) DecodeData(data []byte) (*DataMessage, error) {
    msg, err := p.Decode(data)
    if err != nil {
        return nil, err
    }
    return &DataMessage{ConnID: msg.ID, Data: msg.Payload}, nil
}

func (p *TextProtocol) EncodeClose(connID string) ([]byte, error) {
    return p.Encode(&Message{Type: MsgTypeClose, ID: connID})
}

func (p *TextProtocol) EncodeNewConn(connID, remoteAddr string) ([]byte, error) {
    return p.Encode(&Message{Type: MsgTypeNewConn, ID: connID, Payload: []byte(remoteAddr)})
}

func (p *TextProtocol) DecodeNewConn(data []byte) (*NewConnMessage, error) {
    msg, err := p.Decode(data)
    if err != nil {
        return nil, err
    }
    return &NewConnMessage{ConnID: msg.ID, RemoteAddr: string(msg.Payload)}, nil
}
```

---

## 三、服务端实现 (MuxServer)

### 3.1 MuxServer 核心结构

```go
// tunnel/mux/server.go

package mux

import (
    "context"
    "crypto/tls"
    "fmt"
    "io"
    "log"
    "net"
    "sync"
    "sync/atomic"
    "time"
)

// ServerConfig 服务端配置
type ServerConfig struct {
    // 监听地址
    ListenAddr string // QUIC/HTTP2 监听地址，如 ":443"

    // TLS 配置
    TLSConfig *tls.Config

    // 认证
    AuthToken string // 认证令牌（可选）

    // 连接管理
    MaxTunnels       int           // 最大隧道数
    MaxConnsPerTunnel int          // 每个隧道最大连接数
    TunnelTimeout    time.Duration // 隧道超时时间
    ConnTimeout      time.Duration // 连接超时时间

    // 协议
    Protocol Protocol // 协议编解码器

    // 端口范围 (TCP 隧道)
    PortRangeStart int
    PortRangeEnd   int
}

// DefaultServerConfig 默认服务端配置
func DefaultServerConfig() ServerConfig {
    return ServerConfig{
        MaxTunnels:        10000,
        MaxConnsPerTunnel: 1000,
        TunnelTimeout:     5 * time.Minute,
        ConnTimeout:       10 * time.Minute,
        Protocol:          NewBinaryProtocol(),
        PortRangeStart:    10000,
        PortRangeEnd:      20000,
    }
}

// MuxServer 多路复用服务端
type MuxServer struct {
    config ServerConfig

    // 隧道管理
    tunnels sync.Map // tunnelID -> *Tunnel

    // 端口管理
    portManager *PortManager

    // 协议
    protocol Protocol

    // 监听器
    listener net.Listener

    // 统计
    activeTunnels  atomic.Int64
    totalTunnels   atomic.Int64
    activeConns    atomic.Int64
    totalConns     atomic.Int64
    bytesIn        atomic.Int64
    bytesOut       atomic.Int64

    // 生命周期
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

// Tunnel 隧道信息
type Tunnel struct {
    ID         string
    Protocol   string      // "tcp" / "http"
    PublicURL  string
    Port       int         // TCP 隧道分配的端口
    LocalAddr  string      // 客户端本地地址

    // 连接
    ControlConn net.Conn   // 控制连接 (QUIC Stream)
    Conns       sync.Map   // connID -> *ConnInfo

    // 统计
    CreatedAt   time.Time
    LastActive  atomic.Int64

    // 外部监听器 (TCP 隧道)
    ExternalListener net.Listener
}

// ConnInfo 连接信息
type ConnInfo struct {
    ID         string
    RemoteAddr string
    LocalConn  net.Conn    // 外部连接
    CreatedAt  time.Time
}

// NewMuxServer 创建多路复用服务端
func NewMuxServer(config ServerConfig) *MuxServer {
    ctx, cancel := context.WithCancel(context.Background())

    return &MuxServer{
        config:      config,
        protocol:    config.Protocol,
        portManager: NewPortManager(config.PortRangeStart, config.PortRangeEnd),
        ctx:         ctx,
        cancel:      cancel,
    }
}

// Start 启动服务端
func (s *MuxServer) Start(ctx context.Context) error {
    // 启动监听
    listener, err := net.Listen("tcp", s.config.ListenAddr)
    if err != nil {
        return fmt.Errorf("listen failed: %w", err)
    }
    s.listener = listener

    log.Printf("MuxServer started on %s", s.config.ListenAddr)

    // 接受连接
    s.wg.Add(1)
    go s.acceptLoop()

    // 启动健康检查
    s.wg.Add(1)
    go s.healthCheckLoop()

    return nil
}

// Stop 停止服务端
func (s *MuxServer) Stop() error {
    s.cancel()

    if s.listener != nil {
        s.listener.Close()
    }

    // 关闭所有隧道
    s.tunnels.Range(func(key, value interface{}) bool {
        tunnel := value.(*Tunnel)
        s.closeTunnel(tunnel.ID)
        return true
    })

    s.wg.Wait()
    return nil
}

// acceptLoop 接受连接循环
func (s *MuxServer) acceptLoop() {
    defer s.wg.Done()

    for {
        select {
        case <-s.ctx.Done():
            return
        default:
        }

        conn, err := s.listener.Accept()
        if err != nil {
            if s.ctx.Err() != nil {
                return
            }
            log.Printf("Accept error: %v", err)
            continue
        }

        s.wg.Add(1)
        go s.handleConnection(conn)
    }
}

// handleConnection 处理连接
func (s *MuxServer) handleConnection(conn net.Conn) {
    defer s.wg.Done()
    defer conn.Close()

    // 设置读写超时
    conn.SetDeadline(time.Now().Add(30 * time.Second))

    // 读取握手消息
    buf := make([]byte, 64*1024)
    n, err := conn.Read(buf)
    if err != nil {
        log.Printf("Read handshake failed: %v", err)
        return
    }

    // 解码握手消息
    handshake, err := s.protocol.DecodeHandshake(buf[:n])
    if err != nil {
        log.Printf("Decode handshake failed: %v", err)
        return
    }

    // 验证认证令牌
    if s.config.AuthToken != "" && handshake.AuthToken != s.config.AuthToken {
        log.Printf("Auth failed for tunnel %s", handshake.TunnelID)
        return
    }

    // 检查隧道数量限制
    if int(s.activeTunnels.Load()) >= s.config.MaxTunnels {
        log.Printf("Max tunnels reached")
        return
    }

    // 清除超时
    conn.SetDeadline(time.Time{})

    // 创建隧道
    tunnel := s.createTunnel(handshake, conn)
    if tunnel == nil {
        return
    }

    // 发送握手响应
    ackMsg := &Message{
        Type: MsgTypeHandshakeAck,
        ID:   tunnel.ID,
        Payload: []byte(fmt.Sprintf("%s:%s:%d", tunnel.PublicURL, tunnel.Protocol, tunnel.Port)),
    }
    ackData, _ := s.protocol.Encode(ackMsg)
    conn.Write(ackData)

    log.Printf("Tunnel registered: %s -> %s (%s)", tunnel.ID, tunnel.PublicURL, tunnel.Protocol)

    // TCP 隧道：启动外部监听
    if tunnel.Protocol == "tcp" && tunnel.Port > 0 {
        go s.startExternalListener(tunnel)
    }

    // 消息处理循环
    s.messageLoop(tunnel)
}

// createTunnel 创建隧道
func (s *MuxServer) createTunnel(handshake *HandshakeMessage, conn net.Conn) *Tunnel {
    tunnelID := handshake.TunnelID
    if tunnelID == "" {
        tunnelID = generateID()
    }

    // 分配公网地址
    var publicURL string
    var port int
    var err error

    if handshake.Protocol == "tcp" {
        // TCP 隧道：分配端口
        port, err = s.portManager.Allocate()
        if err != nil {
            log.Printf("Allocate port failed: %v", err)
            return nil
        }
        publicURL = fmt.Sprintf("tcp://:%d", port)
    } else {
        // HTTP 隧道：使用子域名
        publicURL = fmt.Sprintf("https://%s.example.com", handshake.TunnelID)
    }

    tunnel := &Tunnel{
        ID:          tunnelID,
        Protocol:    handshake.Protocol,
        PublicURL:   publicURL,
        Port:        port,
        LocalAddr:   handshake.LocalAddr,
        ControlConn: conn,
        CreatedAt:   time.Now(),
    }

    s.tunnels.Store(tunnelID, tunnel)
    s.activeTunnels.Add(1)
    s.totalTunnels.Add(1)

    return tunnel
}

// startExternalListener 启动外部监听 (TCP 隧道)
func (s *MuxServer) startExternalListener(tunnel *Tunnel) {
    listener, err := net.Listen("tcp", fmt.Sprintf(":%d", tunnel.Port))
    if err != nil {
        log.Printf("Listen on port %d failed: %v", tunnel.Port, err)
        return
    }
    tunnel.ExternalListener = listener

    log.Printf("External listener started for tunnel %s on :%d", tunnel.ID, tunnel.Port)

    for {
        select {
        case <-s.ctx.Done():
            return
        default:
        }

        externalConn, err := listener.Accept()
        if err != nil {
            if s.ctx.Err() != nil {
                return
            }
            continue
        }

        // 生成连接 ID
        connID := generateID()

        // 保存连接
        connInfo := &ConnInfo{
            ID:         connID,
            RemoteAddr: externalConn.RemoteAddr().String(),
            LocalConn:  externalConn,
            CreatedAt:  time.Now(),
        }
        tunnel.Conns.Store(connID, connInfo)
        s.activeConns.Add(1)
        s.totalConns.Add(1)

        // 发送新连接通知给客户端
        newConnMsg, _ := s.protocol.EncodeNewConn(connID, externalConn.RemoteAddr().String())
        tunnel.ControlConn.Write(newConnMsg)

        // 启动数据转发
        go s.forwardData(tunnel, connID, externalConn)
    }
}

// forwardData 转发数据
func (s *MuxServer) forwardData(tunnel *Tunnel, connID string, externalConn net.Conn) {
    defer func() {
        externalConn.Close()
        tunnel.Conns.Delete(connID)
        s.activeConns.Add(-1)

        // 发送关闭消息给客户端
        closeMsg, _ := s.protocol.EncodeClose(connID)
        tunnel.ControlConn.Write(closeMsg)
    }()

    buf := make([]byte, 32*1024)

    for {
        select {
        case <-s.ctx.Done():
            return
        default:
        }

        n, err := externalConn.Read(buf)
        if err != nil {
            return
        }

        // 编码并发送数据
        dataMsg, _ := s.protocol.EncodeData(connID, buf[:n])
        tunnel.ControlConn.Write(dataMsg)

        s.bytesOut.Add(int64(n))
    }
}

// messageLoop 消息处理循环
func (s *MuxServer) messageLoop(tunnel *Tunnel) {
    buf := make([]byte, 64*1024)

    for {
        select {
        case <-s.ctx.Done():
            return
        default:
        }

        n, err := tunnel.ControlConn.Read(buf)
        if err != nil {
            if s.ctx.Err() == nil {
                log.Printf("Read error for tunnel %s: %v", tunnel.ID, err)
            }
            s.closeTunnel(tunnel.ID)
            return
        }

        // 解码消息
        msg, err := s.protocol.Decode(buf[:n])
        if err != nil {
            log.Printf("Decode error: %v", err)
            continue
        }

        // 更新活跃时间
        tunnel.LastActive.Store(time.Now().Unix())

        // 处理消息
        switch msg.Type {
        case MsgTypeData:
            s.handleDataMessage(tunnel, msg)

        case MsgTypeClose:
            s.handleCloseMessage(tunnel, msg)

        case MsgTypeHeartbeat:
            // 心跳响应
            ackMsg, _ := s.protocol.Encode(&Message{Type: MsgTypeHeartbeatAck})
            tunnel.ControlConn.Write(ackMsg)
        }
    }
}

// handleDataMessage 处理数据消息
func (s *MuxServer) handleDataMessage(tunnel *Tunnel, msg *Message) {
    connInfo, ok := tunnel.Conns.Load(msg.ID)
    if !ok {
        return
    }

    conn := connInfo.(*ConnInfo)
    n, err := conn.LocalConn.Write(msg.Payload)
    if err != nil {
        conn.LocalConn.Close()
        tunnel.Conns.Delete(msg.ID)
        s.activeConns.Add(-1)
        return
    }

    s.bytesIn.Add(int64(n))
}

// handleCloseMessage 处理关闭消息
func (s *MuxServer) handleCloseMessage(tunnel *Tunnel, msg *Message) {
    connInfo, ok := tunnel.Conns.Load(msg.ID)
    if !ok {
        return
    }

    conn := connInfo.(*ConnInfo)
    conn.LocalConn.Close()
    tunnel.Conns.Delete(msg.ID)
    s.activeConns.Add(-1)
}

// closeTunnel 关闭隧道
func (s *MuxServer) closeTunnel(tunnelID string) {
    value, ok := s.tunnels.LoadAndDelete(tunnelID)
    if !ok {
        return
    }

    tunnel := value.(*Tunnel)

    // 关闭控制连接
    if tunnel.ControlConn != nil {
        tunnel.ControlConn.Close()
    }

    // 关闭外部监听器
    if tunnel.ExternalListener != nil {
        tunnel.ExternalListener.Close()
    }

    // 关闭所有连接
    tunnel.Conns.Range(func(key, value interface{}) bool {
        conn := value.(*ConnInfo)
        conn.LocalConn.Close()
        s.activeConns.Add(-1)
        return true
    })

    // 释放端口
    if tunnel.Port > 0 {
        s.portManager.Release(tunnel.Port)
    }

    s.activeTunnels.Add(-1)
    log.Printf("Tunnel closed: %s", tunnelID)
}

// healthCheckLoop 健康检查循环
func (s *MuxServer) healthCheckLoop() {
    defer s.wg.Done()

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-s.ctx.Done():
            return
        case <-ticker.C:
            // 检查超时隧道
            now := time.Now().Unix()
            s.tunnels.Range(func(key, value interface{}) bool {
                tunnel := value.(*Tunnel)
                lastActive := tunnel.LastActive.Load()
                if now-lastActive > int64(s.config.TunnelTimeout.Seconds()) {
                    log.Printf("Tunnel %s timeout", tunnel.ID)
                    s.closeTunnel(tunnel.ID)
                }
                return true
            })
        }
    }
}

// GetStats 获取统计信息
func (s *MuxServer) GetStats() ServerStats {
    return ServerStats{
        ActiveTunnels: s.activeTunnels.Load(),
        TotalTunnels:  s.totalTunnels.Load(),
        ActiveConns:   s.activeConns.Load(),
        TotalConns:    s.totalConns.Load(),
        BytesIn:       s.bytesIn.Load(),
        BytesOut:      s.bytesOut.Load(),
    }
}

// ServerStats 服务端统计信息
type ServerStats struct {
    ActiveTunnels int64
    TotalTunnels  int64
    ActiveConns   int64
    TotalConns    int64
    BytesIn       int64
    BytesOut      int64
}

// GetTunnel 获取隧道信息
func (s *MuxServer) GetTunnel(tunnelID string) (*Tunnel, bool) {
    value, ok := s.tunnels.Load(tunnelID)
    if !ok {
        return nil, false
    }
    return value.(*Tunnel), true
}

// ListTunnels 列出所有隧道
func (s *MuxServer) ListTunnels() []*Tunnel {
    var tunnels []*Tunnel
    s.tunnels.Range(func(key, value interface{}) bool {
        tunnels = append(tunnels, value.(*Tunnel))
        return true
    })
    return tunnels
}
```

---

## 四、客户端实现 (MuxClient)

### 4.1 MuxClient 核心结构

```go
// tunnel/mux/client.go

package mux

import (
    "context"
    "crypto/tls"
    "fmt"
    "io"
    "log"
    "net"
    "sync"
    "sync/atomic"
    "time"
)

// ClientConfig 客户端配置
type ClientConfig struct {
    // 服务器地址
    ServerAddr string // 如 "tunnel.example.com:443"

    // TLS 配置
    TLSConfig *tls.Config

    // 隧道配置
    TunnelID   string // 隧道ID（可选，自动生成）
    Protocol   string // "tcp" / "http"
    LocalAddr  string // 本地服务地址，如 "localhost:3000"
    Subdomain  string // 子域名（HTTP 隧道可选）
    AuthToken  string // 认证令牌

    // 协议
    ProtocolCodec Protocol // 协议编解码器

    // 重连
    ReconnectInterval time.Duration
    MaxReconnectTries int

    // 超时
    DialTimeout    time.Duration
    ConnTimeout    time.Duration
}

// DefaultClientConfig 默认客户端配置
func DefaultClientConfig() ClientConfig {
    return ClientConfig{
        Protocol:          "tcp",
        ProtocolCodec:     NewBinaryProtocol(),
        ReconnectInterval: 5 * time.Second,
        MaxReconnectTries: 0, // 无限重试
        DialTimeout:       10 * time.Second,
        ConnTimeout:       10 * time.Minute,
    }
}

// MuxClient 多路复用客户端
type MuxClient struct {
    config ClientConfig

    // 协议
    protocol Protocol

    // 控制连接
    controlConn net.Conn

    // 本地连接管理
    localConns sync.Map // connID -> *LocalConnInfo

    // 统计
    activeConns atomic.Int64
    totalConns  atomic.Int64
    bytesIn     atomic.Int64
    bytesOut    atomic.Int64

    // 隧道信息
    tunnelID  string
    publicURL string

    // 生命周期
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup

    // 重连
    reconnectCh chan struct{}
    connected   atomic.Bool
}

// LocalConnInfo 本地连接信息
type LocalConnInfo struct {
    ID        string
    LocalConn net.Conn
    CreatedAt time.Time
}

// NewMuxClient 创建多路复用客户端
func NewMuxClient(config ClientConfig) *MuxClient {
    ctx, cancel := context.WithCancel(context.Background())

    if config.TunnelID == "" {
        config.TunnelID = generateID()
    }

    return &MuxClient{
        config:      config,
        protocol:    config.ProtocolCodec,
        ctx:         ctx,
        cancel:      cancel,
        reconnectCh: make(chan struct{}, 1),
    }
}

// Start 启动客户端
func (c *MuxClient) Start(ctx context.Context) error {
    c.ctx, c.cancel = context.WithCancel(ctx)

    // 启动连接循环
    c.wg.Add(1)
    go c.connectLoop()

    return nil
}

// Stop 停止客户端
func (c *MuxClient) Stop() error {
    c.cancel()

    if c.controlConn != nil {
        c.controlConn.Close()
    }

    // 关闭所有本地连接
    c.localConns.Range(func(key, value interface{}) bool {
        conn := value.(*LocalConnInfo)
        conn.LocalConn.Close()
        return true
    })

    c.wg.Wait()
    return nil
}

// connectLoop 连接循环（支持重连）
func (c *MuxClient) connectLoop() {
    defer c.wg.Done()

    reconnectTries := 0

    for {
        select {
        case <-c.ctx.Done():
            return
        default:
        }

        // 连接服务器
        err := c.connect()
        if err != nil {
            log.Printf("Connect failed: %v", err)

            // 检查重试次数
            if c.config.MaxReconnectTries > 0 && reconnectTries >= c.config.MaxReconnectTries {
                log.Printf("Max reconnect tries reached")
                return
            }
            reconnectTries++

            // 等待重连
            select {
            case <-c.ctx.Done():
                return
            case <-time.After(c.config.ReconnectInterval):
                log.Printf("Reconnecting... (attempt %d)", reconnectTries)
                continue
            }
        }

        // 连接成功，重置重试计数
        reconnectTries = 0
        c.connected.Store(true)

        // 等待断开或重连信号
        select {
        case <-c.ctx.Done():
            return
        case <-c.reconnectCh:
            log.Println("Reconnect requested")
            c.connected.Store(false)
        }
    }
}

// connect 连接服务器
func (c *MuxClient) connect() error {
    // 1. 建立连接
    var d net.Dialer
    d.Timeout = c.config.DialTimeout

    conn, err := d.DialContext(c.ctx, "tcp", c.config.ServerAddr)
    if err != nil {
        return fmt.Errorf("dial failed: %w", err)
    }

    // TLS 包装（如果需要）
    if c.config.TLSConfig != nil {
        tlsConn := tls.Client(conn, c.config.TLSConfig)
        if err := tlsConn.Handshake(); err != nil {
            conn.Close()
            return fmt.Errorf("TLS handshake failed: %w", err)
        }
        conn = tlsConn
    }

    c.controlConn = conn
    log.Printf("Connected to %s", c.config.ServerAddr)

    // 2. 发送握手消息
    handshake := &HandshakeMessage{
        Version:   1,
        TunnelID:  c.config.TunnelID,
        AuthToken: c.config.AuthToken,
        Protocol:  c.config.Protocol,
        LocalAddr: c.config.LocalAddr,
    }

    handshakeData, err := c.protocol.EncodeHandshake(handshake)
    if err != nil {
        conn.Close()
        return fmt.Errorf("encode handshake failed: %w", err)
    }

    if _, err := conn.Write(handshakeData); err != nil {
        conn.Close()
        return fmt.Errorf("send handshake failed: %w", err)
    }

    // 3. 等待握手响应
    buf := make([]byte, 64*1024)
    n, err := conn.Read(buf)
    if err != nil {
        conn.Close()
        return fmt.Errorf("read handshake ack failed: %w", err)
    }

    ackMsg, err := c.protocol.Decode(buf[:n])
    if err != nil {
        conn.Close()
        return fmt.Errorf("decode handshake ack failed: %w", err)
    }

    if ackMsg.Type != MsgTypeHandshakeAck {
        conn.Close()
        return fmt.Errorf("expected HANDSHAKE_ACK, got %s", ackMsg.Type)
    }

    c.tunnelID = ackMsg.ID
    c.publicURL = string(ackMsg.Payload)

    log.Printf("Tunnel established: %s -> %s", c.tunnelID, c.publicURL)

    // 4. 启动心跳
    c.wg.Add(1)
    go c.heartbeatLoop()

    // 5. 启动消息处理
    c.wg.Add(1)
    go c.messageLoop()

    return nil
}

// heartbeatLoop 心跳循环
func (c *MuxClient) heartbeatLoop() {
    defer c.wg.Done()

    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-c.ctx.Done():
            return
        case <-ticker.C:
            if !c.connected.Load() {
                return
            }

            // 发送心跳
            heartbeatMsg, _ := c.protocol.Encode(&Message{Type: MsgTypeHeartbeat})
            if _, err := c.controlConn.Write(heartbeatMsg); err != nil {
                log.Printf("Heartbeat failed: %v", err)
                c.triggerReconnect()
                return
            }
        }
    }
}

// messageLoop 消息处理循环
func (c *MuxClient) messageLoop() {
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
                c.triggerReconnect()
            }
            return
        }

        // 解码消息
        msg, err := c.protocol.Decode(buf[:n])
        if err != nil {
            log.Printf("Decode error: %v", err)
            continue
        }

        // 处理消息
        switch msg.Type {
        case MsgTypeNewConn:
            c.handleNewConn(msg)

        case MsgTypeData:
            c.handleData(msg)

        case MsgTypeClose:
            c.handleClose(msg)

        case MsgTypeHeartbeatAck:
            // 心跳响应，忽略
        }
    }
}

// handleNewConn 处理新连接通知
func (c *MuxClient) handleNewConn(msg *Message) {
    newConnMsg, err := c.protocol.DecodeNewConn(msg.Payload)
    if err != nil {
        log.Printf("Decode NEWCONN failed: %v", err)
        return
    }

    connID := msg.ID
    remoteAddr := newConnMsg.RemoteAddr

    log.Printf("New connection: %s from %s", connID, remoteAddr)

    // 连接本地服务
    localConn, err := net.DialTimeout("tcp", c.config.LocalAddr, 5*time.Second)
    if err != nil {
        log.Printf("Connect local service failed: %v", err)
        // 发送关闭消息
        closeMsg, _ := c.protocol.EncodeClose(connID)
        c.controlConn.Write(closeMsg)
        return
    }

    // 保存连接
    connInfo := &LocalConnInfo{
        ID:        connID,
        LocalConn: localConn,
        CreatedAt: time.Now(),
    }
    c.localConns.Store(connID, connInfo)
    c.activeConns.Add(1)
    c.totalConns.Add(1)

    // 启动本地->远程转发
    go c.forwardLocalToRemote(connID, localConn)
}

// forwardLocalToRemote 从本地转发到远程
func (c *MuxClient) forwardLocalToRemote(connID string, localConn net.Conn) {
    defer func() {
        localConn.Close()
        c.localConns.Delete(connID)
        c.activeConns.Add(-1)

        // 发送关闭消息
        closeMsg, _ := c.protocol.EncodeClose(connID)
        c.controlConn.Write(closeMsg)
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

        // 编码并发送数据
        dataMsg, _ := c.protocol.EncodeData(connID, buf[:n])
        if _, err := c.controlConn.Write(dataMsg); err != nil {
            return
        }

        c.bytesOut.Add(int64(n))
    }
}

// handleData 处理数据消息
func (c *MuxClient) handleData(msg *Message) {
    connInfo, ok := c.localConns.Load(msg.ID)
    if !ok {
        return
    }

    localConn := connInfo.(*LocalConnInfo).LocalConn
    n, err := localConn.Write(msg.Payload)
    if err != nil {
        localConn.Close()
        c.localConns.Delete(msg.ID)
        c.activeConns.Add(-1)
        return
    }

    c.bytesIn.Add(int64(n))
}

// handleClose 处理关闭消息
func (c *MuxClient) handleClose(msg *Message) {
    connInfo, ok := c.localConns.Load(msg.ID)
    if !ok {
        return
    }

    localConn := connInfo.(*LocalConnInfo).LocalConn
    localConn.Close()
    c.localConns.Delete(msg.ID)
    c.activeConns.Add(-1)
}

// triggerReconnect 触发重连
func (c *MuxClient) triggerReconnect() {
    select {
    case c.reconnectCh <- struct{}{}:
    default:
    }
}

// GetStats 获取统计信息
func (c *MuxClient) GetStats() ClientStats {
    return ClientStats{
        TunnelID:    c.tunnelID,
        PublicURL:   c.publicURL,
        Connected:   c.connected.Load(),
        ActiveConns: c.activeConns.Load(),
        TotalConns:  c.totalConns.Load(),
        BytesIn:     c.bytesIn.Load(),
        BytesOut:    c.bytesOut.Load(),
    }
}

// ClientStats 客户端统计信息
type ClientStats struct {
    TunnelID    string
    PublicURL   string
    Connected   bool
    ActiveConns int64
    TotalConns  int64
    BytesIn     int64
    BytesOut    int64
}

// PublicURL 获取公网地址
func (c *MuxClient) PublicURL() string {
    return c.publicURL
}

// TunnelID 获取隧道ID
func (c *MuxClient) TunnelID() string {
    return c.tunnelID
}
```

---

## 五、辅助组件

### 5.1 端口管理器

```go
// tunnel/mux/port_manager.go

package mux

import (
    "sync"
)

// PortManager 端口管理器
type PortManager struct {
    start, end int
    used       map[int]bool
    mu         sync.Mutex
}

// NewPortManager 创建端口管理器
func NewPortManager(start, end int) *PortManager {
    return &PortManager{
        start: start,
        end:   end,
        used:  make(map[int]bool),
    }
}

// Allocate 分配端口
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

// Release 释放端口
func (m *PortManager) Release(port int) {
    m.mu.Lock()
    defer m.mu.Unlock()
    delete(m.used, port)
}

// Used 获取已使用端口数量
func (m *PortManager) Used() int {
    m.mu.Lock()
    defer m.mu.Unlock()
    return len(m.used)
}

// Available 获取可用端口数量
func (m *PortManager) Available() int {
    m.mu.Lock()
    defer m.mu.Unlock()
    return (m.end - m.start + 1) - len(m.used)
}
```

### 5.2 ID 生成器

```go
// tunnel/mux/util.go

package mux

import (
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "time"
)

// generateID 生成唯一ID
func generateID() string {
    b := make([]byte, 8)
    rand.Read(b)
    return fmt.Sprintf("%d%s", time.Now().UnixNano(), hex.EncodeToString(b))
}
```

---

## 六、使用示例

### 6.1 服务端启动

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/Talbot3/go-tunnel/tunnel/mux"
)

func main() {
    // 加载 TLS 证书
    cert, _ := tls.LoadX509KeyPair("cert.pem", "key.pem")

    config := mux.ServerConfig{
        ListenAddr:       ":443",
        TLSConfig: &tls.Config{
            Certificates: []tls.Certificate{cert},
        },
        AuthToken:        os.Getenv("AUTH_TOKEN"),
        MaxTunnels:       10000,
        MaxConnsPerTunnel: 1000,
        Protocol:         mux.NewBinaryProtocol(),
        PortRangeStart:   10000,
        PortRangeEnd:     20000,
    }

    server := mux.NewMuxServer(config)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    if err := server.Start(ctx); err != nil {
        log.Fatal(err)
    }

    log.Println("MuxServer started on :443")

    // 等待信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    log.Println("Shutting down...")
    server.Stop()
}
```

### 6.2 客户端启动

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/Talbot3/go-tunnel/tunnel/mux"
)

func main() {
    config := mux.ClientConfig{
        ServerAddr:       "tunnel.example.com:443",
        TLSConfig: &tls.Config{
            InsecureSkipVerify: false,
        },
        Protocol:         "tcp",
        LocalAddr:        "localhost:3000",
        AuthToken:        os.Getenv("AUTH_TOKEN"),
        ProtocolCodec:    mux.NewBinaryProtocol(),
        ReconnectInterval: 5 * time.Second,
    }

    client := mux.NewMuxClient(config)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    if err := client.Start(ctx); err != nil {
        log.Fatal(err)
    }

    log.Printf("Tunnel client started, waiting for connection...")
    log.Printf("Public URL: %s", client.PublicURL())

    // 等待信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    log.Println("Shutting down...")
    client.Stop()
}
```

---

## 七、自定义协议扩展

### 7.1 自定义协议示例

```go
package main

import (
    "github.com/Talbot3/go-tunnel/tunnel/mux"
)

// CustomProtocol 自定义协议
type CustomProtocol struct {
    // 可以添加自定义配置
    magicNumber uint32
    version     uint8
}

func NewCustomProtocol() *CustomProtocol {
    return &CustomProtocol{
        magicNumber: 0x54554E4E, // "TUNN"
        version:     1,
    }
}

func (p *CustomProtocol) Encode(msg *mux.Message) ([]byte, error) {
    // 自定义编码逻辑
    // 例如：添加 magic number、版本号、校验和等
    buf := new(bytes.Buffer)

    // 写入 magic number
    binary.Write(buf, binary.BigEndian, p.magicNumber)

    // 写入版本
    buf.WriteByte(p.version)

    // 写入消息类型
    buf.WriteByte(byte(msg.Type))

    // 写入 ID
    idBytes := []byte(msg.ID)
    binary.Write(buf, binary.BigEndian, uint16(len(idBytes)))
    buf.Write(idBytes)

    // 写入 payload
    binary.Write(buf, binary.BigEndian, uint32(len(msg.Payload)))
    buf.Write(msg.Payload)

    // 写入校验和
    checksum := crc32.ChecksumIEEE(buf.Bytes())
    binary.Write(buf, binary.BigEndian, checksum)

    return buf.Bytes(), nil
}

func (p *CustomProtocol) Decode(data []byte) (*mux.Message, error) {
    // 自定义解码逻辑
    // 验证 magic number、版本、校验和等
    // ...
}

// 实现其他 Protocol 接口方法...
```

### 7.2 使用自定义协议

```go
// 服务端
serverConfig := mux.ServerConfig{
    // ...
    Protocol: NewCustomProtocol(),
}

// 客户端
clientConfig := mux.ClientConfig{
    // ...
    ProtocolCodec: NewCustomProtocol(),
}
```

---

## 八、消息类型扩展

### 8.1 用户自定义消息

```go
// 定义自定义消息类型
const (
    MsgTypeFileTransfer  mux.MessageType = 0x20  // 文件传输
    MsgTypeStreamStart   mux.MessageType = 0x21  // 流开始
    MsgTypeStreamData    mux.MessageType = 0x22  // 流数据
    MsgTypeStreamEnd     mux.MessageType = 0x23  // 流结束
)

// 处理自定义消息
func (s *MuxServer) handleCustomMessage(tunnel *Tunnel, msg *Message) {
    switch msg.Type {
    case MsgTypeFileTransfer:
        // 处理文件传输
    case MsgTypeStreamStart:
        // 处理流开始
    // ...
    }
}
```

---

## 九、性能优化建议

### 9.1 连接池

```go
// 使用连接池复用本地连接
type LocalConnPool struct {
    pool sync.Pool
}

func NewLocalConnPool() *LocalConnPool {
    return &LocalConnPool{
        pool: sync.Pool{
            New: func() interface{} {
                return &LocalConnInfo{}
            },
        },
    }
}
```

### 9.2 批量发送

```go
// 批量编码发送，减少系统调用
func (c *MuxClient) batchSend(messages []*Message) error {
    buf := new(bytes.Buffer)
    for _, msg := range messages {
        data, _ := c.protocol.Encode(msg)
        buf.Write(data)
    }
    _, err := c.controlConn.Write(buf.Bytes())
    return err
}
```

### 9.3 零拷贝优化

```go
// 对于大文件传输，使用零拷贝
func (c *MuxClient) sendFile(connID string, file *os.File) error {
    // 使用 sendfile 系统调用
    // ...
}
```

---

## 十、总结

### 10.1 组件职责

| 组件 | 职责 | 文件 |
|------|------|------|
| `Protocol` | 消息编解码 | `protocol.go` |
| `BinaryProtocol` | 二进制协议实现 | `protocol.go` |
| `TextProtocol` | 文本协议实现 | `protocol.go` |
| `MuxServer` | 服务端核心 | `server.go` |
| `MuxClient` | 客户端核心 | `client.go` |
| `PortManager` | 端口管理 | `port_manager.go` |

### 10.2 扩展点

| 扩展点 | 接口 | 用途 |
|--------|------|------|
| 协议编解码 | `Protocol` | 自定义消息格式 |
| 消息类型 | `MessageType` | 自定义消息类型 (0x20-0x7F) |
| 认证 | `AuthToken` | 自定义认证逻辑 |
| TLS | `tls.Config` | 自定义 TLS 配置 |
