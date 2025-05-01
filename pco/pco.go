package pco

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
)

const baseURL = "https://api.planningcenteronline.com"

type Meta struct {
	CanFilter  []string `json:"can_filter"`
	CanInclude []string `json:"can_include"`
	CanOrderBy []string `json:"can_order_by"`
	CanQueryBy []string `json:"can_query_by"`
	Count      int      `json:"count"`
	Parent     General  `json:"parent"`
	TotalCount int      `json:"total_count"`
}

type General struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type Links struct {
	Self string `json:"self"`
	HTML string `json:"html"`
}

func NewRequest[Response interface{}](
	method string,
	url string,
	body interface{},
) (response Response, err error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return response, err
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return response, err
	}

	req.SetBasicAuth(os.Getenv("PCO_CLIENT_ID"), os.Getenv("PCO_SECRET"))
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return response, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return response, err
	}

	err = json.Unmarshal(responseBody, &response)
	return
}
