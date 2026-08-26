// YourQL — app-wide promise-based confirm service.
// Any component:  import { confirm } from '$lib/confirm.svelte.js'
//                 if (await confirm({ title, body, danger })) { ... }
// Exactly ONE <ConfirmHost /> is mounted near the app root (App.svelte and
// MinimalistView.svelte each render one, since either can be active).

const state = $state({ req: null });

export function confirm({
  title = "Are you sure?",
  body = "",
  confirmLabel = "Confirm",
  danger = false,
} = {}) {
  return new Promise((resolve) => {
    state.req = { title, body, confirmLabel, danger, resolve };
  });
}

/** Internal: rendered by ConfirmHost. */
export function _getRequest() {
  return state.req;
}

export function _resolve(value) {
  const r = state.req;
  state.req = null;
  r?.resolve?.(value);
}
