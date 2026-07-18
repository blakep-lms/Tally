package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/blakep-lms/tally/internal/server"
	"github.com/spf13/cobra"
)

func uiCmd() *cobra.Command {
	var addr string
	var noOpen bool
	c := &cobra.Command{
		Use:   "ui",
		Short: "Serve the local dashboard and open it in your browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()

			if addr == "" {
				addr = app.Cfg.UIAddr
			}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("bind %s: %w", addr, err)
			}
			url := "http://" + ln.Addr().String()

			srv := &http.Server{Handler: server.New(app).Handler()}
			go func() {
				if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
					fmt.Println("server error:", err)
				}
			}()

			fmt.Printf("Tally dashboard: %s  (Ctrl+C to stop)\n", url)
			if !noOpen {
				openBrowser(url)
			}
			<-cmd.Context().Done()
			shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			return srv.Shutdown(shutCtx)
		},
	}
	c.Flags().StringVar(&addr, "addr", "", "address to bind (default from config)")
	c.Flags().BoolVar(&noOpen, "no-open", false, "do not open a browser")
	return c
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	_ = exec.Command(cmd, append(args, url)...).Start()
}
