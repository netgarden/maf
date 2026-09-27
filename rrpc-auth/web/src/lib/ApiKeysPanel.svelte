<script lang="ts">
	import { onMount } from 'svelte';
	import { errorName } from './errors';
	import type { ApiKeyItem, ApiKeysApi } from './types';

	// Self-service API keys: a personal-access-token-style credential the
	// user can generate for scripts/agents to call the API non-interactively
	// (Authorization: Bearer <token>), instead of a browser session. Shown
	// alongside ProfilePage's own "Change password" card.
	let { api }: { api: ApiKeysApi } = $props();

	let keys = $state<ApiKeyItem[]>([]);
	let loading = $state(true);
	let loadError = $state('');

	async function load() {
		loadError = '';
		try {
			keys = (await api.list()).keys;
		} catch {
			loadError = 'Failed to load API keys.';
		} finally {
			loading = false;
		}
	}
	onMount(load);

	let showModal = $state(false);
	let name = $state('');
	let expiresAt = $state(''); // yyyy-mm-dd from <input type=date>, or empty (never)
	let allowedIps = $state(''); // one IP/CIDR per line, or empty (any source)
	let formError = $state('');
	let saving = $state(false);
	// Set right after create: the one and only time the plaintext token is shown.
	let freshToken = $state('');
	let copied = $state(false);

	function openModal() {
		name = '';
		expiresAt = '';
		allowedIps = '';
		formError = '';
		freshToken = '';
		copied = false;
		showModal = true;
	}

	function closeModal() {
		showModal = false;
	}

	async function create(event: SubmitEvent) {
		event.preventDefault();
		formError = '';
		saving = true;
		try {
			const created = await api.create({
				name,
				expiresAt: expiresAt ? new Date(expiresAt).toISOString() : undefined,
				allowedIps: allowedIps
					.split('\n')
					.map((l) => l.trim())
					.filter(Boolean)
			});
			freshToken = created.token;
			await load();
		} catch (err) {
			formError =
				errorName(err) === 'ApiKeyRestricted'
					? "An API key can't be used to create another one — log in normally to do this."
					: 'Failed to create the API key.';
		} finally {
			saving = false;
		}
	}

	async function copyToken() {
		try {
			await navigator.clipboard.writeText(freshToken);
			copied = true;
		} catch {
			// clipboard access can be denied; the token is still shown to copy by hand
		}
	}

	async function revoke(key: ApiKeyItem) {
		if (!confirm(`Revoke "${key.name}"? Anything using it will stop working immediately.`)) return;
		try {
			await api.delete(key.id);
			await load();
		} catch {
			loadError = 'Failed to revoke the key.';
		}
	}

	function fmt(iso?: string) {
		return iso ? new Date(iso).toLocaleString() : '';
	}
</script>

<div class="card mt-4" style="max-width: 32rem;">
	<div class="card-body">
		<div class="d-flex align-items-center mb-2">
			<h2 class="h5 card-title mb-0">API keys</h2>
			<button type="button" class="btn btn-sm btn-outline-primary ms-auto" onclick={openModal}>New key</button>
		</div>
		<p class="text-body-secondary small">
			A key lets a script or agent call the API as you, without logging in interactively. It has your full
			privilege — treat it like a password.
		</p>

		{#if loadError}<div class="alert alert-danger py-2">{loadError}</div>{/if}

		{#if loading}
			<div class="text-body-secondary small">Loading…</div>
		{:else if keys.length === 0}
			<div class="text-body-secondary small">No API keys yet.</div>
		{:else}
			<ul class="list-group list-group-flush">
				{#each keys as key (key.id)}
					<li class="list-group-item px-0">
						<div class="d-flex align-items-start">
							<div>
								<div class="fw-medium">{key.name}</div>
								<div class="small text-body-secondary font-monospace">{key.prefix}…</div>
								<div class="small text-body-secondary">
									Created {fmt(key.createdAt)}
									{#if key.lastUsedAt}· last used {fmt(key.lastUsedAt)}{:else}· never used{/if}
									{#if key.expiresAt}· expires {fmt(key.expiresAt)}{/if}
									{#if key.allowedIps.length}· restricted to {key.allowedIps.length} address{key.allowedIps.length === 1 ? '' : 'es'}{/if}
								</div>
							</div>
							<button type="button" class="btn btn-sm btn-outline-danger ms-auto" onclick={() => revoke(key)}>Revoke</button>
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</div>

{#if showModal}
	<div class="modal d-block" tabindex="-1" role="dialog">
		<div class="modal-dialog">
			<div class="modal-content">
				<form onsubmit={create}>
					<div class="modal-header">
						<h5 class="modal-title">New API key</h5>
						<button type="button" class="btn-close" onclick={closeModal} aria-label="Close"></button>
					</div>
					<div class="modal-body">
						{#if freshToken}
							<div class="alert alert-warning py-2">
								<strong>Copy this key now — it won't be shown again.</strong>
								<div class="input-group input-group-sm mt-2">
									<input class="form-control font-monospace" readonly value={freshToken} />
									<button type="button" class="btn btn-outline-secondary" onclick={copyToken}>{copied ? 'Copied' : 'Copy'}</button>
								</div>
							</div>
						{:else}
							<div class="mb-3">
								<label class="form-label" for="ak-name">Name</label>
								<input id="ak-name" class="form-control" bind:value={name} required maxlength="100" placeholder="e.g. deploy script" />
							</div>
							<div class="mb-3">
								<label class="form-label" for="ak-expires">Expires</label>
								<input id="ak-expires" class="form-control" type="date" bind:value={expiresAt} />
								<div class="form-text">Leave empty for a key that never expires.</div>
							</div>
							<div class="mb-3">
								<label class="form-label" for="ak-ips">Restrict to source IPs</label>
								<textarea
									id="ak-ips"
									class="form-control font-monospace"
									rows="3"
									bind:value={allowedIps}
									placeholder="one per line: 203.0.113.4 or 203.0.113.0/24 — empty means any source"
								></textarea>
							</div>
						{/if}
						{#if formError}<div class="alert alert-danger py-2 mt-3">{formError}</div>{/if}
					</div>
					<div class="modal-footer">
						<button type="button" class="btn btn-secondary" onclick={closeModal}>{freshToken ? 'Done' : 'Cancel'}</button>
						{#if !freshToken}
							<button type="submit" class="btn btn-primary" disabled={saving}>{saving ? 'Creating…' : 'Create'}</button>
						{/if}
					</div>
				</form>
			</div>
		</div>
	</div>
	<div class="modal-backdrop show"></div>
{/if}
