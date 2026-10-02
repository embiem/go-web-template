/*
Package email renders the Indie Game Gems emails: the newsletter itself and
the double-opt-in confirmation mail.

Everything is hand-inlined, table-based HTML (no external CSS, no forms, no
JS) so it survives Gmail, Outlook and Apple Mail, plus a plain-text
alternative generated from the same document. Callers build a Doc (or
ConfirmDoc) and render both parts; the package never touches the database or
the network.
*/
package email

import "fmt"

// Doc is one rendered newsletter issue.
type Doc struct {
	Subject   string
	Preheader string
	Blocks    []Block
	Footer    Footer
}

// Footer ends every newsletter mail.
type Footer struct {
	HomeURL       string // link on the wordmark
	BrowserURL    string // "read it in the browser" archive link
	UnsubURL      string // per-recipient; may be a placeholder pre-send
	PostalAddress string
	Reason        string // why the recipient gets this mail
}

// Block is one content block. Exactly one field must be set — use
// NewProse/NewGotd/… constructors and keep Validate as the guard.
type Block struct {
	ProseHTML string // markdown-derived email-safe HTML
	ProseText string // same prose, plain text (multipart alternative)
	Gotd      *Gotd
	Radar     *Section
	Top       *Section
	Game      *GameCard
}

// Gotd is the Game of the Day block.
type Gotd struct {
	Heading  string
	Game     GameCard
	NoteHTML string // editorial note, markdown-derived
	NoteText string
}

// Section is a titled list of games (Release Radar / Top of the Month).
type Section struct {
	Heading string
	Items   []GameCard
}

// GameCard is one game anywhere in the mail.
type GameCard struct {
	Title       string
	Tagline     string
	Developer   string
	GameURL     string
	ImageURL    string // absolute; may be empty (design works without)
	ReleaseLine string // e.g. "Released 2026-09-29" or "Early access"
	ScoreLine   string // e.g. "Gem score 88"; may be empty
	Stores      []StoreButton
}

// StoreButton is one store link under a game card.
type StoreButton struct {
	Label string
	URL   string
}

// Validate reports blocks that set zero or several fields.
func (b Block) Validate() error {
	set := 0
	for _, v := range []bool{b.ProseHTML != "", b.Gotd != nil, b.Radar != nil, b.Top != nil, b.Game != nil} {
		if v {
			set++
		}
	}
	if set != 1 {
		return fmt.Errorf("email block must set exactly one of Prose/Gotd/Radar/Top/Game, got %d", set)
	}
	return nil
}

// Validate checks the whole document.
func (d Doc) Validate() error {
	if d.Subject == "" {
		return fmt.Errorf("email doc: subject is required")
	}
	for i, b := range d.Blocks {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("email doc: block %d: %w", i, err)
		}
	}
	return nil
}

// ConfirmDoc is the double-opt-in confirmation mail (transactional: no bulk
// headers, no unsubscribe footer).
type ConfirmDoc struct {
	ConfirmURL    string
	HomeURL       string
	PostalAddress string
	// Reason tells the recipient why they got this mail (someone signed up
	// this address) and what to do if it wasn't them.
	Reason string
}
