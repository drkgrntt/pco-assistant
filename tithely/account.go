package tithely

import (
	"fmt"
)

type Address struct {
	StreetAddress string `json:"street_address"`
	City          string `json:"city"`
	State         string `json:"state"`
	Postal        string `json:"postal"`
	Country       string `json:"country"`
}

type Account struct {
	AccountID   string  `json:"account_id"`
	CreatedDate string  `json:"created_date"`
	Email       string  `json:"email"`
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	Address     Address `json:"address"`
	PhoneNumber string  `json:"phone_number"`
}

type AccountsResponse struct {
	Data   []*Account `json:"data"`
	Status string     `json:"status"`
	Type   string     `json:"type"`
}

type AccountsParams struct {
	Limit         int64  `json:"limit"`
	OrderBy       string `json:"order_by"`
	StartingAfter string `json:"starting_after"`
	EndingBefore  string `json:"ending_before"`
}

// https://docs.tithe.ly/reference/list-all-accounts-1
func GetAccounts(params *AccountsParams) (response AccountsResponse, err error) {
	url := fmt.Sprintf("%s/accounts-list-all?", baseURL)

	if params == nil {
		params = &AccountsParams{}
	}

	if params.Limit != 0 {
		url += fmt.Sprintf("limit=%d&", params.Limit)
	}

	if params.OrderBy != "" {
		url += fmt.Sprintf("order_by=%s&", params.OrderBy)
	}

	if params.StartingAfter != "" {
		url += fmt.Sprintf("starting_after=%s&", params.StartingAfter)
	}

	if params.EndingBefore != "" {
		url += fmt.Sprintf("ending_before=%s&", params.EndingBefore)
	}

	return NewRequest[AccountsResponse]("GET", url, nil)
}
