package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetUserPasswordSendsTheWholeUser(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/application/users/3":
			w.Write([]byte(`{"object":"user","attributes":{"id":3,"email":"a@b.c","username":"admin","first_name":"A","last_name":"B","language":"en","root_admin":true,"external_id":null}}`))
		case "PATCH /api/application/users/3":
			json.NewDecoder(r.Body).Decode(&got)
			w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, AppKey: "k", Kind: Pterodactyl2}
	if err := c.SetUserPassword(context.Background(), 3, "secret123"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"password": "secret123", "email": "a@b.c", "username": "admin", "first_name": "A", "last_name": "B", "language": "en"}
	if len(got) != len(want) {
		t.Fatalf("body %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}
