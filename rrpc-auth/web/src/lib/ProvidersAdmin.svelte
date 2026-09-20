<script lang="ts">
	import { onMount } from 'svelte';
	import { Datatable, DatatableView, type Column } from '@netgarden/svelte-datatables';
	import { errorName } from './errors';
	import type { OidcProvidersApi, ProviderItem, ProvidersApi } from './types';

	let { api, oidcApi }: { api: ProvidersApi; oidcApi: OidcProvidersApi } = $props();

	// The generic row shape (id/type/slug/name/enabled) every provider has,
	// regardless of protocol — see Providers.list. Editing/creating is only
	// wired up for type "oidc" today (see actionsCell) since that's the
	// only protocol with its own admin service so far.

	type FormState = {
		slug: string;
		name: string;
		issuerUrl: string;
		clientId: string;
		clientSecret: string;
		scopes: string;
		enabled: boolean;
		adminClaimPath: string;
		adminClaimValues: string;
	};

	function emptyForm(): FormState {
		return {
			slug: '',
			name: '',
			issuerUrl: '',
			clientId: '',
			clientSecret: '',
			scopes: '',
			enabled: true,
			adminClaimPath: '',
			adminClaimValues: ''
		};
	}

	// adminClaimValues is a []string on the wire; the admin UI edits it as a
	// single comma-separated field rather than a dynamic list-of-inputs —
	// simplest thing that works for what's usually 1-3 group/role names.
	function parseAdminClaimValues(raw: string): string[] {
		return raw
			.split(',')
			.map((v) => v.trim())
			.filter((v) => v.length > 0);
	}

	const columns: Column<ProviderItem>[] = [
		{ title: 'Type', render: typeCell, sortKey: 'type' },
		{ title: 'Slug', render: slugCell, sortKey: 'slug' },
		{ title: 'Name', render: nameCell, sortKey: 'name' },
		{ title: 'Status', render: statusCell },
		{ title: 'Actions', render: actionsCell }
	];

	const table = new Datatable<ProviderItem>(columns, (q) =>
		api.list(q).then((r) => ({ items: r.providers, pageInfo: r.pageInfo }))
	);

	let showModal = $state(false);
	let editingId = $state<string | null>(null);
	let form = $state<FormState>(emptyForm());
	let saving = $state(false);
	let formError = $state('');
	let loadingDetail = $state(false);

	let deletingId = $state<string | null>(null);
	let togglingId = $state<string | null>(null);

	onMount(() => table.load());

	function openCreate() {
		editingId = null;
		form = emptyForm();
		formError = '';
		showModal = true;
	}

	async function openEdit(provider: ProviderItem) {
		editingId = provider.id;
		form = emptyForm();
		formError = '';
		showModal = true;
		loadingDetail = true;
		try {
			const detail = await oidcApi.get(provider.id);
			form = {
				slug: detail.slug,
				name: detail.name,
				issuerUrl: detail.issuerUrl,
				clientId: detail.clientId,
				clientSecret: '',
				scopes: detail.scopes,
				enabled: detail.enabled,
				adminClaimPath: detail.adminClaimPath,
				adminClaimValues: detail.adminClaimValues.join(', ')
			};
		} catch {
			formError = 'Failed to load provider details.';
		} finally {
			loadingDetail = false;
		}
	}

	function closeModal() {
		showModal = false;
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		saving = true;
		formError = '';
		try {
			if (editingId) {
				await oidcApi.update(editingId, {
					name: form.name,
					issuerUrl: form.issuerUrl,
					clientId: form.clientId,
					clientSecret: form.clientSecret,
					scopes: form.scopes,
					enabled: form.enabled,
					adminClaimPath: form.adminClaimPath,
					adminClaimValues: parseAdminClaimValues(form.adminClaimValues)
				});
			} else {
				await oidcApi.create({
					slug: form.slug,
					name: form.name,
					issuerUrl: form.issuerUrl,
					clientId: form.clientId,
					clientSecret: form.clientSecret,
					scopes: form.scopes,
					enabled: form.enabled,
					adminClaimPath: form.adminClaimPath,
					adminClaimValues: parseAdminClaimValues(form.adminClaimValues)
				});
			}
			showModal = false;
			await table.load();
		} catch (err) {
			if (errorName(err) === 'OIDCProviderAlreadyExists') {
				formError = 'That slug is already taken.';
			} else {
				formError = 'Failed to save provider.';
			}
		} finally {
			saving = false;
		}
	}

	async function remove(provider: ProviderItem) {
		if (!confirm(`Delete provider "${provider.name}"?`)) return;
		deletingId = provider.id;
		table.error = '';
		try {
			await api.delete(provider.id);
			await table.load();
		} catch {
			table.error = 'Failed to delete provider.';
		} finally {
			deletingId = null;
		}
	}

	async function toggleEnabled(provider: ProviderItem) {
		togglingId = provider.id;
		table.error = '';
		try {
			await api.setEnabled(provider.id, { enabled: !provider.enabled });
			await table.load();
		} catch {
			table.error = 'Failed to update provider.';
		} finally {
			togglingId = null;
		}
	}
</script>

