# Configuring srchdsh

srchdsh reads its configuration from `$XDG_CONFIG_HOME/srchdsh/config.yaml`
when that file exists, or from the path given with `-conf`. Both commands take
the flag; without a configuration file the defaults apply.

In most cases, you are only going to use it to tweak the weights assigned to an
engine or to add a blocklist.

The sections in this page identify specific keys in the configuration file, and
a sample configuration file is provided at `./docs/config.yaml.example`.

These sections are also manually updated, but an effort is made to keep them in
sync with the code that works with them.
Consult `./config.go` and `./search/config.go` for updated/new configuration
values.

## `http_proxy`

Specifies the default HTTP proxy.
Overrides the `HTTP_PROXY` environment variable, but can be overridden by an engine's `http_proxy` setting.

The special value "-" will cause srchdsh to behave as if the `HTTP_PROXY` environment variable is not set.

By default, this is blank and as such `HTTP_PROXY` will be used if it is set.

## `rewrite`

`rewrite` provides a list of rules to rewrite domain names in results.
This will eventually provide more functionality, but works for the author's uses right now.

**Example**:

```yaml
rewrite:
    - hostname: www.youtube.com
      replace: yewtu.be
    - hostname: www.reddit.com
      replace: old.reddit.com
```

### `find`

`find` matches a regular expression.

**`find` and `hostname` cannot both be set in the same rule.**

### `hostname`

`hostname` matches an exact hostname.

**`find` and `hostname` cannot both be set in the same rule.**

### `replace`

`replace` specifies the value to replace the value found by the regular expression, or the value to replace the hostname.
You should use blacklists instead.

**(DEPRECATED, use blacklists instead)** When this value is an empty string, then any search result that matches this rewrite rule **will be removed**.
This can be used to "block" specific domains.

## `blacklists`

`blacklists` specifies a list of files containing [uBlacklist rulesets](https://iorate.github.io/ublacklist/docs/advanced-features#rules).
File paths are relative to the file where your configuration is stored.

**srchdsh does not fully implement rulesets.**
Currently, it supports [match patterns](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/Match_patterns) and [Go syntax regular expressions](https://pkg.go.dev/regexp/syntax).
This is likely enough for most use cases.

**Example**:

```yaml
blacklists:
    - ./ublacklist-ai.txt
    - ./ublacklist-spam.txt
    - /var/lib/srchd/ublacklist-other.txt
```

## `engines`

`engines` specifies configuration settings for engines supported by srchdsh.
The key of an engine specifies the name of the engine, and the value is the configuration of that engine.

Engines that are listed here, are not enabled by default, and are not listed in `disabled` will be implicitly enabled by having a configuration.

**Example**:

```yaml
engines:
    wiby:
        weight: 0.5
    google:
        weight: 1.5
        debug: true
```

Engines may or may not take configuration values that are not named here.

### `type`

Determines the type of the engine.
This defaults to the name of the engine, so it is safe to write configurations like this:

```yaml
engines:
    bing:
        debug: true
```

Possible values are the engine names printed by `srchdsh engines`: `bing`,
`brave`, `ddg`, `google`, `marginalia`, `mediawiki`, `mojeek`, `startpage`,
`wiby`, `yahoo` and `yandex`.

### `user_agent`

Specifies the user agent that is used when making requests to the engine.
srchdsh tries to mock a Chrome browser and as such uses a Chrome user agent by default.
You should not change this value unless you have a reason to.

### `timeout`

Specifies the amount of time requests must complete in.
This uses Go's [`time.Duration` format](https://pkg.go.dev/time#ParseDuration), so you can specify values like `5s` or `15s`.
The default is `10s`.

### `weight`

The value of this determines the order in which results are ranked.
An engine with a higher `weight` value will have its results placed higher than those of lower `weight` value.
Note that results are combined with the `weight` value taken into consideration and have their score recalculated, so if multiple search engines return the same result then it will likely be your top search result.

The default is `1.0`.

### `quic`

Enables HTTP/3 connections on this engine.
This may or may not work.

Currently, only Google is known to work with HTTP/3.

### `quic-0rtt`

Use 0-RTT on QUIC connections; requires `quic` to be set to true.

Using 0-RTT can have implications on the security of your connections as it becomes possible to replay the data you send to the server.
Generally it is only safe to use it if the requests you are doing are idempotent.
For srchd, this is always the case as of writing.

For more information, refer to [section 8 of RFC 8446](https://datatracker.ietf.org/doc/html/rfc8446#section-8).

### `http_proxy`

Configures a HTTP proxy to send requests through instead of using the one set in the `HTTP_PROXY` environment variable, if any.
The special value `"-"` explicitly asks to use no proxy at all, i.e. srchdsh will pretend both `http_proxy` (config) and `$HTTP_PROXY` (environment variable) are not set.

### `debug`

Setting `debug` to true logs more information about HTTP requests and may or may not enable additional logging in the engine itself.
You should always leave this at false unless you are debugging an engine, because it reveals information about searches.

### Engine-specific configuration options

**Wikipedia**:

- `endpoint`: Specifies the Mediawiki API endpoint to use (e.g. `https://en.wikipedia.org/w/api.php`)

## `disabled`

`disabled` is a list of engine names that should be explicitly disabled.
Engines listed here will never be used at any point by srchd, even if requested with the `:engine` operator.

**Example**:

```yaml
disabled:
    - bing
    - google
```
