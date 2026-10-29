package googleapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestLabelWriteMethodsAndNoAutomaticRetry(t *testing.T) {
	for _, action := range []string{"create", "rename", "delete"} {
		t.Run(action, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				method := map[string]string{"create": "POST", "rename": "PATCH", "delete": "DELETE"}[action]
				if r.Method != method {
					t.Error(r.Method)
				}
				if action == "delete" {
					w.WriteHeader(204)
					return
				}
				var input struct {
					Name string `json:"name"`
				}
				if json.NewDecoder(r.Body).Decode(&input) != nil || input.Name != "New" {
					t.Error(input)
				}
				fmt.Fprint(w, `{"id":"Label_x","name":"New","type":"user"}`)
			}))
			defer server.Close()
			api := &API{Base: server.URL, HTTP: server.Client(), Tokens: testToken{}}
			if _, err := api.ChangeLabel(t.Context(), action, "Label_x", "New"); err != nil || calls.Load() != 1 {
				t.Fatal(err, calls.Load())
			}
		})
	}
	for _, status := range []int{403, 429, 500, 307} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "/labels")
				w.WriteHeader(status)
			}))
			defer server.Close()
			api := &API{Base: server.URL, HTTP: server.Client(), Tokens: testToken{}}
			result, err := api.ChangeLabel(t.Context(), "create", "", "New")
			if err == nil || calls.Load() != 1 || result.Uncertain != (status == 500 || status == 307) {
				t.Fatal(result, err, calls.Load())
			}
		})
	}
}
func TestLabelDelete404AndMalformedSuccess(t *testing.T) {
	for _, test := range []struct {
		action    string
		status    int
		body      string
		uncertain bool
		wantError bool
	}{
		{"delete", 404, "", false, false},
		{"create", 200, `{bad`, true, true},
		{"rename", 200, `{"id":"unexpected","name":"New","type":"user"}`, true, true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status); fmt.Fprint(w, test.body) }))
		api := &API{Base: server.URL, HTTP: server.Client(), Tokens: testToken{}}
		result, err := api.ChangeLabel(t.Context(), test.action, "Label_x", "New")
		server.Close()
		if (err != nil) != test.wantError || result.Uncertain != test.uncertain {
			t.Fatal(test, result, err)
		}
	}
}

func TestInvalidLabelListsCannotPruneCache(t *testing.T) {
	for _, labels := range [][]Label{nil, {{ID: "Label_x", Name: "X", Type: "user"}}, {{ID: "INBOX", Name: "Inbox", Type: "user"}}, {{ID: "INBOX", Name: "Inbox", Type: "system"}, {ID: "INBOX", Name: "Inbox", Type: "system"}}} {
		if err := ValidateLabels(labels); err == nil {
			t.Fatal(labels)
		}
	}
}
