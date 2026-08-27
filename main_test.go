package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/blurbery/silo-plugin-markers-introdb-app/provider"
)

func testMarkerServer(t *testing.T, handler http.HandlerFunc) *markerServer {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := provider.NewClient()
	client.SetBaseURL(server.URL)
	return &markerServer{runtime: &runtimeServer{provider: provider.NewProvider(client)}}
}

func TestMarkerServerMapsOutroToCredits(t *testing.T) {
	server := testMarkerServer(t, func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/segments" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"imdb_id":"tt0455275","season":1,"episode":15,
			"intro":{"start_ms":299000,"end_ms":330000,"confidence":1,"submission_count":1},
			"recap":{"start_ms":0,"end_ms":52000,"confidence":0.9,"submission_count":2},
			"outro":{"start_ms":2579000,"end_ms":2648000,"confidence":0.8,"submission_count":3}
		}`))
	})

	response, err := server.FetchMarkers(context.Background(), &pluginv1.FetchMarkersRequest{
		ItemType:        "episode",
		ExternalIds:     &pluginv1.MarkerExternalIDs{ImdbId: "tt0455275"},
		SeasonNumber:    1,
		EpisodeNumber:   15,
		DurationSeconds: 2700,
	})
	if err != nil {
		t.Fatalf("FetchMarkers: %v", err)
	}
	if len(response.GetMarkers()) != 3 {
		t.Fatalf("markers = %d, want 3", len(response.GetMarkers()))
	}
	credits := response.GetMarkers()[1]
	if credits.GetSegment() != "credits" || credits.GetStartSeconds() != 2579 || credits.GetEndSeconds() != 2648 {
		t.Fatalf("credits = %+v", credits)
	}
	if credits.GetAlgorithm() != provider.Algorithm || credits.GetSubmissionCount() != 3 {
		t.Fatalf("credits metadata = %+v", credits)
	}
}

func TestMarkerServerIsReadOnly(t *testing.T) {
	server := &markerServer{}
	_, err := server.SubmitMarker(context.Background(), &pluginv1.SubmitMarkerRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("SubmitMarker code = %v, want Unimplemented", status.Code(err))
	}
}

func TestEmbeddedManifestDeclaresManagedReadOnlyProvider(t *testing.T) {
	manifest, err := loadManifest()
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if manifest.GetPluginId() != "silo.introdb-app" {
		t.Fatalf("plugin id = %q", manifest.GetPluginId())
	}
	if len(manifest.GetCapabilities()) != 1 || manifest.GetCapabilities()[0].GetId() != "introdb-app" {
		t.Fatalf("capabilities = %+v", manifest.GetCapabilities())
	}
}
