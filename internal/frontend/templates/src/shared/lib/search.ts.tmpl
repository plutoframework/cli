export type PageSearch = { cursor?: string; trail?: string[] };
export function pageSearch(raw: Record<string, unknown>): PageSearch {
  return {
    cursor:
      typeof raw.cursor === "string" && raw.cursor.length <= 2048
        ? raw.cursor
        : undefined,
    trail: Array.isArray(raw.trail)
      ? raw.trail
          .filter(
            (value): value is string =>
              typeof value === "string" && value.length <= 2048,
          )
          .slice(-30)
      : undefined,
  };
}
export function previousPage(search: PageSearch): PageSearch {
  const trail = [...(search.trail ?? [])];
  const cursor = trail.pop();
  return { cursor: cursor || undefined, trail };
}
export function nextPage(search: PageSearch, next: string): PageSearch {
  return {
    cursor: next,
    trail: [...(search.trail ?? []), search.cursor ?? ""],
  };
}
