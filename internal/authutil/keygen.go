package authutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/CmfH009/microgravity/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("MG_GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("MG_GOOGLE_CLIENT_SECRET"),
		Scopes:       []string{"https://www.googleapis.com/auth/cloud-platform"},
		Endpoint:     google.Endpoint,
		RedirectURL:  "http://127.0.0.1:8080/callback",
	}
}

var OAuthConfig = oauthConfig()

// EnsureValidTokenAndAPIKey checks if OAuth token is expired and automatically refreshes it.
func EnsureValidTokenAndAPIKey() (string, error) {
	cfg, err := config.LoadConfig()
	if err != nil || cfg.Token == nil {
		return "", fmt.Errorf("no OAuth token available")
	}

	if OAuthConfig.ClientID == "" {
		return "", fmt.Errorf("MG_GOOGLE_CLIENT_ID is not configured")
	}
	tokenSource := OAuthConfig.TokenSource(context.Background(), cfg.Token)
	newToken, err := tokenSource.Token()
	if err != nil {
		if cfg.AutoAPIKey != "" {
			return cfg.AutoAPIKey, nil
		}
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}

	// Save refreshed token and re-provision API key
	cfg.Token = newToken
	_ = SaveConfigToken(cfg, newToken)
	_ = ProvisionAPIKey(newToken)

	cfg, _ = config.LoadConfig()
	return cfg.AutoAPIKey, nil
}

func SaveConfigToken(cfg *config.Config, token *oauth2.Token) error {
	cfg.Token = token
	return config.SaveConfig(cfg)
}

type ProjectsResponse struct {
	Projects []struct {
		ProjectId string `json:"projectId"`
	} `json:"projects"`
}

type KeysResponse struct {
	Keys []struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"keys"`
}

type KeyStringResponse struct {
	KeyString string `json:"keyString"`
}

// ProvisionAPIKey uses the user's OAuth token to find a GCP project, enable AI Studio, and generate/fetch an API key.
func ProvisionAPIKey(token *oauth2.Token) error {
	client := &http.Client{Timeout: 10 * time.Second}

	// 1. List Projects
	req, _ := http.NewRequest("GET", "https://cloudresourcemanager.googleapis.com/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var projResp ProjectsResponse
	if err := json.NewDecoder(resp.Body).Decode(&projResp); err != nil {
		return err
	}
	if len(projResp.Projects) == 0 {
		return fmt.Errorf("no Google Cloud projects found for this account. Please create one.")
	}
	projectID := projResp.Projects[0].ProjectId

	// 2. Enable Generative Language API
	enableURL := fmt.Sprintf("https://serviceusage.googleapis.com/v1/projects/%s/services/generativelanguage.googleapis.com:enable", projectID)
	req, _ = http.NewRequest("POST", enableURL, nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, _ = client.Do(req)
	resp.Body.Close() // Don't care about the response, just try to enable

	// 3. List existing API keys
	keysURL := fmt.Sprintf("https://apikeys.googleapis.com/v2/projects/%s/locations/global/keys", projectID)
	req, _ = http.NewRequest("GET", keysURL, nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	resp, err = client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var keysResp KeysResponse
	json.NewDecoder(resp.Body).Decode(&keysResp)

	var keyName string

	keyDisplayName := fmt.Sprintf("Microgravity Auto Key %d", time.Now().Unix())
	body := []byte(fmt.Sprintf(`{"displayName": "%s"}`, keyDisplayName))
	req, _ = http.NewRequest("POST", keysURL, bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err == nil {
		resp.Body.Close()
	}

	time.Sleep(2 * time.Second)
	req, _ = http.NewRequest("GET", keysURL, nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	resp, err = client.Do(req)
	if err == nil {
		json.NewDecoder(resp.Body).Decode(&keysResp)
		resp.Body.Close()
		for _, k := range keysResp.Keys {
			if k.DisplayName == keyDisplayName {
				keyName = k.Name
				break
			}
		}
	}

	if keyName == "" && len(keysResp.Keys) > 0 {
		keyName = keysResp.Keys[len(keysResp.Keys)-1].Name // fallback to latest key
	}

	if keyName == "" {
		return fmt.Errorf("failed to create or find an API key")
	}

	// 5. Get key string
	keyStrURL := fmt.Sprintf("https://apikeys.googleapis.com/v2/%s/keyString", keyName)
	req, _ = http.NewRequest("GET", keyStrURL, nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	resp, err = client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var keyStrResp KeyStringResponse
	json.NewDecoder(resp.Body).Decode(&keyStrResp)

	// Save to config
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = &config.Config{}
	}
	cfg.AutoAPIKey = keyStrResp.KeyString
	config.SaveConfig(cfg)

	return nil
}
