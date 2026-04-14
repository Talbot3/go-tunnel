#!/usr/bin/env bash
# go-tunnel 端到端测试脚本

set -e

echo "=========================================="
echo "    go-tunnel 端到端测试"
echo "=========================================="
echo ""

# 清理函数
cleanup() {
    pkill -f "python3.*echo" 2>/dev/null || true
    pkill -f "./proxy" 2>/dev/null || true
}

trap cleanup EXIT
cleanup
sleep 1

# 测试端口
ECHO_PORT=60000
PROXY_PORT=60080

# 启动 echo 服务器
echo "启动 echo 服务器..."
python3 -c "
import socket, threading
def handle(c):
    try:
        while d := c.recv(65536): c.sendall(d)
    except: pass
    finally: c.close()
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', $ECHO_PORT))
s.listen(50)
while True:
    c, _ = s.accept()
    threading.Thread(target=handle, args=(c,), daemon=True).start()
" &
sleep 1

# 启动代理
echo "启动代理..."
./proxy -protocol tcp -listen :$PROXY_PORT -target 127.0.0.1:$ECHO_PORT 2>&1 &
sleep 2

# 运行测试
python3 << 'PYEOF'
import socket, time, threading

ECHO_PORT = 60000
PROXY_PORT = 60080

print("=== 1. 基本功能测试 ===")

# 测试基本转发
def test_basic():
    for port, name in [(ECHO_PORT, "直接"), (PROXY_PORT, "代理")]:
        sock = socket.socket()
        sock.connect(('127.0.0.1', port))
        sock.send(b'HELLO')
        data = sock.recv(1024)
        assert data == b'HELLO', f"{name} 转发失败"
        sock.close()
    print("✓ 基本转发测试通过")

test_basic()

print("\n=== 2. 吞吐量测试 ===")

data = b'X' * 1024 * 1024  # 1MB

# 直接连接
sock = socket.socket()
sock.connect(('127.0.0.1', ECHO_PORT))
start = time.time()
for _ in range(10):
    sock.sendall(data)
    r = 0
    while r < len(data): r += len(sock.recv(65536))
elapsed = time.time() - start
direct_tp = 10*2/elapsed
print(f"直接: {direct_tp:.0f} MB/s ({direct_tp*8/1000:.2f} Gbps)")
sock.close()

# 代理
sock = socket.socket()
sock.connect(('127.0.0.1', PROXY_PORT))
start = time.time()
for _ in range(10):
    sock.sendall(data)
    r = 0
    while r < len(data): r += len(sock.recv(65536))
elapsed = time.time() - start
proxy_tp = 10*2/elapsed
print(f"代理: {proxy_tp:.0f} MB/s ({proxy_tp*8/1000:.2f} Gbps)")
sock.close()

print(f"性能损失: {(1-proxy_tp/direct_tp)*100:.1f}%")

print("\n=== 3. 延迟测试 ===")

# 直接连接
lats = []
for _ in range(100):
    sock = socket.socket()
    sock.connect(('127.0.0.1', ECHO_PORT))
    start = time.time()
    sock.send(b'PING')
    sock.recv(1024)
    lats.append(time.time() - start)
    sock.close()
direct_lat = sum(lats)/len(lats)*1000
print(f"直接: 平均 {direct_lat:.3f}ms")

# 代理
lats = []
for _ in range(100):
    sock = socket.socket()
    sock.connect(('127.0.0.1', PROXY_PORT))
    start = time.time()
    sock.send(b'PING')
    sock.recv(1024)
    lats.append(time.time() - start)
    sock.close()
proxy_lat = sum(lats)/len(lats)*1000
print(f"代理: 平均 {proxy_lat:.3f}ms")
print(f"延迟增加: {proxy_lat - direct_lat:.3f}ms")

print("\n=== 4. 并发测试 ===")

def test_concurrent(port, name):
    results = []
    def worker():
        for _ in range(50):
            sock = socket.socket()
            sock.connect(('127.0.0.1', port))
            sock.send(b'X' * 1024)
            sock.recv(1024)
            sock.close()

    start = time.time()
    threads = [threading.Thread(target=worker) for _ in range(10)]
    for t in threads: t.start()
    for t in threads: t.join()
    elapsed = time.time() - start
    print(f"{name}: {10*50/elapsed:.0f} RPS")

test_concurrent(ECHO_PORT, "直接")
test_concurrent(PROXY_PORT, "代理")

print("\n=== 测试完成 ===")
PYEOF
