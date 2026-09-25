package catalogruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

func TestHTTPErrorClassification(t *testing.T) {
	tests := []struct {
		status int
		kind provider.ErrorKind
	}{
		{http.StatusUnauthorized, provider.ErrorUnauthorized},
		{http.StatusForbidden, provider.ErrorUnauthorized},
		{http.StatusNotFound, provider.ErrorModelNotFound},
		{http.StatusTooManyRequests, provider.ErrorRateLimited},
		{http.StatusRequestTimeout, provider.ErrorRateLimited},
		{http.StatusConflict, provider.ErrorOverloaded},
		{http.StatusUnprocessableEntity, provider.ErrorBadRequest},
		{http.StatusBadRequest, provider.ErrorBadRequest},
		{http.StatusInternalServerError, provider.ErrorProviderUnavailable},
		{http.StatusBadGateway, provider.ErrorProviderUnavailable},
		{http.StatusServiceUnavailable, provider.ErrorProviderUnavailable},
	}
	for _, test := range tests {
		err := classifyHTTPError("test", test.status, http.Header{"Retry-After": []string{"7"}}, []byte("provider error"))
		if err.Kind != test.kind {
			t.Fatalf("status %d: kind = %q, want %q", test.status, err.Kind, test.kind)
		}
		if err.RetryAfter != 7 {
			t.Fatalf("status %d: retry-after = %d, want 7", test.status, err.RetryAfter)
		}
	}
}

func TestListModelsFiltersUnsafeAndDuplicateIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"default"},{"id":"auto"},{"id":" real-model "},{"id":"real-model"},{"id":""}]}`))
	}))
	defer server.Close()

	p := New("test", "", server.URL, "", 10000, 1000, 500)
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0] != "real-model" {
		t.Fatalf("models = %#v, want [real-model]", models)
	}
}
