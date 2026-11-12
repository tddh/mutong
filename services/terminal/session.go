package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"gitee.com/tddh/mutong/interfaces"
)

// AuditEvent 终端会话审计事件，由 controller 注入回调写入操作审计
type AuditEvent struct {
	SessionID string
	Cluster   string
	Namespace string
	Pod       string
	Container string
	Shell     string
	Action    string
	Result    string
}

// AuditFunc 终端审计回调
type AuditFunc func(AuditEvent)

// 默认拒绝的系统命名空间（未配置白名单时生效）
var defaultDeniedNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
}

type TerminalSession struct {
	ID         string
	Cluster    string
	Namespace  string
	Pod        string
	Container  string
	Conn       *websocket.Conn
	sizeQueue  *terminalSizeQueue
	k8sClient  *kubernetes.Clientset
	restConfig *rest.Config
	stdinChan  chan []byte
	writeCh    chan []byte
	writerOnce sync.Once
	closeCh    chan struct{}
	createdAt  time.Time
	logger     interfaces.Logger
	auditHook  AuditFunc
	shell      string
}

type TerminalSessionManager struct {
	sessions          map[string]*TerminalSession
	mu                sync.RWMutex
	stopCh            chan struct{}
	stopOnce          sync.Once
	allowedNamespaces []string
	logger            interfaces.Logger
}

func NewTerminalSessionManager() *TerminalSessionManager {
	return &TerminalSessionManager{
		sessions: make(map[string]*TerminalSession),
		stopCh:   make(chan struct{}),
	}
}

// SetLogger 注入服务端日志器（会话与执行细节只写服务端日志，不回显客户端）
func (m *TerminalSessionManager) SetLogger(logger interfaces.Logger) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logger = logger
}

// SetAllowedNamespaces 设置终端可访问的命名空间白名单；
// 空列表表示默认策略：拒绝系统命名空间，其余允许。
func (m *TerminalSessionManager) SetAllowedNamespaces(namespaces []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowedNamespaces = append([]string(nil), namespaces...)
}

// IsNamespaceAllowed 校验命名空间是否允许建立终端会话
func (m *TerminalSessionManager) IsNamespaceAllowed(namespace string) bool {
	if !validNamespaceName(namespace) {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.allowedNamespaces) == 0 {
		return !defaultDeniedNamespaces[namespace]
	}
	for _, ns := range m.allowedNamespaces {
		if ns == namespace {
			return true
		}
	}
	return false
}

// validNamespaceName 校验 K8s 命名空间命名字符集（小写字母、数字、'-'、'.'）
func validNamespaceName(ns string) bool {
	if ns == "" || len(ns) > 253 {
		return false
	}
	for _, r := range ns {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func (m *TerminalSessionManager) StartCleanup(interval, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.CleanupExpired(maxAge)
			case <-m.stopCh:
				return
			}
		}
	}()
}

func (m *TerminalSessionManager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
}

// StopCleanup 停止 StartCleanup 启动的清理协程，幂等，可安全多次调用。
func (m *TerminalSessionManager) StopCleanup() {
	m.Stop()
}

