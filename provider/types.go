// Package provider implements IntroDB.app marker lookup and submission logic.
package provider

import (
	"fmt"
	"time"
)

const (
	ProviderID     = "introdb-app"
	Algorithm      = "introdb-app:v1"
	DefaultBaseURL = "https://api.introdb.app"

	ExternalIDKeyTMDB = "tmdb"
	ExternalIDKeyIMDB = "imdb"
	ExternalIDKeyTVDB = "tvdb"
)

type ItemKind int

const (
	ItemKindEpisode ItemKind = iota + 1
	ItemKindMovie
)

type MarkerKind int

const (
	MarkerKindIntro MarkerKind = iota + 1
	MarkerKindCredits
	MarkerKindRecap
)

type Request struct {
	Kind          ItemKind
	ExternalIDs   map[string]string
	SeasonNumber  int
	EpisodeNumber int
	Duration      time.Duration
}

type Result struct {
	Markers []Marker
}

type Marker struct {
	Kind            MarkerKind
	Start           time.Duration
	End             time.Duration
	Confidence      float64
	SubmissionCount int
	Algorithm       string
}

type RetryAfterError struct {
	RetryAfter time.Duration
	Message    string
}

func (e *RetryAfterError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("retry after %s", e.RetryAfter)
}

type segment struct {
	StartMS         int64   `json:"start_ms"`
	EndMS           int64   `json:"end_ms"`
	StartSeconds    float64 `json:"start_sec"`
	EndSeconds      float64 `json:"end_sec"`
	Confidence      float64 `json:"confidence"`
	SubmissionCount int     `json:"submission_count"`
}

type segmentsResponse struct {
	IMDBID  string   `json:"imdb_id"`
	Season  int      `json:"season"`
	Episode int      `json:"episode"`
	Intro   *segment `json:"intro"`
	Recap   *segment `json:"recap"`
	Outro   *segment `json:"outro"`
}
