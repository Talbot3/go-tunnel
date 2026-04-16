# QUIC 内网穿透系统研发任务分配

## 项目概述

基于 Code Review 结果，将改进任务按优先级和模块分配给研发团队。

**目标**：将系统评分从 7.0/10 提升至 9.0/10

---

## 一、团队角色定义

| 角色 | 负责人 | 职责 |
|------|--------|------|
| 架构师 | @arch-lead | 整体架构设计、技术选型、Code Review |
| 核心开发 | @core-dev | QUIC 核心功能、协议实现 |
| 安全工程师 | @security-eng | 安全审计、加密、认证 |
| 性能工程师 | @perf-eng | 性能优化、监控、压测 |
| 测试工程师 | @test-eng | 测试用例、CI/CD、质量保障 |
| 运维工程师 | @ops-eng | 部署、监控、文档 |

---

## 二、任务看板

### 🔴 P0 - 必须修复（本周完成）

#### TASK-001: controlStream 并发安全修复
- **负责人**: @core-dev
- **优先级**: P0
- **工时**: 2h
- **截止日期**: Day 1
- **描述**: 为 `MuxClient.controlStream` 添加读写锁保护
- **验收标准**:
  - 所有 controlStream 访问通过 getter/setter
  - 并发测试通过（race detector）
  - 代码覆盖率 ≥ 90%
- **关联文件**: `quic/quic.go:1766`, `quic/quic.go:1898-1904`

```go
// 实现示例
type MuxClient struct {
    controlStreamMu sync.RWMutex
    controlStream   quic.Stream
}

func (s *MuxClient) setControlStream(stream quic.Stream) {
    s.controlStreamMu.Lock()
    defer s.controlStreamMu.Unlock()
    s.controlStream = stream
}
```

---

#### TASK-002: Stream 超时清理机制
- **负责人**: @core-dev
- **优先级**: P0
- **工时**: 4h
- **截止日期**: Day 2
- **描述**: 添加僵尸 Stream 检测和清理机制
- **验收标准**:
  - 空闲超过 10 分钟的 Stream 自动关闭
  - 清理日志完整
  - 内存泄漏测试通过
- **关联文件**: `quic/quic.go:1164-1233`

---

#### TASK-003: 错误处理完善
- **负责人**: @core-dev
- **优先级**: P0
- **工时**: 3h
- **截止日期**: Day 2
- **描述**: 完善错误日志，添加上下文信息
- **验收标准**:
  - 所有错误包含 tunnelID、connID、操作类型
  - 错误分级（Debug/Info/Warn/Error）
  - 敏感信息脱敏
- **关联文件**: `quic/quic.go` 全文件

---

### 🟠 P1 - 重要改进（本月完成）

#### TASK-004: 0-RTT 快速重连完善
- **负责人**: @core-dev + @security-eng
- **优先级**: P1
- **工时**: 2d
- **截止日期**: Week 2
- **描述**: 实现完整的 0-RTT 重连机制
- **子任务**:
  - [ ] TASK-004a: 实现 DialEarly 连接 (@core-dev, 4h)
  - [ ] TASK-004b: 实现状态持久化 (@core-dev, 4h)
  - [ ] TASK-004c: 实现服务端状态恢复 (@core-dev, 4h)
  - [ ] TASK-004d: 重放攻击防护 (@security-eng, 4h)
- **验收标准**:
  - 断线重连延迟 < 50ms
  - 重放攻击测试通过
  - 状态恢复一致性测试通过

```go
// 实现示例
func (s *MuxClient) connect0RTT() error {
    conn, err := quic.DialEarly(ctx, udpConn, addr, tlsConf, quicConf)
    
    // 立即发送 SessionRestore（0-RTT 数据）
    restoreMsg := buildSessionRestoreMsg(s.savedState)
    conn.SendDatagram(restoreMsg)
}
```

---

#### TASK-005: UDP 协议支持
- **负责人**: @core-dev
- **优先级**: P1
- **工时**: 1.5d
- **截止日期**: Week 2
- **描述**: 添加 UDP 协议穿透支持
- **子任务**:
  - [ ] TASK-005a: 添加 ProtocolUDP 常量 (0.5h)
  - [ ] TASK-005b: 实现 UDP 可靠模式转发 (4h)
  - [ ] TASK-005c: 实现 UDP 不可靠模式转发 (4h)
  - [ ] TASK-005d: UDP 测试用例 (4h)
