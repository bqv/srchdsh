# Testing

`go test ./...` runs offline. It covers result merging and ranking, the JSON
document, the query operators, the engine parsers and challenge solvers, the
cache, and the CLI's argument handling. Nothing in that run touches the network.

Engine tests that talk to the real search engines are behind the `live` build
tag:

```sh
go test -tags live ./search/engines
```

They are not part of the default run because a fresh checkout has no recorded
fixtures, so every one of them would hit the network (and take minutes).

## engtest

Engine tests use `internal/engtest`, which records every request and response
of a search and replays it on later runs. Fixtures live under
`search/engines/testdata/<engine>/<query>/`, are excluded from git by
`.gitignore`, and may contain parts of the queries that produced them.

If a fixture is missing, or `-update` is passed, the test performs the real
search and writes the fixture instead of comparing. That is how a new engine
test starts:

```sh
go test -tags live -update ./search/engines -run TestDDGSearch
```

`-updatewait` (seconds, default 3) is the delay between live queries, to avoid
hammering an engine.

The recordings are gob files whose names are hashes of the request URL. To pull
the HTML of the last response out of one, for debugging a parser:

```sh
go run ./internal/engtest/tool ./search/engines/testdata/<engine>/<query>/<hash>
```

Add `-headers` for the response headers, or `-req` for the request (which
implies `-headers`). Most breakage is an engine changing its HTML, so the
response body is usually all you need.
