/*
Package newsletter contains the backend of the Indie Game Gems email
newsletter: subscriber state, opt-in tokens, issue authoring (markdown files
with data directives), rendering into emails, and resumable bulk sending.

Split of concerns:

  - Pure logic (this package, no I/O): email normalisation/validation,
    subscriber state transitions, unsubscribe + confirmation tokens, issue
    file parsing and directive validation. Covered by unit tests.
  - Rendering: newsletter.Renderer resolves directives against the database
    into view/email documents.
  - Sending: newsletter.Sender snapshots issues, creates deliveries and sends
    them resumably via the mail package.

The HTTP handlers (phase B) and the gems newsletter CLI both sit on top of
these and stay thin.
*/
package newsletter