- **验收标准**:
  - 支持 UDP 可靠/不可靠两种模式
  - UDP 包大小限制检查
  - 与 TCP 性能对比测试

---

#### TASK-006: 会话头 AddrType 字段
- **负责人**: @core-dev
- **优先级**: P1
- **工时**: 4h
- **截止日期**: Week 2
- **描述**: 完善会话头格式，添加地址类型字段
- **验收标准**:
  - 支持 IPv4/IPv6/Domain 三种地址类型
  - 向后兼容旧格式
  - 编解码测试通过

```go
// 新格式
type SessionHeader struct {
    Protocol byte
    AddrType byte   // 0x01=IPv4, 0x02=IPv6, 0x03=Domain
    Target   string
    Flags    byte
    ConnID   string
}
```

---

#### TASK-007: HTTP/2 Stream 映射优化
- **负责人**: @core-dev
- **优先级**: P1
- **工时**: 1d
- **截止日期**: Week 3
- **描述**: 实现 HTTP/2 Stream 到 QUIC Stream 的映射优化
- **验收标准**:
  - HTTP/2 Stream ID 映射到 QUIC Stream
  - 保留 HTTP/2 优先级和流控
  - 性能提升 ≥ 20%

---

#### TASK-008: HTTP/3 服务端终止
- **负责人**: @core-dev
- **优先级**: P1
- **工时**: 1.5d
- **截止日期**: Week 3
- **描述**: 实现 HTTP/3 服务端终止模式
- **验收标准**:
  - 服务端终止外层 QUIC
  - 支持 QPACK 动态表
  - 支持 HTTP/3 Push

---

### 🟡 P2 - 性能优化（下月完成）

#### TASK-009: Linux 零拷贝优化
- **负责人**: @perf-eng
- **优先级**: P2
- **工时**: 2d
- **截止日期**: Month 2
- **描述**: 使用 splice/io_uring 实现零拷贝转发
- **验收标准**:
  - Linux 平台使用 splice
  - CPU 占用降低 ≥ 30%
  - 吞吐量提升 ≥ 20%

```go
// 实现示例
//go:build linux
func spliceTCPToStream(src *net.TCPConn, dst quic.Stream) error {
    srcFd, _ := src.File()
    // syscall.Splice(...)
}
```

---

#### TASK-010: OpenTelemetry 监控集成
- **负责人**: @perf-eng + @ops-eng
- **优先级**: P2
- **工时**: 1.5d
- **截止日期**: Month 2
- **描述**: 添加 OpenTelemetry 指标导出
- **子任务**:
  - [ ] TASK-010a: QUIC 层指标 (4h)
  - [ ] TASK-010b: 应用层指标 (4h)
  - [ ] TASK-010c: Grafana 面板 (2h)
- **验收标准**:
  - 导出 Prometheus 格式指标
  - Grafana 面板完整
  - 告警规则配置

**指标清单**:
```
# QUIC 层
quic_rtt_ms
quic_packet_loss_rate
quic_congestion_window
quic_streams_active
quic_0rtt_success_rate

# 应用层
tunnel_connections_active
tunnel_bytes_transferred
tunnel_latency_ms
tunnel_errors_total
```

---

#### TASK-011: 缓冲池动态优化
- **负责人**: @perf-eng
- **优先级**: P2
- **工时**: 1d
- **截止日期**: Month 2
- **描述**: 根据内存动态调整缓冲区大小
- **验收标准**:
  - 根据系统内存自动配置
  - 内存占用不超过阈值
  - 性能测试通过

---

#### TASK-012: QLOG 调试支持
- **负责人**: @perf-eng
- **优先级**: P2
- **工时**: 1d
- **截止日期**: Month 2
- **描述**: 添加 QLOG 格式导出，支持 qvis 可视化
- **验收标准**:
  - 导出标准 QLOG 格式
  - qvis 可正常解析
  - 按需开启/关闭

---

### 🟢 P3 - 架构演进（季度规划）

