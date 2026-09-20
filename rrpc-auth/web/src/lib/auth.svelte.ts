import type { AuthApi, Me } from './types';

// The access token is short-lived (auth.token.ttl, 15min by default - see
// maf/auth's GetConfigSchema) and nothing else re-mints it, so without a
// periodic refresh any tab left open past that TTL goes silently
// unauthenticated: every RPC call 401s from then on with no retry. Comfortably
// under the default TTL so it still lands in time even with a backgrounded
// tab's timer throttling.
const REFRESH_INTERVAL_MS = 5 * 60 * 1000;

export class AuthStore {
	user = $state<Me | null>(null);
	loading = $state(true);

	#api: AuthApi;
	#setAccessToken: (token: string | null) => void;
	#refreshHandle: ReturnType<typeof setInterval> | undefined;

	constructor(api: AuthApi, setAccessToken: (token: string | null) => void) {
		this.#api = api;
		this.#setAccessToken = setAccessToken;
	}

	// A valid session cookie (set by a previous login) lets us silently mint
	// a fresh access token without asking for credentials again.
	async restore() {
		try {
			const { accessToken } = await this.#api.refresh();
			this.#setAccessToken(accessToken);
			this.user = await this.#api.me();
			this.#startRefreshTimer();
		} catch {
			this.#setAccessToken(null);
			this.user = null;
		} finally {
			this.loading = false;
		}
	}

	async login(username: string, password: string) {
		const { accessToken } = await this.#api.login({ username, password });
		this.#setAccessToken(accessToken);
		this.user = await this.#api.me();
		this.#startRefreshTimer();
	}

	async logout() {
		await this.#api.logout();
		this.#stopRefreshTimer();
		this.#setAccessToken(null);
		this.user = null;
	}

	#startRefreshTimer() {
		this.#stopRefreshTimer();
		this.#refreshHandle = setInterval(async () => {
			try {
				const { accessToken } = await this.#api.refresh();
				this.#setAccessToken(accessToken);
			} catch {
				// Refresh cookie expired/revoked - drop to logged-out state
				// instead of leaving every subsequent call 401ing forever.
				this.#stopRefreshTimer();
				this.#setAccessToken(null);
				this.user = null;
			}
		}, REFRESH_INTERVAL_MS);
	}

	#stopRefreshTimer() {
		if (this.#refreshHandle) {
			clearInterval(this.#refreshHandle);
			this.#refreshHandle = undefined;
		}
	}
}

// createAuthStore wires the store to an app's generated `authApi.authService`
// and its `setAccessToken` (which must push the token to every generated
// client the app owns).
export function createAuthStore(
	api: AuthApi,
	setAccessToken: (token: string | null) => void
): AuthStore {
	return new AuthStore(api, setAccessToken);
}
