import test from "node:test";
import assert from "node:assert/strict";
import { createRenderer, h, ref } from "vue";
import { useQueryRows } from "../src/composables/useQueryRows";
import { useLeaveConfirmation } from "../src/composables/useLeaveConfirmation";

test("changed filters hide old actions immediately and reject delayed responses", () => {
  const query = ref("page=1&q=");
  const items = ref<{ id: string }[]>([]);
  const list = useQueryRows(items, query);
  assert.equal(list.commit(query.value, [{ id: "old" }]), true);
  assert.deepEqual(list.rows.value, [{ id: "old" }]);
  query.value = "page=1&q=new";
  assert.deepEqual(list.rows.value, []);
  assert.equal(list.commit("page=1&q=", [{ id: "late" }]), false);
  assert.deepEqual(list.rows.value, []);
  assert.equal(list.commit(query.value, [{ id: "new" }]), true);
  assert.deepEqual(list.rows.value, [{ id: "new" }]);
  // A refresh of the same query preserves usable content while waiting.
  assert.equal(list.current.value, true);
  assert.equal(list.commit(query.value, [{ id: "refreshed" }]), true);
  assert.deepEqual(list.rows.value, [{ id: "refreshed" }]);
  query.value = "page=2&q=new";
  assert.deepEqual(list.rows.value, []);
  assert.equal(list.commit(query.value, []), true);
  assert.equal(
    list.current.value,
    true,
    "an empty successful result is current",
  );
});

const renderer = createRenderer<object, object>({
  patchProp() {},
  insert() {},
  remove() {},
  createElement: () => ({}),
  createText: () => ({}),
  createComment: () => ({}),
  setText() {},
  setElementText() {},
  parentNode: () => null,
  nextSibling: () => null,
});
test("leave confirmation handles cancel, repeated navigation, accept and teardown", async () => {
  let state!: ReturnType<typeof useLeaveConfirmation>;
  const app = renderer.createApp({
    setup() {
      state = useLeaveConfirmation();
      return () => h("div");
    },
  });
  app.mount({});
  const first = state.request();
  assert.equal(state.open.value, true);
  assert.equal(state.request(), first);
  state.resolve(false);
  assert.equal(await first, false);
  assert.equal(state.open.value, false);
  const accepted = state.request();
  state.resolve(true);
  assert.equal(await accepted, true);
  const abandoned = state.request();
  app.unmount();
  assert.equal(await abandoned, false);
  assert.equal(state.open.value, false);
});