#### TASK-013: CID 路由无状态网关
- **负责人**: @arch-lead + @core-dev
- **优先级**: P3
- **工时**: 5d
- **截止日期**: Q2
- **描述**: 实现基于 CID 的无状态网关架构
- **子任务**:
  - [ ] TASK-013a: 自定义 CID 生成器 (1d)
  - [ ] TASK-013b: CID 一致性哈希路由 (1d)
  - [ ] TASK-013c: UDP 负载均衡配置 (1d)
  - [ ] TASK-013d: 多实例测试 (2d)
- **验收标准**:
  - 网关无状态，可随意扩缩容
  - 连接迁移测试通过
  - 性能线性扩展

---

#### TASK-014: 状态外置 (Redis/etcd)
- **负责人**: @core-dev + @ops-eng
- **优先级**: P3
- **工时**: 3d
- **截止日期**: Q2
- **描述**: 实现状态外置，支持集群部署
- **子任务**:
  - [ ] TASK-014a: StateStore 接口定义 (0.5d)
  - [ ] TASK-014b: Redis 实现 (1d)
  - [ ] TASK-014c: etcd 实现 (1d)
  - [ ] TASK-014d: 故障恢复测试 (0.5d)
- **验收标准**:
  - 支持 Redis 和 etcd 两种后端
  - 故障切换时间 < 5s
  - 数据一致性保证

```go
// 接口定义
type StateStore interface {
    SaveTunnel(ctx context.Context, tunnelID string, state *TunnelState) error
    LoadTunnel(ctx context.Context, tunnelID string) (*TunnelState, error)
    DeleteTunnel(ctx context.Context, tunnelID string) error
    ListTunnels(ctx context.Context) ([]*TunnelState, error)
}
```

---

#### TASK-015: 连接迁移应用层支持
- **负责人**: @core-dev
- **优先级**: P3
- **工时**: 2d
- **截止日期**: Q2
- **描述**: 添加连接迁移的应用层回调和支持
- **验收标准**:
  - 迁移事件回调
  - 迁移统计指标
  - 移动端测试通过

---

## 三、测试任务

#### TASK-016: 混沌工程测试
- **负责人**: @test-eng
- **优先级**: P1
- **工时**: 2d
- **截止日期**: Week 3
- **描述**: 添加混沌测试用例
- **子任务**:
  - [ ] TASK-016a: 网络分区测试 (4h)
  - [ ] TASK-016b: 高延迟测试 (4h)
  - [ ] TASK-016c: 包丢失测试 (4h)
  - [ ] TASK-016d: 资源耗尽测试 (4h)

---

#### TASK-017: 模糊测试
- **负责人**: @test-eng
- **优先级**: P2
- **工时**: 1d
- **截止日期**: Month 2
- **描述**: 添加 Go fuzz 测试
- **验收标准**:
  - SessionHeader 编解码 fuzz
  - 控制消息解析 fuzz
  - 无 panic/crash

---

#### TASK-018: 性能基准测试
- **负责人**: @perf-eng + @test-eng
- **优先级**: P2
- **工时**: 2d
- **截止日期**: Month 2
- **描述**: 建立性能基准测试套件
- **验收标准**:
  - 吞吐量基准
  - 延迟基准
  - 内存基准
  - CI 集成

---

## 四、安全任务

#### TASK-019: 安全审计
- **负责人**: @security-eng
- **优先级**: P1
- **工时**: 2d
- **截止日期**: Week 2
- **描述**: 全面安全审计
- **子任务**:
  - [ ] TASK-019a: OWASP Top 10 检查 (4h)
  - [ ] TASK-019b: 敏感数据泄露检查 (4h)
  - [ ] TASK-019c: 权限检查 (4h)
  - [ ] TASK-019d: 安全报告 (4h)

---

#### TASK-020: 渗透测试
- **负责人**: @security-eng
- **优先级**: P2
- **工时**: 3d
- **截止日期**: Month 2
- **描述**: 执行渗透测试
- **验收标准**:
  - 无高危漏洞
  - 中危漏洞有缓解措施
  - 测试报告完整

---

## 五、文档任务

#### TASK-021: API 文档完善
- **负责人**: @ops-eng
- **优先级**: P2
- **工时**: 1d
- **截止日期**: Week 3
- **描述**: 完善 API 文档和示例
- **验收标准**:
  - 所有公开 API 有文档
  - 使用示例完整
  - 中文/英文双语

