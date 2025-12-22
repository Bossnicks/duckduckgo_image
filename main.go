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
	Queries    []string `json:"queries"`
	Categories []string `json:"categories"`
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

/* =========================
   GOOGLE KEY POOL
========================= */

var (
	googleKeys []string
	keyIndex   int
	keyMu      sync.Mutex
)

func nextKey() string {
	keyMu.Lock()
	defer keyMu.Unlock()
	key := googleKeys[keyIndex]
	keyIndex = (keyIndex + 1) % len(googleKeys)
	return key
}

/* =========================
   CATEGORY → CX MAP
========================= */

var categoryCX = map[string]string{
	"Недвижимость":           "CX_REAL_ESTATE",
	"Транспорт":              "CX_TRANSPORT",
	"Спец/сельхоз техника":   "CX_SPECIAL_TECH",
	"Оборудование":           "CX_EQUIPMENT",
	"Строительство и ремонт": "CX_CONSTRUCTION",
	"Бизнес":                 "CX_BUSINESS",
	"Одежда и обувь":         "CX_FASHION",
	"Товары для дома":        "CX_HOME_GOODS",
	"Бытовая и оргтехника":   "CX_ELECTRONICS",
	"Иное":                   "CX_MISC",
}

/* =========================
   GOOGLE IMAGE SEARCH
========================= */

func getImages(query, cx string, limit int) ([]string, error) {
	for i := 0; i < len(googleKeys); i++ {
		key := nextKey()

		searchURL := fmt.Sprintf(
			"https://www.googleapis.com/customsearch/v1?"+
				"key=%s&cx=%s&q=%s&searchType=image"+
				"&num=%d&imgType=photo&imgSize=large"+
				"&imgColorType=color",
			key,
			cx,
			url.QueryEscape(query),
			limit,
		)

		client := &http.Client{Timeout: 20 * time.Second}
		resp, err := client.Get(searchURL)
		if err != nil {
			continue
		}

		body, _ := ioutil.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 || strings.Contains(string(body), "quota") {
			continue // переключаем ключ
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("google error: %s", body)
		}

		var data GoogleResponse
		if err := json.Unmarshal(body, &data); err != nil {
			return nil, err
		}

		var images []string
		for _, item := range data.Items {
			images = append(images, item.Link)
		}
		return images, nil
	}

	return nil, fmt.Errorf("all GOOGLE_KEYS exhausted")
}

/* =========================
   BATCH HANDLER
========================= */

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

	if len(req.Queries) != len(req.Categories) {
		http.Error(w, "queries and categories count mismatch", 400)
		return
	}

	results := make(map[string][]string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	for i := range req.Queries {
		query := strings.TrimSpace(req.Queries[i])
		category := strings.TrimSpace(req.Categories[i])

		cxEnv, ok := categoryCX[category]
		if !ok {
			continue
		}

		cx := os.Getenv(cxEnv)
		if cx == "" {
			continue
		}

		wg.Add(1)
		go func(q, cx string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			imgs, err := getImages(q+" photo", cx, 5)
			if err == nil {
				mu.Lock()
				results[q] = imgs
				mu.Unlock()
			}

			time.Sleep(1000 * time.Millisecond)
		}(query, cx)
	}

	wg.Wait()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

/* =========================
   MAIN
========================= */

func main() {
	keys := os.Getenv("GOOGLE_KEYS")
	if keys == "" {
		panic("GOOGLE_KEYS not set")
	}
	googleKeys = strings.Split(keys, ",")

	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.HandleFunc("/batch", batchHandler)

	fmt.Println("Server started on http://localhost:8888")
	http.ListenAndServe(":8888", nil)
}
