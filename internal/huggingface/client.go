package huggingface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the default Hugging Face Hub endpoint.
	DefaultBaseURL = "https://huggingface.co"

	maxErrorBodyBytes = 64 * 1024
	maxAPIBodyBytes   = 16 * 1024 * 1024
	downloadBufSize   = 64 * 1024
)

var (
	// ErrNoGGUFFiles indicates a repository contains no .gguf model files.
	ErrNoGGUFFiles = errors.New("no GGUF files found in repository")
	// ErrInsufficientRAM indicates none of the available GGUF files fit in available RAM.
	ErrInsufficientRAM = errors.New("no GGUF quantization fits available RAM")
	// ErrQuantNotFound indicates the explicitly requested quantization was not found.
	ErrQuantNotFound = errors.New("requested quantization not found in repository")
	// ErrInvalidRepoID indicates an invalid or unsafe Hugging Face repository identifier.
	ErrInvalidRepoID = errors.New("invalid Hugging Face repository ID")
	// ErrInvalidFilename indicates an invalid or unsafe file path.
	ErrInvalidFilename = errors.New("invalid Hugging Face filename")
	// ErrUnauthorized indicates authentication is required or rejected (401/403).
	ErrUnauthorized = errors.New("huggingface API unauthorized or gated model requires HF_TOKEN")
	// ErrNotFound indicates the requested model or file was not found (404).
	ErrNotFound = errors.New("huggingface repository or file not found")
)

// Client interacts with the Hugging Face Hub REST API and file endpoints.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func newDefaultHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
	}
	return &http.Client{
		Transport: transport,
	}
}

// NewClient constructs a Hugging Face client using baseURL (defaulting to https://huggingface.co).
func NewClient(baseURL string) *Client {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		trimmed = DefaultBaseURL
	}
	return &Client{
		BaseURL:    strings.TrimRight(trimmed, "/"),
		HTTPClient: newDefaultHTTPClient(),
	}
}

func (c *Client) baseURL() string {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return DefaultBaseURL
	}
	return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return newDefaultHTTPClient()
}

func (c *Client) authToken() string {
	if c != nil && strings.TrimSpace(c.Token) != "" {
		return strings.TrimSpace(c.Token)
	}
	if tok := strings.TrimSpace(os.Getenv("HF_TOKEN")); tok != "" {
		return tok
	}
	return strings.TrimSpace(os.Getenv("HUGGING_FACE_HUB_TOKEN"))
}

func (c *Client) newRequest(ctx context.Context, method, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "microgravity-cli/1.0")
	if tok := c.authToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return req, nil
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxErrorBodyBytes))
	_ = body.Close()
}

func readBoundedErrorBody(body io.ReadCloser) string {
	if body == nil {
		return ""
	}
	data, _ := io.ReadAll(io.LimitReader(body, maxErrorBodyBytes))
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxErrorBodyBytes))
	_ = body.Close()
	return strings.TrimSpace(string(data))
}

func mapHTTPError(statusCode int, bodyText string) error {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		if bodyText != "" {
			return fmt.Errorf("%w (HTTP %d): %s", ErrUnauthorized, statusCode, bodyText)
		}
		return fmt.Errorf("%w (HTTP %d)", ErrUnauthorized, statusCode)
	case http.StatusNotFound:
		if bodyText != "" {
			return fmt.Errorf("%w (HTTP %d): %s", ErrNotFound, statusCode, bodyText)
		}
		return fmt.Errorf("%w (HTTP %d)", ErrNotFound, statusCode)
	default:
		if bodyText != "" {
			return fmt.Errorf("huggingface API returned HTTP %d: %s", statusCode, bodyText)
		}
		return fmt.Errorf("huggingface API returned HTTP %d", statusCode)
	}
}

// SearchOptions configures Hugging Face /api/models search queries.
type SearchOptions struct {
	Query    string
	Filter   string
	GGUFOnly bool
	Sort     string // "downloads", "likes", "lastModified"
	Limit    int
}

