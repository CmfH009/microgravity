package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/CmfH009/microgravity/internal/authutil"
	"github.com/CmfH009/microgravity/internal/config"
	"github.com/CmfH009/microgravity/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with Google Account via OAuth",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("To use Cloud Gemini/Gemma models, get a free API key at:")
		fmt.Println("👉 https://aistudio.google.com/app/apikey")
		fmt.Println()

		openBrowser("https://aistudio.google.com/app/apikey")

		fmt.Print("Enter your Gemini API key (or press Enter for OAuth): ")
		var inputKey string
		fmt.Scanln(&inputKey)
		inputKey = strings.TrimSpace(inputKey)

		if inputKey != "" {
			if err := config.SetAutoAPIKey(inputKey); err != nil {
				fmt.Printf("%s\n", ui.PrintError(err))
				os.Exit(1)
			}
			ui.PrintStepSuccess("Successfully saved Gemini API key to config!")
			return
		}

		pkceVerifier := oauth2.GenerateVerifier()
		url := authutil.OAuthConfig.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(pkceVerifier))
		fmt.Println("Opening Google Account authentication in browser...")
		fmt.Println("If the browser fails to open or the URL gets truncated, copy and paste this ENTIRE URL into your browser:")
		fmt.Printf("\n%s\n\n", url)
		openBrowser(url)

		fmt.Println("Waiting for authentication in browser...")

		// Start local server to receive code
		mux := http.NewServeMux()
		srv := &http.Server{Addr: "127.0.0.1:8080", Handler: mux}

		mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/favicon.ico" {
				http.NotFound(w, r)
				return
			}
			code := r.URL.Query().Get("code")
			if code == "" {
				return
			}
			
			token, err := authutil.OAuthConfig.Exchange(context.Background(), code, oauth2.VerifierOption(pkceVerifier))

			if err != nil {
				fmt.Fprintf(w, "Failed to exchange token: %v", err)
				return
			}

			cfg, err := config.LoadConfig()
			if err != nil {
				cfg = &config.Config{}
			}
			cfg.Token = token
			config.SaveConfig(cfg)

			fmt.Println("\nProvisioning Cloud API Access...")
			err = authutil.ProvisionAPIKey(token)
			if err != nil {
				fmt.Printf("\nWarning: Could not automatically provision Cloud API access: %v\n", err)
			}

			fmt.Fprint(w, "Success! You are authenticated. You can close this window and return to the terminal.")
			
			go func() {
				srv.Shutdown(context.Background())
			}()
		})

		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
		
		fmt.Println("Authentication successful! You can now run the CLI.")
	},
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("cmd", "/c", "start", "", strings.ReplaceAll(url, "&", "^&")).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	}
	if err != nil {
		// ignore
	}
}

var authKeyCmd = &cobra.Command{
	Use:   "key <api-key>",
	Short: "Set your Google Gemini API key manually",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		if err := config.SetAutoAPIKey(key); err != nil {
			fmt.Printf("%s\n", ui.PrintError(err))
			os.Exit(1)
		}
		ui.PrintStepSuccess("Successfully saved Gemini API key to config!")
	},
}

var (
	vertexProjectFlag  string
	vertexLocationFlag string
	vertexKeyFlag      string
	vertexModeFlag     string
)

