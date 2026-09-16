package system

import (
	"context"
	"errors"
	"testing"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// fakeQuerier stands in for the generated db.Querier. Only Ping is exercised
// here; the rest of the interface is satisfied by the embedded nil, which
// panics if anything reaches for it — a louder failure than a zero value.
type fakeQuerier struct {
	db.Querier
	pingErr error
}

func (f fakeQuerier) Ping(context.Context) (int32, error) {
	if f.pingErr != nil {
		return 0, f.pingErr
	}
	return 1, nil
}

func TestGetHealthOK(t *testing.T) {
	h := New(Deps{Querier: fakeQuerier{}, Version: "1.2.3"})

	resp, err := h.GetHealth(context.Background(), api.GetHealthRequestObject{})
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}

	ok, is := resp.(api.GetHealth200JSONResponse)
	if !is {
		t.Fatalf("response = %T, want GetHealth200JSONResponse", resp)
	}
	if ok.Status != api.Ok {
		t.Errorf("status = %v, want ok", ok.Status)
	}
	if ok.Version == nil || *ok.Version != "1.2.3" {
		t.Errorf("version = %v, want 1.2.3", ok.Version)
	}
}

func TestGetHealthDegradedWhenDatabaseUnreachable(t *testing.T) {
	h := New(Deps{Querier: fakeQuerier{pingErr: errors.New("connection refused")}, Version: "1.2.3"})

	resp, err := h.GetHealth(context.Background(), api.GetHealthRequestObject{})
	// The handler must not return an error: a database that is down is a
	// reportable state, not a panic-worthy one, and the probe needs a body.
	if err != nil {
		t.Fatalf("GetHealth returned an error instead of a degraded response: %v", err)
	}

	degraded, is := resp.(api.GetHealth503JSONResponse)
	if !is {
		t.Fatalf("response = %T, want GetHealth503JSONResponse", resp)
	}
	if degraded.Status != api.Degraded {
		t.Errorf("status = %v, want degraded", degraded.Status)
	}
}

func TestGetAuthMethodsReportsConfiguredProviders(t *testing.T) {
	for _, tc := range []struct{ local, google bool }{
		{true, false}, // the MVP posture
		{true, true},  // once Google lands
		{false, true}, // Google-only, if a deployment ever wants it
	} {
		h := New(Deps{Querier: fakeQuerier{}, LocalEnabled: tc.local, GoogleEnabled: tc.google})

		resp, err := h.GetAuthMethods(context.Background(), api.GetAuthMethodsRequestObject{})
		if err != nil {
			t.Fatalf("GetAuthMethods: %v", err)
		}
		got := resp.(api.GetAuthMethods200JSONResponse)
		if got.Local != tc.local || got.Google != tc.google {
			t.Errorf("methods = {local:%v google:%v}, want {local:%v google:%v}",
				got.Local, got.Google, tc.local, tc.google)
		}
	}
}
