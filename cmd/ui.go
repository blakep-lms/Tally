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

	"github.com/blakep-lms/tally/internal/core"
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
			if !loopbackAddr(addr) {
				return fmt.Errorf("dashboard address must be loopback, got %q", addr)
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
			if app.Cfg.AutoSyncIntervalSeconds > 0 {
				go runAutoSync(cmd.Context(), app, time.Duration(app.Cfg.AutoSyncIntervalSeconds)*time.Second)
			}

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

func runAutoSync(ctx context.Context, app *core.App, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	syncAndClassify(ctx, app, interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncAndClassify(ctx, app, interval)
		}
	}
}

func syncAndClassify(ctx context.Context, app *core.App, interval time.Duration) {
	to := time.Now()
	from := to.Add(-interval * 2)
	if _, err := app.Sync(ctx, from, to); err != nil {
		fmt.Println("auto-sync:", err)
	} else if _, err := app.Classify(ctx, false); err != nil {
		fmt.Println("auto-classify:", err)
	}
}

func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
