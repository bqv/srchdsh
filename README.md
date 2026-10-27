# srchdsh

A metasearch backend for [DSH](https://github.com/deepseek-ai/deepseek-harness),
forked from [srchd](https://git.sr.ht/~cmcevoy/srchd) by cmcevoy.

It queries several engines at once, merges and ranks their results, and emits
one JSON document. `srchdsh search` is a one-shot command, so DSH's web seam can
run it per query with no daemon; `srchdsh serve` exposes the same document over
HTTP for a second consumer.

srchdsh is not a search engine for people. The web interface, templates, static
files and settings pages of the original are gone; the only contract is the
document described below.

## Build

```sh
go build -o srchdsh .
```

## Use

```sh
# One JSON document on stdout; diagnostics on stderr.
srchdsh search -q 'deepseek harness' | jq

# Human-readable when stdout is a terminal; -json/-table override.
srchdsh search -q 'deepseek harness' -max 5

# Restrict the engines (the colon operator, as in srchd).
srchdsh search -q ':ddg deepseek harness'

# Optional HTTP mode, same document.
srchdsh serve -addr 127.0.0.1:8089
curl -s localhost:8089/v1/search -d '{"query":"deepseek harness","maxResults":5}'

srchdsh engines          # enabled engines, from the current configuration
srchdsh version
```

### From Node

`js/` is a dependency-free ESM package that runs the binary and returns the
document:

```js
import { available, search } from 'srchdsh';

if (!available()) throw new Error('srchdsh is not on PATH');

const doc = await search('deepseek harness', { max: 5, signal });
```

It forwards an `AbortSignal` to the child, and attaches the document to the
error when every engine failed, so per-engine reasons are not lost. Installing
it is `npm install ./js` or importing `./js/index.js` directly. Overhead over
the CLI is about 11 ms per search.

### The document

```json
{
  "query": "deepseek harness",
  "tookMs": 612,
  "truncated": false,
  "sources": [
    {
      "url": "https://www.deepseek.com/en/harness/",
      "title": "DeepSeek Harness | Explore the limits of intelligence",
      "snippet": "Use DeepSeek Harness to work with documents, analyze spreadsheets, …"
    }
  ],
  "engines": [
    { "id": "ddg", "ms": 431, "count": 10 },
    { "id": "startpage", "ms": 502, "count": 0, "error": "captcha" }
  ]
}
```

- `sources` is already deduplicated and ranked, and capped to `-max`
  (`maxResults` over HTTP); `truncated` records that something was dropped.
- `publishedAt` only appears when an engine supplies a date; no engine invents
  one.
- `engines` reports every selected engine, including failures, so a slow or
  broken engine is visible in the same call. Errors are short machine strings:
  `captcha`, `timeout`, `http 429`, `error`.

### Exit codes

| code | meaning |
|---|---|
| 0 | at least one source |
| 1 | no sources, but nothing failed outright |
| 2 | failure: bad usage, no engines enabled, or every engine failed |

## Configuration

YAML, read from `$XDG_CONFIG_HOME/srchdsh/config.yaml` when it exists, or from
`-conf PATH`. Without either, defaults apply: engine defaults, no blocklists,
no rewrites.

An example is at [`docs/config.yaml.example`](docs/config.yaml.example), and the
fields are documented in [`docs/config.md`](docs/config.md).

## Engines

| engine | notes |
|---|---|
| DuckDuckGo | works |
| Startpage | Google's index; solves its Anubis proof of work, then caches the cookie for five minutes |
| Mojeek | its own index; solves the Altcha challenge, first page only, and `robots.txt` disallows `/search` |
| Bing | works through its RSS feed, which is the only structured output left; first page only |
| Brave | off by default: upstream parser returns nothing |
| Google | off by default: consent wall, then JavaScript challenges |
| Marginalia, Wiby, Yahoo, Yandex | upstream engines, unchanged |
| Mediawiki | requires configuration |

`srchdsh engines` prints the enabled set.

## Licence and provenance

AGPL-3.0. This is a derivative work of srchd; see [`LICENSE`](LICENSE) and
[`NOTICE`](NOTICE) for the upstream commit. It cannot be relicensed.
