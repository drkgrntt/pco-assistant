package tithely

type PaymentMethod struct {
	PMID        string `json:"pm_id"`
	Last4Digits string `json:"last_4_digits"`
	Brand       string `json:"brand"`
	PmType      string `json:"pm_type"`
	AccountID   string `json:"account_id"`
}
