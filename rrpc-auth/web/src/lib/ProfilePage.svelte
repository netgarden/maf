<script lang="ts">
	import ApiKeysPanel from './ApiKeysPanel.svelte';
	import { errorName } from './errors';
	import type { ApiKeysApi, Me, ProfileApi } from './types';

	let { api, user, apiKeys }: { api: ProfileApi; user: Me | null; apiKeys?: ApiKeysApi } = $props();

	let currentPassword = $state('');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let error = $state('');
	let success = $state(false);
	let saving = $state(false);

	async function changePassword(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		success = false;

		if (newPassword !== confirmPassword) {
			error = 'New password and confirmation do not match.';
			return;
		}

		saving = true;
		try {
			await api.changePassword({ currentPassword, newPassword });
			success = true;
			currentPassword = '';
			newPassword = '';
			confirmPassword = '';
		} catch (err) {
			if (errorName(err) === 'WrongPassword') {
				error = 'Current password is incorrect.';
			} else {
				error = 'Failed to change password.';
			}
		} finally {
			saving = false;
		}
	}
</script>

<h1 class="h3 mb-4">Profile</h1>

{#if user}
	<div class="card mb-4" style="max-width: 32rem;">
		<div class="card-body">
			<h2 class="h5 card-title">Account</h2>
			<dl class="row mb-0">
				<dt class="col-4">Username</dt>
				<dd class="col-8">{user.username}</dd>
				<dt class="col-4">Name</dt>
				<dd class="col-8">{user.firstName} {user.lastName}</dd>
				<dt class="col-4">Email</dt>
				<dd class="col-8">{user.email}</dd>
				<dt class="col-4">Role</dt>
				<dd class="col-8">{user.admin ? 'Administrator' : 'User'}</dd>
			</dl>
		</div>
	</div>
{/if}

<div class="card" style="max-width: 32rem;">
	<div class="card-body">
		<h2 class="h5 card-title">Change password</h2>
		<form onsubmit={changePassword}>
			<div class="mb-3">
				<label class="form-label" for="current-password">Current password</label>
				<input
					id="current-password"
					class="form-control"
					type="password"
					bind:value={currentPassword}
					autocomplete="current-password"
					required
				/>
			</div>
			<div class="mb-3">
				<label class="form-label" for="new-password">New password</label>
				<input
					id="new-password"
					class="form-control"
					type="password"
					bind:value={newPassword}
					autocomplete="new-password"
					required
				/>
			</div>
			<div class="mb-3">
				<label class="form-label" for="confirm-password">Confirm new password</label>
				<input
					id="confirm-password"
					class="form-control"
					type="password"
					bind:value={confirmPassword}
					autocomplete="new-password"
					required
				/>
			</div>
			{#if error}
				<div class="alert alert-danger py-2">{error}</div>
			{/if}
			{#if success}
				<div class="alert alert-success py-2">Password changed.</div>
			{/if}
			<button class="btn btn-primary" type="submit" disabled={saving}>
				{saving ? 'Saving…' : 'Change password'}
			</button>
		</form>
	</div>
</div>

{#if apiKeys}
	<ApiKeysPanel api={apiKeys} />
{/if}