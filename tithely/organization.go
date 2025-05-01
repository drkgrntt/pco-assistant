package tithely

import (
	"fmt"
)

type GivingType struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Bank struct {
	AccountNumberLast4 string `json:"account_number_last4"`
	Name               string `json:"name"`
	Country            string `json:"country"`
	Currency           string `json:"currency"`
}

type Legal struct {
	EntityType       string `json:"entity_type"`
	OrganizationName string `json:"organization_name"`
	FirstName        string `json:"first_name"`
	LastName         string `json:"last_name"`
	DateOfBirth      string `json:"date_of_birth"`
}

type Organization struct {
	OrganizationID  string       `json:"organization_id"`
	WidgetID        string       `json:"widget_id"`
	AccountID       string       `json:"account_id"`
	CreatedDate     string       `json:"created_date"`
	Name            string       `json:"name"`
	PhoneNumber     string       `json:"phone_number"`
	Website         string       `json:"website"`
	Address         Address      `json:"address"`
	GivingTypes     []string     `json:"giving_types"`
	GivingTypesFull []GivingType `json:"giving_types_full"`
	Bank            Bank         `json:"bank"`
	Legal           Legal        `json:"legal"`
}

type OrganizationsResponse struct {
	Data   []Organization `json:"data"`
	Status string         `json:"status"`
	Type   string         `json:"type"`
}

type OrganizationsParams struct {
	Limit         int64  `json:"limit"`
	OrderBy       string `json:"order_by"`
	StartingAfter string `json:"starting_after"`
	EndingBefore  string `json:"ending_before"`
}

// https://docs.tithe.ly/reference/list-all-organizations
func GetOrganizations(params *OrganizationsParams) (response OrganizationsResponse, err error) {
	url := fmt.Sprintf("%s/organizations-list?", baseURL)

	if params == nil {
		params = &OrganizationsParams{}
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

	return NewRequest[OrganizationsResponse]("GET", url, nil)
}