func (m *TerminalSessionManager) CreateSession(cluster, namespace, pod, container string, conn *websocket.Conn) (*TerminalSession, error) {
	if !m.IsNamespaceAllowed(namespace) {
		return nil, fmt.Errorf("namespace %q is not allowed for terminal access", namespace)
	}
	id := generateSessionID()
	m.mu.RLock()
	logger := m.logger
	m.mu.RUnlock()
	sess := &TerminalSession{
		ID:        id,
		Cluster:   cluster,
		Namespace: namespace,
		Pod:       pod,
		Container: container,
		Conn:      conn,
		sizeQueue: &terminalSizeQueue{ch: make(chan remoteTerminalSize, 10)},
		stdinChan: make(chan []byte, 100),
		writeCh:   make(chan []byte, 64),
		closeCh:   make(chan struct{}),
		createdAt: time.Now(),
		logger:    logger,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = sess
	return sess, nil
}

func (sess *TerminalSession) SetK8sClient(client *kubernetes.Clientset, config *rest.Config) {
	sess.k8sClient = client
	sess.restConfig = config
}

// SetLogger 注入服务端日志器
func (sess *TerminalSession) SetLogger(logger interfaces.Logger) {
	sess.logger = logger
}

// SetAuditHook 注入审计回调（会话建立/exec 事件留痕）
func (sess *TerminalSession) SetAuditHook(hook AuditFunc) {
	sess.auditHook = hook
}

// audit 记录一条终端审计事件（回调为空时静默跳过）
func (sess *TerminalSession) audit(action, result string) {
	if sess == nil || sess.auditHook == nil {
		return
	}
	sess.auditHook(AuditEvent{
		SessionID: sess.ID,
		Cluster:   sess.Cluster,
		Namespace: sess.Namespace,
		Pod:       sess.Pod,
		Container: sess.Container,
		Shell:     sess.shell,
		Action:    action,
		Result:    result,
	})
}

// logError 服务端日志（细节不回显客户端）
func (sess *TerminalSession) logError(msg string, err error) {
	if sess == nil || sess.logger == nil {
		return
	}
	sess.logger.Error(msg,
		zap.String("session", sess.ID),
		zap.String("namespace", sess.Namespace),
		zap.String("pod", sess.Pod),
		zap.String("container", sess.Container),
		zap.Error(err))
}

func (sess *TerminalSession) WriteStdin(data []byte) {
	if sess != nil && sess.stdinChan != nil {
		select {
		case sess.stdinChan <- data:
		default:
		}
	}
}

func (sess *TerminalSession) Resize(width uint16, height uint16) {
	if sess != nil && sess.sizeQueue != nil {
		sess.sizeQueue.Push(width, height)
	}
}

func (m *TerminalSessionManager) CloseSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, ok := m.sessions[id]; ok {
		select {
		case <-sess.closeCh:
		default:
			close(sess.closeCh)
		}
		if sess.Conn != nil {
			_ = sess.Conn.Close()
		}
		delete(m.sessions, id)
	}
	return nil
}

func (m *TerminalSessionManager) CleanupExpired(maxAge time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, sess := range m.sessions {
		if time.Since(sess.createdAt) > maxAge {
			select {
			case <-sess.closeCh:
			default:
				close(sess.closeCh)
			}
			if sess.Conn != nil {
				_ = sess.Conn.Close()
			}
			delete(m.sessions, id)
		}
	}
}

func (m *TerminalSessionManager) GetSession(id string) *TerminalSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte("sess_" + time.Now().Format("20060102150405")))
	}
	return hex.EncodeToString(b)
}

type remoteTerminalSize struct {
	Width  uint16
	Height uint16
}

type terminalSizeQueue struct {
	ch chan remoteTerminalSize
}

func (q *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case s := <-q.ch:
		return &remotecommand.TerminalSize{Width: s.Width, Height: s.Height}
	default:
		return nil
	}
}

func (q *terminalSizeQueue) Push(width uint16, height uint16) {
	if q != nil {
		select {
		case q.ch <- remoteTerminalSize{Width: width, Height: height}:
		default:
		}
	}
}

// startWriter serialises all WebSocket writes through a single goroutine.
// gorilla/websocket forbids concurrent writers.
func (sess *TerminalSession) startWriter() {
	sess.writerOnce.Do(func() {
		go func() {
			for {
				select {
				case msg := <-sess.writeCh:
					if sess.Conn == nil {
						continue
					}
					_ = sess.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if err := sess.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
						return
					}
				case <-sess.closeCh:
					return
				}
			}
		}()
	})
}

// writeText enqueues a status/error message for the writer goroutine.
func (sess *TerminalSession) writeText(msg string) {
	sess.writeBytes([]byte(msg))
}

// writeBytes enqueues a copy of msg. The copy is required because callers
// reuse their read buffers.
func (sess *TerminalSession) writeBytes(msg []byte) {
	if sess == nil || len(msg) == 0 {
		return
	}
	buf := append([]byte(nil), msg...)
	select {
	case sess.writeCh <- buf:
	default:
		// Never block the reader on a slow client.
	}
}

