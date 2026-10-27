/**
 * DSH web-search provider backed by srchdsh.
 *
 * The provider is deliberately dumb: it asks the binding for one document and
 * maps it onto the seam's WebSearchResult. Everything interesting — fanning
 * out over engines, merging, ranking, captcha handling — happens in the Go
 * binary.
 *
 * Register it and pin `searchProvider: srchdsh` in the deployment; with the
 * id pinned, a missing binary fails the call as
 * WEB_PROVIDER_CONFIGURED_UNAVAILABLE instead of silently falling back to
 * another provider.
 */

import { available, search } from 'srchdsh';

export const name = 'web-search-srchdsh';

/** The web capability service must exist before this provider registers. */
export const inject = ['web'];

/**
 * @param {import('@deepseek-ai/cordis').Context} ctx
 * @param {{ bin?: string, timeoutMs?: number }} [config]
 */
export function apply(ctx, config = {}) {
  const bin = config.bin;
  // Below the tool's 60 s budget: let srchdsh's per-engine timeouts fire
  // first and leave room for the process to be reaped.
  const timeoutMs = config.timeoutMs ?? 25_000;

  ctx.web.registerSearchProvider({
    id: 'srchdsh',

    // Local executable check only; the seam forbids network calls here.
    available: () => available({ bin }),

    /**
     * @param {{ query: string, maxResults?: number }} request
     * @param {AbortSignal} [signal]
     */
    async search(request, signal) {
      const doc = await search(request.query, {
        bin,
        max: request.maxResults,
        timeoutMs,
        signal,
      });

      return {
        // `content` is intentionally absent: srchdsh returns sources, never
        // generated answer text.
        sources: doc.sources.map(toSource),
        truncated: doc.truncated,
      };
    },
  });
}

/**
 * Map a srchdsh source onto the seam's shape, omitting fields the engine did
 * not provide rather than inventing empty strings.
 *
 * @param {{ url: string, title?: string, snippet?: string, publishedAt?: string }} source
 */
function toSource(source) {
  const out = { url: source.url };
  if (source.title) {
    out.title = source.title;
  }
  if (source.snippet) {
    out.snippet = source.snippet;
  }
  if (source.publishedAt) {
    out.publishedAt = source.publishedAt;
  }
  return out;
}
