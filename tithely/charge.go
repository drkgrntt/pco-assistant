package tithely

import (
	"fmt"
	"time"
)

type Charge struct {
	ChargeID      string `json:"charge_id"`
	ChargeStatus  string `json:"charge_status"`
	Amount        string `json:"amount"`
	NetAmount     string `json:"net_amount"`
	Fees          int64  `json:"fees"`
	Currency      string `json:"currency"`
	GivingType    string `json:"giving_type"`
	ChargeDate    string `json:"charge_date"`
	PaymentMethod string `json:"payment_method"`
	Organization  string `json:"organization"`
	DonorAccount  string `json:"donor_account"`
}

type ChargeListResponse struct {
	Status string   `json:"status"`
	Type   string   `json:"type"`
	Data   []Charge `json:"data"`
}

type ChargesParams struct {
	AccountID      string    `json:"account_id"`
	OrganizationID string    `json:"organization_id"`
	Limit          int64     `json:"limit"`
	OrderBy        string    `json:"order_by"`
	StartingAfter  string    `json:"starting_after"`
	EndingBefore   string    `json:"ending_before"`
	CreatedAfter   time.Time `json:"created_after"`
	CreatedBefore  time.Time `json:"created_before"`
}

// https://docs.tithe.ly/reference/list-all-charges
func GetCharges(params *ChargesParams) (response ChargeListResponse, err error) {
	url := fmt.Sprintf("%s/charges-list?", baseURL)

	if params == nil {
		params = &ChargesParams{}
	}

	if params.AccountID != "" {
		url += fmt.Sprintf("account_id=%s&", params.AccountID)
	}

	if params.OrganizationID != "" {
		url += fmt.Sprintf("organization_id=%s&", params.OrganizationID)
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

	if params.CreatedAfter != (time.Time{}) {
		url += fmt.Sprintf("created_after=%s&", params.CreatedAfter)
	}

	if params.CreatedBefore != (time.Time{}) {
		url += fmt.Sprintf("created_before=%s&", params.CreatedBefore)
	}

	response, err = NewRequest[ChargeListResponse]("GET", url, nil)
	return
}