{#snippet typeCell(provider: ProviderItem)}
	<span class="badge text-bg-secondary text-uppercase">{provider.type}</span>
{/snippet}

{#snippet slugCell(provider: ProviderItem)}
	<code>{provider.slug}</code>
{/snippet}

{#snippet nameCell(provider: ProviderItem)}
	{provider.name}
{/snippet}

{#snippet statusCell(provider: ProviderItem)}
	<span class="badge {provider.enabled ? 'text-bg-success' : 'text-bg-secondary'}">
		{provider.enabled ? 'Enabled' : 'Disabled'}
	</span>
{/snippet}

{#snippet actionsCell(provider: ProviderItem)}
	<div class="text-end">
		<button
			class="btn btn-sm btn-outline-secondary me-2"
			disabled={togglingId === provider.id}
			onclick={() => toggleEnabled(provider)}
		>
			{togglingId === provider.id ? 'Updating…' : provider.enabled ? 'Disable' : 'Enable'}
		</button>
		{#if provider.type === 'oidc'}
			<button class="btn btn-sm btn-outline-secondary me-2" onclick={() => openEdit(provider)}>
				Edit
			</button>
		{/if}
		<button
			class="btn btn-sm btn-outline-danger"
			disabled={deletingId === provider.id}
			onclick={() => remove(provider)}
		>
			{deletingId === provider.id ? 'Deleting…' : 'Delete'}
		</button>
	</div>
{/snippet}

<div class="d-flex justify-content-between align-items-center mb-4">
	<h1 class="h3 mb-0">Providers</h1>
	<button class="btn btn-primary" onclick={openCreate}>Add OIDC provider</button>
</div>

<div class="row g-2 mb-3">
	<div class="col-auto">
		<input
			class="form-control form-control-sm"
			type="search"
			placeholder="Search slug…"
			value={table.filters.slug ?? ''}
			oninput={(e) => table.setFilter('slug', e.currentTarget.value)}
		/>
	</div>
	<div class="col-auto">
		<input
			class="form-control form-control-sm"
			type="search"
			placeholder="Search name…"
			value={table.filters.name ?? ''}
			oninput={(e) => table.setFilter('name', e.currentTarget.value)}
		/>
	</div>
</div>

<DatatableView {table} />

{#if showModal}
	<div class="modal d-block" tabindex="-1" role="dialog">
		<div class="modal-dialog">
			<div class="modal-content">
				<form onsubmit={save}>
					<div class="modal-header">
						<h5 class="modal-title">{editingId ? 'Edit OIDC provider' : 'Add OIDC provider'}</h5>
						<button type="button" class="btn-close" onclick={closeModal} aria-label="Close"></button>
					</div>
					<div class="modal-body">
						{#if loadingDetail}
							<p class="text-muted">Loading…</p>
						{:else}
							<div class="mb-3">
								<label class="form-label" for="provider-slug">Slug</label>
								{#if editingId}
									<input
										id="provider-slug"
										class="form-control"
										type="text"
										value={form.slug}
										disabled
									/>
									<div class="form-text">Slug can't be changed after a provider is created.</div>
								{:else}
									<input
										id="provider-slug"
										class="form-control"
										type="text"
										bind:value={form.slug}
										placeholder="okta"
										required
									/>
									<div class="form-text">Used in the login URL: /api/auth/oidc/&lcub;slug&rcub;/login</div>
								{/if}
							</div>
							<div class="mb-3">
								<label class="form-label" for="provider-name">Display name</label>
								<input
									id="provider-name"
									class="form-control"
									type="text"
									bind:value={form.name}
									placeholder="Okta"
									required
								/>
							</div>
							<div class="mb-3">
								<label class="form-label" for="provider-issuer-url">Issuer URL</label>
								<input
									id="provider-issuer-url"
									class="form-control"
									type="url"
									bind:value={form.issuerUrl}
									placeholder="https://your-tenant.okta.com"
									required
								/>
							</div>
							<div class="row">
								<div class="col mb-3">
									<label class="form-label" for="provider-client-id">Client ID</label>
									<input
										id="provider-client-id"
										class="form-control"
										type="text"
										bind:value={form.clientId}
										required
									/>
								</div>
								<div class="col mb-3">
									<label class="form-label" for="provider-client-secret">Client secret</label>
									<input
										id="provider-client-secret"
										class="form-control"
										type="password"
										bind:value={form.clientSecret}
										autocomplete="new-password"
										placeholder={editingId ? 'Leave blank to keep the current secret' : ''}
										required={!editingId}
									/>
								</div>
							</div>
							<div class="mb-3">
								<label class="form-label" for="provider-scopes">Scopes</label>
								<input
									id="provider-scopes"
									class="form-control"
									type="text"
									bind:value={form.scopes}
									placeholder="openid profile email"
								/>
							</div>
							<div class="row">
								<div class="col mb-3">
									<label class="form-label" for="provider-admin-claim-path">
										Admin claim (optional)
									</label>
									<input
										id="provider-admin-claim-path"
										class="form-control"
										type="text"
										bind:value={form.adminClaimPath}
										placeholder="groups"
									/>
								</div>
								<div class="col mb-3">
									<label class="form-label" for="provider-admin-claim-values">
										Admin claim values
									</label>
									<input
										id="provider-admin-claim-values"
										class="form-control"
										type="text"
										bind:value={form.adminClaimValues}
										placeholder="admins, platform-team"
										disabled={!form.adminClaimPath}
									/>
								</div>
							</div>
							<div class="form-text mb-3">
								When set, a user whose ID token carries this claim with one of these values is made
								an admin automatically on every login; left blank, admin status is always managed
								manually.
							</div>
							<div class="form-check">
								<input
									id="provider-enabled"
									class="form-check-input"
									type="checkbox"
									bind:checked={form.enabled}
								/>
								<label class="form-check-label" for="provider-enabled">Enabled</label>
							</div>
						{/if}
						{#if formError}
							<div class="alert alert-danger py-2 mt-3">{formError}</div>
						{/if}
					</div>
					<div class="modal-footer">
						<button type="button" class="btn btn-secondary" onclick={closeModal}>Cancel</button>
						<button type="submit" class="btn btn-primary" disabled={saving || loadingDetail}>
							{saving ? 'Saving…' : 'Save'}
						</button>
					</div>
				</form>
			</div>
		</div>
	</div>
	<div class="modal-backdrop show"></div>
{/if}
