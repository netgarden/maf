<script lang="ts">
	import { onMount } from 'svelte';
	import { Datatable, DatatableView, type Column } from '@netgarden/svelte-datatables';
	import { errorName } from './errors';
	import type { UserItem as User, UsersApi } from './types';

	let {
		api,
		currentUserId = undefined,
		allowCredentialsEmail = false
	}: {
		// The app's generated `authApi.usersService`.
		api: UsersApi;
		// Disables the row's Delete button so admins can't delete themselves.
		currentUserId?: string;
		// Shows the "email these credentials" checkbox on create. Only enable
		// when the app has the mailer module wired up.
		allowCredentialsEmail?: boolean;
	} = $props();

	type FormState = {
		username: string;
		password: string;
		email: string;
		firstName: string;
		lastName: string;
		admin: boolean;
		active: boolean;
		sendCredentialsEmail: boolean;
	};

	function emptyForm(): FormState {
		return {
			username: '',
			password: '',
			email: '',
			firstName: '',
			lastName: '',
			admin: false,
			active: true,
			sendCredentialsEmail: false
		};
	}

	const columns: Column<User>[] = [
		{ title: 'Username', render: usernameCell, sortKey: 'username' },
		{ title: 'Name', render: nameCell },
		{ title: 'Email', render: emailCell, sortKey: 'email' },
		{ title: 'Role', render: roleCell, sortKey: 'admin' },
		{ title: 'Status', render: statusCell, sortKey: 'active' },
		{ title: 'Actions', render: actionsCell }
	];

	const table = new Datatable<User>(columns, (q) =>
		api.list(q).then((r) => ({ items: r.users, pageInfo: r.pageInfo }))
	);

	let showModal = $state(false);
	let editingId = $state<string | null>(null);
	let form = $state<FormState>(emptyForm());
	let saving = $state(false);
	let formError = $state('');

	let deletingId = $state<string | null>(null);

	onMount(() => table.load());

	function openCreate() {
		editingId = null;
		form = emptyForm();
		formError = '';
		showModal = true;
	}

	function openEdit(user: User) {
		editingId = user.id;
		form = {
			username: user.username,
			password: '',
			email: user.email,
			firstName: user.firstName,
			lastName: user.lastName,
			admin: user.admin,
			active: user.active,
			sendCredentialsEmail: false
		};
		formError = '';
		showModal = true;
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
				await api.update(editingId, {
					username: form.username,
					email: form.email,
					firstName: form.firstName,
					lastName: form.lastName,
					admin: form.admin,
					active: form.active
				});
			} else {
				await api.create({
					username: form.username,
					password: form.password,
					email: form.email,
					firstName: form.firstName,
					lastName: form.lastName,
					admin: form.admin,
					sendCredentialsEmail: allowCredentialsEmail && form.sendCredentialsEmail
				});
			}
			showModal = false;
			await table.load();
		} catch (err) {
			if (errorName(err) === 'UserAlreadyExists') {
				formError = 'That username is already taken.';
			} else {
				formError = 'Failed to save user.';
			}
		} finally {
			saving = false;
		}
	}

	async function remove(user: User) {
		if (!confirm(`Delete user "${user.username}"?`)) return;
		deletingId = user.id;
		table.error = '';
		try {
			await api.delete(user.id);
			await table.load();
		} catch {
			table.error = 'Failed to delete user.';
		} finally {
			deletingId = null;
		}
	}
</script>

