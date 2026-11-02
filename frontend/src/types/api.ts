/**
 * Shared REST envelope and pagination contracts.
 * The backend uses code = 0 for success; data may be absent on action responses.
 * Pagination returns a page list and a total count rather than embedding metadata
 * into each resource type.
 */
/** Backend response wrapper; request() returns data only when code is zero. */
export interface Envelope<T = unknown> {
  code: number;
  message: string;
  data?: T;
}

/** A single result page and the total matching row count before pagination. */
export interface Paged<T> {
  list: T[];
  total: number;
}

/** Use a type alias to remain assignable to the request query record. */
export type PageQuery = {
  page?: number;
  page_size?: number;
  keyword?: string;
};
