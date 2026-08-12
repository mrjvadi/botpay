package botprofile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"
)

type recordedCall struct {
	method  string
	payload map[string]string
}

func newTestBot(t *testing.T) (*tele.Bot, func() []recordedCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []recordedCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if method == "getMe" {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"old","username":"test_bot"}}`))
			return
		}
		payload := map[string]string{}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		calls = append(calls, recordedCall{method: method, payload: payload})
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	t.Cleanup(server.Close)

	bot, err := tele.NewBot(tele.Settings{Token: "1:test", URL: server.URL, Offline: false})
	if err != nil {
		t.Fatal(err)
	}
	return bot, func() []recordedCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedCall(nil), calls...)
	}
}

func TestSyncLabelsNonProductionName(t *testing.T) {
	bot, calls := newTestBot(t)
	if err := Sync(bot, Config{Environment: "development", ServiceName: "Uploader Bot"}); err != nil {
		t.Fatal(err)
	}

	got := calls()
	if len(got) != 10 {
		t.Fatalf("got %d calls, want 10", len(got))
	}
	counts := map[string]int{}
	for _, call := range got {
		counts[call.method]++
		switch call.method {
		case "setMyName":
			if call.payload["name"] != "Uploader Bot (development)" {
				t.Fatalf("unexpected name: %q", call.payload["name"])
			}
		case "setMyDescription":
			if call.payload["description"] != "" {
				t.Fatalf("description was not cleared")
			}
		case "setMyShortDescription":
			if call.payload["short_description"] != "" {
				t.Fatalf("short description was not cleared")
			}
		}
	}
	if counts["setMyName"] != 3 || counts["setMyDescription"] != 3 || counts["setMyShortDescription"] != 3 {
		t.Fatalf("unexpected profile call counts: %#v", counts)
	}
	if counts["removeMyProfilePhoto"] != 1 {
		t.Fatalf("profile photo removal count: %d", counts["removeMyProfilePhoto"])
	}
}

func TestSyncLabelsEmptyEnvironmentAsDev(t *testing.T) {
	bot, calls := newTestBot(t)
	if err := Sync(bot, Config{Environment: "", ServiceName: "Uploader Bot"}); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls() {
		if call.method == "setMyName" && call.payload["name"] != "Uploader Bot (dev)" {
			t.Fatalf("unexpected name: %q", call.payload["name"])
		}
	}
}

func TestSyncNormalizesProductionProfile(t *testing.T) {
	bot, calls := newTestBot(t)
	if err := Sync(bot, Config{Environment: "PROD", ServiceName: "  Uploader Bot  "}); err != nil {
		t.Fatal(err)
	}

	got := calls()
	if len(got) != 10 {
		t.Fatalf("got %d calls, want 10", len(got))
	}
	counts := map[string]int{}
	for _, call := range got {
		counts[call.method]++
		switch call.method {
		case "setMyName":
			if call.payload["name"] != "Uploader Bot" {
				t.Fatalf("unexpected name: %q", call.payload["name"])
			}
		case "setMyDescription":
			if call.payload["description"] != "" {
				t.Fatalf("description was not cleared")
			}
		case "setMyShortDescription":
			if call.payload["short_description"] != "" {
				t.Fatalf("short description was not cleared")
			}
		}
	}
	if counts["setMyName"] != 3 || counts["setMyDescription"] != 3 || counts["setMyShortDescription"] != 3 {
		t.Fatalf("unexpected profile call counts: %#v", counts)
	}
	if counts["removeMyProfilePhoto"] != 1 {
		t.Fatalf("profile photo removal count: %d", counts["removeMyProfilePhoto"])
	}
}

func TestIsProductionIsExplicit(t *testing.T) {
	for _, value := range []string{"prod", "production", " PROD "} {
		if !IsProduction(value) {
			t.Fatalf("%q should be production", value)
		}
	}
	for _, value := range []string{"", "dev", "staging", "true"} {
		if IsProduction(value) {
			t.Fatalf("%q must not be production", value)
		}
	}
}
