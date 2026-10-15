package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProfileIncludesPublicMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("request path = %q, want /user", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"Enoch7768","name":"Enoch","html_url":"https://github.com/Enoch7768","avatar_url":"https://avatars.githubusercontent.com/u/1","bio":"Developer","company":"FuzeWeb","location":"Kampala","public_repos":12,"followers":34,"following":56}`))
	}))
	defer server.Close()

	client := New("test-token")
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	profile, err := client.Profile(t.Context())
	if err != nil {
		t.Fatalf("Profile returned error: %v", err)
	}
	if profile.Login != "Enoch7768" || profile.Name != "Enoch" || profile.AvatarURL == "" || profile.PublicRepos != 12 || profile.Followers != 34 || profile.Following != 56 {
		t.Fatalf("profile metadata was not decoded: %#v", profile)
	}
}
