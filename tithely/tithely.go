package tithely

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
)

const baseURL = "https://tithe.ly/api/v1"

func NewRequest[Response interface{}](
	method string,
	url string,
	body io.Reader,
) (response Response, err error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return response, err
	}

	req.Header.Add("accept", "application/json")
	req.SetBasicAuth(os.Getenv("TITHELY_PUBLIC_KEY"), os.Getenv("TITHELY_PRIVATE_KEY"))

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return response, err
	}

	defer res.Body.Close()
	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return response, err
	}

	err = json.Unmarshal(responseBody, &response)
	return
}