{#snippet usernameCell(user: User)}
	{user.username}
{/snippet}

{#snippet nameCell(user: User)}
	{user.firstName} {user.lastName}
{/snippet}

{#snippet emailCell(user: User)}
	{user.email}
{/snippet}

{#snippet roleCell(user: User)}
	{#if user.admin}<span class="badge text-bg-primary">Admin</span>{:else}User{/if}
{/snippet}

{#snippet statusCell(user: User)}
	<span class="badge {user.active ? 'text-bg-success' : 'text-bg-secondary'}">
		{user.active ? 'Active' : 'Inactive'}
	</span>
{/snippet}

{#snippet actionsCell(user: User)}
	<div class="text-end">
		<button class="btn btn-sm btn-outline-secondary me-2" onclick={() => openEdit(user)}> Edit </button>
		<button
			class="btn btn-sm btn-outline-danger"
			disabled={deletingId === user.id || user.id === currentUserId}
			title={user.id === currentUserId ? "You can't delete your own account" : ''}
			onclick={() => remove(user)}
		>
			{deletingId === user.id ? 'Deleting…' : 'Delete'}
		</button>
	</div>
{/snippet}

<div class="d-flex justify-content-between align-items-center mb-4">
	<h1 class="h3 mb-0">Users</h1>
	<button class="btn btn-primary" onclick={openCreate}>Add user</button>
</div>

<div class="row g-2 mb-3">
	<div class="col-auto">
		<input
			class="form-control form-control-sm"
			type="search"
			placeholder="Search username…"
			value={table.filters.username ?? ''}
			oninput={(e) => table.setFilter('username', e.currentTarget.value)}
		/>
	</div>
	<div class="col-auto">
		<input
			class="form-control form-control-sm"
			type="search"
			placeholder="Search email…"
			value={table.filters.email ?? ''}
			oninput={(e) => table.setFilter('email', e.currentTarget.value)}
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
						<h5 class="modal-title">{editingId ? 'Edit user' : 'Add user'}</h5>
						<button type="button" class="btn-close" onclick={closeModal} aria-label="Close"></button>
					</div>
					<div class="modal-body">
						<div class="mb-3">
							<label class="form-label" for="user-username">Username</label>
							<input
								id="user-username"
								class="form-control"
								type="text"
								bind:value={form.username}
								required
							/>
						</div>
						{#if !editingId}
							<div class="mb-3">
								<label class="form-label" for="user-password">Password</label>
								<input
									id="user-password"
									class="form-control"
									type="password"
									bind:value={form.password}
									autocomplete="new-password"
									required
								/>
							</div>
							{#if allowCredentialsEmail}
							<div class="form-check mb-3">
								<input
									id="user-send-credentials-email"
									class="form-check-input"
									type="checkbox"
									bind:checked={form.sendCredentialsEmail}
								/>
								<label class="form-check-label" for="user-send-credentials-email">
									Email these credentials to the user
								</label>
							</div>
							{/if}
						{/if}
						<div class="mb-3">
							<label class="form-label" for="user-email">Email</label>
							<input
								id="user-email"
								class="form-control"
								type="email"
								bind:value={form.email}
								required
							/>
						</div>
						<div class="row">
							<div class="col mb-3">
								<label class="form-label" for="user-first-name">First name</label>
								<input
									id="user-first-name"
									class="form-control"
									type="text"
									bind:value={form.firstName}
								/>
							</div>
							<div class="col mb-3">
								<label class="form-label" for="user-last-name">Last name</label>
								<input
									id="user-last-name"
									class="form-control"
									type="text"
									bind:value={form.lastName}
								/>
							</div>
						</div>
						<div class="form-check">
							<input
								id="user-admin"
								class="form-check-input"
								type="checkbox"
								bind:checked={form.admin}
							/>
							<label class="form-check-label" for="user-admin">Administrator</label>
						</div>
						{#if editingId}
							<div class="form-check">
								<input
									id="user-active"
									class="form-check-input"
									type="checkbox"
									bind:checked={form.active}
								/>
								<label class="form-check-label" for="user-active">Active</label>
							</div>
						{/if}
						{#if formError}
							<div class="alert alert-danger py-2 mt-3">{formError}</div>
						{/if}
					</div>
					<div class="modal-footer">
						<button type="button" class="btn btn-secondary" onclick={closeModal}>Cancel</button>
						<button type="submit" class="btn btn-primary" disabled={saving}>
							{saving ? 'Saving…' : 'Save'}
						</button>
					</div>
				</form>
			</div>
		</div>
	</div>
	<div class="modal-backdrop show"></div>
{/if}
