import test from "node:test";
import assert from "node:assert/strict";
import { effectScope, ref } from "vue";
import {
  usePagination,
  usePaginationState,
} from "../src/composables/usePagination";

test("list paging handles empty, last-page deletion and incoming refreshes", () => {
  const scope = effectScope();
  scope.run(() => {
    const items = ref(Array.from({ length: 41 }, (_, id) => ({ id })));
    const paging = usePagination(items);
    assert.equal(paging.pageSize.value, 20);
    paging.page.value = 3;
    assert.deepEqual(
      paging.rows.value.map((item) => item.id),
      [40],
    );
    items.value = items.value.slice(0, 40);
    assert.equal(paging.page.value, 2);
    assert.equal(paging.rows.value.length, 20);
    items.value.push({ id: 41 });
    assert.equal(paging.page.value, 2, "refresh must not force page one");
    items.value = [];
    assert.equal(paging.page.value, 1);
    assert.equal(paging.pageCount.value, 1);
    assert.deepEqual(paging.rows.value, []);
  });
  scope.stop();
});

test("page-size changes reset the page and server totals clamp an invalid page", () => {
  const scope = effectScope();
  scope.run(() => {
    const total = ref(120);
    const paging = usePaginationState(total);
    paging.page.value = 6;
    paging.pageSize.value = 50;
    assert.equal(paging.page.value, 1);
    assert.equal(paging.pageCount.value, 3);
    paging.page.value = 3;
    total.value = 35;
    assert.equal(paging.page.value, 1);
    paging.page.value = -1;
    assert.equal(paging.page.value, 1);
  });
  scope.stop();
});
