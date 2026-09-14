# Mail runtime topology correction

Completed documentation correction under the active V1 subtree. It removes the
false Mailu runtime dependency and makes the future V3 deployment boundary
match GOTTH Mail's canonical control-plane, front/proxy, Postfix, Dovecot,
Rspamd, and PostgreSQL architecture.

This feature changes no planner source, journal implementation, adapter,
credential, DNS record, host, or deployment.
