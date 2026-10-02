package newsletter

import "fmt"

// Status is the lifecycle state of a subscriber. Transitions are pure
// functions below so the DB layer and the handlers cannot invent states.
//
//	pending ──confirm──▶ confirmed ──unsubscribe──▶ unsubscribed
//	   │                                                      ▲
//	   └──────────────────unsubscribe─────────────────────────┘
//
// unsubscribed ──resubscribe──▶ pending is the only way out of
// unsubscribed: the user must re-confirm (double opt-in) before receiving
// mail again, which both honours the withdrawal immediately and keeps a
// consent trail. We never silently flip unsubscribed back to confirmed.
type Status string

const (
	StatusPending      Status = "pending"
	StatusConfirmed    Status = "confirmed"
	StatusUnsubscribed Status = "unsubscribed"
)

// ErrAlreadyUnsubscribed is returned when someone tries to confirm an
// address that has unsubscribed. Handlers surface it as a notice ("you
// unsubscribed — sign up again to resubscribe"), never as an auto-confirm.
var ErrAlreadyUnsubscribed = fmt.Errorf("this address has unsubscribed; sign up again to resubscribe")

// Confirm transitions into confirmed. Idempotent for already-confirmed
// addresses (a second click on an old link is not an error).
func Confirm(cur Status) (Status, error) {
	switch cur {
	case StatusPending:
		return StatusConfirmed, nil
	case StatusConfirmed:
		return StatusConfirmed, nil
	case StatusUnsubscribed:
		return "", ErrAlreadyUnsubscribed
	default:
		return "", fmt.Errorf("unknown subscriber status %q", cur)
	}
}

// Unsubscribe transitions into unsubscribed from any live state.
// Idempotent: repeated one-click POSTs from mailbox providers are expected
// (RFC 8058) and must stay safe.
func Unsubscribe(cur Status) (Status, error) {
	switch cur {
	case StatusPending, StatusConfirmed, StatusUnsubscribed:
		return StatusUnsubscribed, nil
	default:
		return "", fmt.Errorf("unknown subscriber status %q", cur)
	}
}

// Resubscribe moves an unsubscribed address back to pending so the normal
// double-opt-in flow starts over. It is the caller's job to require a fresh
// signup (consent), not just a stale confirmation link.
func Resubscribe(cur Status) (Status, error) {
	switch cur {
	case StatusUnsubscribed:
		return StatusPending, nil
	case StatusPending, StatusConfirmed:
		return cur, nil // nothing to do
	default:
		return "", fmt.Errorf("unknown subscriber status %q", cur)
	}
}
