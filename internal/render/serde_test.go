package render

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// testdata/serde_f32.json is what serde_json wrote for each f32, from
// cargo run -p glint-studio --example go_compose -- f32 internal/render/testdata/serde_f32.json
func TestF32SpelledAsSerdeSpellsIt(t *testing.T) {
	data, err := os.ReadFile("testdata/serde_f32.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Bits uint32 `json:"bits"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 2000 {
		t.Fatalf("only %d cases", len(cases))
	}
	bad := 0
	for _, c := range cases {
		f := math.Float32frombits(c.Bits)
		if got := string(appendF32(nil, f)); got != c.Text {
			bad++
			if bad <= 10 {
				t.Errorf("%g (bits %#x): %s, serde %s", f, c.Bits, got, c.Text)
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d of %d spellings differ", bad, len(cases))
	}
}

func TestStyleJSONSpellsFloatsAsSerde(t *testing.T) {
	s := DefaultStyle()
	s.Aspect = &Aspect{16, 9}
	got, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"background":{"Gradient":{"stops":[1667691007,2824206335,3964180991],"angle":135.0}},"padding":64.0,"inset":0.0,"auto_balance":true,"radius":12.0,"shadow":40.0,"shadow_opacity":0.45,"aspect":{"w":16.0,"h":9.0},"align":4,"chrome":"None","chrome_title":true}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
