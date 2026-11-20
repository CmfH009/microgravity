package authutil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/purav/microgravity/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// VertexCredentials represents credentials for Vertex AI standard mode.
type VertexCredentials struct {
	ProjectID string
	Location  string
	Token     *oauth2.Token
	APIKey    string
	Mode      string // "express" or "standard"
}

// GetADCCredentials attempts to load Application Default Credentials from standard Google Cloud locations.
func GetADCCredentials(ctx context.Context) (*oauth2.Token, string, error) {
	// 1. Check GOOGLE_APPLICATION_CREDENTIALS environment variable
	if customPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); customPath != "" {
		if data, err := os.ReadFile(customPath); err == nil {
			token, projectID, err := parseServiceAccountOrUserCreds(ctx, data)
			if err == nil {
				return token, projectID, nil
			}
		}
	}

	// 2. Check standard gcloud ADC path
	home, err := os.UserHomeDir()
	if err == nil {
		var adcPaths []string
		if appData := os.Getenv("APPDATA"); appData != "" {
			adcPaths = append(adcPaths, filepath.Join(appData, "gcloud", "application_default_credentials.json"))
		}
		adcPaths = append(adcPaths, filepath.Join(home, ".config", "gcloud", "application_default_credentials.json"))

		for _, path := range adcPaths {
			if data, err := os.ReadFile(path); err == nil {
				token, projectID, err := parseServiceAccountOrUserCreds(ctx, data)
				if err == nil {
					return token, projectID, nil
				}
			}
		}
	}

	// 3. Fallback to golang google.FindDefaultCredentials
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err == nil && creds != nil {
		tok, err := creds.TokenSource.Token()
		if err == nil {
			return tok, creds.ProjectID, nil
		}
	}

	return nil, "", fmt.Errorf("no Application Default Credentials found")
}

func parseServiceAccountOrUserCreds(ctx context.Context, data []byte) (*oauth2.Token, string, error) {
	var fileMap map[string]interface{}
	if err := json.Unmarshal(data, &fileMap); err != nil {
		return nil, "", err
	}

	projectID, _ := fileMap["project_id"].(string)
	if projectID == "" {
		projectID, _ = fileMap["quota_project_id"].(string)
	}

	creds, err := google.CredentialsFromJSON(ctx, data, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, "", err
	}

	tok, err := creds.TokenSource.Token()
	if err != nil {
		return nil, "", err
	}

	if creds.ProjectID != "" {
		projectID = creds.ProjectID
	}

	return tok, projectID, nil
}

// ListUserProjects fetches accessible Google Cloud projects using the provided OAuth token.
func ListUserProjects(ctx context.Context, token *oauth2.Token) ([]string, error) {
	if token == nil || token.AccessToken == "" {
		return nil, fmt.Errorf("no access token provided")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://cloudresourcemanager.googleapis.com/v1/projects", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list projects failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resource manager returned HTTP %d", resp.StatusCode)
	}

	var projResp ProjectsResponse
	if err := json.NewDecoder(resp.Body).Decode(&projResp); err != nil {
		return nil, err
	}

	var projectIDs []string
	for _, p := range projResp.Projects {
		if p.ProjectId != "" {
			projectIDs = append(projectIDs, p.ProjectId)
		}
	}
	return projectIDs, nil
}

// ResolveVertexCredentials figures out the current active credentials for Vertex AI.
func ResolveVertexCredentials(ctx context.Context) (*VertexCredentials, error) {
	authType, projectID, location, apiKey := config.GetVertexConfig()

	// Environment variable overrides
	if envKey := os.Getenv("VERTEX_API_KEY"); envKey != "" {
		apiKey = envKey
		authType = "vertex-express"
	}
	if envProject := os.Getenv("VERTEX_PROJECT_ID"); envProject != "" {
		projectID = envProject
	}
	if envProject := os.Getenv("GOOGLE_CLOUD_PROJECT"); envProject != "" && projectID == "" {
		projectID = envProject
	}
	if envLoc := os.Getenv("VERTEX_LOCATION"); envLoc != "" {
		location = envLoc
	}
	if location == "" {
		location = "us-central1"
	}

	// 1. Express Mode (API Key direct to Vertex)
	if authType == "vertex-express" || (apiKey != "" && authType != "vertex-standard") {
		return &VertexCredentials{
			Location: location,
			APIKey:   apiKey,
			Mode:     "express",
		}, nil
	}

	// 2. Standard Mode: check ADC or stored OAuth token
	token, adcProject, err := GetADCCredentials(ctx)
	if err == nil && token != nil && token.Valid() {
		if projectID == "" {
			projectID = adcProject
		}
		return &VertexCredentials{
			ProjectID: projectID,
			Location:  location,
			Token:     token,
			Mode:      "standard",
		}, nil
	}

	// Try stored config token
	cfgToken := config.GetToken()
	if cfgToken != nil && cfgToken.AccessToken != "" {
		if projectID == "" {
			if projs, err := ListUserProjects(ctx, cfgToken); err == nil && len(projs) > 0 {
				projectID = projs[0]
			}
		}
		return &VertexCredentials{
			ProjectID: projectID,
			Location:  location,
			Token:     cfgToken,
			Mode:      "standard",
		}, nil
	}

	// If project ID is set without token, but apiKey exists, fallback to express
	if apiKey != "" {
		return &VertexCredentials{
			Location: location,
			APIKey:   apiKey,
			Mode:     "express",
		}, nil
	}

	return nil, fmt.Errorf("no Vertex AI credentials found. Run 'mg auth vertex' or set VERTEX_API_KEY / GOOGLE_APPLICATION_CREDENTIALS")
}

// BuildVertexEndpoint generates the Vertex AI streamGenerateContent endpoint URL.
func BuildVertexEndpoint(mode, location, projectID, model string) string {
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")

	if mode == "express" {
		// Express Mode: Direct Vertex endpoint using API key
		return fmt.Sprintf("https://aiplatform.googleapis.com/v1beta/publishers/google/models/%s:streamGenerateContent?alt=sse", model)
	}

	// Standard Mode: Regional Vertex endpoint using Google Cloud Project ID
	if location == "" {
		location = "us-central1"
	}
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:streamGenerateContent?alt=sse", location, projectID, location, model)
}
