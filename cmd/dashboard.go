package cmd

import (
	"fmt"
	"net/http"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/CmfH009/microgravity/internal/agentui"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
)

var (
	webMode  bool
	webPort  int
	liveMode bool
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Launch Web & Terminal Telemetry Control Center dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDashboard(webMode, webPort, liveMode)
	},
}

func renderDashboard() {
	_ = runDashboard(false, 8085, false)
}

func runDashboard(web bool, port int, live bool) error {
	telemetry := agentui.GetDashboardTelemetry("")
	dashboardView := agentui.RenderFullDashboard(telemetry)

	fmt.Println(dashboardView)

	var server *agentui.TelemetryServer
	if web {
		server = agentui.NewTelemetryServer(port)
		go func() {
			if err := server.Start(); err != nil && err != http.ErrServerClosed {
				// Server closed or port error
			}
		}()
		fmt.Println(lipgloss.NewStyle().Foreground(ui.ColorMint).Bold(true).Render(
			fmt.Sprintf("\n🌐 Web Telemetry Control Center active at: http://localhost:%d", server.Port()),
		))
	}

	if live {
		fmt.Println(lipgloss.NewStyle().Foreground(ui.ColorCyan).Italic(true).Render("\n Entering live stream telemetry mode (Press Ctrl+C to exit)..."))
		for i := 0; i < 3; i++ {
			time.Sleep(500 * time.Millisecond)
			t := agentui.GetDashboardTelemetry("")
			fmt.Print("\033[H\033[2J")
			fmt.Println(agentui.RenderFullDashboard(t))
		}
	} else {
		fmt.Println(lipgloss.NewStyle().Foreground(ui.ColorMuted).Italic(true).Render("\n Press Enter or Ctrl+C to return to CLI command prompt..."))
	}

	return nil
}

func init() {
	dashboardCmd.Flags().BoolVarP(&webMode, "web", "w", false, "Launch Web Telemetry Control Center HTTP/WebSocket server")
	dashboardCmd.Flags().IntVarP(&webPort, "port", "p", 8085, "Port for Web Telemetry Control Center server")
	dashboardCmd.Flags().BoolVarP(&liveMode, "live", "l", false, "Continuous live stream monitoring mode in terminal")
	rootCmd.AddCommand(dashboardCmd)
}
