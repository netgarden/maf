<script lang="ts">
	import { onMount } from 'svelte';
	import { Datatable, DatatableView, type Column } from '@netgarden/svelte-datatables';
	import type { EmailItem, MailerApi } from './types';

	// The app's generated `mailerApi.mailerService`.
	let { api }: { api: MailerApi } = $props();

	const STATUSES = ['queued', 'sending', 'sent', 'failed', 'cancelled'];

	let actingId = $state<string | null>(null);

	let testEmailAddress = $state('');
	let sendingTest = $state(false);
	let testEmailNotice = $state('');

	function isActionable(status: string): boolean {
		return status === 'queued' || status === 'failed';
	}

	async function retry(email: EmailItem) {
		actingId = email.id;
		table.error = '';
		try {
			await api.retry(email.id);
			await table.load();
		} catch {
			table.error = `Failed to retry the email to ${email.to.join(', ')}.`;
		} finally {
			actingId = null;
		}
	}

	async function cancel(email: EmailItem) {
		if (!confirm(`Cancel the queued email to ${email.to.join(', ')}?`)) return;
		actingId = email.id;
		table.error = '';
		try {
			await api.cancel(email.id);
			await table.load();
		} catch {
			table.error = `Failed to cancel the email to ${email.to.join(', ')}.`;
		} finally {
			actingId = null;
		}
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'sent':
				return 'text-bg-success';
			case 'failed':
				return 'text-bg-danger';
			case 'cancelled':
				return 'text-bg-secondary';
			case 'sending':
				return 'text-bg-info';
			default:
				return 'text-bg-warning';
		}
	}

	function formatTime(value: string): string {
		return value ? new Date(value).toLocaleString() : '—';
	}

	// The one timestamp that matters depends on where the email currently is
	// in its lifecycle — showing all four columns would mostly be empty cells.
	function timingLabel(email: EmailItem): string {
		switch (email.status) {
			case 'sent':
				return 'Sent';
			case 'cancelled':
				return 'Cancelled';
			case 'failed':
				return 'Gave up';
			default:
				return 'Next attempt';
		}
	}

	function timingValue(email: EmailItem): string {
		switch (email.status) {
			case 'sent':
				return email.sentAt;
			case 'cancelled':
				return email.cancelledAt;
			case 'failed':
				return email.gaveUpAt;
			default:
				return email.nextAttemptAt;
		}
	}

	const columns: Column<EmailItem>[] = [
		{ title: 'To', render: toCell },
		{ title: 'Subject', render: subjectCell, sortKey: 'subject' },
		{ title: 'Status', render: statusCell, sortKey: 'status' },
		{ title: 'Attempts', render: attemptsCell },
		{ title: 'Created', render: createdCell, sortKey: 'createdAt' },
		{ title: 'Timing', render: timingCell },
		{ title: 'Last error', render: lastErrorCell },
		{ title: 'Actions', render: actionsCell }
	];

	const table = new Datatable<EmailItem>(
		columns,
		(q) => api.list(q).then((r) => ({ items: r.items, pageInfo: r.pageInfo })),
		20
	);

	onMount(() => table.load());

	async function sendTest(event: SubmitEvent) {
		event.preventDefault();
		sendingTest = true;
		table.error = '';
		testEmailNotice = '';
		try {
			await api.sendTest({ to: testEmailAddress });
			testEmailNotice = 'Test email queued — see the table below for delivery status.';
			testEmailAddress = '';
			await table.load();
		} catch {
			table.error = 'Failed to send test email.';
		} finally {
			sendingTest = false;
		}
	}
</script>

{#snippet toCell(email: EmailItem)}
	{email.to.join(', ')}
{/snippet}

{#snippet subjectCell(email: EmailItem)}
	{email.subject}
{/snippet}

{#snippet statusCell(email: EmailItem)}
	<span class="badge {statusBadgeClass(email.status)}">{email.status}</span>
{/snippet}

{#snippet attemptsCell(email: EmailItem)}
	{email.attempts}
{/snippet}

{#snippet createdCell(email: EmailItem)}
	{formatTime(email.createdAt)}
{/snippet}

{#snippet timingCell(email: EmailItem)}
	<span class="text-muted small d-block">{timingLabel(email)}</span>
	{formatTime(timingValue(email))}
{/snippet}

{#snippet lastErrorCell(email: EmailItem)}
	{#if email.lastError}
		<span class="text-danger small" title={email.lastError}>
			{email.lastError.length > 40 ? email.lastError.slice(0, 40) + '…' : email.lastError}
		</span>
	{:else}
		<span class="text-muted">—</span>
	{/if}
{/snippet}

{#snippet actionsCell(email: EmailItem)}
	<div class="text-end">
		<button
			class="btn btn-sm btn-outline-secondary me-2"
			disabled={!isActionable(email.status) || actingId === email.id}
			onclick={() => retry(email)}
		>
			{actingId === email.id ? 'Working…' : 'Retry'}
		</button>
		<button
			class="btn btn-sm btn-outline-danger"
			disabled={!isActionable(email.status) || actingId === email.id}
			onclick={() => cancel(email)}
		>
			Cancel
		</button>
	</div>
{/snippet}

<div class="d-flex justify-content-between align-items-center mb-4">
	<h1 class="h3 mb-0">Mail queue</h1>
</div>

<form class="row g-2 align-items-center mb-3" onsubmit={sendTest}>
	<div class="col-auto">
		<input
			class="form-control"
			type="email"
			placeholder="Recipient email address"
			bind:value={testEmailAddress}
			required
		/>
	</div>
	<div class="col-auto">
		<button type="submit" class="btn btn-outline-primary" disabled={sendingTest}>
			{sendingTest ? 'Sending…' : 'Send test email'}
		</button>
	</div>
	{#if testEmailNotice}
		<div class="col-auto">
			<span class="text-success small">{testEmailNotice}</span>
		</div>
	{/if}
</form>

<div class="row g-2 mb-3">
	<div class="col-auto">
		<select
			class="form-select"
			value={table.filters.status ?? ''}
			onchange={(e) => table.setFilter('status', e.currentTarget.value)}
		>
			<option value="">All statuses</option>
			{#each STATUSES as s (s)}
				<option value={s}>{s}</option>
			{/each}
		</select>
	</div>
	<div class="col-auto">
		<input
			class="form-control"
			type="search"
			placeholder="Search subject…"
			value={table.filters.subject ?? ''}
			oninput={(e) => table.setFilter('subject', e.currentTarget.value)}
		/>
	</div>
</div>

<DatatableView {table} />
