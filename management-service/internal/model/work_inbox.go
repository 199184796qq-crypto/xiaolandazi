package model

import "time"

// Terminal customers follow progress in their business pages, not a work inbox.
// This product gate never substitutes for business permissions or ownership checks.
func WorkInboxAvailable(role string) bool {
	switch role {
	case "platform_admin", "staff", "sales_staff", "agent_admin":
		return true
	default:
		return false
	}
}

// The inbox is a read projection. It never authorizes or performs a business action.
type InboxScope struct {
	Actor                   Actor              `json:"actor"`
	Access                  StaffAccessContext `json:"access"`
	RequireDistinctReviewer bool               `json:"require_distinct_reviewer"`
}
type InboxGroup struct {
	Key         string `json:"key"`
	Topic       string `json:"topic"`
	Department  string `json:"department"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	To          string `json:"to"`
	Count       int64  `json:"count"`
	DueCount    int64  `json:"due_count"`
	SharedQueue bool   `json:"shared_queue"`
}
type InboxSnapshot struct {
	Groups      []InboxGroup `json:"groups"`
	Total       int64        `json:"total"`
	Version     string       `json:"version"`
	AsOf        time.Time    `json:"as_of"`
	Stale       bool         `json:"stale"`
	Unavailable []string     `json:"unavailable"`
}
type InboxItem struct {
	Key       string     `json:"key"`
	ID        int64      `json:"id"`
	Reference string     `json:"reference"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	DueAt     *time.Time `json:"due_at,omitempty"`
	To        string     `json:"to"`
}
type InboxPage struct {
	Group      InboxGroup  `json:"group"`
	Items      []InboxItem `json:"items"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	Total      int64       `json:"total"`
	TotalPages int64       `json:"total_pages"`
}
