// mautrix-teams - A Matrix-Microsoft Teams puppeting bridge.
// Copyright (C) 2026 Sandwich
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
package msteams

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type tokenEndpoint func(*http.Request) (*http.Response, error)

func (f tokenEndpoint) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func answering(body string) *http.Client {
	return &http.Client{Transport: tokenEndpoint(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
}

func TestPollDeviceCodeReportsAnExpiredCode(t *testing.T) {
	for _, code := range []string{"expired_token", "code_expired"} {
		_, err := PollDeviceCode(context.Background(), answering(`{"error":"`+code+`"}`), "common", false, "dc", time.Millisecond)
		if !errors.Is(err, ErrDeviceCodeExpired) {
			t.Errorf("%s: err = %v, want ErrDeviceCodeExpired", code, err)
		}
	}
}

func TestPollDeviceCodeReportsADeclinedLogin(t *testing.T) {
	for _, code := range []string{"authorization_declined", "access_denied"} {
		_, err := PollDeviceCode(context.Background(), answering(`{"error":"`+code+`"}`), "common", false, "dc", time.Millisecond)
		if !errors.Is(err, ErrDeviceCodeDeclined) {
			t.Errorf("%s: err = %v, want ErrDeviceCodeDeclined", code, err)
		}
	}
}

func TestPollDeviceCodeOtherErrorsAreNeither(t *testing.T) {
	_, err := PollDeviceCode(context.Background(), answering(`{"error":"invalid_grant"}`), "common", false, "dc", time.Millisecond)
	if err == nil || errors.Is(err, ErrDeviceCodeExpired) || errors.Is(err, ErrDeviceCodeDeclined) {
		t.Fatalf("err = %v, want a plain error", err)
	}
}

func TestPollDeviceCodeReportsAnOrganisationBlock(t *testing.T) {
	for _, code := range []string{"53003", "53000", "53001", "50105", "7000112"} {
		body := `{"error":"invalid_grant","error_description":"AADSTS` + code + `: refused.","error_codes":[` + code + `]}`
		_, err := PollDeviceCode(context.Background(), answering(body), "common", false, "dc", time.Millisecond)
		if !errors.Is(err, ErrSignInBlocked) {
			t.Errorf("AADSTS%s: err = %v, want ErrSignInBlocked", code, err)
		}
	}
	// Another AADSTS error is not a block.
	_, err := PollDeviceCode(context.Background(), answering(`{"error":"invalid_grant","error_codes":[70000]}`), "common", false, "dc", time.Millisecond)
	if errors.Is(err, ErrSignInBlocked) {
		t.Errorf("AADSTS70000 reported as a block")
	}
}

// Consent and app-registration refusals are the organisation's too, and an
// error code wins over the generic access_denied: a policy refusal that
// comes back as access_denied is not "the user declined".
func TestPollDeviceCodeReportsConsentRefusalsAsABlock(t *testing.T) {
	for _, code := range []string{"65001", "90094", "90095", "700016", "500011"} {
		body := `{"error":"invalid_grant","error_codes":[` + code + `]}`
		if _, err := PollDeviceCode(context.Background(), answering(body), "common", false, "dc", time.Millisecond); !errors.Is(err, ErrSignInBlocked) {
			t.Errorf("AADSTS%s: err = %v, want ErrSignInBlocked", code, err)
		}
	}
	body := `{"error":"access_denied","error_codes":[53003]}`
	if _, err := PollDeviceCode(context.Background(), answering(body), "common", false, "dc", time.Millisecond); !errors.Is(err, ErrSignInBlocked) {
		t.Errorf("access_denied with AADSTS53003: err = %v, want ErrSignInBlocked", err)
	}
}
