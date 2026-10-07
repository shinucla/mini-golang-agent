package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.Status, e.Body)
}

var errStopStream = errors.New("stop stream")

const maxAttempts = 4

func postStream(ctx context.Context, url string, headers map[string]string, body any) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
		} else if resp.StatusCode < 300 {
			return resp, nil
		} else {
			lastErr = readAPIError(resp)
			if !retryable(resp.StatusCode) {
				return nil, lastErr
			}
		}
		if attempt == maxAttempts {
			break
		}
		if err := sleep(ctx, backoff(attempt, resp)); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func getJSON(ctx context.Context, url string, headers map[string]string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if 300 <= resp.StatusCode {
		return readAPIError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func readAPIError(resp *http.Response) error {
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	body := strings.TrimSpace(string(data))
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &parsed) == nil && parsed.Error.Message != "" {
		body = parsed.Error.Message
	}
	return &APIError{Status: resp.StatusCode, Body: body}
}

func retryable(status int) bool {
	switch status {
	case 408, 409, 429, 500, 502, 503, 504, 529:
		return true
	}
	return false
}

func backoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && 0 < secs && secs <= 60 {
			return time.Duration(secs) * time.Second
		}
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func readSSE(r io.Reader, handle func(data []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	var buf bytes.Buffer
	flush := func() error {
		if buf.Len() == 0 {
			return nil
		}
		data := bytes.Clone(buf.Bytes())
		buf.Reset()
		return handle(data)
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		value, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		if buf.Len() != 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(strings.TrimPrefix(value, " "))
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

func streamSSE(resp *http.Response, handle func(data []byte) error) error {
	defer resp.Body.Close()
	err := readSSE(resp.Body, handle)
	if errors.Is(err, errStopStream) {
		return nil
	}
	return err
}
