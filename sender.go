package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Sender handles all communication with the Valpero API.
// Security: TLS cert verification is always on (default http.Client behaviour).
type Sender struct {
	apiURL string
	token  string
	client *http.Client
}

func NewSender(apiURL, token string) *Sender {
	return &Sender{
		apiURL: apiURL,
		token:  token,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *Sender) post(path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequest("POST", s.apiURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("User-Agent", "valpero-agent/"+version)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API %s: %d %s", path, resp.StatusCode, string(body))
	}
	return nil
}

func (s *Sender) Register() error {
	info, err := GetSystemInfo()
	if err != nil {
		return fmt.Errorf("get system info: %w", err)
	}
	return s.post("/agents/register", map[string]interface{}{
		"hostname": info.Hostname,
		"os":       info.OS,
		"arch":     info.Arch,
		"kernel":   info.Kernel,
	})
}

func (s *Sender) Push(m *Metrics) error {
	return s.post("/agents/push", m)
}
