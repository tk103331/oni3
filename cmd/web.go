package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/cloudwego/eino-ext/callbacks/langfuse"
	"github.com/cloudwego/eino/callbacks"
	"github.com/spf13/cobra"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/logger"
	"github.com/tk103331/oni3/mcp"
	"github.com/tk103331/oni3/version"
	"github.com/tk103331/oni3/web"
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Serve agents in a local web interface",
	Long: `Serve the configured agents in a browser over HTTP.

The interface streams replies with Server-Sent Events, shows tool calls and
reasoning, and asks for approval before dangerous tools run. Conversations are
saved in the same session files as the TUI, so they can be resumed from either
interface.

The server listens on 127.0.0.1 by default. Binding it to a non-loopback
address is safer with --token, which requires an access token before the
interface or API can be used.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.GetConfig()
		if cfg == nil {
			return fmt.Errorf("global configuration not initialized")
		}

		if cfg.Settings.Langfuse != nil {
			handler, flusher := langfuse.NewLangfuseHandler(cfg.Settings.Langfuse.SDKConfig())
			defer flusher()
			callbacks.AppendGlobalHandlers(handler)
		}

		agentName, _ := cmd.Flags().GetString("agent")
		if strings.TrimSpace(agentName) == "" {
			name, err := resolveDefaultAgent(cfg)
			if err != nil {
				return err
			}
			agentName = name
		}
		if _, ok := cfg.Agents[agentName]; !ok {
			return fmt.Errorf("Agent configuration does not exist: %s", agentName)
		}

		addr, _ := cmd.Flags().GetString("addr")
		addr = strings.TrimSpace(addr)
		if addr == "" {
			addr = "127.0.0.1:8080"
		}

		token, _ := cmd.Flags().GetString("token")
		token = strings.TrimSpace(token)
		if token == "" {
			token = strings.TrimSpace(os.Getenv("ONI3_WEB_TOKEN"))
		}

		workDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to read working directory: %w", err)
		}

		// Agent runs may use tools from external MCP servers. Initialize the
		// manager synchronously so the first browser turn already sees them.
		baseCtx := cmd.Context()
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		if err := mcp.InitializeGlobalManager(baseCtx, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize MCP manager: %v\n", err)
		} else {
			defer mcp.CloseGlobalManager()
		}

		app, err := web.New(cfg, web.Options{
			DefaultAgent: agentName,
			WorkDir:      workDir,
			Version:      version.Version,
			Token:        token,
		})
		if err != nil {
			return err
		}

		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", addr, err)
		}
		defer listener.Close()

		baseURL := webURL(listener.Addr().String())
		browserURL := baseURL
		if token != "" {
			browserURL += "?token=" + url.QueryEscape(token)
		}
		fmt.Printf("Oni3 web is running at %s\n", baseURL)
		fmt.Printf("Default agent: %s\n", agentName)
		if token != "" {
			fmt.Println("Token authentication: enabled")
		}
		fmt.Println("Press Ctrl+C to stop")
		if !isLoopbackAddr(listener.Addr().String()) {
			if token == "" {
				fmt.Fprintln(os.Stderr, "Warning: the web interface is reachable from the network and has no authentication; anyone who can reach it may run configured tools. Use --token to require a token.")
			} else {
				fmt.Fprintln(os.Stderr, "Note: token authentication is enabled; keep the token secret.")
			}
		}

		if open, _ := cmd.Flags().GetBool("open"); open {
			if err := openBrowser(browserURL); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to open the browser: %v\n", err)
			}
		}

		ctx, stop := signal.NotifyContext(baseCtx, os.Interrupt, syscall.SIGTERM)
		defer stop()

		httpServer := &http.Server{
			Handler:           app.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdownCtx)
		}()

		logger.Info("WEB", fmt.Sprintf("Serving web interface on %s", listener.Addr().String()))
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("web server stopped: %w", err)
		}
		return nil
	},
}

// webURL renders a browser URL for a listener address, replacing wildcard
// hosts with loopback so the printed link is always usable.
func webURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func init() {
	RootCmd.AddCommand(webCmd)
	webCmd.Flags().String("addr", "127.0.0.1:8080", "HTTP listen address")
	webCmd.Flags().StringP("agent", "a", "", "Default agent (default: last used or the built-in OniThree agent)")
	webCmd.Flags().String("token", "", "Require this access token (default: $ONI3_WEB_TOKEN)")
	webCmd.Flags().Bool("open", false, "Open the interface in the default browser")
}
