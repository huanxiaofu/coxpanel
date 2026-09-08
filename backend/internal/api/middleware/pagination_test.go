package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestP2PaginationLimitsAndContinuation(t *testing.T) {
	for _, query := range []string{"limit=0", "limit=201", "limit=abc", "cursor=-1", "cursor=bad"} {
		if _, err := ParsePage(httptest.NewRequest("GET", "/?"+query, nil)); err == nil {
			t.Fatal("invalid pagination accepted")
		}
	}
	page, err := ParsePage(httptest.NewRequest("GET", "/?limit=2&cursor=1", nil))
	if err != nil {
		t.Fatal(err)
	}
	result := PageItems([]int64{1, 2, 3, 4}, page, func(value int64) int64 { return value }, false)
	if len(result.Items) != 2 || result.Items[0] != 2 || result.NextCursor != "3" {
		t.Fatal("continuation invalid")
	}
	result = PageItems([]int64{4, 3, 2, 1}, PageRequest{Cursor: 3, Limit: 2}, func(value int64) int64 { return value }, true)
	if len(result.Items) != 2 || result.NextCursor != "" || result.Items[0] != 2 {
		t.Fatal("descending continuation invalid")
	}
}
