package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

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
}

type TerminalSessionManager struct {
	sessions map[string]*TerminalSession
	mu       sync.RWMutex
}

func NewTerminalSessionManager() *TerminalSessionManager {
	return &TerminalSessionManager{
		sessions: make(map[string]*TerminalSession),
	}
}

func (m *TerminalSessionManager) CreateSession(cluster, namespace, pod, container string, conn *websocket.Conn) (*TerminalSession, error) {
	id := generateSessionID()
	sess := &TerminalSession{
		ID:        id,
		Cluster:   cluster,
		Namespace: namespace,
		Pod:       pod,
		Container: container,
		Conn:      conn,
		sizeQueue: &terminalSizeQueue{ch: make(chan remoteTerminalSize, 10)},
		stdinChan: make(chan []byte, 100),
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
		if sess.Conn != nil {
			sess.Conn.Close()
		}
		delete(m.sessions, id)
	}
	return nil
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

func (sess *TerminalSession) StartK8sExec(cluster, namespace, pod, container, shell string) {
	if sess.Conn != nil {
		_ = sess.Conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[36mStarting exec session...\x1b[0m\r\n"))
	}

	if sess.k8sClient == nil {
		if sess.Conn != nil {
			_ = sess.Conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31mError: K8s client is nil\x1b[0m\r\n"))
		}
		return
	}
	if sess.restConfig == nil {
		if sess.Conn != nil {
			_ = sess.Conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31mError: K8s restConfig is nil\x1b[0m\r\n"))
		}
		return
	}

	if shell == "" {
		shell = "/bin/sh"
	}

	if sess.Conn != nil {
		if os.Getenv("MUTONG_DEBUG") == "true" {
			_ = sess.Conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[36mK8s API Host: "+sess.restConfig.Host+"\x1b[0m\r\n"))
		}
		_ = sess.Conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[36mShell: "+shell+"\x1b[0m\r\n"))
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

	if sess.Conn != nil {
		_ = sess.Conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[36mExec URL: "+req.URL().String()+"\x1b[0m\r\n"))
	}

	executor, err := remotecommand.NewSPDYExecutor(sess.restConfig, "POST", req.URL())
	if err != nil {
		if sess.Conn != nil {
			_ = sess.Conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31mSPDY Executor Error: "+err.Error()+"\x1b[0m\r\n"))
		}
		return
	}

	if sess.Conn != nil {
		_ = sess.Conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[36mSPDY Executor created, starting stream...\x1b[0m\r\n"))
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
			if n > 0 && sess.Conn != nil {
				_ = sess.Conn.WriteMessage(websocket.TextMessage, buf[:n])
			}
		}
	}()

	go func() {
		for msg := range sess.stdinChan {
			_, _ = stdinWriter.Write(msg)
		}
	}()

	err = executor.StreamWithContext(context.Background(), remotecommand.StreamOptions{
		Stdin:             stdinReader,
		Stdout:            stdoutWriter,
		Stderr:            stdoutWriter,
		Tty:               true,
		TerminalSizeQueue: sess.sizeQueue,
	})

	if sess.Conn != nil {
		if err != nil {
			_ = sess.Conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31mStream Error: "+err.Error()+"\x1b[0m\r\n"))
		} else {
			_ = sess.Conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[33mSession ended normally\x1b[0m\r\n"))
		}
	}
}
