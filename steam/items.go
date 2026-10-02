package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// MaxItemsPerRequest caps the appids sent in one GetItems call. The ids
// travel in the input_json query parameter: 200 ids (~6 KB URL) succeed,
// 300 (~9 KB) are rejected with HTTP 400.
const MaxItemsPerRequest = 100

// itemTagCount is the number of top user tags GetItems returns per app
// (the store page lists at most 20).
const itemTagCount = 20

// assetBaseURL prefixes the asset paths GetItems returns; the result is the
// same URL appdetails reports (e.g. header_image).
const assetBaseURL = "https://shared.akamai.steamstatic.com/store_item_assets/"

// StoreItem is the subset of an IStoreBrowseService/GetItems entry we use.
type StoreItem struct {
	AppID     int64  `json:"appid"`
	Name      string `json:"name"`
	IsFree    bool   `json:"is_free"`
	BasicInfo struct {
		ShortDescription string        `json:"short_description"`
		Developers       []storeEntity `json:"developers"`
		Publishers       []storeEntity `json:"publishers"`
	} `json:"basic_info"`
	// Tags are the top user tags by descending weight; names via TagNames.
	Tags []struct {
		TagID int64 `json:"tagid"`
	} `json:"tags"`
	Reviews struct {
		// Summary is the store page's "All Reviews" aggregate: Steam
		// purchasers only, off-topic review activity excluded. Counts are
		// therefore lower than appreviews' purchase_type=all totals.
		Summary *StoreReviewSummary `json:"summary_filtered"`
	} `json:"reviews"`
	Assets struct {
		URLFormat string `json:"asset_url_format"` // contains ${FILENAME}
		Header    string `json:"header"`
	} `json:"assets"`
}

type storeEntity struct {
	Name string `json:"name"`
}

// StoreReviewSummary is GetItems' review aggregate.
type StoreReviewSummary struct {
	ReviewCount     int64  `json:"review_count"`
	PercentPositive int    `json:"percent_positive"` // whole percent, 0-100
	ReviewScore     int    `json:"review_score"`     // 0-9, Steam's scale
	ReviewScoreDesc string `json:"review_score_label"`
}

// DeveloperNames returns the developer names in store order.
func (it *StoreItem) DeveloperNames() []string { return entityNames(it.BasicInfo.Developers) }

// PublisherNames returns the publisher names in store order.
func (it *StoreItem) PublisherNames() []string { return entityNames(it.BasicInfo.Publishers) }

func entityNames(es []storeEntity) []string {
	names := make([]string, 0, len(es))
	for _, e := range es {
		names = append(names, e.Name)
	}
	return names
}

// HeaderImageURL returns the absolute 460×215 header URL, or "" when the
// app has no header asset.
func (it *StoreItem) HeaderImageURL() string {
	if it.Assets.Header == "" || it.Assets.URLFormat == "" {
		return ""
	}
	return assetBaseURL + strings.Replace(it.Assets.URLFormat, "${FILENAME}", it.Assets.Header, 1)
}

/*
Items fetches store metadata, top tags and the review summary for up to
MaxItemsPerRequest apps in one IStoreBrowseService/GetItems request (keyless).
The result is keyed by appid; unknown or hidden apps are absent.
*/
func (c *Client) Items(ctx context.Context, appids []int64) (map[int64]*StoreItem, error) {
	if len(appids) > MaxItemsPerRequest {
		return nil, fmt.Errorf("steam GetItems: %d appids exceed the limit of %d per request", len(appids), MaxItemsPerRequest)
	}
	type itemID struct {
		AppID int64 `json:"appid"`
	}
	input := struct {
		IDs     []itemID `json:"ids"`
		Context struct {
			Language    string `json:"language"`
			CountryCode string `json:"country_code"`
		} `json:"context"`
		DataRequest struct {
			BasicInfo bool `json:"include_basic_info"`
			Reviews   bool `json:"include_reviews"`
			Assets    bool `json:"include_assets"`
			TagCount  int  `json:"include_tag_count"`
		} `json:"data_request"`
	}{IDs: make([]itemID, len(appids))}
	for i, id := range appids {
		input.IDs[i] = itemID{AppID: id}
	}
	input.Context.Language = "english"
	input.Context.CountryCode = "US"
	input.DataRequest.BasicInfo = true
	input.DataRequest.Reviews = true
	input.DataRequest.Assets = true
	input.DataRequest.TagCount = itemTagCount
	b, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	u := c.apiBaseURL + "/IStoreBrowseService/GetItems/v1/?input_json=" + url.QueryEscape(string(b))
	var resp struct {
		Response struct {
			StoreItems []struct {
				ID      int64 `json:"id"`
				Success int   `json:"success"` // EResult: 1 = OK
				StoreItem
			} `json:"store_items"`
		} `json:"response"`
	}
	if err := c.do(ctx, u, &resp); err != nil {
		return nil, err
	}
	out := make(map[int64]*StoreItem, len(resp.Response.StoreItems))
	for i := range resp.Response.StoreItems {
		e := &resp.Response.StoreItems[i]
		if e.Success != 1 {
			continue
		}
		out[e.ID] = &e.StoreItem
	}
	return out, nil
}

// TagNames fetches the English names of all store tags, keyed by tag id.
func (c *Client) TagNames(ctx context.Context) (map[int64]string, error) {
	var resp struct {
		Response struct {
			Tags []struct {
				TagID int64  `json:"tagid"`
				Name  string `json:"name"`
			} `json:"tags"`
		} `json:"response"`
	}
	if err := c.do(ctx, c.apiBaseURL+"/IStoreService/GetTagList/v1/?language=english", &resp); err != nil {
		return nil, err
	}
	if len(resp.Response.Tags) == 0 {
		return nil, fmt.Errorf("steam GetTagList: empty tag list")
	}
	names := make(map[int64]string, len(resp.Response.Tags))
	for _, t := range resp.Response.Tags {
		names[t.TagID] = t.Name
	}
	return names, nil
}
