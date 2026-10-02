package cli

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The batch travels as JSON, one selector per element, so that nothing along
// the way has to guess where one selector ends and the next begins.
func TestBatchStylesBodyKeepsEachSelectorWhole(t *testing.T) {
	selectors := []string{
		`//div[@class="row"]/span[contains(., "9 items")]`,
		`h1, h2`,
		`button:has-text("Save, then close")`,
		`#plain`,
	}

	body := batchStylesBody(selectors, "fontSize, color,,fontWeight ")

	// Through the encoder and back, as the daemon will read it.
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Selectors  []string `json:"selectors"`
		Properties []string `json:"properties"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded.Selectors, selectors) {
		t.Errorf("selectors arrived as\n  %q\nwant\n  %q", decoded.Selectors, selectors)
	}
	if want := []string{"fontSize", "color", "fontWeight"}; !reflect.DeepEqual(decoded.Properties, want) {
		t.Errorf("properties = %q, want %q", decoded.Properties, want)
	}
}

// No properties means the daemon's default set. The key is left out rather
// than sent empty, so the request says nothing it does not mean.
func TestBatchStylesBodyOmitsPropertiesWhenNoneAreAsked(t *testing.T) {
	body := batchStylesBody([]string{"h1"}, "")
	if _, present := body["properties"]; present {
		t.Errorf("body = %v, want no properties key", body)
	}
}
