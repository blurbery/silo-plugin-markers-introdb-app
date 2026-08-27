package provider

import (
	"context"
	"strings"
	"time"
)

type Provider struct {
	client *Client
}

func NewProvider(client *Client) *Provider {
	return &Provider{client: client}
}

func (p *Provider) ID() string { return ProviderID }

func (p *Provider) FetchMarkers(ctx context.Context, req Request) (Result, error) {
	if p == nil || p.client == nil || req.Kind != ItemKindEpisode {
		return Result{}, nil
	}
	imdbID := strings.TrimSpace(req.ExternalIDs[ExternalIDKeyIMDB])
	if imdbID == "" || req.SeasonNumber <= 0 || req.EpisodeNumber <= 0 {
		return Result{}, nil
	}

	response, err := p.client.FetchEpisode(ctx, imdbID, req.SeasonNumber, req.EpisodeNumber)
	if err != nil || response == nil {
		return Result{}, err
	}

	result := Result{}
	appendMarker := func(value *segment, kind MarkerKind) {
		marker, ok := markerFromSegment(value, kind, req.Duration)
		if ok {
			result.Markers = append(result.Markers, marker)
		}
	}
	appendMarker(response.Intro, MarkerKindIntro)
	appendMarker(response.Outro, MarkerKindCredits)
	appendMarker(response.Recap, MarkerKindRecap)
	return result, nil
}

func markerFromSegment(value *segment, kind MarkerKind, duration time.Duration) (Marker, bool) {
	if value == nil || value.StartMS < 0 || value.EndMS <= value.StartMS {
		return Marker{}, false
	}
	start := time.Duration(value.StartMS) * time.Millisecond
	end := time.Duration(value.EndMS) * time.Millisecond
	if duration > 0 {
		if start >= duration {
			return Marker{}, false
		}
		if end > duration {
			end = duration
		}
	}
	if end <= start {
		return Marker{}, false
	}
	confidence := value.Confidence
	if confidence < 0 {
		confidence = 0
	} else if confidence > 1 {
		confidence = 1
	}
	return Marker{
		Kind:            kind,
		Start:           start,
		End:             end,
		Confidence:      confidence,
		SubmissionCount: value.SubmissionCount,
		Algorithm:       Algorithm,
	}, true
}
