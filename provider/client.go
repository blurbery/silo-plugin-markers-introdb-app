package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	maxResponseBody         = 256 << 10
	defaultTimeout          = 3 * time.Second
	defaultCompleteCacheTTL = 24 * time.Hour
	defaultIncompleteTTL    = 15 * time.Minute
	defaultBlockedCooldown  = 5 * time.Minute
	clientUserAgent         = "Silo-Server-IntroDB-App-Plugin/0.1 (+https://github.com/blurbery/silo-plugin-markers-introdb-app)"
)

type Client struct {
	httpClient *http.Client
	mu         sync.RWMutex
	baseURL    string
	limiter    *rate.Limiter
	cache      *ttlCache[*segmentsResponse]

	completeCacheTTL time.Duration
	incompleteTTL    time.Duration
	blockedUntil     time.Time

	inflightMu sync.Mutex
	inflight   map[string]*inflightCall
}

type inflightCall struct {
	done     chan struct{}
	response *segmentsResponse
	err      error
}

func NewClient() *Client {
	return &Client{
		httpClient:       &http.Client{Timeout: defaultTimeout},
		baseURL:          DefaultBaseURL,
		limiter:          rate.NewLimiter(2, 4),
		cache:            newTTLCache[*segmentsResponse](),
		completeCacheTTL: defaultCompleteCacheTTL,
		incompleteTTL:    defaultIncompleteTTL,
		inflight:         make(map[string]*inflightCall),
	}
}

func (c *Client) SetBaseURL(baseURL string) {
	c.mu.Lock()
	c.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	c.mu.Unlock()
}

func (c *Client) FetchEpisode(ctx context.Context, imdbID string, season, episode int) (*segmentsResponse, error) {
	imdbID = strings.TrimSpace(imdbID)
	if !validIMDbID(imdbID) {
		return nil, fmt.Errorf("introdb.app: valid imdb_id required")
	}
	if season <= 0 || episode <= 0 {
		return nil, fmt.Errorf("introdb.app: season and episode must be positive")
	}
	query := url.Values{
		"imdb_id": {imdbID},
		"season":  {strconv.Itoa(season)},
		"episode": {strconv.Itoa(episode)},
	}
	key := query.Encode()
	if cached, ok := c.cache.Get(key); ok {
		return cached, nil
	}
	if remaining := c.blockedCooldownRemaining(); remaining > 0 {
		return nil, &RetryAfterError{RetryAfter: remaining, Message: "introdb.app: requests paused after HTTP 403"}
	}

	c.inflightMu.Lock()
	if call, ok := c.inflight[key]; ok {
		c.inflightMu.Unlock()
		select {
		case <-call.done:
			return call.response, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &inflightCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.inflightMu.Unlock()

	response, err := c.fetchEpisode(ctx, query, key)
	c.inflightMu.Lock()
	call.response = response
	call.err = err
	close(call.done)
	delete(c.inflight, key)
	c.inflightMu.Unlock()
	return response, err
}

func (c *Client) fetchEpisode(ctx context.Context, query url.Values, key string) (*segmentsResponse, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	c.mu.RLock()
	baseURL := c.baseURL
	c.mu.RUnlock()
	requestURL := baseURL + "/segments?" + query.Encode()

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("introdb.app: create request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", clientUserAgent)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("introdb.app: request failed: %w", err)
		}

		switch {
		case resp.StatusCode == http.StatusNotFound:
			resp.Body.Close()
			c.cache.Set(key, nil, c.incompleteTTL)
			return nil, nil
		case resp.StatusCode == http.StatusTooManyRequests:
			resp.Body.Close()
			retryAfter := retryAfterDuration(resp, 30*time.Second)
			c.pauseRequests(retryAfter)
			return nil, &RetryAfterError{RetryAfter: retryAfter, Message: "introdb.app: read rate limit reached"}
		case resp.StatusCode == http.StatusForbidden:
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			c.pauseRequests(defaultBlockedCooldown)
			message := "introdb.app: HTTP 403; requests paused"
			if strings.EqualFold(resp.Header.Get("Server"), "cloudflare") || bytes.Contains(bytes.ToLower(body), []byte("cloudflare")) {
				message = "introdb.app: Cloudflare HTTP 403; requests paused"
			}
			if ray := strings.TrimSpace(resp.Header.Get("CF-Ray")); ray != "" {
				message += " (cf-ray " + ray + ")"
			}
			return nil, &RetryAfterError{RetryAfter: defaultBlockedCooldown, Message: message}
		case resp.StatusCode >= 500 && attempt == 0:
			resp.Body.Close()
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		case resp.StatusCode >= 400:
			message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return nil, fmt.Errorf("introdb.app: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
		}

		var response segmentsResponse
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&response)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("introdb.app: decode response: %w", decodeErr)
		}
		ttl := c.incompleteTTL
		if response.Intro != nil && response.Outro != nil {
			ttl = c.completeCacheTTL
		}
		c.cache.Set(key, &response, ttl)
		return &response, nil
	}
	return nil, fmt.Errorf("introdb.app: request failed after retry")
}

func validIMDbID(value string) bool {
	if len(value) != 9 && len(value) != 10 {
		return false
	}
	if !strings.HasPrefix(value, "tt") {
		return false
	}
	for _, char := range value[2:] {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func retryAfterDuration(resp *http.Response, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		if remaining := time.Until(when); remaining > 0 {
			return remaining
		}
	}
	return fallback
}

func (c *Client) pauseRequests(duration time.Duration) {
	c.mu.Lock()
	until := time.Now().Add(duration)
	if until.After(c.blockedUntil) {
		c.blockedUntil = until
	}
	c.mu.Unlock()
}

func (c *Client) blockedCooldownRemaining() time.Duration {
	c.mu.RLock()
	remaining := time.Until(c.blockedUntil)
	c.mu.RUnlock()
	if remaining < 0 {
		return 0
	}
	return remaining
}
