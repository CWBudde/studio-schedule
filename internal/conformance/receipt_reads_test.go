package conformance

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

type receiptSearchView struct {
	ID         string
	Number     string
	Kind       string
	Payer      *struct{ ID string }
	Cancels    *struct{ ID string }
	CanceledBy *struct{ ID string }
	Lines      []struct{ Gross int64 }
	Gross      int64
	Cancelable bool
}

type receiptSearchPage struct {
	Items []receiptSearchView
	Total int
	Years []int
}

func receiptListsAndSearch(t *testing.T, b *backend) {
	t.Helper()

	search := request{method: http.MethodPost, path: "/api/v1/admin/receipts/search", body: `{}`}
	mine := request{method: http.MethodGet, path: "/api/v1/my/receipts"}
	member := browser(t)
	b.call(t, member, search, http.StatusUnauthorized)
	b.call(t, member, mine, http.StatusUnauthorized)
	login(t, b, member, demoMember, demoPassword)
	b.call(t, member, search, http.StatusForbidden)

	office := browser(t)
	login(t, b, office, demoAdmin, demoPassword)
	all := read[receiptSearchPage](t, b.call(t, office, search, http.StatusOK))
	if all.Total < 2 || all.Total != len(all.Items) || !slices.Contains(all.Years, 2026) {
		t.Fatalf("expected every seeded receipt and the 2026 year, got %d items, total %d, years %v", len(all.Items), all.Total, all.Years)
	}
	canceled := slices.IndexFunc(all.Items, func(r receiptSearchView) bool { return r.CanceledBy != nil })
	if canceled < 0 {
		t.Fatal("the seeded sale canceled by a refund is missing")
	}

	sale := all.Items[canceled]
	if sale.CanceledBy == nil || sale.Cancelable || sale.Kind != "sale" {
		t.Fatalf("canceled sale has inconsistent metadata: %+v", sale)
	}
	filter, err := json.Marshal(map[string]any{"query": " " + strings.ToLower(sale.Number) + " ", "year": 2026})
	if err != nil {
		t.Fatal(err)
	}
	search.body = string(filter)
	match := read[receiptSearchPage](t, b.call(t, office, search, http.StatusOK))
	if match.Total != 1 || len(match.Items) != 1 || match.Items[0].ID != sale.ID || !slices.Equal(match.Years, all.Years) {
		t.Fatalf("number search did not find only %s and preserve the available years: total %d, items %v, years %v", sale.Number, match.Total, match.Items, match.Years)
	}

	search.body = `{"limit":1,"offset":1}`
	page := read[receiptSearchPage](t, b.call(t, office, search, http.StatusOK))
	if len(page.Items) != 1 || page.Items[0].ID != all.Items[1].ID || page.Total != all.Total {
		t.Fatal("receipt pagination did not preserve order and total")
	}
	search.body = `{"limit":101}`
	b.call(t, office, search, http.StatusUnprocessableEntity)

	ref := request{
		method: http.MethodGet, path: "/api/v1/admin/receipts/{receiptId}",
		params: map[string]string{"receiptId": sale.CanceledBy.ID},
	}
	b.call(t, member, ref, http.StatusForbidden)
	refund := read[receiptSearchView](t, b.call(t, office, ref, http.StatusOK))
	if refund.Cancels == nil || refund.Cancels.ID != sale.ID || refund.Cancelable || refund.Kind != "cancellation" ||
		refund.Gross != -sale.Gross || len(refund.Lines) != len(sale.Lines) || len(refund.Lines) == 0 {
		t.Fatalf("refund details do not identify and negate the original sale: %+v", refund)
	}
	ref.params["receiptId"] = "a961f110-a017-7241-8123-123456789abc"
	b.call(t, office, ref, http.StatusNotFound)

	me := read[struct{ PersonID string }](t, get(t, b, member, "/api/v1/auth/me"))
	owned := read[struct{ Items []receiptSearchView }](t, b.call(t, member, mine, http.StatusOK))
	if me.PersonID == "" || len(owned.Items) == 0 || len(owned.Items) >= all.Total {
		t.Fatalf("member receipt list did not select a non-empty subset: %d receipts", len(owned.Items))
	}
	for _, item := range owned.Items {
		if item.Payer == nil || item.Payer.ID != me.PersonID {
			t.Fatalf("member receipt list included a different payer on %s", item.Number)
		}
	}
}
