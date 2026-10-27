import assert from 'node:assert/strict';
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, test } from 'node:test';

import { apply, inject, name } from '../index.js';

const dir = mkdtempSync(join(tmpdir(), 'srchdsh-plugin-'));
after(() => rmSync(dir, { recursive: true, force: true }));

// Stand-in for the Go binary with the same argv contract.
const stub = join(dir, 'srchdsh');
writeFileSync(
  stub,
  `#!/usr/bin/env node
const query = process.argv[process.argv.indexOf('--query') + 1];

if (query === 'hang-me') {
  setInterval(() => {}, 1000);
  return;
}

process.stdout.write(JSON.stringify({
  query,
  tookMs: 12,
  truncated: true,
  sources: [
    {
      url: 'https://a.example/',
      title: 'A',
      snippet: 'first',
      publishedAt: '2026-01-02T03:04:05Z',
    },
    { url: 'https://b.example/' },
  ],
  engines: [
    { id: 'bing', ms: 10, count: 2 },
    { id: 'brave', ms: 5, count: 0, error: 'captcha' },
  ],
}));
`,
);
chmodSync(stub, 0o755);

function fakeContext() {
  const providers = [];
  return {
    providers,
    web: {
      registerSearchProvider(provider) {
        providers.push(provider);
        return () => {};
      },
    },
  };
}

test('declares its name and the web service dependency', () => {
  assert.equal(name, 'web-search-srchdsh');
  assert.deepEqual(inject, ['web']);
});

test('registers one provider with the documented id', () => {
  const ctx = fakeContext();
  apply(ctx, { bin: stub });

  assert.equal(ctx.providers.length, 1);
  assert.equal(ctx.providers[0].id, 'srchdsh');
});

test('maps the document onto the seam result', async () => {
  const ctx = fakeContext();
  apply(ctx, { bin: stub });

  const provider = ctx.providers[0];
  const result = await provider.search(
    { query: 'deepseek harness', maxResults: 2 },
    undefined,
  );

  assert.deepEqual(result, {
    truncated: true,
    sources: [
      {
        url: 'https://a.example/',
        title: 'A',
        snippet: 'first',
        publishedAt: '2026-01-02T03:04:05Z',
      },
      { url: 'https://b.example/' },
    ],
  });
  // No generated answer text: srchdsh never produces one.
  assert.equal(result.content, undefined);
});

test('available is a local check of the binary', () => {
  const ctx = fakeContext();
  apply(ctx, { bin: stub });
  assert.equal(ctx.providers[0].available(), true);

  const other = fakeContext();
  apply(other, { bin: join(dir, 'missing') });
  assert.equal(other.providers[0].available(), false);
});

test('passes cancellation through to the search', async () => {
  const ctx = fakeContext();
  apply(ctx, { bin: stub });

  const controller = new AbortController();
  const pending = ctx.providers[0].search(
    { query: 'hang-me' },
    controller.signal,
  );

  setTimeout(() => controller.abort(), 100);

  await assert.rejects(pending, /aborted/);
});
