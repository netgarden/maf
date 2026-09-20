import type { DatatableQuery, PageInfo } from '@netgarden/svelte-datatables';

// Structural mirror of the generated rrpc types for maf/mailer's Mailer
// service (rpc/def/mailer.rrpc). Any app's `mailerApi.mailerService` satisfies
// MailerApi as long as it was generated from the same definition.
export type EmailItem = {
	id: string;
	to: string[];
	cc: string[];
	bcc: string[];
	subject: string;
	status: string;
	attempts: number;
	lastError: string;
	createdAt: string;
	nextAttemptAt: string;
	sentAt: string;
	cancelledAt: string;
	gaveUpAt: string;
};

export type TemplateItem = {
	id: string;
	subject: string;
	bodyText: string;
	bodyHtml: string;
	isCustomized: boolean;
	description: string;
};

export type UpdateTemplateRequest = {
	subject: string;
	bodyText: string;
	bodyHtml: string;
};

export interface MailerApi {
	list(request: DatatableQuery): Promise<{ items: EmailItem[]; pageInfo: PageInfo }>;
	retry(id: string): Promise<EmailItem>;
	cancel(id: string): Promise<EmailItem>;
	sendTest(request: { to: string }): Promise<EmailItem>;
	listTemplates(): Promise<{ items: TemplateItem[] }>;
	updateTemplate(id: string, request: UpdateTemplateRequest): Promise<TemplateItem>;
	resetTemplate(id: string): Promise<void>;
}
