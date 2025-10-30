package requests

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type Request[T any] struct {
	URL     url.URL
	Params  url.Values
	Result  *[]T
	Page    int
	Retries int
}

func FetchAllPages[T any](baseURL url.URL, initialQueryParameters url.Values, previousETags map[int]string, responseType []T, responseLength int) (allItems []T, updatedETags map[int]string, err error) {

	baseURL.RawQuery = initialQueryParameters.Encode()

	requestQueue := make(chan Request[T], 100)
	rateLimiter := time.NewTicker(time.Second / time.Duration(1))

	defer rateLimiter.Stop()

	var wg sync.WaitGroup

	completedPages := make(map[int]bool)
	updatedETags = make(map[int]string)

	wg.Add(1)

	requestQueue <- Request[T]{
		URL:     baseURL,
		Params:  initialQueryParameters,
		Result:  &responseType,
		Page:    1,
		Retries: 0,
	}

	go func() {
		for request := range requestQueue {
			<-rateLimiter.C

			if !completedPages[request.Page] || request.Retries > 0 {
				completedPages[request.Page] = true
			}

			if prevETag, exists := previousETags[request.Page]; exists {
				request.Params.Set("If-None-Match", prevETag)
			}

			response, headers, fetchErr := fetchData(request.URL, request.Params, request.Result)
			if fetchErr != nil {
				if request.Retries < 3 {
					fmt.Printf("Error fetching: %v, retrying...\n", fetchErr)
					request.Retries++
					wg.Add(1)
					requestQueue <- request
				} else {
					fmt.Printf("Error fetching: %v, max retries reached.\n", fetchErr)
				}
				wg.Done()
				continue
			}

			allItems = append(allItems, response...)
			updatedETags[request.Page] = headers.Get("ETag")

			pagesAvailable := getAvailablePageCount(headers)
			for page := 2; page <= pagesAvailable; page++ {
				if !completedPages[page] {
					newParams := setPageQueryParameters(initialQueryParameters, page)
					wg.Add(1)
					requestQueue <- Request[T]{URL: baseURL, Params: newParams, Result: &responseType, Page: page, Retries: 0}
				}
			}

			wg.Done()
		}

	}()

	wg.Wait()

	close(requestQueue)

	return
}

func fetchData[T any](baseURL url.URL, queryParameters url.Values, responseType *[]T) (items []T, headers http.Header, err error) {
	items, headers, err = MakeHttpRequest(baseURL.String(), nil, queryParameters, *responseType)
	return
}

func setPageQueryParameters(initialQueryParameters url.Values, page int) (queryParameters url.Values) {
	queryParameters = url.Values{}
	for key, values := range initialQueryParameters {
		queryParameters[key] = append([]string{}, values...)
	}

	queryParameters.Set("page", strconv.Itoa(page))
	return
}

func getAvailablePageCount(headers http.Header) (pageCount int) {
	pageCount = 1

	if headers == nil {
		return
	}

	pagesAsString := headers.Get("X-Pages")
	if pagesAsString == "" {
		return
	}

	pageCount, err := strconv.Atoi(pagesAsString)
	if err != nil {
		pageCount = 1
		fmt.Println("error parsing X-Pages header: %w", err)
	}

	return
}
