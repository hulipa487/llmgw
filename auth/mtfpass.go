package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MTFPassClient handles communication with MTFPass authentication service
type MTFPassClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// MTFPassUser represents user info from MTFPass
type MTFPassUser struct {
	UID      int64  `json:"uid"`      // Telegram ID
	Username string `json:"username"` // Telegram username
	Role     string `json:"role"`     // "user" or "admin"
	Credits  int    `json:"credits"`  // -1 for admin = unlimited
}

// MTFPassResponse represents the response from MTFPass API
type MTFPassResponse struct {
	Success bool         `json:"success"`
	Data    *MTFPassUser `json:"data"`
	Error   string       `json:"error"`
}

// LoginResponse represents the response from login endpoint
type LoginResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Token       string `json:"token"`
		BotLink     string `json:"bot_link"`
		BotUsername string `json:"bot_username"`
	} `json:"data"`
	Error string `json:"error"`
}

// ConsumeCreditsRequest represents the request body for consuming credits
type ConsumeCreditsRequest struct {
	Amount int `json:"amount"`
}

// NewMTFPassClient creates a new MTFPass client
func NewMTFPassClient(baseURL string) *MTFPassClient {
	return &MTFPassClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ValidateToken validates a JWT token with MTFPass and returns user info
func (c *MTFPassClient) ValidateToken(jwtToken string) (*MTFPassUser, error) {
	url := fmt.Sprintf("%s/api/v1/auth/check", c.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.AddCookie(&http.Cookie{
		Name:  "mtf_auth",
		Value: jwtToken,
	})

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call MTFPass: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var mtfResp MTFPassResponse
	if err := json.Unmarshal(body, &mtfResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !mtfResp.Success {
		return nil, fmt.Errorf("MTFPass auth failed: %s", mtfResp.Error)
	}

	if mtfResp.Data == nil {
		return nil, fmt.Errorf("no user data in MTFPass response")
	}

	return mtfResp.Data, nil
}

// ConsumeCredits deducts credits from the user's account in MTFPass
func (c *MTFPassClient) ConsumeCredits(jwtToken string, amount int) error {
	url := fmt.Sprintf("%s/api/v1/credits/consume", c.BaseURL)

	reqBody := ConsumeCreditsRequest{Amount: amount}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  "mtf_auth",
		Value: jwtToken,
	})

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call MTFPass: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		var errResp MTFPassResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("MTFPass consume credits failed: %s", string(respBody))
	}

	return nil
}

// Logout calls the MTFPass logout endpoint to invalidate the session
func (c *MTFPassClient) Logout(jwtToken string) error {
	url := fmt.Sprintf("%s/api/v1/auth/logout", c.BaseURL)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.AddCookie(&http.Cookie{
		Name:  "mtf_auth",
		Value: jwtToken,
	})

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call MTFPass: %w", err)
	}
	defer resp.Body.Close()

	// We don't strictly require success here - we'll clear the cookie anyway
	return nil
}

// IsAdmin checks if the user has admin role
func (u *MTFPassUser) IsAdmin() bool {
	return u.Role == "admin"
}

// HasUnlimitedCredits checks if the user has unlimited credits (admin)
func (u *MTFPassUser) HasUnlimitedCredits() bool {
	return u.Credits == -1 || u.IsAdmin()
}
