# Silo IntroDB.app Plugin

Independent Silo marker provider for [IntroDB](https://introdb.app).
It is designed to be distributed and maintained as a Silo-managed plugin.

The plugin implements `marker_provider.v1` for TV episodes. It fetches IntroDB's
`intro`, `recap`, and `outro` segments in one request and maps `outro` to Silo's
`credits` marker. Silo persists accepted results with the media file, so later
playback sessions and other users reuse them without another API request.

## Configuration

No IntroDB account or API key is required. This plugin is deliberately
read-only and only fetches markers already stored in IntroDB's public database.

When this plugin and TheIntroDB are both enabled, Silo queries them concurrently
and merges the best available marker for each segment. Provider priority remains
configurable in Silo's admin settings.

## Performance

Marker discovery never sits in the playback startup path. Silo starts it in the
background, then persists accepted results so later sessions and other users do
not repeat the provider request.

The plugin fetches intro, recap, and outro in one call, limits requests to two
per second with a small burst, caps each lookup at three seconds, and collapses
identical concurrent lookups. Complete intro-and-credit responses are cached for
24 hours; incomplete and missing responses use a 15-minute cache so new markers
can still appear without hammering IntroDB. Rate limits and HTTP 403 responses
start a cooldown instead of triggering request storms.

## Coordinated Silo changes

- [Server marker refresh, persistence, and read-only provider support](https://github.com/blurbery/silo-server/pull/54)
- [TheIntroDB fallback, caching, and Cloudflare handling](https://github.com/Silo-Server/silo-plugin-markers-theintrodb/pull/5)
- [Apple/Aether Skip Intro and Skip Credits reliability](https://github.com/blurbery/silo-apple/pull/9)
- [Silo catalog entry](https://github.com/Silo-Server/silo-plugins/pull/7)

## Development

```sh
GOWORK=off go test ./...
make build
```