---

#### TASK-022: 部署文档
- **负责人**: @ops-eng
- **优先级**: P2
- **工时**: 1d
- **截止日期**: Week 3
- **描述**: 编写部署运维文档
- **子任务**:
  - [ ] TASK-022a: Docker 部署 (2h)
  - [ ] TASK-022b: Kubernetes 部署 (4h)
  - [ ] TASK-022c: 监控配置 (2h)

---

## 六、里程碑规划

```
Week 1: P0 任务完成
├── TASK-001: controlStream 并发安全 ✅
├── TASK-002: Stream 超时清理 ✅
└── TASK-003: 错误处理完善 ✅

Week 2: P1 核心功能
├── TASK-004: 0-RTT 完善 ✅
├── TASK-005: UDP 支持 ✅
├── TASK-006: AddrType 字段 ✅
└── TASK-019: 安全审计 ✅

Week 3: P1 协议优化
├── TASK-007: HTTP/2 优化 ✅
├── TASK-008: HTTP/3 终止 ✅
├── TASK-016: 混沌测试 ✅
└── TASK-021: API 文档 ✅

Month 2: P2 性能优化
├── TASK-009: 零拷贝 ✅
├── TASK-010: OpenTelemetry ✅
├── TASK-017: 模糊测试 ✅
└── TASK-018: 性能基准 ✅

Q2: P3 架构演进
├── TASK-013: CID 路由 ✅
├── TASK-014: 状态外置 ✅
└── TASK-015: 连接迁移 ✅
```

---

## 七、资源分配

### 人力投入

| 角色 | Week 1 | Week 2 | Week 3 | Month 2 | Q2 |
|------|--------|--------|--------|---------|-----|
| @arch-lead | 20% | 10% | 10% | 5% | 30% |
| @core-dev | 100% | 100% | 80% | 50% | 60% |
| @security-eng | 0% | 50% | 20% | 30% | 10% |
| @perf-eng | 0% | 0% | 20% | 80% | 20% |
| @test-eng | 20% | 30% | 50% | 50% | 20% |
| @ops-eng | 0% | 0% | 30% | 40% | 30% |

### 预算估算

| 类别 | 金额 | 说明 |
|------|------|------|
| 人力成本 | ¥120,000 | 6人 × 2月 |
| 云资源 | ¥10,000 | 测试环境、压测 |
| 安全审计 | ¥5,000 | 外部审计 |
| **总计** | **¥135,000** | |

---

## 八、风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 0-RTT 安全漏洞 | 中 | 高 | 安全工程师全程参与，渗透测试 |
| 性能优化不达预期 | 低 | 中 | 分阶段验证，保留回滚方案 |
| 协议兼容性问题 | 中 | 中 | 完善测试用例，版本协商机制 |
| 人员变动 | 低 | 高 | 文档完善，知识共享 |

---

## 九、验收标准

### 整体验收

| 指标 | 当前 | 目标 |
|------|------|------|
| 代码评分 | 7.0/10 | 9.0/10 |
| 测试覆盖率 | 80.1% | 90%+ |
| 重连延迟 | ~200ms | <50ms |
| 吞吐量 | ~6 Gbps | ~8 Gbps |
| 内存泄漏 | 未验证 | 无泄漏 |

### 功能验收

- [ ] 所有 P0 任务完成
- [ ] 所有 P1 任务完成
- [ ] 安全审计通过
- [ ] 性能基准通过
- [ ] 文档完整

---

## 十、沟通机制

### 日常沟通

- **每日站会**: 10:00，15分钟
- **周会**: 周五 14:00，1小时
- **代码评审**: PR 提交后 24h 内完成

### 汇报机制

- **日报**: 每日 18:00 发送进度
- **周报**: 每周五 17:00 发送总结
- **里程碑报告**: 每个里程碑完成后

### 工具

- **任务管理**: GitHub Projects / Jira
- **代码管理**: GitHub
- **文档**: Markdown / Notion
- **沟通**: Slack / 钉钉

---

*文档版本: v1.0*
*创建日期: 2026-04-16*
*负责人: @arch-lead*
