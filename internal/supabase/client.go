// Package supabase provides Supabase auth integration and cross-device sync.
// It uses raw net/http to talk to the Supabase Auth API and PostgREST;
// the frontend owns the session (via @supabase/supabase-js) and sends its
// JWT in Authorization: Bearer headers which the Go backend validates here.
package supabase

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Config holds the Supabase project credentials, loaded from env vars.
type Config struct {
	URL        string // https://xxx.supabase.co
	AnonKey    string // publishable / anon key (SUPABASE_PUBLISHABLE_KEY)
	ServiceKey string // service role secret key (SUPABASE_SERVICE_KEY)
	JWTSecret  string // from Supabase → Settings → API → JWT Secret
}

// ConfigFromEnv reads Supabase credentials from environment variables.
// Returns nil if the project URL is not set (i.e. Supabase not configured).
func ConfigFromEnv() *Config {
	url := os.Getenv("SUPABASE_URL")
	if url == "" {
		return nil
	}
	return &Config{
		URL:        strings.TrimRight(url, "/"),
		AnonKey:    os.Getenv("SUPABASE_PUBLISHABLE_KEY"),
		ServiceKey: os.Getenv("SUPABASE_SERVICE_KEY"),
		JWTSecret:  os.Getenv("SUPABASE_JWT_SECRET"),
	}
}

// jwksCache caches the ECDSA public keys fetched from Supabase's JWKS endpoint.
var (
	jwksMu      sync.RWMutex
	jwksKeys    map[string]*ecdsa.PublicKey
	jwksFetched time.Time
)

// fetchJWKS retrieves and caches EC public keys from the Supabase JWKS endpoint.
func (c *Config) fetchJWKS() (map[string]*ecdsa.PublicKey, error) {
	jwksMu.RLock()
	if time.Since(jwksFetched) < 5*time.Minute && jwksKeys != nil {
		keys := jwksKeys
		jwksMu.RUnlock()
		return keys, nil
	}
	jwksMu.RUnlock()

	resp, err := http.Get(c.URL + "/auth/v1/.well-known/jwks.json")
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()

	var raw struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}

	keys := make(map[string]*ecdsa.PublicKey)
	for _, k := range raw.Keys {
		if k.Kty != "EC" || k.Crv != "P-256" {
			continue
		}
		xb, err1 := base64.RawURLEncoding.DecodeString(k.X)
		yb, err2 := base64.RawURLEncoding.DecodeString(k.Y)
		if err1 != nil || err2 != nil {
			continue
		}
		keys[k.Kid] = &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(xb),
			Y:     new(big.Int).SetBytes(yb),
		}
	}

	jwksMu.Lock()
	jwksKeys = keys
	jwksFetched = time.Now()
	jwksMu.Unlock()
	return keys, nil
}

// ValidateJWT parses the Bearer token and returns the Supabase user ID (sub claim).
// Supports both HS256 (legacy) and ES256 (current Supabase default) signed tokens.
func (c *Config) ValidateJWT(tokenString string) (userID string, err error) {
	tok, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		switch t.Method.(type) {
		case *jwt.SigningMethodHMAC:
			return []byte(c.JWTSecret), nil
		case *jwt.SigningMethodECDSA:
			keys, err := c.fetchJWKS()
			if err != nil {
				return nil, err
			}
			kid, _ := t.Header["kid"].(string)
			if key, ok := keys[kid]; ok {
				return key, nil
			}
			// No kid match — try any available EC key.
			for _, key := range keys {
				return key, nil
			}
			return nil, fmt.Errorf("no JWKS key found for kid %q", kid)
		default:
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
	})
	if err != nil {
		return "", err
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok || !tok.Valid {
		return "", fmt.Errorf("invalid token claims")
	}
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", fmt.Errorf("token missing sub claim")
	}
	return sub, nil
}

