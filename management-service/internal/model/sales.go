package model

import "time"

type SalesStaffSummary struct {
	StaffID       int64     `json:"staff_id"`
	UserID        int64     `json:"user_id"`
	EmployeeCode  string    `json:"employee_code"`
	Username      string    `json:"username"`
	DisplayName   string    `json:"display_name"`
	Phone         string    `json:"phone"`
	Email         string    `json:"email"`
	Province      string    `json:"province"`
	City          string    `json:"city"`
	District      string    `json:"district"`
	Status        string    `json:"status"`
	TeamID        *int64    `json:"team_id,omitempty"`
	TeamName      string    `json:"team_name"`
	CustomerCount int64     `json:"customer_count"`
	CreatedAt     time.Time `json:"created_at"`
}
