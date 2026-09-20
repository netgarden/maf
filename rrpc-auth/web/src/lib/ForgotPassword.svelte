<script lang="ts">
	import type { AuthApi } from './types';

	// loginHref: where "Back to login" goes (the app passes loginHref).
	let { api, loginHref }: { api: AuthApi; loginHref: string } = $props();

	let username = $state('');
	let submitting = $state(false);
	let submitted = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		submitting = true;
		try {
			await api.requestPasswordReset({ username });
		} catch {
			// Deliberately ignored — see the success message below: this
			// endpoint never reveals whether the account exists, so the UI
			// shows the same outcome whether or not the request actually
			// found a user or a mailer is even configured.
		} finally {
			submitting = false;
			submitted = true;
		}
	}
</script>

<div class="d-flex vh-100 justify-content-center align-items-center bg-light">
	<div class="card shadow-sm" style="width: 22rem;">
		<div class="card-body p-4">
			<h1 class="h4 mb-4 text-center">Forgot password</h1>

			{#if submitted}
				<div class="alert alert-success py-2" role="alert">
					If that account exists, we've sent a password reset email.
				</div>
				<a href={loginHref} class="btn btn-primary w-100">Back to login</a>
			{:else}
				<form onsubmit={submit}>
					<div class="mb-3">
						<label class="form-label" for="username">Username</label>
						<input
							id="username"
							class="form-control"
							type="text"
							bind:value={username}
							autocomplete="username"
							required
						/>
					</div>
					<button class="btn btn-primary w-100" type="submit" disabled={submitting}>
						{submitting ? 'Sending…' : 'Send reset link'}
					</button>
					<a href={loginHref} class="d-block text-center mt-3 small">Back to login</a>
				</form>
			{/if}
		</div>
	</div>
</div>