func (sess *TerminalSession) StartK8sExec(cluster, namespace, pod, container, shell string) {
	sess.startWriter()
	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	go func() {
		select {
		case <-sess.closeCh:
			streamCancel()
		case <-streamCtx.Done():
		}
	}()
	if sess.Conn != nil {
		sess.writeText(string([]byte("\r\n\x1b[36mStarting exec session...\x1b[0m\r\n")))
	}

	if shell == "" {
		shell = "/bin/sh"
	}
	sess.shell = shell

	if sess.k8sClient == nil {
		sess.logError("Terminal exec rejected: K8s client is nil", nil)
		sess.audit("terminal_exec", "failed: k8s client not configured")
		if sess.Conn != nil {
			sess.writeText(string([]byte("\r\n\x1b[31mError: terminal backend not available\x1b[0m\r\n")))
		}
		return
	}
	if sess.restConfig == nil {
		sess.logError("Terminal exec rejected: K8s restConfig is nil", nil)
		sess.audit("terminal_exec", "failed: k8s rest config not available")
		if sess.Conn != nil {
			sess.writeText(string([]byte("\r\n\x1b[31mError: terminal backend not available\x1b[0m\r\n")))
		}
		return
	}

	if sess.Conn != nil {
		sess.writeText(string([]byte("\r\n\x1b[36mShell: " + shell + "\x1b[0m\r\n")))
	}

	req := sess.k8sClient.CoreV1().RESTClient().Post().
		Resource("pods").Name(pod).Namespace(namespace).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   []string{shell},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	// Exec URL / API 地址属于内部信息，只写服务端日志与审计，不回显客户端
	if sess.logger != nil {
		sess.logger.Info("Terminal exec request",
			zap.String("session", sess.ID),
			zap.String("cluster", cluster),
			zap.String("namespace", namespace),
			zap.String("pod", pod),
			zap.String("container", container),
			zap.String("shell", shell),
			zap.String("execURL", req.URL().String()))
	}
	sess.audit("terminal_exec", "started")

	executor, err := remotecommand.NewSPDYExecutor(sess.restConfig, "POST", req.URL())
	if err != nil {
		sess.logError("Failed to create SPDY executor", err)
		sess.audit("terminal_exec", "failed: "+err.Error())
		if sess.Conn != nil {
			sess.writeText(string([]byte("\r\n\x1b[31mError: failed to start exec session\x1b[0m\r\n")))
		}
		return
	}

	if sess.Conn != nil {
		sess.writeText(string([]byte("\r\n\x1b[36mSPDY Executor created, starting stream...\x1b[0m\r\n")))
	}

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdoutReader.Read(buf)
			if err != nil {
				break
			}
			if n > 0 {
				sess.writeBytes(buf[:n])
			}
		}
	}()

	go func() {
		for {
			select {
			case msg, ok := <-sess.stdinChan:
				if !ok {
					return
				}
				_, _ = stdinWriter.Write(msg)
			case <-sess.closeCh:
				return
			}
		}
	}()

	err = executor.StreamWithContext(streamCtx, remotecommand.StreamOptions{
		Stdin:             stdinReader,
		Stdout:            stdoutWriter,
		Stderr:            stdoutWriter,
		Tty:               true,
		TerminalSizeQueue: sess.sizeQueue,
	})

	// Close the pipe ends so the stdout pump and stdin pump can exit even if
	// the transport stream has already terminated.
	_ = stdoutWriter.Close()
	_ = stdinWriter.Close()

	if err != nil {
		sess.logError("Terminal exec stream failed", err)
		sess.audit("terminal_exec", "failed: "+err.Error())
		if sess.Conn != nil {
			sess.writeText(string([]byte("\r\n\x1b[31mError: exec stream terminated\x1b[0m\r\n")))
		}
		return
	}
	sess.audit("terminal_exec", "ended")
	if sess.Conn != nil {
		sess.writeText(string([]byte("\r\n\x1b[33mSession ended normally\x1b[0m\r\n")))
	}
}

// SessionCount returns the number of live terminal sessions.
func (m *TerminalSessionManager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
