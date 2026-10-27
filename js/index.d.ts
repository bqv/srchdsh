/** One citeable search result. */
export interface Source {
  url: string;
  title?: string;
  snippet?: string;
  /** ISO-8601; only present when an engine supplies a date. */
  publishedAt?: string;
}

/** What one engine did for one search. */
export interface EngineStatus {
  id: string;
  ms: number;
  count: number;
  /** Short machine string on failure: "captcha", "timeout", "http 429", "error". */
  error?: string;
}

/** The document `srchdsh search --json` prints. */
export interface Document {
  query: string;
  tookMs: number;
  truncated: boolean;
  sources: Source[];
  engines: EngineStatus[];
}

export interface SearchOptions {
  /** Maximum number of sources; 0 means no limit. */
  max?: number;
  /** Result page, starting at 1. */
  page?: number;
  /** Configuration file passed through as --conf. */
  conf?: string;
  /** Binary to run; defaults to $SRCHDSH_BIN, then `srchdsh` on PATH. */
  bin?: string;
  /** Overall search timeout; the child is killed and the promise rejects. */
  timeoutMs?: number;
  /** Cancelling this kills the child process. */
  signal?: AbortSignal;
}

export class SrchdshError extends Error {
  /** Exit code, or null when the process never ran. */
  code: number | null;
  /** The document srchdsh printed, when it printed one. */
  document: Document | null;
  /** Diagnostics srchdsh wrote to stderr. */
  stderr: string;
}

/** Run one search and resolve with the document. */
export function search(query: string, options?: SearchOptions): Promise<Document>;

/** Whether the binary exists and is executable. No network call. */
export function available(options?: { bin?: string }): boolean;

/** The binary path that would be used. */
export function binPath(options?: { bin?: string }): string;

declare const srchdsh: {
  search: typeof search;
  available: typeof available;
  binPath: typeof binPath;
  SrchdshError: typeof SrchdshError;
};
export default srchdsh;
