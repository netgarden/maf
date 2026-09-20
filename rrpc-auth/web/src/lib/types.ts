import type { DatatableQuery, PageInfo } from '@netgarden/svelte-datatables';

// Structural mirror of the generated rrpc types for maf/rrpc-auth's Users
// service (rpc/def/users.rrpc). The library deliberately does not import an
// app's generated client: any app's `authApi.usersService` satisfies
// UsersApi as long as it was generated from the same definition.
export type UserItem = {
	id: string;
	username: string;
	email: string;
	firstName: string;
	lastName: string;
	admin: boolean;
	active: boolean;
};

export type CreateUserRequest = {
	username: string;
	password: string;
	email: string;
	firstName: string;
	lastName: string;
	admin: boolean;
	sendCredentialsEmail: boolean;
};

export type UpdateUserRequest = {
	username: string;
	email: string;
	firstName: string;
	lastName: string;
	admin: boolean;
	active: boolean;
};

export interface UsersApi {
	list(request: DatatableQuery): Promise<{ users: UserItem[]; pageInfo: PageInfo }>;
	create(request: CreateUserRequest): Promise<UserItem>;
	update(id: string, request: UpdateUserRequest): Promise<void>;
	delete(id: string): Promise<void>;
}

// ---- Providers (providers.rrpc / oidc.rrpc) ----

export type ProviderItem = {
	id: string;
	type: string;
	slug: string;
	name: string;
	enabled: boolean;
};

export type OidcProviderDetail = {
	id: string;
	slug: string;
	name: string;
	issuerUrl: string;
	clientId: string;
	scopes: string;
	enabled: boolean;
	adminClaimPath: string;
	adminClaimValues: string[];
};

export type OidcProviderInput = {
	name: string;
	issuerUrl: string;
	clientId: string;
	clientSecret: string;
	scopes: string;
	enabled: boolean;
	adminClaimPath: string;
	adminClaimValues: string[];
};

export type CreateOidcProviderRequest = OidcProviderInput & { slug: string };

export interface ProvidersApi {
	list(request: DatatableQuery): Promise<{ providers: ProviderItem[]; pageInfo: PageInfo }>;
	setEnabled(id: string, request: { enabled: boolean }): Promise<void>;
	delete(id: string): Promise<void>;
}

// The generated client names this service `oIDCProvidersService`.
export interface OidcProvidersApi {
	get(id: string): Promise<OidcProviderDetail>;
	create(request: CreateOidcProviderRequest): Promise<OidcProviderDetail>;
	update(id: string, request: OidcProviderInput): Promise<OidcProviderDetail>;
}

export type PublicProvider = { type: string; slug: string; name: string };

export interface PublicProvidersApi {
	list(): Promise<{ providers: PublicProvider[] }>;
}

// ---- Auth / profile (auth.rrpc / profile.rrpc) ----

export type Me = {
	id: string;
	username: string;
	email: string;
	firstName: string;
	lastName: string;
	admin: boolean;
};

export interface AuthApi {
	login(request: { username: string; password: string }): Promise<{ accessToken: string }>;
	logout(): Promise<void>;
	refresh(): Promise<{ accessToken: string }>;
	me(): Promise<Me>;
	requestPasswordReset(request: { username: string }): Promise<void>;
	confirmPasswordReset(request: { token: string; newPassword: string }): Promise<void>;
}

export interface ProfileApi {
	changePassword(request: { currentPassword: string; newPassword: string }): Promise<void>;
}
