<script lang="ts">
	import { onMount } from 'svelte';
	import { errorName } from './errors';
	import type { AuthApi } from './types';

	// loginHref / forgotPasswordHref: the app's own routes (via resolve()).
	let {
		api,
		loginHref,
		forgotPasswordHref
	}: { api: AuthApi; loginHref: string; forgotPasswordHref: string } = $props();

	// Read directly from window.location rather than $app/state's `page`
	// store: `page.url.searchParams` throws during prerendering (adapter-
	// static bakes one static shell per route regardless of query string),
	// so this has to be client-only — onMount never runs during prerender.
	let token = $state('');
	let tokenChecked = $state(false);
	onMount(() => {
		token = new URLSearchParams(window.location.search).get('token') ?? '';
		tokenChecked = true;
	});

	let newPassword = $state('');
	let confirmPassword = $state('');
	let error = $state('');
	let success = $state(false);
	let submitting = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';

		if (newPassword !== confirmPassword) {
			error = 'New password and confirmation do not match.';
			return;
		}

		submitting = true;
		try {
			await api.confirmPasswordReset({ token, newPassword });
			success = true;
		} catch (err) {
			if (errorName(err) === 'InvalidResetToken') {
				error = 'This reset link is invalid or has expired. Request a new one.';
			} else {
				error = 'Failed to reset password.';
			}
		} finally {
			submitting = false;
		}
	}
</script>

<div class="d-flex vh-100 justify-content-center align-items-center bg-light">
	<div class="card shadow-sm" style="width: 22rem;">
		<div class="card-body p-4">
			<h1 class="h4 mb-4 text-center">Reset password</h1>

			{#if success}
				<div class="alert alert-success py-2" role="alert">
					Your password has been reset. You can now log in with your new password.
				</div>
				<a href={loginHref} class="btn btn-primary w-100">Back to login</a>
			{:else if !tokenChecked}
				<div class="d-flex justify-content-center">
					<div class="spinner-border" role="status">
						<span class="visually-hidden">Loading…</span>
					</div>
				</div>
			{:else if !token}
				<div class="alert alert-danger py-2" role="alert">
					This link is missing its reset token. Request a new one from the
					<a href={forgotPasswordHref}>forgot password</a> page.
				</div>
			{:else}
				<form onsubmit={submit}>
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
					<button class="btn btn-primary w-100" type="submit" disabled={submitting}>
						{submitting ? 'Resetting…' : 'Reset password'}
					</button>
				</form>
			{/if}
		</div>
	</div>
</div>
