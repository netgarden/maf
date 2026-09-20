// errorName returns the server-side error name (e.g. 'UserAlreadyExists')
// carried by an rrpc RRPCError, matched structurally so this library needn't
// import the app's vendored rrpc.ts.
export function errorName(e: unknown): string | undefined {
	return (e as { error?: string } | null)?.error;
}