var authVertexCmd = &cobra.Command{
	Use:   "vertex",
	Short: "Configure Google Cloud Vertex AI authentication (Express API Key or Standard ADC/OAuth)",
	Long: `Configure Vertex AI authentication for Microgravity.

Modes supported:
  1. Express Mode (API Key):
     mg auth vertex --mode express --key <vertex-api-key> [--location us-central1]

  2. Standard Mode (GCP Project ID + ADC/OAuth):
     mg auth vertex --mode standard --project <project-id> [--location us-central1]

If flags are omitted, an interactive setup wizard will guide you through project selection
and discovery from your existing Google Cloud credentials.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		// 1. Direct Flag handling
		if vertexKeyFlag != "" || vertexModeFlag == "express" {
			loc := vertexLocationFlag
			if loc == "" {
				loc = "us-central1"
			}
			key := vertexKeyFlag
			if key == "" {
				fmt.Print("Enter your Vertex AI Express API Key: ")
				fmt.Scanln(&key)
				key = strings.TrimSpace(key)
				if key == "" {
					fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("API key cannot be empty in express mode")))
					os.Exit(1)
				}
			}
			if err := config.SetVertexConfig("vertex-express", vertexProjectFlag, loc, key); err != nil {
				fmt.Printf("%s\n", ui.PrintError(err))
				os.Exit(1)
			}
			ui.PrintStepSuccess(fmt.Sprintf("Successfully configured Vertex AI Express Mode (Region: %s)!", loc))
			return
		}

		if vertexProjectFlag != "" || vertexModeFlag == "standard" {
			loc := vertexLocationFlag
			if loc == "" {
				loc = "us-central1"
			}
			proj := vertexProjectFlag
			if proj == "" {
				fmt.Print("Enter your Google Cloud Project ID: ")
				fmt.Scanln(&proj)
				proj = strings.TrimSpace(proj)
				if proj == "" {
					fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("project ID cannot be empty in standard mode")))
					os.Exit(1)
				}
			}
			if err := config.SetVertexConfig("vertex-standard", proj, loc, ""); err != nil {
				fmt.Printf("%s\n", ui.PrintError(err))
				os.Exit(1)
			}
			ui.PrintStepSuccess(fmt.Sprintf("Successfully configured Vertex AI Standard Mode (Project: %s, Region: %s)!", proj, loc))
			return
		}

		// 2. Interactive Discovery Wizard
		fmt.Println("=== Google Cloud Vertex AI Authentication Setup ===")
		fmt.Println()
		fmt.Println("Select Vertex AI authentication mode:")
		fmt.Println("  [1] Express Mode (Vertex AI API Key - fastest, no GCP project configuration needed)")
		fmt.Println("  [2] Standard Mode (Google Cloud Project ID + Application Default Credentials / OAuth)")
		fmt.Print("\nChoice [1/2] (default: 1): ")

		var choice string
		fmt.Scanln(&choice)
		choice = strings.TrimSpace(choice)

		loc := vertexLocationFlag
		if loc == "" {
			loc = "us-central1"
		}

		if choice == "2" || choice == "standard" {
			// Check for available GCP projects via ADC or Token
			fmt.Println("\nDetecting accessible Google Cloud Projects...")
			var detectedProjects []string
			if token, proj, err := authutil.GetADCCredentials(ctx); err == nil && token != nil {
				if proj != "" {
					detectedProjects = append(detectedProjects, proj)
				}
				if projs, err := authutil.ListUserProjects(ctx, token); err == nil {
					detectedProjects = append(detectedProjects, projs...)
				}
			}
			if token := config.GetToken(); token != nil && token.AccessToken != "" {
				if projs, err := authutil.ListUserProjects(ctx, token); err == nil {
					detectedProjects = append(detectedProjects, projs...)
				}
			}

			// Deduplicate
			seen := make(map[string]bool)
			var uniqueProjects []string
			for _, p := range detectedProjects {
				if !seen[p] && p != "" {
					seen[p] = true
					uniqueProjects = append(uniqueProjects, p)
				}
			}

			var selectedProject string
			if len(uniqueProjects) > 0 {
				fmt.Println("\nFound accessible Google Cloud Projects:")
				for i, p := range uniqueProjects {
					fmt.Printf("  [%d] %s\n", i+1, p)
				}
				fmt.Printf("\nSelect a project [1-%d] or enter project ID manually: ", len(uniqueProjects))
				var projInput string
				fmt.Scanln(&projInput)
				projInput = strings.TrimSpace(projInput)

				var idx int
				if n, err := fmt.Sscanf(projInput, "%d", &idx); err == nil && n == 1 && idx >= 1 && idx <= len(uniqueProjects) {
					selectedProject = uniqueProjects[idx-1]
				} else if projInput != "" {
					selectedProject = projInput
				} else {
					selectedProject = uniqueProjects[0]
				}
			} else {
				fmt.Print("Enter your Google Cloud Project ID: ")
				fmt.Scanln(&selectedProject)
				selectedProject = strings.TrimSpace(selectedProject)
			}

			if selectedProject == "" {
				fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("project ID cannot be empty")))
				os.Exit(1)
			}

			fmt.Printf("Enter Vertex AI Region/Location [%s]: ", loc)
			var locInput string
			fmt.Scanln(&locInput)
			locInput = strings.TrimSpace(locInput)
			if locInput != "" {
				loc = locInput
			}

			if err := config.SetVertexConfig("vertex-standard", selectedProject, loc, ""); err != nil {
				fmt.Printf("%s\n", ui.PrintError(err))
				os.Exit(1)
			}
			ui.PrintStepSuccess(fmt.Sprintf("Configured Vertex AI Standard Mode! (Project: %s, Location: %s)", selectedProject, loc))
		} else {
			// Express Mode
			fmt.Println("\n👉 Express Mode lets you use Vertex AI endpoints using an API key directly.")
			fmt.Print("Enter your Vertex AI API Key: ")
			var key string
			fmt.Scanln(&key)
			key = strings.TrimSpace(key)
			if key == "" {
				// Check if user has an existing auto API key that can be used
				if existing := config.GetAutoAPIKey(); existing != "" {
					key = existing
					fmt.Println("Using existing saved Google API key from config.")
				} else {
					fmt.Printf("%s\n", ui.PrintError(fmt.Errorf("API key cannot be empty")))
					os.Exit(1)
				}
			}

			fmt.Printf("Enter Vertex AI Region/Location [%s]: ", loc)
			var locInput string
			fmt.Scanln(&locInput)
			locInput = strings.TrimSpace(locInput)
			if locInput != "" {
				loc = locInput
			}

			if err := config.SetVertexConfig("vertex-express", "", loc, key); err != nil {
				fmt.Printf("%s\n", ui.PrintError(err))
				os.Exit(1)
			}
			ui.PrintStepSuccess(fmt.Sprintf("Configured Vertex AI Express Mode! (Location: %s)", loc))
		}
	},
}

func init() {
	authVertexCmd.Flags().StringVarP(&vertexProjectFlag, "project", "p", "", "Google Cloud Project ID")
	authVertexCmd.Flags().StringVarP(&vertexLocationFlag, "location", "l", "us-central1", "Vertex AI Location / Region (e.g. us-central1)")
	authVertexCmd.Flags().StringVarP(&vertexKeyFlag, "key", "k", "", "Vertex AI Express Mode API Key")
	authVertexCmd.Flags().StringVarP(&vertexModeFlag, "mode", "m", "", "Vertex AI mode: 'express' or 'standard'")

	authCmd.AddCommand(authKeyCmd)
	authCmd.AddCommand(authVertexCmd)
	rootCmd.AddCommand(authCmd)
}
