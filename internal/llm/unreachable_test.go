package llm

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func closedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestUnreachableServerFailsAtOnceWithoutRetries(t *testing.T) {
	addr := closedPort(t)
	start := time.Now()
	_, err := postStream(context.Background(), "http://"+addr+"/v1/chat/completions", nil, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "cannot reach the model server at "+addr) {
		t.Fatalf("err = %v", err)
	}
	if time.Second < time.Since(start) {
		t.Fatalf("a refused connection is not retried: took %s", time.Since(start))
	}
	var models struct{}
	if err := getJSON(context.Background(), "http://"+addr+"/v1/models", nil, &models); err == nil || !strings.Contains(err.Error(), "cannot reach the model server") {
		t.Fatalf("model list err = %v", err)
	}
}

func TestSilentServerStopsAtTheConnectTimeout(t *testing.T) {
	saved := client
	client = newClient(200 * time.Millisecond)
	t.Cleanup(func() { client = saved })
	start := time.Now()
	_, err := postStream(context.Background(), "http://10.255.255.1:81/v1/chat/completions", nil, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "cannot reach the model server at 10.255.255.1:81") {
		t.Fatalf("err = %v", err)
	}
	if 3*time.Second < time.Since(start) {
		t.Fatalf("a silent server fails after one connect timeout: took %s", time.Since(start))
	}
}
