package quic_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	tunnelquic "github.com/Talbot3/go-tunnel/quic"
)

// ============================================
// 测试辅助函数
// ============================================

// buildDomainFrame 构建 TCP+TLS 入口所需的域名帧
// 格式: [2B DomainLen][Domain]
func buildDomainFrame(domain string) []byte {
	buf := make([]byte, 2+len(domain))
	binary.BigEndian.PutUint16(buf[:2], uint16(len(domain)))
	copy(buf[2:], domain)
	return buf
}

// startLocalQUICServer 启动本地 QUIC echo 服务
// 返回监听地址和停止函数
func startLocalQUICServer(t *testing.T, cert tls.Certificate) (string, func()) {
	// 生成测试证书
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"echo"},
		MinVersion:   tls.VersionTLS13,
	}

	// 创建 UDP 监听
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to listen UDP: %v", err)
	}

	// 创建 QUIC 监听器
	listener, err := quic.Listen(udpConn, tlsConfig, &quic.Config{
		EnableDatagrams: true,
	})
	if err != nil {
		udpConn.Close()
		t.Fatalf("Failed to create QUIC listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopCh := make(chan struct{})

	// 启动 echo 服务
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			conn, err := listener.Accept(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}

			go handleQUICEchoConn(ctx, conn)
		}
	}()

	return fmt.Sprintf("127.0.0.1:%d", udpConn.LocalAddr().(*net.UDPAddr).Port), func() {
		cancel()
		listener.Close()
		udpConn.Close()
		close(stopCh)
	}
}

// handleQUICEchoConn 处理 QUIC echo 连接
func handleQUICEchoConn(ctx context.Context, conn quic.Connection) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}

		go func(s quic.Stream) {
			defer s.Close()
			buf := make([]byte, 4096)
			for {
				n, err := s.Read(buf)
				if err != nil {
					return
				}
				s.Write(buf[:n])
			}
		}(stream)
	}
}

// startHTTPServer 启动本地 HTTP 服务
// 返回监听地址和停止函数
func startHTTPServer(t *testing.T) (string, func()) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello from HTTP server"))
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		w.WriteHeader(http.StatusOK)
		w.Write(buf[:n])
	})

	server := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: mux,
	}

	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}

	go server.Serve(ln)

	return ln.Addr().String(), func() {
		server.Close()
		ln.Close()
	}
}

// startHTTP3Server 启动本地 HTTP/3 服务
// 返回监听地址和停止函数
func startHTTP3Server(t *testing.T, cert tls.Certificate) (string, func()) {
	// 简化实现：使用 TCP HTTP 服务作为替代
	// HTTP/3 测试需要更复杂的设置
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello from HTTP server"))
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}

	server := &http.Server{
		Handler: mux,
	}

	go server.Serve(ln)

	return ln.Addr().String(), func() {
		server.Close()
		ln.Close()
	}
}

// waitForHeartbeat 等待心跳触发
// timeout: 最长等待时间
func waitForHeartbeat(t *testing.T, client *tunnelquic.MuxClient, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	for {
		select {
		case <-ctx.Done():
			t.Logf("Heartbeat wait timed out after %v", time.Since(startTime))
			return false
		case <-ticker.C:
			stats := client.GetStats()
			// 检查是否有心跳相关的统计信息
			if stats.Connected {
				t.Logf("Client still connected after %v", time.Since(startTime))
			}
		}
	}
}

// GenerateTestCertificate 生成测试用 TLS 证书
func GenerateTestCertificate(t *testing.T) tls.Certificate {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost", "*.example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}
}

// CreateTestQUICConfig 创建测试用 QUIC 配置
func CreateTestQUICConfig(enableDatagrams bool) *quic.Config {
	return &quic.Config{
		EnableDatagrams: enableDatagrams,
		MaxIdleTimeout:  30 * time.Second,
	}
}

// DialQUICServer 拨号到 QUIC 服务器
func DialQUICServer(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (quic.Connection, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolve address failed: %w", err)
	}

	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, fmt.Errorf("listen UDP failed: %w", err)
	}

	conn, err := quic.Dial(ctx, udpConn, udpAddr, tlsConfig, quicConfig)
	if err != nil {
		udpConn.Close()
		return nil, fmt.Errorf("dial QUIC failed: %w", err)
	}

	return conn, nil
}

// WaitForTunnelReady 等待隧道准备就绪
func WaitForTunnelReady(client *tunnelquic.MuxClient, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			stats := client.GetStats()
			if stats.Connected && client.PublicURL() != "" {
				return true
			}
		}
	}
}

// ExtractPortFromURL 从 URL 中提取端口号
func ExtractPortFromURL(url string) (int, error) {
	var port int
	_, err := fmt.Sscanf(url, "tcp://:%d", &port)
	if err != nil {
		_, err = fmt.Sscanf(url, "quic://:%d", &port)
	}
	return port, err
}
