# srchdsh (Node)

A dependency-free ESM binding for the `srchdsh` binary: run a search, get the
document back.

```js
import { available, search } from 'srchdsh';

if (!available()) {
  throw new Error('srchdsh is not on PATH');
}

const doc = await search('deepseek harness', { max: 5 });

for (const source of doc.sources) {
  console.log(source.title, source.url);
}
```

No optional flags are required. The document is the same one
`srchdsh search --json` prints; see the top-level README for its shape.

## Options

| option | meaning |
|---|---|
| `bin` | binary to run; defaults to `$SRCHDSH_BIN`, then `srchdsh` on `PATH` |
| `max` | maximum sources; 0 means no limit |
| `page` | result page, starting at 1 |
| `conf` | configuration file, passed as `--conf` |
| `timeoutMs` | overall timeout; rounded up to whole seconds for the CLI |
| `signal` | an `AbortSignal`; aborting kills the child process |

## Errors

`search()` resolves for exit code 0 (results) and 1 (no results). It rejects
with `SrchdshError` for exit code 2 (every engine failed), a missing binary, or
an abort. When srchdsh printed a document, it is attached as `err.document`, so
the per-engine reasons (`captcha`, `timeout`, `http 429`) are not lost.

`available()` only checks that the binary is executable; it makes no network
call, so it is safe in a provider's availability check.

## Why not compile the search into Node?

A `js/wasm` build of the same code starts in about 716 ms (19 MB to
instantiate, against 7 ms for the process), and srchdsh's HTTP client sets its
own transport, so it bypasses the fetch shim and cannot reach the network under
wasm at all. Spawning costs about 11 ms end to end, which disappears beside a
search that takes hundreds of milliseconds.

## Tests

```sh
node --test
```
