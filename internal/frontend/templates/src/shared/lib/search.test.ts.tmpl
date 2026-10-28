import { expect, test } from "vitest";
import { nextPage, previousPage, pageSearch } from "./search";
test("cursor history round-trips through the URL without numeric page assumptions", () => {
  const second = nextPage({}, "boundary-two");
  const third = nextPage(second, "boundary-three");
  expect(previousPage(third)).toEqual(second);
  expect(previousPage(second).cursor).toBeUndefined();
  expect(pageSearch({ cursor: 42, trail: [42, "valid"] })).toEqual({
    cursor: undefined,
    trail: ["valid"],
  });
});
