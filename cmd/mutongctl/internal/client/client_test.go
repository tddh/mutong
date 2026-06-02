package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDo_GET_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token", "test")
	data, err := c.Do(context.Background(), "GET", "/api/test", nil)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if !strings.Contains(string(data), "ok") {
		t.Errorf("body = %s, want contains 'ok'", string(data))
	}
}

func TestDo_401_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "bad-token", "test")
	_, err := c.Do(context.Background(), "GET", "/api/test", nil)
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "[ERROR] 认证失败") {
		t.Errorf("error should contain '[ERROR] 认证失败', got: %v", err)
	}
}

func TestDo_404_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token", "test")
	_, err := c.Do(context.Background(), "GET", "/api/nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "[ERROR] 资源不存在") {
		t.Errorf("error should contain '[ERROR] 资源不存在', got: %v", err)
	}
}

func TestDo_NetworkError(t *testing.T) {
	c := NewClient("http://127.0.0.1:19999", "token", "test")
	_, err := c.Do(context.Background(), "GET", "/api/test", nil)
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "[ERROR] 网络连接失败") {
		t.Errorf("error should contain '[ERROR] 网络连接失败', got: %v", err)
	}
}

func TestDo_POST_WithBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		w.WriteHeader(201)
		w.Write([]byte(`{"created":true}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token", "test")
	data, err := c.Do(context.Background(), "POST", "/api/create", map[string]string{"name": "test"})
	if err != nil {
		t.Fatalf("POST Do error: %v", err)
	}
	if !strings.Contains(string(data), "created") {
		t.Errorf("body = %s", string(data))
	}
}
