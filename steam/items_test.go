package steam

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestItemsBatch(t *testing.T) {
	fixture := mustFixture(t, "getitems_367520_2201700_999999999.json")
	var gotIDs []int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/IStoreBrowseService/GetItems/v1/" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			IDs []struct {
				AppID int64 `json:"appid"`
			} `json:"ids"`
		}
		if err := json.Unmarshal([]byte(r.URL.Query().Get("input_json")), &in); err != nil {
			t.Errorf("input_json: %v", err)
		}
		for _, id := range in.IDs {
			gotIDs = append(gotIDs, id.AppID)
		}
		w.Write(fixture)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	c.limiter = time.Tick(time.Millisecond)

	items, err := c.Items(t.Context(), []int64{367520, 2201700, 999999999})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotIDs) != 3 || gotIDs[0] != 367520 || gotIDs[2] != 999999999 {
		t.Errorf("requested ids = %v", gotIDs)
	}
	if _, ok := items[999999999]; ok || len(items) != 2 {
		t.Errorf("unknown appid must be absent; got %d items", len(items))
	}

	hk := items[367520]
	if hk == nil || hk.Name != "Hollow Knight" {
		t.Fatalf("367520: %+v", hk)
	}
	if devs := hk.DeveloperNames(); len(devs) != 1 || devs[0] != "Team Cherry" {
		t.Errorf("developers = %v", devs)
	}
	if s := hk.Reviews.Summary; s == nil || s.ReviewCount != 504430 || s.PercentPositive != 96 || s.ReviewScoreDesc != "Overwhelmingly Positive" {
		t.Errorf("reviews = %+v", s)
	}
	const wantHeader = "https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/367520/3c3489495136b26b34f8a9543c7f5645b99d388c/header.jpg?t=1776125684"
	if got := hk.HeaderImageURL(); got != wantHeader {
		t.Errorf("header = %q", got)
	}
	if len(hk.Tags) != itemTagCount || hk.Tags[0].TagID != 1628 {
		t.Errorf("tags = %+v", hk.Tags)
	}

	soon := items[2201700]
	if soon == nil || !soon.IsFree || soon.Reviews.Summary == nil || soon.Reviews.Summary.ReviewCount != 0 {
		t.Errorf("2201700: %+v", soon)
	}
}

func TestItemsRejectsOversizedBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("oversized batch must not reach Steam")
	}))
	defer srv.Close()
	ids := make([]int64, MaxItemsPerRequest+1)
	if _, err := NewClient(srv.URL).Items(t.Context(), ids); err == nil {
		t.Fatal("expected error above MaxItemsPerRequest")
	}
}

func TestHeaderImageURLWithoutAsset(t *testing.T) {
	var it StoreItem
	it.Assets.URLFormat = "steam/apps/1/${FILENAME}?t=1"
	if got := it.HeaderImageURL(); got != "" {
		t.Errorf("header without asset = %q", got)
	}
}

func TestTagNames(t *testing.T) {
	fixture := mustFixture(t, "gettaglist_english.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/IStoreService/GetTagList/v1/" || r.URL.Query().Get("language") != "english" {
			http.NotFound(w, r)
			return
		}
		w.Write(fixture)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	c.limiter = time.Tick(time.Millisecond)

	names, err := c.TagNames(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if names[indieTagID] != "Indie" || names[1628] != "Metroidvania" {
		t.Errorf("492=%q 1628=%q", names[indieTagID], names[1628])
	}
}
