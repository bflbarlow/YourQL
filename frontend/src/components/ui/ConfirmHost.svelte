<script>
  // Mount once near the app root; services all promise-based confirm() calls.
  import ConfirmDialog from './ConfirmDialog.svelte'
  import { _getRequest, _resolve } from '../../lib/confirm.svelte.js'

  let req = $derived(_getRequest())
</script>

<ConfirmDialog
  open={!!req}
  title={req?.title || ''}
  body={req?.body || ''}
  confirmLabel={req?.confirmLabel || 'Confirm'}
  danger={req?.danger || false}
  onconfirm={() => _resolve(true)}
  onclose={() => _resolve(false)}
/>
