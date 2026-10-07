package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxFetchChars = 100000

var (
	htmlDropBlocks = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script\b.*?</script>`),
		regexp.MustCompile(`(?is)<style\b.*?</style>`),
		regexp.MustCompile(`(?is)<noscript\b.*?</noscript>`),
		regexp.MustCompile(`(?is)<svg\b.*?</svg>`),
		regexp.MustCompile(`(?s)<!--.*?-->`),
	}
	htmlBreaks     = regexp.MustCompile(`(?i)<(br|/p|/div|/li|/tr|/h[1-6]|/section|/article)\b[^>]*>`)
	htmlListItem   = regexp.MustCompile(`(?i)<li\b[^>]*>`)
	htmlTags       = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRuns      = regexp.MustCompile(`[ \t\f\r]+`)
	blankLineRuns  = regexp.MustCompile(`\n\s*\n(\s*\n)+`)
	fetchClient    = &http.Client{Timeout: 30 * time.Second}
	fetchUserAgent = "Mozilla/5.0 (compatible; mga/1.0)"
)

type WebFetch struct{}

func (WebFetch) Name() string   { return "WebFetch" }
func (WebFetch) ReadOnly() bool { return false }

func (WebFetch) Description() string {
	return "Fetch a URL over HTTP(S) and return its content as plain text. HTML is converted to text. Output is truncated at 100000 characters."
}

func (WebFetch) Schema() map[string]any {
	return schema(map[string]any{"url": str("The URL to fetch")}, "url")
}

func (WebFetch) Summary(input json.RawMessage) string { return field(input, "url") }

func Host(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (WebFetch) Run(ctx context.Context, _ *Env, input json.RawMessage) (string, error) {
	in, err := decode[struct {
		URL string `json:"url"`
	}](input)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid URL %q", in.URL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	resp, err := fetchClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", err
	}
	text := string(data)
	if strings.Contains(resp.Header.Get("Content-Type"), "html") {
		text = HTMLToText(text)
	}
	text = Truncate(text, maxFetchChars)
	if 400 <= resp.StatusCode {
		return text, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return text, nil
}

func HTMLToText(s string) string {
	for _, re := range htmlDropBlocks {
		s = re.ReplaceAllString(s, "")
	}
	s = htmlListItem.ReplaceAllString(s, "\n- ")
	s = htmlBreaks.ReplaceAllString(s, "\n")
	s = htmlTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = spaceRuns.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	s = strings.Join(lines, "\n")
	s = blankLineRuns.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
