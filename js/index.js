/**
 * Node binding for srchdsh.
 *
 * The search itself runs in the Go binary; this module starts it, hands back
 * the JSON document it prints, and forwards an AbortSignal so a cancelled
 * search (a DSH request, a timeout) kills the child instead of leaving it
 * running.
 *
 * One binary per search. srchdsh starts in about 7 ms and a search takes
 * hundreds of milliseconds, so there is nothing to gain from keeping it
 * resident.
 */

import { spawn } from 'node:child_process';
import { accessSync, constants } from 'node:fs';
import { delimiter, isAbsolute, join } from 'node:path';

/** Largest document we will buffer; a normal search is a few kilobytes. */
const MAX_OUTPUT_BYTES = 16 * 1024 * 1024;

const DEFAULT_BIN = 'srchdsh';
const DEFAULT_TIMEOUT_MS = 30_000;

/** Error raised when srchdsh cannot run or the search fails outright. */
export class SrchdshError extends Error {
  /**
   * @param {string} message
   * @param {{code?: number|null, document?: object|null, stderr?: string}} [details]
   */
  constructor(message, { code = null, document = null, stderr = '' } = {}) {
    super(message);
    this.name = 'SrchdshError';

    /** Exit code, or null when the process never ran. */
    this.code = code;

    /** The document srchdsh printed, when it printed one. */
    this.document = document;

    /** Diagnostics srchdsh wrote to stderr. */
    this.stderr = stderr;
  }
}

/**
 * The binary to run: the `bin` option, then $SRCHDSH_BIN, then `srchdsh` on
 * PATH.
 *
 * @param {{bin?: string}} [options]
 * @returns {string}
 */
export function binPath(options = {}) {
  return options.bin ?? process.env.SRCHDSH_BIN ?? DEFAULT_BIN;
}

/**
 * Whether the binary can be executed.
 *
 * This is a local check, no network call, so it is safe to use as a provider's
 * `available()`.
 *
 * @param {{bin?: string}} [options]
 * @returns {boolean}
 */
export function available(options = {}) {
  const bin = binPath(options);

  if (bin.includes('/') || isAbsolute(bin)) {
    return executable(bin);
  }

  for (const dir of (process.env.PATH ?? '').split(delimiter)) {
    if (dir && executable(join(dir, bin))) {
      return true;
    }
  }
  return false;
}

function executable(path) {
  try {
    accessSync(path, constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

/**
 * Run one search.
 *
 * Resolves with the document for an exit code of 0 (results) or 1 (no
 * results). Rejects with {@link SrchdshError} when every engine failed (exit
 * code 2), when the binary is missing, or when the signal aborts; the document
 * is attached to the error when srchdsh printed one.
 *
 * @param {string} query
 * @param {{
 *   max?: number,
 *   page?: number,
 *   conf?: string,
 *   bin?: string,
 *   timeoutMs?: number,
 *   signal?: AbortSignal,
 * }} [options]
 * @returns {Promise<object>} the document
 */
export function search(query, options = {}) {
  const bin = binPath(options);

  if (typeof query !== 'string' || query.trim() === '') {
    return Promise.reject(new SrchdshError('query must be a non-empty string'));
  }

  const args = ['search', '--json', '--query', query];

  if (options.max !== undefined) {
    args.push('--max', String(options.max));
  }
  if (options.page !== undefined) {
    args.push('--page', String(options.page));
  }
  if (options.conf !== undefined) {
    args.push('--conf', String(options.conf));
  }
  if (options.timeoutMs !== undefined) {
    // The CLI takes a Go duration; whole seconds are plenty.
    args.push('--timeout', `${Math.max(1, Math.ceil(options.timeoutMs / 1000))}s`);
  }

  return new Promise((resolve, reject) => {
    let stdout = '';
    let stderr = '';
    let settled = false;

    let child;
    try {
      child = spawn(bin, args, {
        stdio: ['ignore', 'pipe', 'pipe'],
        signal: options.signal,
      });
    } catch (err) {
      reject(new SrchdshError(`cannot start ${bin}: ${err.message}`));
      return;
    }

    const fail = (err) => {
      if (settled) {
        return;
      }
      settled = true;
      reject(err);
    };

    child.stdout.setEncoding('utf8');
    child.stderr.setEncoding('utf8');

    child.stdout.on('data', (chunk) => {
      stdout += chunk;
      if (stdout.length > MAX_OUTPUT_BYTES) {
        child.kill('SIGKILL');
        fail(new SrchdshError('srchdsh produced too much output'));
      }
    });

    child.stderr.on('data', (chunk) => {
      stderr += chunk;
    });

    child.on('error', (err) => {
      // A missing binary and an aborted spawn both arrive here.
      const message =
        err.name === 'AbortError'
          ? 'search aborted'
          : `cannot run ${bin}: ${err.message}`;
      fail(new SrchdshError(message, { stderr }));
    });

    child.on('close', (code, signal) => {
      if (settled) {
        return;
      }
      settled = true;

      const document = parseDocument(stdout);

      if (code === 2) {
        reject(
          new SrchdshError(
            document
              ? 'every engine failed'
              : `srchdsh exited ${code}${signal ? ` (${signal})` : ''}`,
            { code, document, stderr },
          ),
        );
        return;
      }

      if (code !== 0 && code !== 1) {
        reject(
          new SrchdshError(
            `srchdsh exited ${code}${signal ? ` (${signal})` : ''}`,
            { code, document, stderr },
          ),
        );
        return;
      }

      if (!document) {
        reject(
          new SrchdshError('srchdsh printed no JSON document', {
            code,
            stderr,
          }),
        );
        return;
      }

      resolve(document);
    });
  });
}

function parseDocument(stdout) {
  const text = stdout.trim();
  if (text === '') {
    return null;
  }

  try {
    const doc = JSON.parse(text);
    if (doc && typeof doc === 'object' && Array.isArray(doc.sources)) {
      return doc;
    }
  } catch {
    // Fall through: the caller reports the missing document.
  }

  return null;
}

export default { search, available, binPath, SrchdshError };
