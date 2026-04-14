#!/usr/bin/env bash
set -e

echo "🚀 Starting benchmark (requires iperf3 & ncat)"

# Build the proxy
echo "📦 Building proxy..."
go build -ldflags="-s -w" -o proxy .

# 1. Start proxy in background
echo "🔧 Starting proxy server..."
./proxy -protocol tcp -listen :8080 -target 127.0.0.1:9000 &
PROXY_PID=$!
sleep 1

# 2. Start a simple echo server for testing
echo "📡 Starting echo server..."
ncat -l 9000 --keep-open --sh-exec "cat" &
ECHO_PID=$!
sleep 1

# 3. TCP throughput test (iperf3)
echo "📊 Testing TCP throughput..."
if command -v iperf3 &> /dev/null; then
    # Start iperf3 server
    iperf3 -s -p 9000 &
    IPERF_PID=$!
    sleep 1

    # Run iperf3 client through proxy
    iperf3 -c 127.0.0.1 -p 8080 -t 5 -P 4 || true

    kill $IPERF_PID 2>/dev/null || true
else
    echo "⚠️ iperf3 not installed, skipping throughput test"
fi

# 4. Concurrent connection test
echo "⏱️ Testing concurrent connection latency..."
if command -v ncat &> /dev/null; then
    start_time=$(date +%s.%N)
    for i in {1..100}; do
        echo "PING" | ncat 127.0.0.1 8080 -w 1 > /dev/null 2>&1 &
    done
    wait
    end_time=$(date +%s.%N)
    elapsed=$(echo "$end_time - $start_time" | bc)
    echo "✅ 100 concurrent connections completed in ${elapsed}s"
else
    echo "⚠️ ncat not installed, skipping concurrent test"
fi

# 5. Cleanup
echo "🧹 Cleaning up..."
kill $PROXY_PID 2>/dev/null || true
kill $ECHO_PID 2>/dev/null || true

echo "✅ Benchmark completed"
