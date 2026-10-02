package view

import (
	"reflect"
	"testing"

	"github.com/embiem/indie-game-gems/data"
)

func TestStoreButtonsOrderingAndLabels(t *testing.T) {
	links := []data.StoreLink{
		{Store: "itch", Url: "https://playdead.itch.io/limbo"},
		{Store: "steam", Url: "https://store.steampowered.com/app/48000/"},
		{Store: "epic", Url: "https://store.epicgames.com/p/limbo"},
		{Store: "humble", Url: "https://www.humblebundle.com/store/limbo"},
		{Store: "gog", Url: "https://www.gog.com/game/limbo"},
		{Store: "direct", Url: "https://playdead.com"},
		{Store: "unknown_future_store", Url: "https://example.com"}, // dropped
		{Store: "gog", Url: ""},                                     // empty URL ignored (PK makes this rare)
	}

	got := StoreButtons(links, "released")
	want := []StoreButton{
		{Store: "steam", Label: "Buy on Steam", URL: "https://store.steampowered.com/app/48000/", Primary: true},
		{Store: "gog", Label: "GOG", URL: "https://www.gog.com/game/limbo"},
		{Store: "epic", Label: "Epic Games Store", URL: "https://store.epicgames.com/p/limbo"},
		{Store: "itch", Label: "itch.io", URL: "https://playdead.itch.io/limbo"},
		{Store: "humble", Label: "Humble", URL: "https://www.humblebundle.com/store/limbo"},
		{Store: "direct", Label: "Developer's store", URL: "https://playdead.com"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StoreButtons(released) = %+v\nwant %+v", got, want)
	}
}

func TestSteamLabelFollowsReleaseStatus(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"announced", "Wishlist on Steam"},
		{"upcoming", "Wishlist on Steam"},
		{"early_access", "Buy on Steam"},
		{"released", "Buy on Steam"},
		{"", "Buy on Steam"}, // unknown degrades to the buy CTA
	}
	for _, tt := range tests {
		got := StoreButtons([]data.StoreLink{{Store: "steam", Url: "https://store.steampowered.com/app/1/"}}, tt.status)
		if len(got) != 1 || got[0].Label != tt.want {
			t.Errorf("status %q: label = %+v, want %q", tt.status, got, tt.want)
		}
		if got[0].Primary != true {
			t.Errorf("status %q: steam must be primary", tt.status)
		}
	}
}

func TestDeveloperSocialsOrderedAndFiltered(t *testing.T) {
	socials := []byte(`{"bluesky":"https://bsky.app/profile/x","youtube":"https://youtube.com/@x","x":"https://x.com/x","secret":"nope"}`)
	got := DeveloperSocials(socials)
	want := []SocialLink{
		{"X", "https://x.com/x"},
		{"Bluesky", "https://bsky.app/profile/x"},
		{"YouTube", "https://youtube.com/@x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeveloperSocials = %+v, want %+v", got, want)
	}

	if got := DeveloperSocials([]byte(`not json`)); got != nil {
		t.Errorf("invalid JSON should yield nil, got %+v", got)
	}
	if got := DeveloperSocials(nil); len(got) != 0 {
		t.Errorf("nil JSON should yield empty, got %+v", got)
	}
}
