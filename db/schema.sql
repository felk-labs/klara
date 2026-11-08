create table if not exists domains (
	id     text not null,
	name   text not null,
	attrs  jsonb,

	constraint "domains.pkey" primary key (id),
	constraint "domains.unique" unique (name),
	constraint "domains.check-attrs" check (jsonb_typeof(attrs) = 'object')
);

create table if not exists accts (
	id      text not null,
	domain  text not null,
	name    text not null,
	attrs   jsonb,

	constraint "accts.pkey" primary key (id),
	constraint "accts.unique" unique (domain, name),
	constraint "accts.fkey-domain" foreign key (domain)
		references domains(name)
		on delete restrict,
	constraint "accts.check-attrs" check (jsonb_typeof(attrs) = 'object')
);

create table if not exists mail (
	id     text      not null,
	ts     timestamp not null,
	typ    integer   not null,
	status integer   not null,
	data   bytea,
	attrs  jsonb,

	constraint "mail.pkey" primary key (id),
	constraint "mail.check-attrs" check (jsonb_typeof(attrs) = 'object')
);

create table if not exists mboxes (
	id      text not null,
	acct_id text not null,
	name    text not null,
	attrs   jsonb,

	constraint "mboxes.pkey" primary key (id),
	constraint "mboxes.unique" unique (acct_id, name),
	constraint "mboxes.fkey-domain" foreign key (acct_id)
		references accts(id)
		on delete restrict,
	constraint "mboxes.check-attrs" check (jsonb_typeof(attrs) = 'object')
);

create table if not exists msgs (
	id       text      not null,
	ts       timestamp not null,
	mbox_id  text,
	uid      integer,
	attrs    jsonb,

	constraint "msgs.pkey" primary key (id),
	constraint "msgs.unique" unique (mbox_id, uid),
	constraint "msgs.fkey-mbox_id" foreign key (mbox_id)
		references mboxes(id)
		on delete restrict,
	constraint "msgs.check-attrs" check (jsonb_typeof(attrs) = 'object')
);
