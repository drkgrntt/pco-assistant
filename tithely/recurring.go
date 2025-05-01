package tithely

type Recurring struct {
	RecurringID  string       `json:"recurring_id"`
	Amount       string       `json:"amount"`
	Currency     string       `json:"currency"`
	GivingType   string       `json:"giving_type"`
	StartDate    string       `json:"start_date"`
	Term         string       `json:"term"`
	Organization Organization `json:"organization"`
	DonorAccount Account      `json:"donor_account"`
}
