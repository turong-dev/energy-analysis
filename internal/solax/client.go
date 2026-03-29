package solax

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// key and iv are set per-client from config — no hardcoded values in source.

const (
	baseURL     = "https://euapi.solaxcloud.com"
	originLogin = "https://www.solaxcloud.com"
	originData  = "https://global.solaxcloud.com"
	userAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.2 Safari/605.1.15"
)

// ---------------------------------------------------------------------------
// Crypto
// ---------------------------------------------------------------------------

func (c *Client) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	padded := pkcs7Pad([]byte(plaintext), aes.BlockSize)
	dst := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, c.iv).CryptBlocks(dst, padded)
	return base64.StdEncoding.EncodeToString(dst), nil
}

func (c *Client) decrypt(ciphertextB64 string) (json.RawMessage, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	cipher.NewCBCDecrypter(block, c.iv).CryptBlocks(raw, raw)
	plaintext := pkcs7Unpad(raw)
	return json.RawMessage(plaintext), nil
}

func (c *Client) encryptJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return c.encrypt(string(b))
}

func (c *Client) encryptQS(params map[string]any) (string, error) {
	parts := make([]string, 0, len(params))
	for k, v := range params {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return c.encrypt(strings.Join(parts, "&"))
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func pkcs7Unpad(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	padding := int(data[len(data)-1])
	if padding > len(data) {
		return data
	}
	return data[:len(data)-padding]
}

// ---------------------------------------------------------------------------
// Misc helpers
// ---------------------------------------------------------------------------

func md5hex(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}

func randHex(n int) string {
	const chars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func tsMs() int64 {
	return time.Now().UnixMilli()
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

type Client struct {
	email    string
	password string
	key      []byte
	iv       []byte
	http     *http.Client
	token    string
	tokenExp time.Time
}

func NewClient(email, password, cryptoKey, cryptoIV string) (*Client, error) {
	if cryptoKey == "" || cryptoIV == "" {
		return nil, fmt.Errorf("SOLAX_CRYPTO_KEY and SOLAX_CRYPTO_IV must be set")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Client{
		email:    email,
		password: password,
		key:      []byte(cryptoKey),
		iv:       []byte(cryptoIV),
		http:     &http.Client{Jar: jar, Timeout: 30 * time.Second},
	}, nil
}

func (c *Client) EnsureLoggedIn() error {
	if c.token != "" && time.Now().Before(c.tokenExp) {
		return nil
	}
	return c.Login()
}

func (c *Client) Login() error {
	ts := tsMs()
	qs, err := c.encryptQS(map[string]any{"timeStamp": ts, "requestId": randHex(8)})
	if err != nil {
		return fmt.Errorf("encrypt qs: %w", err)
	}
	body, err := c.encryptJSON(map[string]any{
		"loginName": c.email,
		"password":  md5hex(c.password),
		"service":   "",
	})
	if err != nil {
		return fmt.Errorf("encrypt body: %w", err)
	}

	resp, err := c.post(
		baseURL+"/unionUser/web/v2/public/login",
		map[string]string{"data": qs},
		map[string]any{"data": body},
		map[string]string{
			"crytoVer":         "1",
			"deviceId":         randHex(8),
			"deviceType":       "3",
			"Lang":             "en_US",
			"Origin":           originLogin,
			"Referer":          originLogin + "/",
			"source":           "0",
			"websiteType":      "0",
			"x-request-source": "3",
			"x-transaction-id": fmt.Sprintf("%s-%d-%d", randHex(8), rand.Intn(900000)+100000, ts),
		},
	)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}

	result, err := c.decrypt(resp["data"].(string))
	if err != nil {
		return fmt.Errorf("decrypt login response: %w", err)
	}

	var loginResult struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Result  struct {
			Token string `json:"token"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &loginResult); err != nil {
		return fmt.Errorf("parse login response: %w", err)
	}
	if !loginResult.Success {
		return fmt.Errorf("login failed: %s", loginResult.Message)
	}

	c.token = loginResult.Result.Token
	c.tokenExp = jwtExpiry(c.token).Add(-60 * time.Second)
	return nil
}

// GetEnergyInfo fetches the energy data for a single day.
// Returns nil if the API returns no data for that day.
func (c *Client) GetEnergyInfo(siteID string, day time.Time) (*Raw, error) {
	if err := c.EnsureLoggedIn(); err != nil {
		return nil, err
	}

	ts := tsMs()
	qs, err := c.encryptQS(map[string]any{"timeStamp": ts, "requestId": randHex(8)})
	if err != nil {
		return nil, fmt.Errorf("encrypt qs: %w", err)
	}
	body, err := c.encryptJSON(map[string]any{
		"year":      day.Year(),
		"month":     int(day.Month()),
		"day":       day.Day(),
		"siteId":    siteID,
		"dimension": 1,
	})
	if err != nil {
		return nil, fmt.Errorf("encrypt body: %w", err)
	}

	resp, err := c.post(
		baseURL+"/zeus/v1/overview/energyInfo",
		map[string]string{"data": qs},
		map[string]any{"data": body},
		map[string]string{
			"crytoVer":           "1",
			"deviceId":           fmt.Sprintf("%s-%d", randHex(8), ts),
			"deviceType":         "3",
			"Lang":               "en_US",
			"Origin":             originData,
			"Referer":            originData + "/",
			"Permission-Version": "v7.2.0",
			"platform":           "1",
			"queryTime":          time.Now().Format("2006-01-02 15:04:05"),
			"source":             "0",
			"token":              c.token,
			"version":            "blue",
			"websiteType":        "0",
			"x-transaction-id":   fmt.Sprintf("%s-%d", randHex(8), ts),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("energy info request: %w", err)
	}

	result, err := c.decrypt(resp["data"].(string))
	if err != nil {
		return nil, fmt.Errorf("decrypt response: %w", err)
	}

	var payload struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if !payload.Success || payload.Result == nil || string(payload.Result) == "null" {
		return nil, nil
	}

	var raw Raw
	if err := json.Unmarshal(payload.Result, &raw); err != nil {
		return nil, fmt.Errorf("parse energy data: %w", err)
	}
	return &raw, nil
}

func (c *Client) HasRealData(r *Raw) bool {
	return r.Yield.TotalYield > 0 || r.Consumed.TotalConsumed > 0
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

func (c *Client) post(endpoint string, qs map[string]string, body map[string]any, headers map[string]string) (map[string]any, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	u, _ := url.Parse(endpoint)
	q := u.Query()
	for k, v := range qs {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// JWT helpers
// ---------------------------------------------------------------------------

func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Now().Add(time.Hour)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Now().Add(time.Hour)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Now().Add(time.Hour)
	}
	return time.Unix(claims.Exp, 0)
}
