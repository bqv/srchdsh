# dsh-search-srchdsh

A DSH web-search provider backed by srchdsh. It registers as `srchdsh` on the
`ctx.web` seam and maps one srchdsh document onto `WebSearchResult`.

Nothing in this package decides which engines run, how results merge, or how a
captcha is handled: that is the Go binary's job. The provider only adapts the
document.

## Install

The provider needs the `srchdsh` Node package (`../js`) and the Go binary.

```sh
dsh plugin --profile web add /path/to/srchdsh/plugin
```

Then register it and pin it in `$DSH_HOME/profiles/web/cordis.patch.yml`:

```yaml
- id: web-search-srchdsh
  name: dsh-search-srchdsh
  config:
    # Omit to use `srchdsh` from PATH.
    bin: /home/user/bin/srchdsh
    timeoutMs: 25000

- id: web
  config:
    searchProvider: srchdsh
```

Pinning is the point. With the id pinned, a missing or broken binary fails the
call as `WEB_PROVIDER_CONFIGURED_UNAVAILABLE`; it never quietly falls back to
the DeepSeek provider. Registering without pinning while another search
provider is usable fails as `WEB_PROVIDER_AMBIGUOUS`.

## Behaviour

- `available()` checks that the binary is executable, and makes no network
  call — the seam forbids one there.
- `search()` returns `sources` and `truncated`; `content` is never set, because
  srchdsh returns sources, not generated answer text.
- Sources keep srchdsh's order and deduplication; the seam truncates to
  `maxResults` itself.
- An abort kills the child process rather than orphaning a search.
- Per-engine failures do not fail the call: they stay in the document's
  `engines[]` for logging. Only "every engine failed" (exit code 2) rejects.

## Tests

```sh
npm install
node --test
```

The tests mock `ctx.web` and use a stub binary, so they need neither the
network nor a working srchdsh build.