// BearerFromRequest extracts the JWT from an Authorization: Bearer header.
// Returns "" if the header is absent or malformed.
func BearerFromRequest(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

// ── Auth API helpers ──────────────────────────────────────────────────────────

type authResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	// Nested user object (returned when a session is created immediately).
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
	// Root-level user fields (returned when email confirmation is pending —
	// Supabase sends the user at root with no access_token in that case).
	ID               string `json:"id"`
	Email            string `json:"email"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Message          string `json:"msg"` // OTP endpoint uses "msg"
}

// userID returns the Supabase user ID from whichever field is populated.
func (ar *authResponse) userID() string {
	if ar.User.ID != "" {
		return ar.User.ID
	}
	return ar.ID
}

func (c *Config) authPost(path string, body any) (*authResponse, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.URL+"/auth/v1"+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", c.AnonKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	var ar authResponse
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, fmt.Errorf("supabase auth: unexpected response: %s", data)
	}
	// Supabase uses different error shapes across versions: check status code
	// first, then fall back to known error fields in the body.
	if resp.StatusCode >= 400 {
		msg := ar.ErrorDescription
		if msg == "" {
			msg = ar.Error
		}
		if msg == "" {
			msg = ar.Message
		}
		if msg == "" {
			msg = string(data)
		}
		return nil, fmt.Errorf("supabase auth (%d): %s", resp.StatusCode, msg)
	}
	return &ar, nil
}

// SignUp creates a new Supabase account. When Supabase requires email
// confirmation, accessToken is empty and the caller must call VerifySignup
// after the user submits the OTP from their inbox.
func (c *Config) SignUp(email, password string) (userID, accessToken string, err error) {
	ar, err := c.authPost("/signup", map[string]string{
		"email":    email,
		"password": password,
	})
	if err != nil {
		return "", "", err
	}
	// accessToken is empty when email confirmation is required.
	return ar.userID(), ar.AccessToken, nil
}

// VerifySignup confirms the signup OTP. Supabase may or may not return a
// session — the caller should sign in separately after calling this.
func (c *Config) VerifySignup(email, token string) error {
	_, err := c.authPost("/verify", map[string]string{
		"type":  "signup",
		"email": email,
		"token": token,
	})
	return err
}

// SignIn authenticates with email+password and returns (userID, accessToken, refreshToken).
func (c *Config) SignIn(email, password string) (userID, accessToken, refreshToken string, err error) {
	ar, err := c.authPost("/token?grant_type=password", map[string]string{
		"email":    email,
		"password": password,
	})
	if err != nil {
		return "", "", "", err
	}
	if ar.AccessToken == "" {
		return "", "", "", fmt.Errorf("sign in failed: no access token in response")
	}
	uid := ar.userID()
	if uid == "" {
		// Newer Supabase versions may omit the user object from the token
		// response — extract the sub claim from the JWT payload instead.
		uid = subFromJWT(ar.AccessToken)
	}
	if uid == "" {
		return "", "", "", fmt.Errorf("sign in failed: could not determine user ID")
	}
	return uid, ar.AccessToken, ar.RefreshToken, nil
}

// subFromJWT decodes the JWT payload without verifying the signature and
// returns the "sub" claim, which is the Supabase user ID.
func subFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Sub
}

// SendOTP requests a magic-link / OTP email.
func (c *Config) SendOTP(email string) error {
	_, err := c.authPost("/otp", map[string]any{
		"email":             email,
		"create_user":       true,
		"should_create_user": true,
	})
	return err
}

// VerifyOTP verifies the 6-digit code sent to email and returns (userID, accessToken, refreshToken).
func (c *Config) VerifyOTP(email, token string) (userID, accessToken, refreshToken string, err error) {
	ar, err := c.authPost("/verify", map[string]string{
		"type":  "email",
		"email": email,
		"token": token,
	})
	if err != nil {
		return "", "", "", err
	}
	return ar.userID(), ar.AccessToken, ar.RefreshToken, nil
}

// ── PostgREST helpers ─────────────────────────────────────────────────────────

func (c *Config) restReq(method, table string, query string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	url := c.URL + "/rest/v1/" + table
	if query != "" {
		url += "?" + query
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", c.ServiceKey)
	req.Header.Set("Authorization", "Bearer "+c.ServiceKey)
	req.Header.Set("Prefer", "return=representation")
	if method == http.MethodPost || method == http.MethodPatch {
		req.Header.Set("Prefer", "resolution=merge-duplicates,return=representation")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("supabase REST %s %s: %s", method, table, data)
	}
	return data, nil
}

// Upsert inserts or updates rows in a table using the service key.
func (c *Config) Upsert(table string, rows any) error {
	_, err := c.restReq(http.MethodPost, table, "", rows)
	return err
}

// Select returns rows from a table matching the given PostgREST query string.
func (c *Config) Select(table, query string) ([]json.RawMessage, error) {
	data, err := c.restReq(http.MethodGet, table, query, nil)
	if err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// Delete removes rows matching query from a table.
func (c *Config) Delete(table, query string) error {
	_, err := c.restReq(http.MethodDelete, table, query, nil)
	return err
}
