package references

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type LogType struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Level       string            `json:"level"`
	Version     int               `json:"version"`
	Fields      map[string]string `json:"fields"`
}

type ServiceReference struct {
	ServiceName string    `json:"serviceName"`
	Types       []LogType `json:"types"`
}

// Sync fetches log type definitions from a Taskcluster root URL and stores
// them as JSON files in cacheDir. It returns the list of service names that
// were successfully synced.
func Sync(rootURL string, cacheDir string) ([]string, error) {
	rootURL = strings.TrimRight(rootURL, "/")

	// Fetch the references index page.
	resp, err := http.Get(rootURL + "/references/references/")
	if err != nil {
		return nil, fmt.Errorf("fetching references index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching references index: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading references index: %w", err)
	}

	// Parse the HTML to find links matching references/{service}/v1/logs.json.
	re := regexp.MustCompile(`href="([^"/]+)/"`)
	matches := re.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no log references found in %s/references/", rootURL)
	}

	// Deduplicate service names while preserving discovery order.
	seen := map[string]bool{}
	var services []string
	for _, m := range matches {
		svc := m[1]
		if !seen[svc] {
			seen[svc] = true
			services = append(services, svc)
		}
	}

	// Create cache directory.
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}

	var synced []string
	for _, svc := range services {
		url := fmt.Sprintf("%s/references/references/%s/v1/logs.json", rootURL, svc)
		data, err := fetchAndValidate(url)
		if err != nil {
			// Skip services that fail to fetch or validate.
			continue
		}

		path := filepath.Join(cacheDir, svc+".json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", path, err)
		}
		synced = append(synced, svc)
	}

	return synced, nil
}

// fetchAndValidate fetches a URL, validates the response as a ServiceReference,
// and returns the raw JSON bytes.
func fetchAndValidate(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: status %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}

	// Validate that the JSON parses as a ServiceReference.
	var ref ServiceReference
	if err := json.Unmarshal(data, &ref); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", url, err)
	}

	return data, nil
}
