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

## Development

```sh
GOWORK=off go test ./...
make build
```
