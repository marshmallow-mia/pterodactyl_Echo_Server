package panel

import (
	"encoding/json"
	"testing"
)

func TestItemDecodesAllShapes(t *testing.T) {
	for _, raw := range []string{
		`{"object":"mount","attributes":{"id":7,"name":"echovr-game"}}`,
		`{"data":{"id":7,"name":"echovr-game"}}`,
		`{"id":7,"name":"echovr-game"}`,
	} {
		m, err := item[Mount](json.RawMessage(raw))
		if err != nil || m.ID != 7 || m.Name != "echovr-game" {
			t.Errorf("%s: %+v %v", raw, m, err)
		}
	}
	list, err := items[Mount](json.RawMessage(`{"data":[{"attributes":{"id":1,"name":"a"}},{"id":2,"name":"b"}]}`))
	if err != nil || len(list) != 2 || list[1].ID != 2 {
		t.Errorf("list %+v %v", list, err)
	}
}
