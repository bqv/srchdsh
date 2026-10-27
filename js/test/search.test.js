import assert from 'node:assert/strict';
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, test } from 'node:test';

import { SrchdshError, available, binPath, search } from '../index.js';

const dir = mkdtempSync(join(tmpdir(), 'srchdsh-js-'));
after(() => rmSync(dir, { recursive: true, force: true }));

// A stand-in for the Go binary: same argv contract, canned documents.
const stub = join(dir, 'srchdsh');
writeFileSync(
  stub,
  `#!/usr/bin/env node
const args = process.argv.slice(2);
const qi = args.indexOf('--query');
const query = qi >= 0 ? args[qi + 1] : '';

const write = (doc, code) => {
  process.stdout.write(JSON.stringify(doc));
  process.exit(code);
};

const doc = (sources, engines) => ({
  query,
  tookMs: 1,
  truncated: false,
  sources,
  engines,
  argv: args,
});

if (query === 'fail-me') {
  write(doc([], [{ id: 'stub', ms: 1, count: 0, error: 'captcha' }]), 2);
} else if (query === 'hang-me') {
  setInterval(() => {}, 1000);
} else if (query === 'nothing') {
  write(doc([], [{ id: 'stub', ms: 1, count: 0 }]), 1);
} else {
  write(doc([{ url: 'https://example.org/', title: 'Example' }], [{ id: 'stub', ms: 1, count: 1 }]), 0);
}
`,
);
chmodSync(stub, 0o755);

test('resolves with the document and forwards the flags', async () => {
  const doc = await search('deepseek harness', { bin: stub, max: 3, page: 2 });

  assert.equal(doc.query, 'deepseek harness');
  assert.equal(doc.sources.length, 1);
  assert.deepEqual(doc.argv, [
    'search',
    '--json',
    '--query',
    'deepseek harness',
    '--max',
    '3',
    '--page',
    '2',
  ]);
});

test('resolves an empty document when there are no sources', async () => {
  const doc = await search('nothing', { bin: stub });

  assert.deepEqual(doc.sources, []);
});

test('rejects with the document when every engine failed', async () => {
  await assert.rejects(search('fail-me', { bin: stub }), (err) => {
    assert.ok(err instanceof SrchdshError);
    assert.equal(err.code, 2);
    assert.equal(err.document.engines[0].error, 'captcha');
    return true;
  });
});

test('rejects a missing binary', async () => {
  await assert.rejects(search('anything', { bin: join(dir, 'missing') }), (err) => {
    assert.ok(err instanceof SrchdshError);
    return true;
  });
});

test('kills the child when the signal aborts', async () => {
  const controller = new AbortController();
  const pending = search('hang-me', { bin: stub, signal: controller.signal });

  setTimeout(() => controller.abort(), 100);

  await assert.rejects(pending, /aborted/);
});

test('available reports whether the binary runs', () => {
  assert.equal(available({ bin: stub }), true);
  assert.equal(available({ bin: join(dir, 'missing') }), false);
});

test('binPath prefers the option, then the environment', () => {
  assert.equal(binPath({ bin: '/x/y' }), '/x/y');

  const previous = process.env.SRCHDSH_BIN;
  process.env.SRCHDSH_BIN = '/from/env';
  try {
    assert.equal(binPath(), '/from/env');
  } finally {
    if (previous === undefined) {
      delete process.env.SRCHDSH_BIN;
    } else {
      process.env.SRCHDSH_BIN = previous;
    }
  }
});
