package workspace

import (
	"errors"
	"strings"
)

type ResourceQuery struct {
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
	Search   string `json:"search"`
}

type ResourceItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	FinishedAt string `json:"finishedAt"`
}

type ResourcePage struct {
	Items []ResourceItem `json:"items"`
	Total int            `json:"total"`
	Page  int            `json:"page"`
}

var errInvalidResourceQuery = errors.New("分页参数无效")
var errResourceSearchTooLong = errors.New("搜索内容过长")

func (a *App) Resources(query ResourceQuery) (ResourcePage, error) {
	if query.Page < 1 || (query.PageSize != 20 && query.PageSize != 50 && query.PageSize != 100) {
		return ResourcePage{}, errInvalidResourceQuery
	}
	search := strings.TrimSpace(query.Search)
	if len(search) > 500 {
		return ResourcePage{}, errResourceSearchTooLong
	}
	// instr treats wildcard characters as literal search text.
	where := " WHERE instr(lower(name),lower(?))>0"
	var total int
	if err := a.Store.DB.QueryRow("SELECT count(*) FROM resources"+where, search).Scan(&total); err != nil {
		return ResourcePage{}, err
	}
	page := min(query.Page, max(1, (total+query.PageSize-1)/query.PageSize))
	rows, err := a.Store.DB.Query("SELECT id,name,size,finished_at FROM resources"+where+" ORDER BY finished_at DESC,id LIMIT ? OFFSET ?", search, query.PageSize, (page-1)*query.PageSize)
	if err != nil {
		return ResourcePage{}, err
	}
	defer rows.Close()
	items := make([]ResourceItem, 0)
	for rows.Next() {
		var r ResourceItem
		if err = rows.Scan(&r.ID, &r.Name, &r.Size, &r.FinishedAt); err != nil {
			return ResourcePage{}, err
		}
		items = append(items, r)
	}
	if err = rows.Err(); err != nil {
		return ResourcePage{}, err
	}
	return ResourcePage{items, total, page}, nil
}
