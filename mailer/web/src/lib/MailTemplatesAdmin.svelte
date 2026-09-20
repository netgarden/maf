<script lang="ts">
	import { onMount } from 'svelte';
	import type { MailerApi, TemplateItem } from './types';

	// The app's generated `mailerApi.mailerService`.
	let { api }: { api: MailerApi } = $props();

	type FormState = {
		subject: string;
		bodyText: string;
		bodyHtml: string;
	};

	let templates = $state<TemplateItem[]>([]);
	let loading = $state(true);
	let error = $state('');

	let showModal = $state(false);
	let editingId = $state<string | null>(null);
	let form = $state<FormState>({ subject: '', bodyText: '', bodyHtml: '' });
	let saving = $state(false);
	let formError = $state('');

	let resettingId = $state<string | null>(null);

	async function load() {
		loading = true;
		error = '';
		try {
			const res = await api.listTemplates();
			templates = res.items;
		} catch {
			error = 'Failed to load templates.';
		} finally {
			loading = false;
		}
	}

	onMount(load);

	function openEdit(template: TemplateItem) {
		editingId = template.id;
		form = {
			subject: template.subject,
			bodyText: template.bodyText,
			bodyHtml: template.bodyHtml
		};
		formError = '';
		showModal = true;
	}

	function closeModal() {
		showModal = false;
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!editingId) return;
		saving = true;
		formError = '';
		try {
			await api.updateTemplate(editingId, {
				subject: form.subject,
				bodyText: form.bodyText,
				bodyHtml: form.bodyHtml
			});
			showModal = false;
			await load();
		} catch (err) {
			// RRPCError carries the server's error name in `.error`; matched
			// structurally so this library needn't import the app's rrpc.ts.
			const e = err as { error?: string; message?: string } | null;
			if (e?.error === 'TemplateInvalid') {
				formError = `Template failed to parse: ${e.message}`;
			} else {
				formError = 'Failed to save template.';
			}
		} finally {
			saving = false;
		}
	}

	async function reset(template: TemplateItem) {
		if (!confirm(`Reset "${template.id}" to its default content?`)) return;
		resettingId = template.id;
		error = '';
		try {
			await api.resetTemplate(template.id);
			await load();
		} catch {
			error = `Failed to reset "${template.id}".`;
		} finally {
			resettingId = null;
		}
	}
</script>

<div class="d-flex justify-content-between align-items-center mb-4">
	<h1 class="h3 mb-0">Mail templates</h1>
</div>

{#if error}
	<div class="alert alert-danger">{error}</div>
{/if}

{#if loading}
	<div class="spinner-border" role="status">
		<span class="visually-hidden">Loading…</span>
	</div>
{:else if templates.length === 0}
	<p class="text-muted">No templates are registered.</p>
{:else}
	<table class="table table-hover align-middle">
		<thead>
			<tr>
				<th>ID</th>
				<th>Subject</th>
				<th>Description</th>
				<th>Status</th>
				<th class="text-end">Actions</th>
			</tr>
		</thead>
		<tbody>
			{#each templates as template (template.id)}
				<tr>
					<td><code>{template.id}</code></td>
					<td>{template.subject}</td>
					<td class="text-muted">{template.description}</td>
					<td>
						<span class="badge {template.isCustomized ? 'text-bg-primary' : 'text-bg-secondary'}">
							{template.isCustomized ? 'Customized' : 'Default'}
						</span>
					</td>
					<td class="text-end">
						<button class="btn btn-sm btn-outline-secondary me-2" onclick={() => openEdit(template)}>
							Edit
						</button>
						<button
							class="btn btn-sm btn-outline-danger"
							disabled={!template.isCustomized || resettingId === template.id}
							onclick={() => reset(template)}
						>
							{resettingId === template.id ? 'Resetting…' : 'Reset'}
						</button>
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
{/if}

{#if showModal}
	<div class="modal d-block" tabindex="-1" role="dialog">
		<div class="modal-dialog modal-lg">
			<div class="modal-content">
				<form onsubmit={save}>
					<div class="modal-header">
						<h5 class="modal-title">Edit template <code>{editingId}</code></h5>
						<button type="button" class="btn-close" onclick={closeModal} aria-label="Close"></button>
					</div>
					<div class="modal-body">
						<div class="mb-3">
							<label class="form-label" for="template-subject">Subject</label>
							<input
								id="template-subject"
								class="form-control"
								type="text"
								bind:value={form.subject}
								required
							/>
						</div>
						<div class="mb-3">
							<label class="form-label" for="template-body-text">Body (plain text)</label>
							<textarea
								id="template-body-text"
								class="form-control font-monospace"
								rows="6"
								bind:value={form.bodyText}
								required
							></textarea>
						</div>
						<div class="mb-3">
							<label class="form-label" for="template-body-html">Body (HTML, optional)</label>
							<textarea
								id="template-body-html"
								class="form-control font-monospace"
								rows="6"
								bind:value={form.bodyHtml}
							></textarea>
						</div>
						<p class="text-muted small">
							Both fields render as Go templates (<code>{'{{.Field}}'}</code> syntax) — see the
							template's description above for available variables.
						</p>
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
