package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type BatchRequest struct {
	Queries []string `json:"queries"`
}

type GoogleResponse struct {
	Items []struct {
		Link string `json:"link"`
	} `json:"items"`
}

func enableCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
}

func getImages(query string, limit int) ([]string, error) {
	cx := os.Getenv("GOOGLE_CX")
	key := os.Getenv("GOOGLE_KEY")
	if cx == "" || key == "" {
		return nil, fmt.Errorf("GOOGLE_CX or GOOGLE_KEY not set")
	}

	searchURL := fmt.Sprintf(
		"https://www.googleapis.com/customsearch/v1?key=%s&cx=%s&q=%s&searchType=image&num=%d",
		key, cx, url.QueryEscape(query), limit,
	)

	resp, err := http.Get(searchURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("Google API error: %s", string(body))
	}

	body, _ := ioutil.ReadAll(resp.Body)

	var data GoogleResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	images := []string{}
	for _, item := range data.Items {
		images = append(images, item.Link)
	}

	return images, nil
}

func batchHandler(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		return
	}

	var req BatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}

	results := make(map[string][]string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4) // параллелизм

	for _, q := range req.Queries {
		query := strings.TrimSpace(q)
		if query == "" {
			continue
		}

		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			imgs, err := getImages(q, 5)
			if err == nil {
				mu.Lock()
				results[q] = imgs
				mu.Unlock()
			} else {
				fmt.Println("Error for query:", q, err)
			}
			time.Sleep(300 * time.Millisecond) // анти-бан
		}(query)
	}

	wg.Wait()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func main() {
	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.HandleFunc("/batch", batchHandler)

	fmt.Println("Server started on http://localhost:8888")
	http.ListenAndServe(":8888", nil)
}
