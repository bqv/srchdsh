# srchd

![srchd search results for "small web"](docs/showcase.jpg)

**10 second pitch**:
srchd is a privacy-respecting metasearch engine written in Go.
Search several search engines at once without switching tabs, and reap the benefits of (occasionally) better results.

**Note**: The canonical repository is located at <https://git.sr.ht/~cmcevoy/srchd>.
[A mirror exists on GitHub](https://github.com/mca3/srchd).

## Rationale

Search engines of today are a far cry from what they used to be.
I find it hard at times to find exactly what I am looking for, and often I would jump across two or three different search engines to find what I am looking for.
This is no fun.

Naturally, I became a user of [SearxNG](https://github.com/searxng/searxng) for a period of time, however I found it hard to run on my memory constrained servers and configuration is complicated.
Though, I can vouch for its usefulness, it's a great piece of software.

I set out to replace SearxNG with something a bit lighter (and to satisfy my [NIH syndrome](https://en.wikipedia.org/wiki/Not_invented_here)) and srchd is the result.
I started using it as my primary search engine about two weeks after I started working on it and haven't looked back.

## Running

**Get up and going in under 30 seconds** (provided your machine is moderately fast):
Ensure you have [Go](https://go.dev) installed and run `go run git.sr.ht/~cmcevoy/srchd@latest`.

From this repository, you can use the usual Go commands to build and run binaries.
Simply use `go build .` to build a binary that holds all of the resources it needs to run within itself, or `go run .` to run the code right out of this repository.

An example configuration file can be found at `./docs/config.yaml.example` and documentation at `./docs/config.md`.

## Search engine support

srchd is a young project and as such doesn't have a lot of support.

- Bing (likely broken, disabled by default)
- Brave
- DuckDuckGo
- Google
- [Marginalia Search](https://marginalia-search.com) (**not** through API)
- Mediawiki (e.g. Wikipedia, requires configuration)
- Mojeek (its own index; solves the Altcha challenge, first page only)
- Startpage (Google's index, behind a proof-of-work challenge)
- Yahoo
- Yandex (experimental, off by default)
- wiby.me (native API)
