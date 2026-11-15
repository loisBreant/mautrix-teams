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
package connector

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-teams/pkg/msteams"
)

func chatsUpdatedAt(base time.Time, minutes ...int) []msteams.Chat {
	out := make([]msteams.Chat, len(minutes))
	for i, m := range minutes {
		out[i] = msteams.Chat{ID: string(rune('a' + i)), LastUpdated: base.Add(time.Duration(m) * time.Minute)}
	}
	return out
}

func chatIDs(chats []msteams.Chat) []string {
	ids := make([]string, len(chats))
	for i, c := range chats {
		ids[i] = c.ID
	}
	return ids
}

func TestStartupChatsKeepsTheMostRecentlyActive(t *testing.T) {
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	// a=+1, b=+5, c=+3, d=never updated, e=+4
	chats := chatsUpdatedAt(base, 1, 5, 3, 0, 4)
	chats[3].LastUpdated = time.Time{}

	cases := []struct {
		name  string
		count int
		want  []string
	}{
		{"two newest", 2, []string{"b", "e"}},
		{"exactly all", 5, []string{"b", "e", "c", "a", "d"}},
		{"more than there are", 50, []string{"b", "e", "c", "a", "d"}},
		{"one", 1, []string{"b"}},
		{"zero creates nothing at startup", 0, []string{}},
		{"negative means no limit", -1, []string{"b", "e", "c", "a", "d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keep, rest := BackfillConfig{ConversationCount: tc.count}.startupChats(append([]msteams.Chat(nil), chats...))
			got := chatIDs(keep)
			if len(got) != len(tc.want) {
				t.Fatalf("kept %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("kept %v, want %v", got, tc.want)
				}
			}
			if len(keep)+len(rest) != len(chats) {
				t.Fatalf("kept %d + rest %d, want %d chats in total", len(keep), len(rest), len(chats))
			}
		})
	}
}

func TestStartupChatsKeepsTiesInListOrder(t *testing.T) {
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	chats := chatsUpdatedAt(base, 2, 2, 2)
	keep, _ := BackfillConfig{ConversationCount: 2}.startupChats(chats)
	if got := chatIDs(keep); got[0] != "a" || got[1] != "b" {
		t.Fatalf("kept %v, want [a b]", got)
	}
}

func TestBackfillConfigIsReadFromTheExampleConfig(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(ExampleConfig), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Backfill.Enabled || cfg.Backfill.ConversationCount != 20 {
		t.Fatalf("example backfill = %+v, want enabled with 20 conversations", cfg.Backfill)
	}
}

func TestFetchMessagesReturnsNothingWhenBackfillIsDisabled(t *testing.T) {
	// No Teams client at all: a disabled backfill must answer before it
	// would make any request, so a nil client proves no request is made.
	tc := &TeamsClient{
		Main:      &TeamsConnector{Config: Config{Backfill: BackfillConfig{Enabled: false, ConversationCount: 20}}},
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: "login"}},
	}
	resp, err := tc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal:  &bridgev2.Portal{Portal: &database.Portal{PortalKey: networkid.PortalKey{ID: "19:abc@thread.v2"}}},
		Count:   50,
		Forward: true,
	})
	if err != nil {
		t.Fatalf("FetchMessages with backfill disabled: %v", err)
	}
	if resp == nil || len(resp.Messages) != 0 || resp.HasMore {
		t.Fatalf("FetchMessages with backfill disabled = %+v, want no messages and no more", resp)
	}
}

func TestLoginErrorsCarryErrcodes(t *testing.T) {
	cases := []struct {
		in   error
		want string
	}{
		{fmt.Errorf("poll: %w", msteams.ErrDeviceCodeExpired), "FI.MAU.TEAMS.LOGIN_EXPIRED"},
		{fmt.Errorf("poll: %w", msteams.ErrDeviceCodeDeclined), "FI.MAU.TEAMS.LOGIN_DECLINED"},
		{fmt.Errorf("poll: %w", msteams.ErrSignInBlocked), "FI.MAU.TEAMS.LOGIN_BLOCKED"},
	}
	for _, tc := range cases {
		var resp bridgev2.RespError
		if !errors.As(loginWaitError(tc.in), &resp) || resp.ErrCode != tc.want {
			t.Errorf("loginWaitError(%v) = %v, want errcode %s", tc.in, loginWaitError(tc.in), tc.want)
		}
	}
	other := errors.New("boom")
	if got := loginWaitError(other); !errors.Is(got, other) {
		t.Errorf("loginWaitError(other) = %v, want it unchanged", got)
	}
}

func TestNetworkIconUploadFollowsTheConfig(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(ExampleConfig), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.ShouldUploadNetworkIcon() {
		t.Error("the example config keeps the icon out")
	}
	off := false
	cfg.UploadNetworkIcon = &off
	if cfg.ShouldUploadNetworkIcon() {
		t.Error("upload_network_icon: false still uploads the icon")
	}
	if !(&Config{}).ShouldUploadNetworkIcon() {
		t.Error("an absent key keeps the icon out; want the upstream default")
	}
}
