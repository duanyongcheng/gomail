package testgmail

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Called with the fixture mutex held. No real provider is ever contacted.
func (m *Mock) serveLabelWrite(w http.ResponseWriter, r *http.Request, path string) {
	m.LabelWrites++
	if m.LabelWriteStatus != 0 {
		w.WriteHeader(m.LabelWriteStatus)
		return
	}
	id := strings.TrimPrefix(path, "/labels/")
	label := m.LabelItems[id]
	if path != "/labels" && label.ID == "" {
		w.WriteHeader(404)
		return
	}
	if path != "/labels" && label.Type != "user" {
		w.WriteHeader(400)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if r.Method != http.MethodDelete {
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.Name == "" {
			w.WriteHeader(400)
			return
		}
		for _, other := range m.LabelItems {
			if other.ID != id && strings.EqualFold(other.Name, input.Name) {
				w.WriteHeader(409)
				return
			}
		}
	}
	switch r.Method {
	case http.MethodPost:
		if path != "/labels" {
			w.WriteHeader(405)
			return
		}
		id = fmt.Sprintf("Label_created_%d", m.LabelWrites)
		label = Label{ID: id, Name: input.Name, Type: "user"}
		m.LabelItems[id] = label
	case http.MethodPatch:
		label.Name = input.Name
		m.LabelItems[id] = label
	case http.MethodDelete:
		delete(m.LabelItems, id)
		for messageID, msg := range m.Messages {
			labels := []string{}
			for _, value := range msg.Labels {
				if value != id {
					labels = append(labels, value)
				}
			}
			msg.Labels = labels
			m.Messages[messageID] = msg
		}
	default:
		w.WriteHeader(405)
		return
	}
	if m.LoseLabelResponse {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
		return
	}
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = json.NewEncoder(w).Encode(label)
}
