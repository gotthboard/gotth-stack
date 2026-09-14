# Public website cold review 1

Verdict: FAIL; corrected before the final pass.

## Blockers

1. The CSS was served for one year as immutable from `/static/site.css`.
   That is stale-cache userspace breakage. The fix content-addressed the route,
   file, embed, template, tests, and generation contract with the exact CSS
   SHA-256.
2. The original HTMX swap replaced the whole explorer. That discarded the
   focused control and replaced the live region itself. The fix keeps the
   navigation and `aria-live` container stable, swaps only the detail markup,
   preserves keyboard focus, and derives the selected visual state from the
   inserted topic.
3. The first strict-query implementation accepted a lone unknown key and
   selected the default topic. The fix rejects every key except one bounded
   `topic` value and adds hostile tests.
4. Go `ServeMux` redirects dot and repeated-slash paths by documented design,
   while the contract promised fixed 404 behavior. The fix rejects empty,
   escaped, dot-segment, and repeated-slash paths before the mux. It also
   disables the server's general `OPTIONS *` shortcut so responses do not
   bypass security headers.
5. One small orange label on the paper background lacked a safe contrast
   margin. The fix uses black text on the orange signal block and disables the
   remaining pulse animation under reduced-motion preference.

No trust, confirmation, controller-authority, or deployment behavior changed.
