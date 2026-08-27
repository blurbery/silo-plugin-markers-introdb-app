package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchEpisodeUsesPublicSegmentsEndpointAndCaches(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/segments" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.URL.Query().Get("imdb_id") != "tt0455275" || request.URL.Query().Get("season") != "1" || request.URL.Query().Get("episode") != "15" {
			t.Fatalf("query = %s", request.URL.RawQuery)
		}
		if request.Header.Get("X-API-Key") != "" || request.Header.Get("Authorization") != "" {
			t.Fatal("anonymous read unexpectedly included credentials")
		}
		if request.Header.Get("User-Agent") != clientUserAgent {
			t.Fatalf("User-Agent = %q", request.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{"imdb_id":"tt0455275","season":1,"episode":15,"intro":null,"recap":null,"outro":null}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient()
	client.SetBaseURL(server.URL)
	for i := 0; i < 2; i++ {
		response, err := client.FetchEpisode(context.Background(), "tt0455275", 1, 15)
		if err != nil || response == nil {
			t.Fatalf("FetchEpisode = %#v, %v", response, err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestFetchEpisodeCollapsesConcurrentRequests(t *testing.T) {
	var requests atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		<-release
		_, _ = w.Write([]byte(`{"imdb_id":"tt0903747","season":1,"episode":1,"intro":null,"recap":null,"outro":null}`))
	}))
	t.Cleanup(server.Close)
	client := NewClient()
	client.SetBaseURL(server.URL)

	const callers = 8
	var wg sync.WaitGroup
	errors := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.FetchEpisode(context.Background(), "tt0903747", 1, 1)
			errors <- err
		}()
	}
	deadline := time.Now().Add(time.Second)
	for requests.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("FetchEpisode: %v", err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestFetchEpisodeCachesNotFoundBriefly(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	client := NewClient()
	client.SetBaseURL(server.URL)

	for i := 0; i < 2; i++ {
		response, err := client.FetchEpisode(context.Background(), "tt0903747", 1, 1)
		if err != nil || response != nil {
			t.Fatalf("FetchEpisode = %#v, %v", response, err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestFetchEpisodeRateLimitReturnsBackoff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "45")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	client := NewClient()
	client.SetBaseURL(server.URL)

	_, err := client.FetchEpisode(context.Background(), "tt0903747", 1, 1)
	retry, ok := err.(*RetryAfterError)
	if !ok || retry.RetryAfter != 45*time.Second {
		t.Fatalf("error = %#v, want 45s RetryAfterError", err)
	}
	_, err = client.FetchEpisode(context.Background(), "tt0903747", 1, 2)
	if _, ok := err.(*RetryAfterError); !ok {
		t.Fatalf("cooldown error = %#v", err)
	}
}

func TestFetchEpisodeCloudflareBlockStartsCooldown(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("CF-Ray", "example-ray")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("blocked"))
	}))
	t.Cleanup(server.Close)
	client := NewClient()
	client.SetBaseURL(server.URL)

	_, firstErr := client.FetchEpisode(context.Background(), "tt0903747", 1, 1)
	_, secondErr := client.FetchEpisode(context.Background(), "tt0903747", 1, 2)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("errors = %v / %v", firstErr, secondErr)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1 during cooldown", requests.Load())
	}
}

func TestFetchEpisodeRejectsInvalidIMDbIDBeforeNetwork(t *testing.T) {
	client := NewClient()
	if _, err := client.FetchEpisode(context.Background(), "1396", 1, 1); err == nil {
		t.Fatal("expected invalid IMDb id error")
	}
}
