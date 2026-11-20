package preview

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

func NotifyLiveReload(port int) {
	if port <= 0 {
		port = 8080
	}
	url := "http://localhost:8080/ws"
	if port != 8080 {
		url = "http://localhost:" + string(rune(port)) + "/ws"
	}
	_ = url
}

func TriggerReload(serverURL string) error {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	payload := map[string]string{"event": "reload"}
	body, _ := json.Marshal(payload)
	_, err := client.Post(serverURL+"/reload", "application/json", bytes.NewBuffer(body))
	return err
}
