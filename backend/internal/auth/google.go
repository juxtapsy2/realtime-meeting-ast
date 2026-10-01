package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GoogleUser is the normalized identity returned by Google's userinfo
// endpoint. Only fields consumed by this application are mapped; vendor
// payloads never escape this package.
type GoogleUser struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	AvatarURL     string `json:"picture"`
}

// GoogleClient implements the Google authorization-code flow.
//
// Identity is verified against Google's userinfo endpoint using the access
// token obtained from the token exchange, so the login gate never trusts a
// client-supplied email address.
type GoogleClient struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTPClient   *http.Client
}

// NewGoogleClient returns a client with a sane default HTTP timeout.
func NewGoogleClient(clientID, clientSecret, redirectURL string) *GoogleClient {
	return &GoogleClient{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		HTTPClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Configured reports whether the client has the credentials required for a
// real OAuth login.
func (c *GoogleClient) Configured() bool {
	return c != nil && c.ClientID != "" && c.ClientSecret != "" && c.RedirectURL != ""
}

// Start builds the Google account chooser URL. The caller must bind `state`
// (a signed token) across redirects to defeat CSRF on the callback.
func (c *GoogleClient) Start(state string) (string, error) {
	u, err := url.Parse("https://accounts.google.com/o/oauth2/v2/auth")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", c.RedirectURL)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("access_type", "online")
	q.Set("prompt", "select_account")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Exchange trades an authorization code for a verified Google identity.
func (c *GoogleClient) Exchange(ctx context.Context, code string) (*GoogleUser, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"redirect_uri":  {c.RedirectURL},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: token exchange: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oauth: read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth: token exchange failed with status %d: %s", resp.StatusCode, truncateForLog(body))
	}

	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("oauth: decode token response: %w", err)
	}
	if token.AccessToken == "" {
		return nil, errors.New("oauth: token response contained no access token")
	}

	ureq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return nil, fmt.Errorf("oauth: build userinfo request: %w", err)
	}
	ureq.Header.Set("Authorization", "Bearer "+token.AccessToken)

	uresp, err := c.HTTPClient.Do(ureq)
	if err != nil {
		return nil, fmt.Errorf("oauth: userinfo: %w", err)
	}
	defer uresp.Body.Close()

	ubody, err := io.ReadAll(io.LimitReader(uresp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oauth: read userinfo response: %w", err)
	}
	if uresp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth: userinfo failed with status %d: %s", uresp.StatusCode, truncateForLog(ubody))
	}

	var user GoogleUser
	if err := json.Unmarshal(ubody, &user); err != nil {
		return nil, fmt.Errorf("oauth: decode userinfo: %w", err)
	}
	if user.Email == "" {
		return nil, errors.New("oauth: userinfo contained no email")
	}
	if !user.EmailVerified {
		return nil, errors.New("oauth: google email not verified")
	}
	return &user, nil
}

func truncateForLog(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