// ModelSummary represents a model entry returned by Hugging Face /api/models.
type ModelSummary struct {
	ID           string   `json:"id"`
	ModelID      string   `json:"modelId,omitempty"`
	Author       string   `json:"author,omitempty"`
	Downloads    int64    `json:"downloads"`
	Likes        int64    `json:"likes"`
	LastModified string   `json:"lastModified,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	PipelineTag  string   `json:"pipeline_tag,omitempty"`
	Private      bool     `json:"private,omitempty"`
	Gated        any      `json:"gated,omitempty"`
}

// SearchModels queries Hugging Face /api/models using SearchOptions.
func (c *Client) SearchModels(ctx context.Context, opts SearchOptions) ([]ModelSummary, error) {
	if c == nil {
		return nil, fmt.Errorf("huggingface client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	u, err := url.Parse(c.baseURL() + "/api/models")
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	q := u.Query()
	if query := strings.TrimSpace(opts.Query); query != "" {
		q.Set("search", query)
	}

	filter := strings.TrimSpace(opts.Filter)
	if filter == "" && opts.GGUFOnly {
		filter = "gguf"
	} else if filter != "" && opts.GGUFOnly && !strings.Contains(strings.ToLower(filter), "gguf") {
		filter = filter + ",gguf"
	}
	if filter != "" {
		q.Set("filter", filter)
	}

	sortBy := strings.TrimSpace(opts.Sort)
	if sortBy == "" {
		sortBy = "downloads"
	}
	q.Set("sort", sortBy)

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	req, err := c.newRequest(ctx, http.MethodGet, u.String())
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("huggingface search cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("huggingface search failed: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		errBody := readBoundedErrorBody(resp.Body)
		return nil, mapHTTPError(resp.StatusCode, errBody)
	}

	var results []ModelSummary
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxAPIBodyBytes))
	if err := dec.Decode(&results); err != nil {
		return nil, fmt.Errorf("decode huggingface search response: %w", err)
	}

	for i := range results {
		if results[i].ID == "" && results[i].ModelID != "" {
			results[i].ID = results[i].ModelID
		}
		if results[i].Author == "" && strings.Contains(results[i].ID, "/") {
			results[i].Author = strings.SplitN(results[i].ID, "/", 2)[0]
		}
	}

	return results, nil
}

// GGUFFile describes a GGUF weight file in a Hugging Face repository.
type GGUFFile struct {
	Filename       string  `json:"filename"`
	SizeBytes      int64   `json:"size_bytes"`
	Quantization   string  `json:"quantization"`
	EstimatedRAMGB float64 `json:"estimated_ram_gb"`
	IsMMProj       bool    `json:"is_mmproj,omitempty"`
}

type apiModelResponse struct {
	ID       string       `json:"id"`
	ModelID  string       `json:"modelId"`
	Siblings []apiSibling `json:"siblings"`
}

type apiSibling struct {
	RFilename string  `json:"rfilename"`
	Size      int64   `json:"size"`
	LFS       *apiLFS `json:"lfs,omitempty"`
}

type apiLFS struct {
	OID         string `json:"oid"`
	Size        int64  `json:"size"`
	PointerSize int64  `json:"pointerSize"`
}

// ValidateRepoID checks that repoID is a safe owner/repo (or single-segment) Hugging Face identifier.
func ValidateRepoID(repoID string) (string, error) {
	clean := strings.TrimSpace(repoID)
	if clean == "" {
		return "", fmt.Errorf("%w: empty repository ID", ErrInvalidRepoID)
	}
	if strings.ContainsAny(clean, "\\\x00?#:") || strings.ContainsAny(clean, " \t\r\n") {
		return "", fmt.Errorf("%w: %q contains disallowed characters", ErrInvalidRepoID, repoID)
	}
	if strings.HasPrefix(clean, "/") || strings.HasSuffix(clean, "/") {
		return "", fmt.Errorf("%w: %q has leading or trailing slash", ErrInvalidRepoID, repoID)
	}

	segments := strings.Split(clean, "/")
	if len(segments) > 2 {
		return "", fmt.Errorf("%w: %q has too many path segments", ErrInvalidRepoID, repoID)
	}
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, "..") {
			return "", fmt.Errorf("%w: %q contains invalid path segment", ErrInvalidRepoID, repoID)
		}
	}
	return clean, nil
}

// ValidateFilename checks that filename is a relative path with no traversal or escape vectors.
func ValidateFilename(filename string) (string, error) {
	trimmed := strings.TrimSpace(filename)
	if trimmed == "" {
		return "", fmt.Errorf("%w: empty filename", ErrInvalidFilename)
	}
	if strings.ContainsAny(trimmed, "\\\x00?#:") {
		return "", fmt.Errorf("%w: %q contains disallowed characters", ErrInvalidFilename, filename)
	}
	if strings.HasPrefix(trimmed, "/") || filepath.IsAbs(trimmed) || filepath.VolumeName(trimmed) != "" {
		return "", fmt.Errorf("%w: %q must be a relative path", ErrInvalidFilename, filename)
	}

	segments := strings.Split(trimmed, "/")
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, "..") {
			return "", fmt.Errorf("%w: %q contains path traversal or empty segment", ErrInvalidFilename, filename)
		}
	}
	return path.Clean(trimmed), nil
}

func escapeRepoPath(repoID string) string {
	parts := strings.Split(repoID, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func escapeFilePath(relPath string) string {
	parts := strings.Split(relPath, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// ParseRef parses hf://<owner>/<repo>[:quant] or hf.co/<owner>/<repo>[:quant].
// It returns the clean repoID, optional quant tag, and whether the input had an HF prefix.
func ParseRef(ref string) (repoID, quant string, isHF bool) {
	trimmed := strings.TrimSpace(ref)
	lower := strings.ToLower(trimmed)

	var body string
	switch {
	case strings.HasPrefix(lower, "hf://"):
		body = trimmed[len("hf://"):]
		isHF = true
	case strings.HasPrefix(lower, "hf.co/"):
		body = trimmed[len("hf.co/"):]
		isHF = true
	case strings.HasPrefix(lower, "https://huggingface.co/"):
		body = trimmed[len("https://huggingface.co/"):]
		isHF = true
	case strings.HasPrefix(lower, "http://huggingface.co/"):
		body = trimmed[len("http://huggingface.co/"):]
		isHF = true
	default:
		body = trimmed
	}

	body = strings.Trim(body, "/")
	if idx := strings.LastIndex(body, ":"); idx != -1 {
		repoID = strings.TrimSpace(body[:idx])
		quant = strings.TrimSpace(body[idx+1:])
	} else {
		repoID = body
	}
	return repoID, quant, isHF
}

// OllamaRef formats an owner/repo and optional quant tag into an Ollama-pullable hf.co reference.
func OllamaRef(repoID, quant string) string {
	cleanRepo, embeddedQuant, _ := ParseRef(repoID)
	q := strings.TrimSpace(quant)
	if q == "" {
		q = embeddedQuant
	}
	if q != "" {
		return fmt.Sprintf("hf.co/%s:%s", cleanRepo, q)
	}
	return fmt.Sprintf("hf.co/%s", cleanRepo)
}

// EstimateRAMGB computes estimated runtime RAM requirement in GiB from GGUF file size bytes
// (model weights + ~20% KV-cache/activation overhead + 0.5 GiB runtime headroom).
func EstimateRAMGB(sizeBytes int64) float64 {
	if sizeBytes <= 0 {
		return 0
	}
	const giB = 1024.0 * 1024.0 * 1024.0
	fileGB := float64(sizeBytes) / giB
	est := fileGB*1.20 + 0.50
	return math.Round(est*100) / 100
}

var quantPattern = regexp.MustCompile(`(?i)(?:^|[-_./])(` +
	`IQ[1-4][-_](?:XXS|XS|NL|M|S)|` +
	`Q[2-8][-_]K[-_](?:XL|L|M|S)|` +
	`Q[2-8][-_]K|` +
	`Q[2-8][-_][01]|` +
	`BF16|FP16|F16|FP32|F32|` +
	`Q[2-8]` +
	`)(?:[-_.]|\.gguf|$)`)

// ExtractQuantization parses a standard GGUF quantization tag from a filename.
func ExtractQuantization(filename string) string {
	base := path.Base(filename)
	m := quantPattern.FindStringSubmatch(base)
	if len(m) < 2 {
		return ""
	}
	return normalizeQuant(m[1])
}

func normalizeQuant(q string) string {
	norm := strings.ToUpper(strings.TrimSpace(q))
	norm = strings.ReplaceAll(norm, "-", "_")
	switch norm {
	case "FP16":
		return "F16"
	case "FP32":
		return "F32"
	default:
		return norm
	}
}

func quantQualityScore(quant string) int {
	switch normalizeQuant(quant) {
	case "F32":
		return 100
	case "BF16":
		return 96
	case "F16":
		return 95
	case "Q8_0", "Q8":
		return 90
	case "Q6_K", "Q6":
		return 85
	case "Q5_K_L":
		return 82
	case "Q5_K_M":
		return 80
	case "Q5_K_S", "Q5_K":
		return 76
	case "Q5_1", "Q5_0", "Q5":
		return 74
	case "Q4_K_XL", "Q4_K_L":
		return 72
	case "Q4_K_M":
		return 70
	case "IQ4_NL":
		return 66
	case "Q4_K_S", "Q4_K":
		return 64
	case "IQ4_XS":
		return 62
	case "Q4_1", "Q4_0", "Q4":
		return 58
	case "Q3_K_XL", "Q3_K_L":
		return 50
	case "Q3_K_M", "Q3_K":
		return 46
	case "Q3_K_S":
		return 42
	case "IQ3_M":
		return 40
	case "IQ3_S", "IQ3_XS", "IQ3_XXS":
		return 35
	case "Q2_K_XL", "Q2_K_L":
		return 30
	case "Q2_K", "Q2_K_S", "Q2":
		return 28
	case "IQ2_M", "IQ2_S", "IQ2_XS", "IQ2_XXS":
		return 20
	case "IQ1_M", "IQ1_S":
		return 10
	default:
		return 1
	}
}

// ListGGUFFiles queries /api/models/<repoID>?blobs=true and extracts .gguf files, sizes,
// quantization tags, and estimated RAM requirements.
func (c *Client) ListGGUFFiles(ctx context.Context, repoID string) ([]GGUFFile, error) {
	if c == nil {
		return nil, fmt.Errorf("huggingface client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cleanRepo, err := ValidateRepoID(repoID)
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/api/models/%s?blobs=true", c.baseURL(), escapeRepoPath(cleanRepo))
	req, err := c.newRequest(ctx, http.MethodGet, reqURL)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("huggingface model metadata cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("huggingface model metadata failed for %s: %w", cleanRepo, err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		errBody := readBoundedErrorBody(resp.Body)
		return nil, mapHTTPError(resp.StatusCode, errBody)
	}

	var modelResp apiModelResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxAPIBodyBytes))
	if err := dec.Decode(&modelResp); err != nil {
		return nil, fmt.Errorf("decode huggingface model metadata for %s: %w", cleanRepo, err)
	}

	var files []GGUFFile
	for _, sib := range modelResp.Siblings {
		if !strings.HasSuffix(strings.ToLower(sib.RFilename), ".gguf") {
			continue
		}
		size := sib.Size
		if size <= 0 && sib.LFS != nil && sib.LFS.Size > 0 {
			size = sib.LFS.Size
		}
		quant := ExtractQuantization(sib.RFilename)
		isMMProj := strings.Contains(strings.ToLower(path.Base(sib.RFilename)), "mmproj")
		files = append(files, GGUFFile{
			Filename:       sib.RFilename,
			SizeBytes:      size,
			Quantization:   quant,
			EstimatedRAMGB: EstimateRAMGB(size),
			IsMMProj:       isMMProj,
		})
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoGGUFFiles, cleanRepo)
	}
	return files, nil
}

// SelectBestGGUF picks the highest quality GGUF file that fits within availableRAMGB
// (including overhead), or matches preferredQuant when specified.
func SelectBestGGUF(files []GGUFFile, availableRAMGB float64, preferredQuant string) (GGUFFile, error) {
	if len(files) == 0 {
		return GGUFFile{}, ErrNoGGUFFiles
	}

	// Normalize candidate metadata in case callers constructed GGUFFile literals directly.
	candidates := make([]GGUFFile, 0, len(files))
	for _, f := range files {
		if f.Quantization == "" {
			f.Quantization = ExtractQuantization(f.Filename)
		}
		if f.EstimatedRAMGB <= 0 && f.SizeBytes > 0 {
			f.EstimatedRAMGB = EstimateRAMGB(f.SizeBytes)
		}
		if !f.IsMMProj && strings.Contains(strings.ToLower(path.Base(f.Filename)), "mmproj") {
			f.IsMMProj = true
		}
		candidates = append(candidates, f)
	}

	// Prefer non-mmproj model weight files unless all files are mmproj.
	modelWeights := make([]GGUFFile, 0, len(candidates))
	for _, f := range candidates {
		if !f.IsMMProj {
			modelWeights = append(modelWeights, f)
		}
	}
	if len(modelWeights) > 0 {
		candidates = modelWeights
	}

	pref := normalizeQuant(preferredQuant)
	if pref != "" {
		var exactMatches []GGUFFile
		for _, f := range candidates {
			if normalizeQuant(f.Quantization) == pref ||
				strings.Contains(normalizeQuant(f.Filename), pref) {
				exactMatches = append(exactMatches, f)
			}
		}
		if len(exactMatches) == 0 {
			return GGUFFile{}, fmt.Errorf("%w: %s", ErrQuantNotFound, preferredQuant)
		}
		best := exactMatches[0]
		for _, f := range exactMatches[1:] {
			if f.SizeBytes > 0 && (best.SizeBytes == 0 || f.SizeBytes < best.SizeBytes) {
				best = f
			}
		}
		return best, nil
	}

	var fitting []GGUFFile
	for _, f := range candidates {
		if availableRAMGB <= 0 {
			fitting = append(fitting, f)
			continue
		}
		if f.EstimatedRAMGB > 0 && f.EstimatedRAMGB <= availableRAMGB {
			fitting = append(fitting, f)
		} else if f.EstimatedRAMGB == 0 && f.SizeBytes == 0 {
			// Unknown file size (e.g. minimal mock): keep as fallback only if sized candidates don't fit.
			continue
		}
	}

	if len(fitting) == 0 && availableRAMGB > 0 {
		// Check if there are only unknown-size files.
		var unknownSized []GGUFFile
		for _, f := range candidates {
			if f.SizeBytes <= 0 {
				unknownSized = append(unknownSized, f)
			}
		}
		if len(unknownSized) == len(candidates) {
			fitting = unknownSized
		} else {
			smallestRAM := math.MaxFloat64
			for _, f := range candidates {
				if f.EstimatedRAMGB > 0 && f.EstimatedRAMGB < smallestRAM {
					smallestRAM = f.EstimatedRAMGB
				}
			}
			return GGUFFile{}, fmt.Errorf("%w: available %.1f GB, smallest GGUF requires ~%.1f GB", ErrInsufficientRAM, availableRAMGB, smallestRAM)
		}
	}

	best := fitting[0]
	bestScore := quantQualityScore(best.Quantization)
	for _, f := range fitting[1:] {
		score := quantQualityScore(f.Quantization)
		if score > bestScore {
			best = f
			bestScore = score
		} else if score == bestScore && f.SizeBytes > 0 && (best.SizeBytes == 0 || f.SizeBytes < best.SizeBytes) {
			best = f
		}
	}
	return best, nil
}

// DownloadGGUF downloads filename from repoID into destDir with strict path-traversal protection,
// resumable .part transfers via HTTP Range, atomic rename on completion, and progress updates.
func (c *Client) DownloadGGUF(ctx context.Context, repoID, filename, destDir string, progressFn func(completed, total int64)) (string, error) {
	if c == nil {
		return "", fmt.Errorf("huggingface client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	cleanRepo, err := ValidateRepoID(repoID)
	if err != nil {
		return "", err
	}
	cleanFile, err := ValidateFilename(filename)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(destDir) == "" {
		return "", fmt.Errorf("%w: destination directory is empty", ErrInvalidFilename)
	}

	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("resolve destination directory: %w", err)
	}
	destPath := filepath.Join(absDestDir, filepath.FromSlash(cleanFile))
	rel, err := filepath.Rel(absDestDir, destPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: resolved path escapes destination directory", ErrInvalidFilename)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", fmt.Errorf("create destination directory: %w", err)
	}

	downloadURL := fmt.Sprintf("%s/%s/resolve/main/%s", c.baseURL(), escapeRepoPath(cleanRepo), escapeFilePath(cleanFile))
	partPath := destPath + ".part"

	return c.downloadWithResume(ctx, downloadURL, partPath, destPath, progressFn, true)
}

func (c *Client) downloadWithResume(ctx context.Context, downloadURL, partPath, destPath string, progressFn func(completed, total int64), allowRetry416 bool) (string, error) {
	var offset int64
	if info, err := os.Stat(partPath); err == nil && !info.IsDir() && info.Size() > 0 {
		offset = info.Size()
	}

	req, err := c.newRequest(ctx, http.MethodGet, downloadURL)
	if err != nil {
		return "", err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("download cancelled: %w", ctx.Err())
		}
		return "", fmt.Errorf("download request failed: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		totalFromHeader := parseContentRangeTotal(resp.Header.Get("Content-Range"))
		if totalFromHeader > 0 && offset == totalFromHeader {
			if err := os.Rename(partPath, destPath); err != nil {
				return "", fmt.Errorf("finalize completed part file: %w", err)
			}
			if progressFn != nil {
				progressFn(offset, totalFromHeader)
			}
			return destPath, nil
		}
		_ = os.Remove(partPath)
		if allowRetry416 {
			return c.downloadWithResume(ctx, downloadURL, partPath, destPath, progressFn, false)
		}
		return "", fmt.Errorf("huggingface returned HTTP 416 for %s", downloadURL)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		errBody := readBoundedErrorBody(resp.Body)
		return "", mapHTTPError(resp.StatusCode, errBody)
	}

	var f *os.File
	var completed int64
	var total int64

	if resp.StatusCode == http.StatusPartialContent && offset > 0 {
		rangeStart, rangeTotal := parseContentRange(resp.Header.Get("Content-Range"))
		if rangeStart >= 0 && rangeStart != offset {
			// Server returned unexpected offset; restart cleanly if 0, otherwise error.
			if rangeStart == 0 {
				f, err = os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				completed = 0
			} else {
				return "", fmt.Errorf("unexpected Content-Range start %d (requested %d)", rangeStart, offset)
			}
		} else {
			f, err = os.OpenFile(partPath, os.O_WRONLY|os.O_APPEND, 0644)
			completed = offset
		}
		if rangeTotal > 0 {
			total = rangeTotal
		} else if resp.ContentLength > 0 {
			total = completed + resp.ContentLength
		}
	} else {
		f, err = os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		completed = 0
		total = resp.ContentLength
	}
	if err != nil {
		return "", fmt.Errorf("open part file %s: %w", partPath, err)
	}

	// Ensure file handle is closed before return or rename.
	closed := false
	closeFile := func() error {
		if closed {
			return nil
		}
		closed = true
		return f.Close()
	}
	defer func() {
		_ = closeFile()
	}()

	if progressFn != nil && completed > 0 {
		progressFn(completed, total)
	}

	buf := make([]byte, downloadBufSize)
	var bytesReadThisAttempt int64
	for {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("download cancelled: %w", err)
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return "", fmt.Errorf("write part file: %w", writeErr)
			}
			bytesReadThisAttempt += int64(n)
			completed += int64(n)
			if progressFn != nil {
				reportTotal := total
				if reportTotal <= 0 {
					reportTotal = completed
				}
				progressFn(completed, reportTotal)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			if ctx.Err() != nil {
				return "", fmt.Errorf("download cancelled: %w", ctx.Err())
			}
			return "", fmt.Errorf("read download stream: %w", readErr)
		}
	}

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("download cancelled: %w", err)
	}
	if resp.ContentLength > 0 && bytesReadThisAttempt < resp.ContentLength {
		return "", fmt.Errorf("incomplete download (%d/%d bytes in response): %w", bytesReadThisAttempt, resp.ContentLength, io.ErrUnexpectedEOF)
	}
	if total > 0 && completed < total {
		return "", fmt.Errorf("incomplete download (%d/%d total bytes): %w", completed, total, io.ErrUnexpectedEOF)
	}

	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync part file: %w", err)
	}
	if err := closeFile(); err != nil {
		return "", fmt.Errorf("close part file: %w", err)
	}

	if err := os.Rename(partPath, destPath); err != nil {
		return "", fmt.Errorf("finalize downloaded file: %w", err)
	}
	return destPath, nil
}

func parseContentRangeTotal(header string) int64 {
	_, total := parseContentRange(header)
	return total
}

// parseContentRange parses headers like "bytes 100-999/1000" or "bytes */1000".
func parseContentRange(header string) (start, total int64) {
	start = -1
	total = -1
	header = strings.TrimSpace(header)
	if header == "" {
		return start, total
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bytes") {
		return start, total
	}
	rangeAndTotal := strings.SplitN(strings.TrimSpace(parts[1]), "/", 2)
	if len(rangeAndTotal) != 2 {
		return start, total
	}
	if rangeAndTotal[1] != "*" {
		if t, err := strconv.ParseInt(strings.TrimSpace(rangeAndTotal[1]), 10, 64); err == nil {
			total = t
		}
	}
	if rangeAndTotal[0] != "*" {
		bounds := strings.SplitN(rangeAndTotal[0], "-", 2)
		if len(bounds) == 2 {
			if s, err := strconv.ParseInt(strings.TrimSpace(bounds[0]), 10, 64); err == nil {
				start = s
			}
		}
	}
	return start, total
}
