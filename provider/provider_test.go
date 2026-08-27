package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProviderMapsIntroRecapAndOutro(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"imdb_id":"tt0434665","season":1,"episode":3,
			"intro":{"start_ms":0,"end_ms":108000,"confidence":1,"submission_count":4},
			"recap":null,
			"outro":{"start_ms":1309337,"end_ms":1384000,"confidence":0.75,"submission_count":2}
		}`))
	}))
	t.Cleanup(server.Close)
	client := NewClient()
	client.SetBaseURL(server.URL)
	provider := NewProvider(client)

	result, err := provider.FetchMarkers(context.Background(), Request{
		Kind:          ItemKindEpisode,
		ExternalIDs:   map[string]string{ExternalIDKeyIMDB: "tt0434665"},
		SeasonNumber:  1,
		EpisodeNumber: 3,
		Duration:      24 * time.Minute,
	})
	if err != nil {
		t.Fatalf("FetchMarkers: %v", err)
	}
	if len(result.Markers) != 2 {
		t.Fatalf("markers = %+v", result.Markers)
	}
	if intro := result.Markers[0]; intro.Kind != MarkerKindIntro || intro.Start != 0 || intro.End != 108*time.Second {
		t.Fatalf("intro = %+v", intro)
	}
	credits := result.Markers[1]
	if credits.Kind != MarkerKindCredits || credits.Start != 1309337*time.Millisecond || credits.End != 1384*time.Second {
		t.Fatalf("credits = %+v", credits)
	}
	if credits.Confidence != 0.75 || credits.SubmissionCount != 2 || credits.Algorithm != Algorithm {
		t.Fatalf("credits metadata = %+v", credits)
	}
}

func TestProviderSkipsUnsupportedMoviesWithoutRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	t.Cleanup(server.Close)
	client := NewClient()
	client.SetBaseURL(server.URL)
	result, err := NewProvider(client).FetchMarkers(context.Background(), Request{
		Kind:        ItemKindMovie,
		ExternalIDs: map[string]string{ExternalIDKeyIMDB: "tt0111161"},
	})
	if err != nil || len(result.Markers) != 0 || requests != 0 {
		t.Fatalf("result = %+v, err = %v, requests = %d", result, err, requests)
	}
}

func TestMarkerFromSegmentClampsToMediaDuration(t *testing.T) {
	marker, ok := markerFromSegment(&segment{
		StartMS:    90000,
		EndMS:      110000,
		Confidence: 2,
	}, MarkerKindCredits, 100*time.Second)
	if !ok {
		t.Fatal("marker rejected")
	}
	if marker.End != 100*time.Second || marker.Confidence != 1 {
		t.Fatalf("marker = %+v", marker)
	}
}

func TestMarkerFromSegmentRejectsInvalidBounds(t *testing.T) {
	if _, ok := markerFromSegment(&segment{StartMS: 20, EndMS: 10}, MarkerKindIntro, time.Minute); ok {
		t.Fatal("invalid marker accepted")
	}
	if _, ok := markerFromSegment(&segment{StartMS: 70000, EndMS: 80000}, MarkerKindIntro, time.Minute); ok {
		t.Fatal("out-of-range marker accepted")
	}
}
