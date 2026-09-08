package middleware

import (
	"errors"
	"net/http"
	"strconv"
)

type PageRequest struct {
	Cursor int64
	Limit  int
}

type PageResponse[Item any] struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"nextCursor"`
}

func ParsePage(request *http.Request) (PageRequest, error) {
	page := PageRequest{Limit: 50}
	var err error
	if value := request.URL.Query().Get("limit"); value != "" {
		page.Limit, err = strconv.Atoi(value)
		if err != nil || page.Limit < 1 || page.Limit > 200 {
			return page, errors.New("invalid limit")
		}
	}
	if value := request.URL.Query().Get("cursor"); value != "" {
		page.Cursor, err = strconv.ParseInt(value, 10, 64)
		if err != nil || page.Cursor <= 0 {
			return page, errors.New("invalid cursor")
		}
	}
	return page, nil
}

func PageItems[Item any](items []Item, page PageRequest, key func(Item) int64, descending bool) PageResponse[Item] {
	result := PageResponse[Item]{Items: []Item{}}
	for _, item := range items {
		id := key(item)
		if page.Cursor > 0 && ((!descending && id <= page.Cursor) || (descending && id >= page.Cursor)) {
			continue
		}
		if len(result.Items) == page.Limit {
			result.NextCursor = strconv.FormatInt(key(result.Items[len(result.Items)-1]), 10)
			break
		}
		result.Items = append(result.Items, item)
	}
	return result
}
