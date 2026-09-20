# Typed GOTTH Mail runtime adapters

Complete at source `370aabd717d6557b3fa3cf498654415da625ff59`.

This child admits one private, fixed Docker mechanism and five typed role
constructors for the GOTTH Mail control plane, front, Postfix, Dovecot, and
Rspamd. Each constructor fixes its image digest, user, capabilities, ports,
mounts, tmpfs, secrets, network, health command, and replacement state
machine. The package verifies the canonical release manifest and configuration
archive before staging any runtime mutation.

It does not expose a generic executor, pull images, delete persistent state,
create secrets, import the operation journal, add an `apply` command, or name a
live target. Controller authority remains a separate active child.

The real disposable proof uses the exact production Rspamd image and release
artifacts to install, replace a secret revision, reopen the adapter, roll back,
and retain SQLite Bayes state. Mail's separate combined production smoke
exercises all five exact images and their native protocol/configuration checks;
that product evidence does not grant Stack controller authority.
