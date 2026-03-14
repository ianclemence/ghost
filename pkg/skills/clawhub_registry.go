package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/utils"
)

const (
	defaultClawHubTimeout  = 30 * time.Second
	defaultMaxZipSize      = 50 * 1024 * 1024 // 50 MB
	defaultMaxResponseSize = 2 * 1024 * 1024  // 2 MB
)

type SearchResult struct {
	Score        float64 `json:"score"`
	Slug         string  `json:"slug"`
	DisplayName  string  `json:"display_name"`
	Summary      string  `json:"summary"`
	Version      string  `json:"version"`
	RegistryName string  `json:"registry_name"`
}

type SkillMeta struct {
	Slug             string `json:"slug"`
	DisplayName      string `json:"display_name"`
	Summary          string `json:"summary"`
	LatestVersion    string `json:"latest_version"`
	RegistryName     string `json:"registry_name"`
	IsMalwareBlocked bool   `json:"is_malware_blocked"`
	IsSuspicious     bool   `json:"is_suspicious"`
}

type InstallResult struct {
	Version          string `json:"version"`
	Summary          string `json:"summary"`
	IsMalwareBlocked bool   `json:"is_malware_blocked"`
	IsSuspicious     bool   `json:"is_suspicious"`
}

type ClawHubRegistry struct {
	baseURL         string
	authToken       string
	searchPath      string
	skillsPath      string
	downloadPath    string
	maxZipSize      int
	maxResponseSize int
	client          *http.Client
}

func NewClawHubRegistry(cfg config.ClawHubConfig) *ClawHubRegistry {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://clawhub.ai"
	}
	searchPath := cfg.SearchPath
	if searchPath == "" {
		searchPath = "/api/v1/search"
	}
	skillsPath := cfg.SkillsPath
	if skillsPath == "" {
		skillsPath = "/api/v1/skills"
	}
	downloadPath := cfg.DownloadPath
	if downloadPath == "" {
		downloadPath = "/api/v1/download"
	}

	timeout := defaultClawHubTimeout
	if cfg.Timeout > 0 {
		timeout = time.Duration(cfg.Timeout) * time.Second
	}

	return &ClawHubRegistry{
		baseURL:         baseURL,
		authToken:       cfg.AuthToken,
		searchPath:      searchPath,
		skillsPath:      skillsPath,
		downloadPath:    downloadPath,
		maxZipSize:      defaultMaxZipSize,
		maxResponseSize: defaultMaxResponseSize,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

type clawhubSearchResponse struct {
	Results []clawhubSearchResult `json:"results"`
}

type clawhubSearchResult struct {
	Score       float64 `json:"score"`
	Slug        *string `json:"slug"`
	DisplayName *string `json:"displayName"`
	Summary     *string `json:"summary"`
	Version     *string `json:"version"`
}

func (c *ClawHubRegistry) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	u, err := url.Parse(c.baseURL + c.searchPath)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	q := u.Query()
	q.Set("q", query)
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	u.RawQuery = q.Encode()

	body, err := c.doGet(ctx, u.String())
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}

	var resp clawhubSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}

	results := make([]SearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		slug := utils.DerefStr(r.Slug, "")
		if slug == "" {
			continue
		}

		results = append(results, SearchResult{
			Score:        r.Score,
			Slug:         slug,
			DisplayName:  utils.DerefStr(r.DisplayName, slug),
			Summary:      utils.DerefStr(r.Summary, ""),
			Version:      utils.DerefStr(r.Version, ""),
			RegistryName: "clawhub",
		})
	}

	return results, nil
}

func (c *ClawHubRegistry) GetSkillMeta(ctx context.Context, slug string) (*SkillMeta, error) {
	if err := utils.ValidateSkillIdentifier(slug); err != nil {
		return nil, err
	}

	u := c.baseURL + c.skillsPath + "/" + url.PathEscape(slug)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Slug          string `json:"slug"`
		DisplayName   string `json:"displayName"`
		Summary       string `json:"summary"`
		LatestVersion struct {
			Version string `json:"version"`
		} `json:"latestVersion"`
		Moderation struct {
			IsMalwareBlocked bool `json:"isMalwareBlocked"`
			IsSuspicious     bool `json:"isSuspicious"`
		} `json:"moderation"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return &SkillMeta{
		Slug:             resp.Slug,
		DisplayName:      resp.DisplayName,
		Summary:          resp.Summary,
		LatestVersion:    resp.LatestVersion.Version,
		IsMalwareBlocked: resp.Moderation.IsMalwareBlocked,
		IsSuspicious:     resp.Moderation.IsSuspicious,
		RegistryName:     "clawhub",
	}, nil
}

func (c *ClawHubRegistry) DownloadAndInstall(ctx context.Context, slug, version, targetDir string) (*InstallResult, error) {
	if err := utils.ValidateSkillIdentifier(slug); err != nil {
		return nil, err
	}

	meta, _ := c.GetSkillMeta(ctx, slug)
	installVersion := version
	if installVersion == "" && meta != nil {
		installVersion = meta.LatestVersion
	}
	if installVersion == "" {
		installVersion = "latest"
	}

	u, err := url.Parse(c.baseURL + c.downloadPath)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("slug", slug)
	if installVersion != "latest" {
		q.Set("version", installVersion)
	}
	u.RawQuery = q.Encode()

	tmpFile, err := os.CreateTemp("", "ghost-dl-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile.Name())

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := utils.DoRequestWithRetry(c.client, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return nil, err
	}
	tmpFile.Close()

	if err := utils.ExtractZipFile(tmpFile.Name(), targetDir); err != nil {
		return nil, err
	}

	res := &InstallResult{Version: installVersion}
	if meta != nil {
		res.Summary = meta.Summary
		res.IsMalwareBlocked = meta.IsMalwareBlocked
		res.IsSuspicious = meta.IsSuspicious
	}
	return res, nil
}

func (c *ClawHubRegistry) doGet(ctx context.Context, urlStr string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := utils.DoRequestWithRetry(c.client, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(c.maxResponseSize)))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}
