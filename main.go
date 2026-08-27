package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
	"github.com/blurbery/silo-plugin-markers-introdb-app/provider"
)

var version string

//go:embed manifest.json
var manifestJSON []byte

type runtimeServer struct {
	pluginv1.UnimplementedRuntimeServer
	manifest *pluginv1.PluginManifest
	provider *provider.Provider
}

type markerServer struct {
	pluginv1.UnimplementedMarkerProviderServer
	runtime *runtimeServer
}

func (s *runtimeServer) GetManifest(context.Context, *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

func (s *runtimeServer) Configure(context.Context, *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	return &pluginv1.ConfigureResponse{}, nil
}

func (s *markerServer) FetchMarkers(ctx context.Context, req *pluginv1.FetchMarkersRequest) (*pluginv1.FetchMarkersResponse, error) {
	result, err := s.runtime.provider.FetchMarkers(ctx, requestFromProto(req))
	if err != nil {
		return nil, providerError(err)
	}
	response := &pluginv1.FetchMarkersResponse{}
	for _, marker := range result.Markers {
		start := marker.Start.Seconds()
		end := marker.End.Seconds()
		response.Markers = append(response.Markers, &pluginv1.MarkerSegment{
			Segment:         markerKindName(marker.Kind),
			StartSeconds:    &start,
			EndSeconds:      &end,
			Confidence:      marker.Confidence,
			SubmissionCount: int32(marker.SubmissionCount),
			Algorithm:       marker.Algorithm,
		})
	}
	return response, nil
}

func (s *markerServer) SubmitMarker(context.Context, *pluginv1.SubmitMarkerRequest) (*pluginv1.SubmitMarkerResponse, error) {
	return nil, status.Error(codes.Unimplemented, "IntroDB.app is a read-only marker provider")
}

func (s *markerServer) GetMarkerProviderStats(context.Context, *pluginv1.GetMarkerProviderStatsRequest) (*pluginv1.MarkerProviderStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "IntroDB.app anonymous reads have no account statistics")
}

func main() {
	manifest, err := loadManifest()
	if err != nil {
		panic(err)
	}
	client := provider.NewClient()
	rt := &runtimeServer{manifest: manifest, provider: provider.NewProvider(client)}
	runtime.Serve(runtime.ServeConfig{
		Servers: runtime.CapabilityServers{
			Runtime:        rt,
			MarkerProvider: &markerServer{runtime: rt},
		},
	})
}

func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}
	if version != "" {
		manifest.Version = version
	}
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return nil, fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])
	return manifest, nil
}

func requestFromProto(req *pluginv1.FetchMarkersRequest) provider.Request {
	return provider.Request{
		Kind:          itemKind(req.GetItemType()),
		ExternalIDs:   externalIDs(req.GetExternalIds()),
		SeasonNumber:  int(req.GetSeasonNumber()),
		EpisodeNumber: int(req.GetEpisodeNumber()),
		Duration:      secondsDuration(req.GetDurationSeconds()),
	}
}

func externalIDs(ids *pluginv1.MarkerExternalIDs) map[string]string {
	result := make(map[string]string)
	if ids == nil {
		return result
	}
	setID(result, provider.ExternalIDKeyTMDB, ids.GetTmdbId())
	setID(result, provider.ExternalIDKeyIMDB, ids.GetImdbId())
	setID(result, provider.ExternalIDKeyTVDB, ids.GetTvdbId())
	if providerIDs := ids.GetProviderIds(); providerIDs != nil {
		for key, value := range providerIDs.AsMap() {
			if text, ok := value.(string); ok {
				setID(result, strings.ToLower(strings.TrimSpace(key)), text)
			}
		}
	}
	return result
}

func setID(ids map[string]string, key, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key != "" && value != "" {
		ids[key] = value
	}
}

func itemKind(value string) provider.ItemKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "episode":
		return provider.ItemKindEpisode
	case "movie":
		return provider.ItemKindMovie
	default:
		return 0
	}
}

func markerKindName(kind provider.MarkerKind) string {
	switch kind {
	case provider.MarkerKindIntro:
		return "intro"
	case provider.MarkerKindCredits:
		return "credits"
	case provider.MarkerKindRecap:
		return "recap"
	default:
		return ""
	}
}

func secondsDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

func providerError(err error) error {
	var retryErr *provider.RetryAfterError
	if !errors.As(err, &retryErr) || retryErr == nil {
		return err
	}
	grpcStatus := status.New(codes.ResourceExhausted, retryErr.Error())
	if retryErr.RetryAfter <= 0 {
		return grpcStatus.Err()
	}
	withDetails, detailErr := grpcStatus.WithDetails(&errdetails.RetryInfo{
		RetryDelay: durationpb.New(retryErr.RetryAfter),
	})
	if detailErr != nil {
		return grpcStatus.Err()
	}
	return withDetails.Err()
}
