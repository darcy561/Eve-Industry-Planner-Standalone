package requests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func MakeHttpRequest[T any](fullURL string, headers map[string]string, queryParameters url.Values, responseType T) (responseObject T, header http.Header, err error) {
	client := http.Client{}
	parsedURL, err := url.Parse(fullURL)
	if err != nil {
		return
	}

	query := parsedURL.Query()

	for key, value := range queryParameters {
		query.Set(key, strings.Join(value, ","))
	}
	parsedURL.RawQuery = query.Encode()

	request, err := http.NewRequest("GET", parsedURL.String(), nil)

	if err != nil {
		return
	}

	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := client.Do(request)
	if err != nil {
		return
	}

	if response != nil {
		defer response.Body.Close()
	}

	if response == nil {
		err = fmt.Errorf("error: calling %s returned empty response", parsedURL.String())
		return
	}

	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		return
	}

	if response.StatusCode != http.StatusOK {
		err = fmt.Errorf("error calling %s:\nstatus:%s\nresponseData: %s", parsedURL.String(), response.Status, responseData)
		return
	}
	err = json.Unmarshal(responseData, &responseObject)
	if err != nil {
		return
	}

	header = response.Header

	return
}
