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
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
)

func mediaClient(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(ClientConfig{UserMRI: "8:orgid:x", TenantID: "tenant", Logger: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFetchableMediaAdmitsMicrosoftAndGiphyOverHTTPSOnly(t *testing.T) {
	c := mediaClient(t)
	for _, raw := range []string{
		"https://eu-api.asm.skype.com/v1/objects/0-weu-d1-abc/views/imgo",
		"https://us-prod.asyncgw.teams.microsoft.com/v1/objects/x/views/imgo",
		"https://statics.teams.cdn.office.net/evergreen-assets/stickers/a.png",
		"https://media.giphy.com/media/abc/giphy.gif",
		"https://media0.giphy.com/media/abc/giphy.gif",
	} {
		u, _ := url.Parse(raw)
		if !c.fetchableMedia(u) {
			t.Errorf("%s refused", raw)
		}
	}
	for _, raw := range []string{
		"http://statics.teams.cdn.office.net/a.png",
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1:8008/_matrix/client/versions",
		"https://tracker.example/x.png",
		"https://giphy.com.evil.example/x.gif",
		"https://evilgiphy.com/x.gif",
		"https://evil.example/.giphy.com/x.gif",
		"https://user@media.giphy.com/x.gif",
		"ftp://media.giphy.com/x.gif",
		"https://10.0.0.1/x.png",
	} {
		u, _ := url.Parse(raw)
		if c.fetchableMedia(u) {
			t.Errorf("%s admitted", raw)
		}
	}
}

// A sticker whose src names any other host is never fetched: the bridge
// sends no request at all, so it tells that host nothing and reaches no
// address inside the bridge's network.
func TestFetchAttachmentSendsNothingToAnUnlistedHost(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	c := mediaClient(t)
	_, _, err := c.fetchAMS(context.Background(), srv.URL+"/x.png", "skype-token", 1<<20)
	if !errors.Is(err, ErrUnlistedMediaHost) {
		t.Fatalf("err = %v, want ErrUnlistedMediaHost", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("the unlisted host got %d requests", hits.Load())
	}
}

func TestSharedFileDownloadOnlyFromSharePointOverHTTPS(t *testing.T) {
	ok := SharedFile{ItemID: "item", SiteURL: "https://contoso.sharepoint.com/sites/team"}
	if _, host, err := sharedFileDownloadEndpoint(ok); err != nil || host != "contoso.sharepoint.com" {
		t.Fatalf("SharePoint site: host %q, err %v", host, err)
	}
	if _, _, err := sharedFileDownloadEndpoint(SharedFile{ItemID: "item", FileURL: "https://contoso-my.sharepoint.com/personal/a_b/Documents/x.docx"}); err != nil {
		t.Fatalf("OneDrive file: %v", err)
	}
	for _, site := range []string{
		"http://contoso.sharepoint.com/sites/team",
		"https://evil.example/sites/team",
		"https://sharepoint.com.evil.example/sites/team",
		"https://contoso.sharepoint.com.evil.example/sites/team",
		"https://user@contoso.sharepoint.com/sites/team",
		"https://contoso.sharepoint.com:8443/sites/team",
	} {
		if _, _, err := sharedFileDownloadEndpoint(SharedFile{ItemID: "item", SiteURL: site}); err == nil {
			t.Errorf("%s admitted: a SharePoint token would be minted for and sent to it", site)
		}
	}
}

// A Teams media host that redirects to another host is not followed there.
func TestFetchAttachmentRefusesARedirectToAnUnlistedHost(t *testing.T) {
	base := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://tracker.example/pixel.png", http.StatusFound)
	}))
	t.Cleanup(base.Close)
	c := newAMSClient(t, base.URL)
	c.http.Transport = base.Client().Transport
	if _, _, err := c.FetchAttachment(context.Background(), base.URL+"/v1/objects/0-x/views/imgo"); !errors.Is(err, ErrUnlistedMediaHost) {
		t.Fatalf("redirect to an unlisted host: %v, want ErrUnlistedMediaHost", err)
	}
}
