package terminal

import (
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdTerminal(f *Factory) *cobra.Command {
	var namespace, pod, container string
	cmd := &cobra.Command{
		Use:   "terminal",
		Short: "Start a WebSocket terminal to a K8s pod (human-only)",
		Long: `Opens an interactive WebSocket terminal session to a Kubernetes pod container.

This command is designed for interactive human use and forwards stdin/stdout
through a WebSocket connection.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !f.IO.IsStdoutTTY() {
				return fmt.Errorf("terminal requires a TTY")
			}
			return terminalRun(f, namespace, pod, container)
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "K8s namespace")
	cmd.Flags().StringVarP(&pod, "pod", "p", "", "Pod name")
	cmd.Flags().StringVarP(&container, "container", "c", "", "Container name (optional)")
	_ = cmd.MarkFlagRequired("pod")
	return cmd
}

func terminalRun(f *Factory, namespace, pod, container string) error {
	path := fmt.Sprintf("/api/v1/terminal/ws?namespace=%s&pod=%s", namespace, pod)
	if container != "" {
		path += "&container=" + container
	}

	ws := client.NewWSClient(f.Client.BaseURL(), "")
	conn, err := ws.Dial(path)
	if err != nil {
		return err
	}
	defer conn.Close()

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("setting terminal raw mode: %w", err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()

	done := make(chan struct{})
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	go func() {
		defer close(done)
		_, _ = io.Copy(conn.UnderlyingConn(), os.Stdin)
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}()

	go func() {
		_, _ = io.Copy(os.Stdout, conn.UnderlyingConn())
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-interrupt:
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		<-done
	}

	return nil
}
