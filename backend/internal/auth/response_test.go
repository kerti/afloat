package auth

import (
	"testing"

	"github.com/kerti/afloat/backend/internal/api"
)

// Each handler's unexpected-failure path must answer its own 500. A nil
// response object is written as nothing, which net/http sends as a 200.
func TestInternalErrorAnswersEachHandlersOwn500(t *testing.T) {
	login, err := internalError[api.LocalLoginResponseObject]()
	if _, is := login.(api.LocalLogin500JSONResponse); !is || err != nil {
		t.Errorf("login = (%T, %v), want (LocalLogin500JSONResponse, nil)", login, err)
	}
	me, err := internalError[api.GetMeResponseObject]()
	if _, is := me.(api.GetMe500JSONResponse); !is || err != nil {
		t.Errorf("me = (%T, %v), want (GetMe500JSONResponse, nil)", me, err)
	}
	logout, err := internalError[api.LogoutResponseObject]()
	if _, is := logout.(api.Logout500JSONResponse); !is || err != nil {
		t.Errorf("logout = (%T, %v), want (Logout500JSONResponse, nil)", logout, err)
	}
}
