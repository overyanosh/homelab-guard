// Package oidc implements an optional OpenID Connect login gate. It is enabled
// only when OIDC_ISSUER and OIDC_CLIENT_ID are provided (e.g. via the compose
// environment), and stays completely inert otherwise.
package oidc

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	stateCookie   = "hg_oidc_state"
	sessionCookie = "hg_session"
)

// Config holds the OIDC settings read from the environment.
type Config struct {
	Issuer         string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	Scopes         string
	AllowedEmails  []string
	AllowedDomains []string
	SessionSecret  []byte
	SessionTTL     time.Duration
}

// Session is the authenticated user stored in the signed cookie.
type Session struct {
	Sub   string `json:"sub,omitempty"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
	Exp   int64  `json:"exp"`
}

// Endpoints are the discovery document fields we need.
type Endpoints struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
}

// Claims returned by the userinfo endpoint.
type Claims struct {
	Sub               string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
}

// Provider is the OIDC client. A nil *Provider means OIDC is disabled.
type Provider struct {
	cfg    Config
	client *http.Client

	mu        sync.Mutex
	endpoints *Endpoints
	fetchedAt time.Time
}

// NewFromEnv returns a Provider when the environment is configured, nil otherwise.
func NewFromEnv(masterKey string) *Provider {
	issuer := strings.TrimSpace(os.Getenv("OIDC_ISSUER"))
	clientID := strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID"))
	if issuer == "" || clientID == "" {
		return nil
	}

	secret := strings.TrimSpace(os.Getenv("OIDC_SESSION_SECRET"))
	if secret == "" {
		secret = masterKey
	}

	scopes := strings.TrimSpace(os.Getenv("OIDC_SCOPES"))
	if scopes == "" {
		scopes = "openid profile email"
	}

	return &Provider{
		cfg: Config{
			Issuer:         strings.TrimRight(issuer, "/"),
			ClientID:       clientID,
			ClientSecret:   os.Getenv("OIDC_CLIENT_SECRET"),
			RedirectURL:    strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL")),
			Scopes:         scopes,
			AllowedEmails:  splitList(os.Getenv("OIDC_ALLOWED_EMAILS")),
			AllowedDomains: splitList(os.Getenv("OIDC_ALLOWED_DOMAINS")),
			SessionSecret:  []byte(secret),
			SessionTTL:     12 * time.Hour,
		},
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if p := strings.ToLower(strings.TrimSpace(part)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Enabled reports whether OIDC is configured.
func (p *Provider) Enabled() bool { return p != nil && p.cfg.Issuer != "" && p.cfg.ClientID != "" }

// Endpoints discovers and caches the provider metadata.
func (p *Provider) Endpoints(ctx context.Context) (*Endpoints, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.endpoints != nil && time.Since(p.fetchedAt) < time.Hour {
		return p.endpoints, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.Issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc discovery returned HTTP %d", resp.StatusCode)
	}
	var ep Endpoints
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ep); err != nil {
		return nil, fmt.Errorf("oidc discovery decode failed: %w", err)
	}
	if ep.AuthorizationEndpoint == "" || ep.TokenEndpoint == "" {
		return nil, errors.New("oidc discovery document is incomplete")
	}
	p.endpoints = &ep
	p.fetchedAt = time.Now()
	return &ep, nil
}

// AuthCodeURL builds the authorization request URL.
func (p *Provider) AuthCodeURL(ep *Endpoints, state, nonce, verifier, redirectURL string) string {
	challenge := pkceChallenge(verifier)
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.cfg.ClientID},
		"redirect_uri":          {redirectURL},
		"scope":                 {p.cfg.Scopes},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return ep.AuthorizationEndpoint + "?" + q.Encode()
}

// Exchange trades an authorization code for an access token (with PKCE).
func (p *Provider) Exchange(ctx context.Context, ep *Endpoints, code, verifier, redirectURL string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURL},
		"client_id":     {p.cfg.ClientID},
		"code_verifier": {verifier},
	}
	if p.cfg.ClientSecret != "" {
		form.Set("client_secret", p.cfg.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", errors.New("token endpoint returned no access_token")
	}
	return tok.AccessToken, nil
}

// Userinfo fetches the authenticated user's claims.
func (p *Provider) Userinfo(ctx context.Context, ep *Endpoints, accessToken string) (*Claims, error) {
	if ep.UserinfoEndpoint == "" {
		return nil, errors.New("provider has no userinfo endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.UserinfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo returned HTTP %d", resp.StatusCode)
	}
	var c Claims
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, fmt.Errorf("userinfo decode failed: %w", err)
	}
	if c.Email == "" {
		c.Email = c.PreferredUsername
	}
	return &c, nil
}

// Allowed applies the optional email/domain allowlist.
func (p *Provider) Allowed(c *Claims) bool {
	if len(p.cfg.AllowedEmails) == 0 && len(p.cfg.AllowedDomains) == 0 {
		return true
	}
	email := strings.ToLower(c.Email)
	for _, e := range p.cfg.AllowedEmails {
		if email == e {
			return true
		}
	}
	if i := strings.LastIndexByte(email, '@'); i >= 0 {
		domain := email[i+1:]
		for _, d := range p.cfg.AllowedDomains {
			if domain == d {
				return true
			}
		}
	}
	return false
}

// Session cookie helpers ----------------------------------------------------

func (p *Provider) sign(value string) string {
	mac := hmac.New(sha256.New, p.cfg.SessionSecret)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// IssueSession writes the signed session cookie.
func (p *Provider) IssueSession(w http.ResponseWriter, r *http.Request, c *Claims) {
	s := Session{
		Sub:   c.Sub,
		Email: c.Email,
		Name:  c.Name,
		Exp:   time.Now().Add(p.cfg.SessionTTL).Unix(),
	}
	raw, _ := json.Marshal(s)
	payload := base64.RawURLEncoding.EncodeToString(raw)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    payload + "." + p.sign(payload),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS(r),
		MaxAge:   int(p.cfg.SessionTTL.Seconds()),
	})
}

// SessionFromRequest returns the authenticated session, if the cookie is valid.
func (p *Provider) SessionFromRequest(r *http.Request) (*Session, bool) {
	ck, err := r.Cookie(sessionCookie)
	if err != nil || ck.Value == "" {
		return nil, false
	}
	parts := strings.SplitN(ck.Value, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}
	if !hmac.Equal([]byte(p.sign(parts[0])), []byte(parts[1])) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	if s.Exp < time.Now().Unix() {
		return nil, false
	}
	return &s, true
}

// ClearSession removes the session cookie.
func (p *Provider) ClearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS(r),
		MaxAge:   -1,
	})
}

// State cookie helpers ------------------------------------------------------

type State struct {
	State       string `json:"state"`
	Nonce       string `json:"nonce"`
	Verifier    string `json:"verifier"`
	RedirectURL string `json:"redirect_url"`
}

// SetState stores the transient flow state in a short-lived signed cookie.
func (p *Provider) SetState(w http.ResponseWriter, r *http.Request, s State) {
	raw, _ := json.Marshal(s)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    payload + "." + p.sign(payload),
		Path:     "/auth/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS(r),
		MaxAge:   600,
	})
}

// TakeState reads and clears the transient flow state.
func (p *Provider) TakeState(w http.ResponseWriter, r *http.Request) (State, bool) {
	var s State
	ck, err := r.Cookie(stateCookie)
	if err != nil || ck.Value == "" {
		return s, false
	}
	// Always clear it, one-shot.
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: "", Path: "/auth/", HttpOnly: true, MaxAge: -1,
		SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r),
	})
	parts := strings.SplitN(ck.Value, ".", 2)
	if len(parts) != 2 || !hmac.Equal([]byte(p.sign(parts[0])), []byte(parts[1])) {
		return s, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(raw, &s) != nil {
		return s, false
	}
	return s, s.State != ""
}

// RedirectURL returns the configured callback or derives it from the request.
func (p *Provider) RedirectURL(r *http.Request) string {
	if p.cfg.RedirectURL != "" {
		return p.cfg.RedirectURL
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/auth/callback"
}

// Helpers -------------------------------------------------------------------

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// RandomToken returns a URL-safe random string of n bytes of entropy.
func RandomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// Exported cookie names for the middleware/frontend contract.
const (
	SessionCookieName = sessionCookie
)
