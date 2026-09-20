<script lang="ts">
	import { onMount } from 'svelte';
	import type { PublicProvider, PublicProvidersApi } from './types';

	let {
		title,
		login,
		providersApi,
		forgotPasswordHref = undefined,
		collapseLocalLogin = false
	}: {
		title: string;
		// The app's auth store login (see createAuthStore); throws on bad credentials.
		login: (username: string, password: string) => Promise<void>;
		// The app's generated `authApi.publicProvidersService`.
		providersApi: PublicProvidersApi;
		// Shows a "Forgot password?" link; omit for apps without the mailer.
		forgotPasswordHref?: string;
		// Hides the username/password form behind a toggle, for apps where SSO
		// is the primary login and local accounts are break-glass only.
		collapseLocalLogin?: boolean;
	} = $props();

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let submitting = $state(false);
	let showLocalLogin = $state(false);

	let providers = $state<PublicProvider[]>([]);
	let providersError = $state('');
	let providersLoaded = $state(false);

	onMount(async () => {
		try {
			providers = (await providersApi.list()).providers;
		} catch {
			// Best-effort — a logged-out visitor should still be able to use
			// password login even if this fails. Only surfaced when SSO is the
			// primary way in.
			providersError = 'Failed to load login providers.';
		} finally {
			providersLoaded = true;
		}
	});

	// Plain browser navigation, not an RPC call the frontend awaits — the
	// server responds with a redirect to the provider's own login page, and
	// the whole point is for the browser's address bar to actually go there.
	function providerLoginUrl(provider: PublicProvider): string {
		return `/api/auth/${provider.type}/${encodeURIComponent(provider.slug)}/login`;
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		submitting = true;
		try {
			await login(username, password);
		} catch {
			error = 'Invalid username or password.';
		} finally {
			submitting = false;
		}
	}

	let formVisible = $derived(!collapseLocalLogin || showLocalLogin);
</script>

<div class="d-flex vh-100 justify-content-center align-items-center bg-body-tertiary px-3">
	<div class="card shadow-sm" style="width: 100%; max-width: 22rem;">
		<div class="card-body p-4">
			<h1 class="h4 mb-4 text-center">{title}</h1>

			{#if collapseLocalLogin && providersError}
				<div class="alert alert-danger py-2" role="alert">{providersError}</div>
			{/if}

			{#if collapseLocalLogin && providersLoaded && providers.length > 0}
				<div class="d-grid gap-2 mb-3">
					{#each providers as provider (provider.type + ':' + provider.slug)}
						<!-- Server route, not a SvelteKit page — resolve() doesn't apply. -->
						<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
						<a class="btn btn-primary" href={providerLoginUrl(provider)}>
							Log in with {provider.name}
						</a>
					{/each}
				</div>
			{/if}

			{#if collapseLocalLogin}
				<button
					type="button"
					class="btn btn-link btn-sm w-100 text-muted"
					onclick={() => (showLocalLogin = !showLocalLogin)}
				>
					{showLocalLogin ? 'Hide local admin login' : 'Local admin login'}
				</button>
			{/if}

			{#if formVisible}
				<form onsubmit={submit} class={collapseLocalLogin ? 'mt-3' : ''}>
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
					<div class="mb-3">
						<label class="form-label" for="password">Password</label>
						<input
							id="password"
							class="form-control"
							type="password"
							bind:value={password}
							autocomplete="current-password"
							required
						/>
					</div>
					{#if error}
						<div class="alert alert-danger py-2" role="alert">{error}</div>
					{/if}
					<button
						class="btn {collapseLocalLogin ? 'btn-outline-secondary' : 'btn-primary'} w-100"
						type="submit"
						disabled={submitting}
					>
						{submitting ? 'Logging in…' : 'Log in'}
					</button>
					{#if forgotPasswordHref}
						<a href={forgotPasswordHref} class="d-block text-center mt-3 small">Forgot password?</a>
					{/if}
				</form>
			{/if}

			{#if !collapseLocalLogin && providersLoaded && providers.length > 0}
				<div class="d-flex align-items-center my-3">
					<hr class="flex-grow-1" />
					<span class="mx-2 small text-muted">or</span>
					<hr class="flex-grow-1" />
				</div>
				<div class="d-grid gap-2">
					{#each providers as provider (provider.type + ':' + provider.slug)}
						<!-- Server route, not a SvelteKit page — resolve() doesn't apply. -->
						<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
						<a href={providerLoginUrl(provider)} class="btn btn-outline-secondary">
							Sign in with {provider.name}
						</a>
					{/each}
				</div>
			{/if}
		</div>
	</div>
</div>
