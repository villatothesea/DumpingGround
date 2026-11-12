package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateCheckFindsNewerRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9","html_url":"https://example.invalid/release"}`)
	}))
	defer srv.Close()

	m := New()
	m.updateURL = srv.URL
	m.performUpdateCheck()
	st := m.updateState()
	if !st.Checked || !st.Newer || st.Version != "v9.9.9" || st.Err != "" {
		t.Fatalf("update state = %+v", st)
	}
}

func TestUpdateCheckReportsFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	m := New()
	m.updateURL = srv.URL
	m.performUpdateCheck()
	st := m.updateState()
	if !st.Checked || st.Newer || st.Err == "" {
		t.Fatalf("update state = %+v", st)
	}
}
